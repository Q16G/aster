package react

import (
	"fmt"
	"strings"

	"aster/internal/ai"
	"aster/internal/builtin_tools"
	"aster/internal/mcp"
)

// AgentFactory builds Agent instances from AgentDefinitions.
// It resolves tool names via a ToolRegistry and skill names via a SkillsCatalog.
type AgentFactory struct {
	toolRegistry      *ToolRegistry
	skillsCatalog     SkillsCatalog
	skillLookup       SkillLookup
	aiClientFactory   ai.ClientFactory
	defaultAIClient   ai.ChatClient
	emitter           *Emitter
	emitterFunc       BaseEmitterFunc
	onHumanInput      builtin_tools.OnHumanInputFunc
	mcpManager        *mcp.Manager
	promptCacheConfig *ai.PromptCacheConfig
}

type FactoryOption func(*AgentFactory)

func WithFactoryToolRegistry(registry *ToolRegistry) FactoryOption {
	return func(f *AgentFactory) {
		if registry != nil {
			f.toolRegistry = registry
		}
	}
}

func WithFactorySkillsCatalog(catalog SkillsCatalog) FactoryOption {
	return func(f *AgentFactory) {
		f.skillsCatalog = catalog
	}
}

func WithFactoryAIClientFactory(factory ai.ClientFactory) FactoryOption {
	return func(f *AgentFactory) {
		f.aiClientFactory = factory
	}
}

func WithFactoryDefaultAIClient(client ai.ChatClient) FactoryOption {
	return func(f *AgentFactory) {
		f.defaultAIClient = client
	}
}

func WithFactoryEmitter(emitter *Emitter) FactoryOption {
	return func(f *AgentFactory) {
		f.emitter = emitter
	}
}

func WithFactoryEmitterFunc(fn BaseEmitterFunc) FactoryOption {
	return func(f *AgentFactory) {
		f.emitterFunc = fn
	}
}

func WithFactoryOnHumanInput(fn builtin_tools.OnHumanInputFunc) FactoryOption {
	return func(f *AgentFactory) {
		f.onHumanInput = fn
	}
}

func WithFactorySkillLookup(lookup SkillLookup) FactoryOption {
	return func(f *AgentFactory) {
		f.skillLookup = lookup
	}
}

func WithFactoryMCPManager(manager *mcp.Manager) FactoryOption {
	return func(f *AgentFactory) {
		f.mcpManager = manager
	}
}

func WithFactoryPromptCacheConfig(cfg *ai.PromptCacheConfig) FactoryOption {
	return func(f *AgentFactory) {
		f.promptCacheConfig = cfg
	}
}

// NewAgentFactory creates a factory with the given options.
func NewAgentFactory(opts ...FactoryOption) *AgentFactory {
	f := &AgentFactory{}
	for _, opt := range opts {
		if opt != nil {
			opt(f)
		}
	}
	return f
}

