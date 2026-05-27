package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"aster/internal/ai"
	"aster/internal/ai/anthropic"
	"aster/internal/ai/openai"
	"aster/internal/builtin_tools"
	"aster/internal/mcp"
	"aster/internal/provider"
	"aster/internal/react"
	"aster/internal/runtimelog"
	"aster/internal/selfupdate"
	"aster/internal/service"
	"aster/internal/tui"
	tuicontext "aster/internal/tui/context"
	skillspkg "aster/skills"
	semgrep_rules "aster/semgrep-rules"
)

var (
	flagProvider string
	flagModel    string
	flagBaseURL  string
	flagAPIKey   string
)

func main() {
	rootCmd := &cobra.Command{
		Use:     tui.AppCLIName,
		Short:   "ASTER - General-purpose agent TUI",
		Version: fmt.Sprintf("%s (commit: %s, built: %s)", Version, Commit, BuildTime),
		RunE:    runTUI,
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
	rootCmd.AddCommand(updateCmd())
	rootCmd.AddCommand(agentCmd())

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func runTUI(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	runtimelog.SetOutput(io.Discard)

	if err := tui.EnsureAppDefaults(); err != nil {
		return fmt.Errorf("init defaults: %w", err)
	}

	if _, err := semgrep_rules.ExtractRulesDir(); err != nil {
		return fmt.Errorf("extract semgrep rules: %w", err)
	}

	if _, err := skillspkg.ExtractSkillsDir(); err != nil {
		return fmt.Errorf("extract builtin skills: %w", err)
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

	credStore := tui.NewCredentialStore(filepath.Join(appDir, "credentials.yaml"))
	localProv := tuicontext.NewLocalProviderFromFile(filepath.Join(appDir, "preferences.json"))

	cachePath := filepath.Join(appDir, "cache", "models.json")
	providerRegistry := provider.InitRegistry(cachePath)
	providerRegistry.StartBackgroundRefresh(ctx)
	react.SetProviderRegistry(providerRegistry)

	bridge := tui.NewEventBridge()
	humanBridge := tui.NewHumanInputBridge()
	syncStore := tuicontext.NewSyncStore()
	defer syncStore.Close()
	bridge.BindSyncStore(syncStore)
	bootstrapEmitter := react.NewEmitter("bootstrap", "bootstrap", bridge.EmitterFunc())
	retryCallback := newRetryCallback(bootstrapEmitter)

	providerCfg := appCfg.ResolveProviderState(flagProvider, flagModel, flagBaseURL, flagAPIKey, providerRegistry, credStore)
	if providerCfg == nil {
		providerCfg = &tui.ProviderState{}
	}
	if flagModel == "" && os.Getenv("ASTER_MODEL") == "" {
		if preferred := localProv.Get().PreferredModels; preferred != nil {
			if modelID, ok := preferred[providerCfg.Name]; ok && modelID != "" {
				providerCfg.ModelID = modelID
			}
		}
	}
	{
		var cfgVariants map[string]map[string]any
		if pc := appCfg.Providers[providerCfg.Name]; pc != nil {
			cfgVariants = pc.Variants
		}
		base, variant, vopts := tui.ParseModelVariant(providerCfg.ModelID, providerRegistry, providerCfg.Name, cfgVariants)
		providerCfg.ModelID = base
		providerCfg.Variant = variant
		providerCfg.VariantOptions = vopts
	}

	updateChecker := selfupdate.NewUpdateChecker(
		Version,
		filepath.Join(appDir, "cache", "update.json"),
		selfupdate.WithProxy(providerCfg.Proxy),
	)
	updateChecker.StartBackgroundCheck(ctx)

	aiClient := newProviderClient(providerCfg, providerRegistry, retryCallback, "")
	clientFactory := ai.NewSimpleClientFactory(aiClient, func(mid string) ai.ChatClient {
		return newProviderClient(providerCfg, providerRegistry, retryCallback, mid)
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
		react.WithFactoryPromptCacheConfig(providerCfg.PromptCache),
	)

	profileRegistry := tui.NewProfileRegistry()
	for _, yp := range tui.ParseDefaultAgentProfiles() {
		profileRegistry.MergeYAML(yp)
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
	for _, def := range profiles {
		for _, sc := range def.MCPServers {
			if sc != nil && sc.Name != "" {
				mcpManager.RegisterServer(sc.Name, sc)
			}
		}
	}
	defaultDef := chooseDefaultAgentDefinition(profiles, localProv.Get().PreferredAgent)

	agentCtx := &tui.AgentExecContext{
		Factory:     factory,
		Definition:  defaultDef,
		MCPManager:  mcpManager,
		ProjectRoot: projectRoot,
		RebuildClient: func(provider *tui.ProviderState) {
			newClient := newProviderClient(provider, providerRegistry, retryCallback, "")
			newFactory := ai.NewSimpleClientFactory(newClient, func(mid string) ai.ChatClient {
				return newProviderClient(provider, providerRegistry, retryCallback, mid)
			})
			factory.UpdateDefaultClient(newClient)
			factory.UpdateClientFactory(newFactory)
			factory.UpdatePromptCacheConfig(provider.PromptCache)
		},
	}

	appModel := tui.NewModel(tui.ModelDeps{
		Store:           store,
		AgentCtx:        agentCtx,
		HumanBridge:     humanBridge,
		ProfileRegistry: profileRegistry,
		SkillService:    skillService,
		MCPManager:      mcpManager,
		Registry:        providerRegistry,
		CredStore:       credStore,
		AppCfg:          appCfg,
		ProviderCfg:     providerCfg,
		LocalProv:       localProv,
		SyncStore:       syncStore,
		CurrentVersion:  Version,
		UpdateChecker:   updateChecker,
	})

	opts := []tea.ProgramOption{
		tea.WithAltScreen(),
		// Enable wheel/trackpad scrolling inside the chat viewport by default.
		// Note: mouse mode can interfere with some terminals' text selection.
		tea.WithMouseCellMotion(),
	}
	p := tea.NewProgram(&appModel, opts...)

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

func chooseDefaultAgentDefinition(profiles []react.AgentDefinition, preferred string) react.AgentDefinition {
	if len(profiles) == 0 {
		return react.AgentDefinition{}
	}
	preferred = strings.TrimSpace(preferred)
	if preferred != "" {
		for _, def := range profiles {
			if strings.EqualFold(strings.TrimSpace(def.Name), preferred) {
				return def
			}
		}
	}
	for _, def := range profiles {
		if strings.EqualFold(strings.TrimSpace(def.Name), "code-audit") {
			return def
		}
	}
	return profiles[0]
}

func newRetryCallback(emitter *react.Emitter) openai.RetryCallback {
	if emitter == nil {
		return nil
	}
	return func(event openai.RetryEvent) {
		emitter.EmitRetry(event.Attempt, event.MaxAttempts, event.Delay, event.Next, event.Message)
	}
}

const (
	defaultMaxOutputTokens = 16384
	maxOutputTokensCap     = 65536
)

func resolveMaxOutputTokens(reg *provider.Registry, modelID string) int {
	if reg != nil && strings.TrimSpace(modelID) != "" {
		if _, outputLimit, found := reg.ResolveContextBudget(modelID); found && outputLimit > 0 {
			if outputLimit > maxOutputTokensCap {
				return maxOutputTokensCap
			}
			return outputLimit
		}
	}
	return defaultMaxOutputTokens
}

func newProviderClient(ps *tui.ProviderState, reg *provider.Registry, retryCallback openai.RetryCallback, modelOverride string) ai.ChatClient {
	if ps == nil {
		ps = &tui.ProviderState{}
	}
	effectiveModel := modelOverride
	if effectiveModel == "" {
		effectiveModel = ps.ModelID
	}
	maxTokens := resolveMaxOutputTokens(reg, effectiveModel)
	switch ps.Protocol {
	case "anthropic":
		return newAnthropicClient(ps, modelOverride, maxTokens)
	default:
		return newOpenAIClient(ps, retryCallback, modelOverride, maxTokens)
	}
}

func newOpenAIClient(ps *tui.ProviderState, retryCallback openai.RetryCallback, modelOverride string, maxTokens int) *openai.Client {
	modelID := modelOverride
	if modelID == "" {
		modelID = ps.ModelID
	}
	opts := []openai.Option{
		openai.WithURL(ps.BaseURL),
		openai.WithURLAutoComplete(true),
		openai.WithAPIKey(ps.APIKey),
		openai.WithModel(modelID),
		openai.WithStream(true),
		openai.WithRetryCallback(retryCallback),
		openai.WithMaxTokens(maxTokens),
	}
	if ps.Proxy != "" {
		opts = append(opts, openai.WithProxy(ps.Proxy))
	}
	if ps.SupportsVision != nil {
		opts = append(opts, openai.WithSupportsVision(*ps.SupportsVision))
	}
	if ps.SupportsAudio != nil {
		opts = append(opts, openai.WithSupportsAudio(*ps.SupportsAudio))
	}
	if len(ps.VariantOptions) > 0 {
		opts = append(opts, openai.WithExtraBody(ps.VariantOptions))
	}
	if ps.Timeout != nil && *ps.Timeout > 0 {
		opts = append(opts, openai.WithTimeout(time.Duration(*ps.Timeout)*time.Second))
	}
	return openai.NewClient(opts...)
}

func newAnthropicClient(ps *tui.ProviderState, modelOverride string, maxTokens int) *anthropic.Client {
	modelID := modelOverride
	if modelID == "" {
		modelID = ps.ModelID
	}
	opts := []anthropic.Option{
		anthropic.WithURL(ps.BaseURL),
		anthropic.WithAPIKey(ps.APIKey),
		anthropic.WithModel(modelID),
		anthropic.WithMaxTokens(maxTokens),
	}
	if ps.Proxy != "" {
		opts = append(opts, anthropic.WithProxy(ps.Proxy))
	}
	if ps.SupportsVision != nil {
		opts = append(opts, anthropic.WithSupportsVision(*ps.SupportsVision))
	}
	if ps.SupportsAudio != nil {
		opts = append(opts, anthropic.WithSupportsAudio(*ps.SupportsAudio))
	}
	for k, v := range ps.Headers {
		opts = append(opts, anthropic.WithHeaders(map[string]string{k: v}))
	}
	if len(ps.VariantOptions) > 0 {
		variantHeaders := make(map[string]string)
		for k, v := range ps.VariantOptions {
			if s, ok := v.(string); ok {
				variantHeaders[k] = s
			}
		}
		if len(variantHeaders) > 0 {
			opts = append(opts, anthropic.WithHeaders(variantHeaders))
		}
	}
	if ps.Timeout != nil && *ps.Timeout > 0 {
		opts = append(opts, anthropic.WithTimeout(time.Duration(*ps.Timeout)*time.Second))
	}
	return anthropic.NewClient(opts...)
}
