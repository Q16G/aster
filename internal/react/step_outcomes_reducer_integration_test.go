package react

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fmt"

	"aster/internal/ai/openai"
	"aster/internal/builtin_tools"

	"gopkg.in/yaml.v3"
)

func resolveOpenCodeGoTestConfig(t *testing.T) (baseURL, apiKey, proxy string) {
	t.Helper()

	if u := os.Getenv("ASTER_BASE_URL"); u != "" {
		baseURL = u
	}
	if k := os.Getenv("ASTER_API_KEY"); k != "" {
		apiKey = k
	}

	homeDir, _ := os.UserHomeDir()
	if homeDir == "" {
		return
	}

	cfgPath := filepath.Join(homeDir, ".aster", "config.yaml")
	data, err := os.ReadFile(cfgPath)
	if err == nil {
		var cfg struct {
			DefaultProvider string `yaml:"default_provider"`
			Providers       map[string]struct {
				BaseURL string            `yaml:"base_url"`
				APIKey  string            `yaml:"api_key"`
				Env     map[string]string `yaml:"env"`
			} `yaml:"providers"`
		}
		if yaml.Unmarshal(data, &cfg) == nil {
			providerName := cfg.DefaultProvider
			if providerName == "" {
				providerName = "opencode-go"
			}
			if p, ok := cfg.Providers[providerName]; ok {
				if baseURL == "" {
					baseURL = p.BaseURL
				}
				if apiKey == "" && p.APIKey != "" {
					apiKey = p.APIKey
				}
				if p.Env != nil && proxy == "" {
					proxy = p.Env["HTTPS_PROXY"]
				}
			}
		}
	}

	if apiKey == "" {
		credPath := filepath.Join(homeDir, ".aster", "credentials.yaml")
		data, err := os.ReadFile(credPath)
		if err == nil {
			var creds map[string]string
			if yaml.Unmarshal(data, &creds) == nil {
				for _, name := range []string{"opencode-go", "opencode", "openai"} {
					if k, ok := creds[name]; ok && k != "" {
						apiKey = k
						break
					}
				}
			}
		}
	}

	return
}

func newLiveReducerClient(t *testing.T) *openai.Client {
	t.Helper()

	baseURL, apiKey, proxy := resolveOpenCodeGoTestConfig(t)
	if baseURL == "" || apiKey == "" {
		t.Skip("opencode-go config not found; need ~/.aster/config.yaml + credentials.yaml or ASTER_BASE_URL/ASTER_API_KEY")
	}

	opts := []openai.Option{
		openai.WithURL(baseURL),
		openai.WithAPIKey(apiKey),
		openai.WithModel("deepseek-v4-flash"),
		openai.WithContextWindowTokens(128000),
		openai.WithURLAutoComplete(true),
		openai.WithStream(false),
		openai.WithTimeout(60 * time.Second),
		openai.WithMaxRetries(2),
	}
	if proxy != "" {
		opts = append(opts, openai.WithProxy(proxy))
	}

	return openai.NewClient(opts...)
}

func TestReduceStepOutcomesInState_LiveLLM(t *testing.T) {
	if os.Getenv("SASTPRO_REACT_LIVE_TEST") != "1" {
		t.Skip("live test disabled; set SASTPRO_REACT_LIVE_TEST=1")
	}

	client := newLiveReducerClient(t)

	agent, err := NewReActAgent("test-reducer-live", client, WithEmitter(NewDummyEmitter()))
	if err != nil {
		t.Fatalf("NewReActAgent: %v", err)
	}

	bigSummary := strings.Repeat("该步骤分析了目标文件中的多个函数调用链，发现了潜在的SQL注入漏洞路径。", 500)
	outcomes := make([]*builtin_tools.StepOutcome, 8)
	for i := range outcomes {
		outcomes[i] = &builtin_tools.StepOutcome{
			StepID:      fmt.Sprintf("%s-%d", t.Name(), i+1),
			Status:      builtin_tools.StepOutcomeCompleted,
			Summary:     "完成代码分析",
			LongSummary: bigSummary,
			KeyFacts:    []string{"发现SQL注入", "涉及3个函数", "影响2个接口"},
			References:  []string{"main.go:42", "handler.go:108"},
		}
	}

	agent.state.SoftReset(outcomes, nil)

	snapBefore := agent.state.Snapshot()
	t.Logf("before: %d outcomes", len(snapBefore.StepOutcomes))

	_, _, exceeded := stepOutcomesExceedBudget(client, snapBefore.StepOutcomes)
	if !exceeded {
		t.Skip("test data did not exceed budget threshold; increase data size")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	agent.reduceStepOutcomesInState(ctx, client)

	snapAfter := agent.state.Snapshot()
	t.Logf("after:  %d outcomes", len(snapAfter.StepOutcomes))

	if len(snapAfter.StepOutcomes) == 0 {
		t.Fatal("StepOutcomes should not be empty after reduction")
	}

	lastN := stepOutcomesReducerKeepLast
	if len(snapAfter.StepOutcomes) < lastN {
		lastN = len(snapAfter.StepOutcomes)
	}
	protectedStart := len(snapAfter.StepOutcomes) - lastN
	for i := protectedStart; i < len(snapAfter.StepOutcomes); i++ {
		o := snapAfter.StepOutcomes[i]
		if o.StepID == "" {
			t.Errorf("protected outcome[%d] lost StepID", i)
		}
		if o.Summary == "" {
			t.Errorf("protected outcome[%d] lost Summary", i)
		}
	}

	for _, o := range snapAfter.StepOutcomes {
		if o.StepID == "" {
			t.Error("reduced outcome has empty StepID")
		}
		if o.Status == "" {
			t.Error("reduced outcome has empty Status")
		}
	}

	t.Logf("reduction: %d → %d outcomes", len(snapBefore.StepOutcomes), len(snapAfter.StepOutcomes))
}
