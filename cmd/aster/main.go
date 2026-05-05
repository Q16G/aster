package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"aster/internal/ai"
	"aster/internal/ai/openai"
	"aster/internal/builtin_tools"
	"aster/internal/mcp"
	"aster/internal/react"
	"aster/internal/service"
	"aster/internal/tui"
	tuicontext "aster/internal/tui/context"
	semgrep_rules "aster/skills/semgrep-rules"
)

var (
	flagProvider string
	flagModel    string
	flagBaseURL  string
	flagAPIKey   string
)

func main() {
	rootCmd := &cobra.Command{
		Use:   tui.AppCLIName,
		Short: "ASTER - General-purpose agent TUI",
		RunE:  runTUI,
	}

	f := rootCmd.Flags()
	f.StringVar(&flagProvider, "provider", "", "AI provider name (overrides config/env)")
	f.StringVar(&flagModel, "model", "", "model ID (overrides config/env)")
	f.StringVar(&flagBaseURL, "base-url", "", "API base URL (overrides config/env)")
	f.StringVar(&flagAPIKey, "api-key", "", "API key (overrides config/env)")

	rootCmd.AddCommand(&cobra.Command{
		Use:   "semgrep-rules-path",
		Short: "Print extracted semgrep rules directory path",
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := semgrep_rules.ExtractRulesDir()
			if err != nil {
				return err
			}
			fmt.Print(path)
			return nil
		},
	})

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func runTUI(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	if err := tui.EnsureAppDefaults(); err != nil {
		return fmt.Errorf("init defaults: %w", err)
	}

	appCfg, err := tui.LoadConfig(tui.DefaultConfigPath())
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	appDir := tui.DefaultAppDir()
	store, err := tui.NewSessionStore(
		filepath.Join(appDir, "data.db"),
		filepath.Join(appDir, "sessions"),
	)
	if err != nil {
		return fmt.Errorf("init store: %w", err)
	}
	defer store.Close()

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("get home dir: %w", err)
	}

	bridge := tui.NewEventBridge()
	humanBridge := tui.NewHumanInputBridge()
	syncStore := tuicontext.NewSyncStore()
	defer syncStore.Close()
	bridge.BindSyncStore(syncStore)
	bootstrapEmitter := react.NewEmitter("bootstrap", "bootstrap", bridge.EmitterFunc())
	retryCallback := newRetryCallback(bootstrapEmitter)

	providerCfg := &tui.ProviderState{}
	providerCfg.Name, providerCfg.BaseURL, providerCfg.APIKey, providerCfg.ModelID = appCfg.ResolveProvider(flagProvider, flagModel, flagBaseURL, flagAPIKey)

	aiClient := openai.NewClient(
		openai.WithURL(providerCfg.BaseURL),
		openai.WithURLAutoComplete(true),
		openai.WithAPIKey(providerCfg.APIKey),
		openai.WithModel(providerCfg.ModelID),
		openai.WithStream(true),
		openai.WithRetryCallback(retryCallback),
	)

	clientFactory := ai.NewSimpleClientFactory(aiClient, func(mid string) ai.ChatClient {
		if mid == "" {
			mid = providerCfg.ModelID
		}
		return openai.NewClient(
			openai.WithURL(providerCfg.BaseURL),
			openai.WithURLAutoComplete(true),
			openai.WithAPIKey(providerCfg.APIKey),
			openai.WithModel(mid),
			openai.WithStream(true),
			openai.WithRetryCallback(retryCallback),
		)
	})

	projectRoot, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get current working directory: %w", err)
	}
	projectRoot, err = filepath.Abs(projectRoot)
	if err != nil {
		return fmt.Errorf("resolve current working directory: %w", err)
	}
	skillService := service.NewSkillServiceWithMemory()
	if _, err := skillService.ImportSkillsFromMultipleSources(ctx, projectRoot, homeDir); err != nil {
		return fmt.Errorf("import skills: %w", err)
	}

	mcpManager := mcp.NewManager()
	if mcpCfg := appCfg.ToMCPConfig(); mcpCfg != nil {
		mcpManager.LoadFromConfigWithProbe(ctx, mcpCfg, bootstrapEmitter)
	}
	defer mcpManager.CloseAll()

	registry := react.NewDefaultToolRegistry()
	registry.Register(builtin_tools.ListSkillsToolName, func(_ builtin_tools.ToolContext) react.Tool {
		return builtin_tools.NewListSkillsTool(skillService)
	})
	registry.Register(builtin_tools.LoadSkillsToolName, func(_ builtin_tools.ToolContext) react.Tool {
		return builtin_tools.NewLoadSkillsTool(skillService)
	})

	factory := react.NewAgentFactory(
		react.WithFactoryDefaultAIClient(aiClient),
		react.WithFactoryAIClientFactory(clientFactory),
		react.WithFactoryToolRegistry(registry),
		react.WithFactorySkillsCatalog(skillService),
		react.WithFactoryEmitterFunc(bridge.EmitterFunc()),
		react.WithFactoryOnHumanInput(humanBridge.OnHumanInput),
		react.WithFactoryMCPManager(mcpManager),
	)

	profileRegistry := tui.NewProfileRegistry()
	for _, def := range tui.DefaultProfiles() {
		profileRegistry.Register(def)
	}
	agentsDir := filepath.Join(appDir, "agents")
	if yamlProfiles, err := tui.LoadProfilesFromDir(agentsDir); err == nil {
		for _, yp := range yamlProfiles {
			profileRegistry.MergeYAML(yp)
		}
	}

	profiles := profileRegistry.List()
	if len(profiles) == 0 {
		return fmt.Errorf("no agent profiles found; check %s for .yaml files", agentsDir)
	}
	defaultDef := profiles[0]

	agentCtx := &tui.AgentExecContext{
		Factory:     factory,
		Definition:  defaultDef,
		MCPManager:  mcpManager,
		ProjectRoot: projectRoot,
		RebuildClient: func(baseURL, apiKey, model string) {
			newClient := openai.NewClient(
				openai.WithURL(baseURL),
				openai.WithURLAutoComplete(true),
				openai.WithAPIKey(apiKey),
				openai.WithModel(model),
				openai.WithStream(true),
				openai.WithRetryCallback(retryCallback),
			)
			newFactory := ai.NewSimpleClientFactory(newClient, func(mid string) ai.ChatClient {
				if mid == "" {
					mid = model
				}
				return openai.NewClient(
					openai.WithURL(baseURL),
					openai.WithURLAutoComplete(true),
					openai.WithAPIKey(apiKey),
					openai.WithModel(mid),
					openai.WithStream(true),
					openai.WithRetryCallback(retryCallback),
				)
			})
			factory.UpdateDefaultClient(newClient)
			factory.UpdateClientFactory(newFactory)
		},
	}

	appModel := tui.NewModel(store, agentCtx, humanBridge, profileRegistry, skillService, mcpManager, appCfg, providerCfg, syncStore)

	p := tea.NewProgram(&appModel, tea.WithAltScreen())

	syncStore.SetFlushCallback(func(events []any) {
		tuiEvents := make([]tui.TuiEvent, 0, len(events))
		for _, e := range events {
			if te, ok := e.(tui.TuiEvent); ok {
				tuiEvents = append(tuiEvents, te)
			}
		}
		if len(tuiEvents) > 0 {
			p.Send(tui.BatchedEventsMsg{Events: tuiEvents})
		}
	})

	bridge.Bind(p)
	humanBridge.Bind(p)

	if _, err := p.Run(); err != nil {
		return fmt.Errorf("tui: %w", err)
	}
	return nil
}

func newRetryCallback(emitter *react.Emitter) openai.RetryCallback {
	if emitter == nil {
		return nil
	}
	return func(event openai.RetryEvent) {
		emitter.EmitRetry(event.Attempt, event.MaxAttempts, event.Delay, event.Next, event.Message, event.RetryAfter)
	}
}