// Build creates an Agent from a definition.
func (f *AgentFactory) Build(def AgentDefinition) (*Agent, error) {
	if f == nil {
		return nil, fmt.Errorf("agent factory is nil")
	}

	client := f.resolveAIClient(def.ModelID)
	if client == nil {
		return nil, fmt.Errorf("no AI client available for agent %q (model_id=%q)", def.Name, def.ModelID)
	}

	opts := []Option{
		WithInstruction(def.BuildInstruction()),
		WithAgentIdentity(def.Role, def.Background),
		WithEmitter(f.resolveEmitter(def.Name)),
	}

	if def.ModelID != "" {
		opts = append(opts, WithModelID(def.ModelID))
	}
	if f.aiClientFactory != nil {
		opts = append(opts, WithAIClientFactory(f.aiClientFactory))
	}

	if f.promptCacheConfig != nil {
		opts = append(opts, WithPromptCacheConfig(f.promptCacheConfig))
	}

	// Policies
	if def.Policies.MaxIterations > 0 {
		opts = append(opts, WithMaxIterations(def.Policies.MaxIterations))
	}
	if def.Policies.AllowBash && def.Policies.BashPermissionContext != nil {
		opts = append(opts, WithBashTool(def.Policies.BashPermissionContext))
	}
	if def.Policies.ResultSource != "" {
		// ResultSource is applied at Execute time via WithResultSource, not at build time.
		// Store it so callers can retrieve it from the definition if needed.
	}

	// Tools: resolve from registry
	if len(def.ToolNames) > 0 && f.toolRegistry != nil {
		resolved, err := f.resolveTools(def.ToolNames)
		if err != nil {
			return nil, fmt.Errorf("agent %q tool resolution failed: %w", def.Name, err)
		}
		opts = append(opts, WithTools(resolved...))
	}

	// Skills
	if f.skillsCatalog != nil {
		opts = append(opts, WithSkillCatalog(f.skillsCatalog, def.SkillNames))
	}

	// Human input
	if f.onHumanInput != nil {
		opts = append(opts, WithOnHumanInput(f.onHumanInput))
	}

	agent, err := NewReActAgent(def.Name, client, opts...)
	if err != nil {
		return nil, fmt.Errorf("build agent %q failed: %w", def.Name, err)
	}

	if err := agent.registerTool(NewSubAgentTool(agent, f)); err != nil {
		return nil, fmt.Errorf("register sub_agent tool for %q failed: %w", def.Name, err)
	}

	if f.skillLookup != nil {
		if err := agent.registerTool(NewSkillTool(agent, f, f.skillLookup)); err != nil {
			return nil, fmt.Errorf("register skill tool for %q failed: %w", def.Name, err)
		}
	}

	if f.mcpManager != nil {
		agent.cfg.MCPManager = f.mcpManager
		for _, entry := range f.mcpManager.ServerEntries() {
			if entry == nil || entry.Status != mcp.MCPStatusConnected {
				continue
			}
			adapters := f.mcpManager.GetAdapters(entry.Name)
			for _, adapter := range adapters {
				_ = agent.registerTool(adapter)
			}
			if agent.state != nil {
				agent.state.AddActiveMCPServers([]string{entry.Name})
			}
		}
	}

	return agent, nil
}

func (f *AgentFactory) resolveAIClient(modelID string) ai.ChatClient {
	modelID = strings.TrimSpace(modelID)
	if modelID != "" && f.aiClientFactory != nil {
		if client := f.aiClientFactory.CreateClient(modelID); client != nil {
			return client
		}
	}
	if f.aiClientFactory != nil {
		if client := f.aiClientFactory.DefaultClient(); client != nil {
			return client
		}
	}
	return f.defaultAIClient
}

func (f *AgentFactory) DefaultClient() ai.ChatClient {
	return f.defaultAIClient
}

func (f *AgentFactory) UpdateDefaultClient(client ai.ChatClient) {
	f.defaultAIClient = client
}

func (f *AgentFactory) UpdateClientFactory(factory ai.ClientFactory) {
	f.aiClientFactory = factory
}

func (f *AgentFactory) UpdatePromptCacheConfig(cfg *ai.PromptCacheConfig) {
	f.promptCacheConfig = cfg
}

func (f *AgentFactory) resolveEmitter(agentName string) *Emitter {
	if f.emitterFunc != nil {
		return NewEmitter("", agentName, f.emitterFunc)
	}
	if f.emitter != nil {
		return f.emitter
	}
	return NewDummyEmitter()
}

func (f *AgentFactory) resolveTools(names []string) ([]Tool, error) {
	if f.toolRegistry == nil {
		return nil, fmt.Errorf("tool registry not configured")
	}
	tools := make([]Tool, 0, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		tool, err := f.toolRegistry.Resolve(name, nil)
		if err != nil {
			return nil, err
		}
		tools = append(tools, tool)
	}
	return tools, nil
}
