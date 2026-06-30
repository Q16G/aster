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

	// maxParallelSteps X2 滚动 fan-out 上限（含主路径）。0 或 1 = 串行（默认）。
	// 由 cmd/aster/main.go 从 AppConfig.React.MaxParallelSteps 读取并通过
	// WithFactoryMaxParallelSteps 注入；Build 时仅在 ≥2 时附加 react.WithMaxParallelSteps Option。
	maxParallelSteps int

	// maxParallelChains 链间维度：并行推进的同类对象数（乘数语义）。0 或 1 = 不放大
	// （默认）。由 cmd/aster/main.go 从 AppConfig.React.MaxParallelChains 读取并通过
	// WithFactoryMaxParallelChains 注入；与 maxParallelSteps 相乘得有效波宽 E，决定
	// requestPool 容量；Build 时仅在 ≥2 时附加 react.WithMaxParallelChains Option。
	maxParallelChains int

	// requestPool 限流所有经本 factory 构造的 Agent 实例（root + 子 Agent + skill fork child）
	// 的 outbound AI 请求并发。容量 = max(1, maxParallelSteps)；NewAgentFactory 末尾一次性初始化。
	// AICallProxy / runStructuredOutputWithRetry 入口把 pool 注入 ctx，ai.ChatExWithOptions /
	// ChatStreamWithOptions 在统一入口 Acquire/Release。
	requestPool *AgentRequestPool
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

// WithFactoryMaxParallelSteps 设置 X2 滚动 fan-out 上限。
// 0/1 = 串行（保持现状）；≥2 启用并发派发同层 ready step。
// 仅对非 sub_agent 根 Agent 生效（sub_agent 不持有 agentFactory，无法 fan-out）。
func WithFactoryMaxParallelSteps(n int) FactoryOption {
	return func(f *AgentFactory) {
		f.maxParallelSteps = n
	}
}

// WithFactoryMaxParallelChains 设置链间维度并行同类对象数（乘数语义）。
// 0/1 = 不放大（保持现状）；≥2 时与 maxParallelSteps 相乘放大有效波宽 E 与 AI 请求池容量。
func WithFactoryMaxParallelChains(n int) FactoryOption {
	return func(f *AgentFactory) {
		f.maxParallelChains = n
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
	// requestPool 容量 = 有效波宽 E = max(1,N_step) × max(1,N_chain)：root + 所有 child
	// Agent 共享一把信号量限制 outbound AI 请求并发，作为两维并发的单一兜底硬上限。
	// N_chain=1 时退化为 max(1,N_step)，与引入链间维度前一致。
	nStep := f.maxParallelSteps
	if nStep < 1 {
		nStep = 1
	}
	nChain := f.maxParallelChains
	if nChain < 1 {
		nChain = 1
	}
	f.requestPool = newAgentRequestPool(nStep * nChain)
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
		WithInstruction(def.Instruction),
		WithAgentIdentity(def.Role, def.Background),
		WithEmitter(f.resolveEmitter(def.Name)),
		WithIsSubAgent(def.IsSubAgent),
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

	// X2 滚动 fan-out 上限：≥2 时附加；<2 保持 AgentConfig 零值 + getter 兜底返回 1。
	// sub_agent 自身不 spawn 远程 step（agentFactory 不注入），即使配置了也无副作用。
	if f.maxParallelSteps >= 2 {
		opts = append(opts, WithMaxParallelSteps(f.maxParallelSteps))
	}
	// 链间维度乘数：≥2 时附加；<2 保持 AgentConfig 零值 + effectiveWaveWidth() 兜底 ×1。
	if f.maxParallelChains >= 2 {
		opts = append(opts, WithMaxParallelChains(f.maxParallelChains))
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

	// 全局 outbound AI 请求 limiter：root + 所有子 Agent 共享 factory 同一把 pool。
	// AICallProxy / runStructuredOutputWithRetry 入口把它注入 ctx，让 ai 包统一入口
	// Acquire/Release。子 Agent 内部 AI 请求也抢同一把信号量。
	agent.requestPool = f.requestPool

	// Orchestration tools are only registered for non-sub-agents. Sub-agents
	// (depth>0) must neither register nor expose these in their prompt.
	if !def.IsSubAgent {
		// X2 fan-out 需要在 spawnRemoteStep 派发远程 step 时复用同一 factory 构造 child agent。
		// 仅注入到根 Agent；sub_agent 自身不持有 factory，避免嵌套 spawn。
		agent.agentFactory = f

		// X2 fan-out 也需要 asyncRegistry 注册远程 step entry。现状 ensureAsyncRegistry
		// 是懒创建，只在 sub_agent_tool 调用时触发；若任务里 LLM 不用 sub_agent，registry
		// 永 nil，fanOutReadyPeers 第一道闸门直接早退——根 agent 在 Build 时提前 ensure
		// 确保 X2 调度可见可用。
		agent.ensureAsyncRegistry()

		if err := agent.registerTool(NewSubAgentTool(agent, f)); err != nil {
			return nil, fmt.Errorf("register sub_agent tool for %q failed: %w", def.Name, err)
		}

		if err := agent.registerTool(NewSubAgentStatusTool(agent)); err != nil {
			return nil, fmt.Errorf("register sub_agent_status tool for %q failed: %w", def.Name, err)
		}

		if err := agent.registerTool(NewAwaitSubAgentsTool(agent)); err != nil {
			return nil, fmt.Errorf("register await_subagents tool for %q failed: %w", def.Name, err)
		}
	}

	if f.skillLookup != nil {
		if err := agent.registerTool(NewSkillTool(agent, f, f.skillLookup)); err != nil {
			return nil, fmt.Errorf("register skill tool for %q failed: %w", def.Name, err)
		}
		if err := agent.registerTool(builtin_tools.NewEjectSkillTool()); err != nil {
			return nil, fmt.Errorf("register eject_skill tool for %q failed: %w", def.Name, err)
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
