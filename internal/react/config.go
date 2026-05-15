package react

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"aster/internal/ai"
	"aster/internal/builtin_tools"
	"aster/internal/structuredoutput"
)

const (
	defaultToolTimeoutMs int64 = 300_000   // 5 min
	maxToolTimeoutMs     int64 = 1_800_000 // 30 min
)

// OnHandoffFunc Agent 交接回调
type OnHandoffFunc func(ctx context.Context, agent *Agent, handoffTo string) string

type StructuredOutputConfig = structuredoutput.Config

// AgentConfig Agent 配置

type AgentConfig struct {
	MaxIterations int
	Instruction   string
	AIClient      ai.ChatClient
	PromptManager PromptManager

	// AIClientFactory 可选：用于按 model_id 动态创建 AIClient。
	AIClientFactory ai.ClientFactory
	// ModelID 可选：本 Agent 默认使用的模型 ID。
	ModelID string

	Tools          []Tool
	OnHandoffFunc  OnHandoffFunc
	InitialHistory []*ai.MsgInfo

	EnableStreaming bool

	TaskPlanner         builtin_tools.TaskPlanner

	HistoryCompressKeepLastRounds int
	HistoryCompressor             HistoryCompressor

	SessionID string
	AgentID   string

	// Emitter 统一事件发射器（推荐使用）
	Emitter *Emitter

	// 人工输入回调（必须保留，用于阻塞等待人工输入）
	OnHumanInput builtin_tools.OnHumanInputFunc

	// SkillsPromptProvider 可选：统一装配 think_act prompt 中的 skills 上下文。
	SkillsPromptProvider SkillsPromptProvider

	// MCPManager 可选：MCP Server 管理器，用于渲染 prompt 中的 MCP 表格。
	MCPManager MCPManagerForPrompt

	StructuredOutput StructuredOutputConfig

	PromptCacheConfig *ai.PromptCacheConfig

	// BashTool 可选：bash 工具配置。非 nil 时 NewReActAgent 在内部注册 BashTool。
	BashTool *BashToolConfig

	// DefaultToolTimeoutMs 所有工具调用的默认超时（毫秒）。0 表示使用内置默认值（300000）。
	DefaultToolTimeoutMs int64
}

// Option Agent 配置选项
type Option func(*AgentConfig)

func defaultAgentConfig(aiClient ai.ChatClient) *AgentConfig {
	return &AgentConfig{
		MaxIterations:       120,
		AIClient:            aiClient,
		OnHandoffFunc:       DefaultOnHandoffFunc,

		HistoryCompressKeepLastRounds: 5,
		StructuredOutput:              structuredoutput.DefaultConfig(),
	}
}

// WithMaxIterations 设置最大迭代次数
func WithMaxIterations(n int) Option {
	return func(c *AgentConfig) {
		if n > 0 {
			c.MaxIterations = n
		}
	}
}

// WithInstruction 设置系统指令
func WithInstruction(instruction string) Option {
	return func(c *AgentConfig) {
		c.Instruction = instruction
	}
}

// WithAIClientFactory 设置按 model_id 动态创建客户端的工厂。
func WithAIClientFactory(factory ai.ClientFactory) Option {
	return func(c *AgentConfig) {
		if c == nil {
			return
		}
		c.AIClientFactory = factory
	}
}

// WithModelID 设置 Agent 默认 model_id。
func WithModelID(modelID string) Option {
	return func(c *AgentConfig) {
		if c == nil {
			return
		}
		c.ModelID = strings.TrimSpace(modelID)
	}
}

// WithTool 添加单个工具
func WithTool(tool Tool) Option {
	return func(c *AgentConfig) {
		if tool == nil {
			return
		}
		c.Tools = append(c.Tools, tool)
	}
}

// WithTools 添加多个工具
func WithTools(tools ...Tool) Option {
	return func(c *AgentConfig) {
		for _, tool := range tools {
			if tool == nil {
				continue
			}
			c.Tools = append(c.Tools, tool)
		}
	}
}

// WithOnHandoffFunc 设置交接回调
func WithOnHandoffFunc(fn OnHandoffFunc) Option {
	return func(c *AgentConfig) {
		if fn == nil {
			return
		}
		c.OnHandoffFunc = fn
	}
}

// WithInitialHistory 设置初始历史
func WithInitialHistory(history []*ai.MsgInfo) Option {
	return func(c *AgentConfig) {
		if c == nil {
			return
		}
		c.InitialHistory = history
	}
}

// WithHistory 设置历史（别名）
func WithHistory(history []*ai.MsgInfo) Option {
	return func(c *AgentConfig) {
		if c == nil {
			return
		}
		c.InitialHistory = history
	}
}

// WithStreamingEnabled 启用流式响应
func WithStreamingEnabled(enabled bool) Option {
	return func(c *AgentConfig) {
		if c == nil {
			return
		}
		c.EnableStreaming = enabled
	}
}

// WithTaskPlanner 设置任务规划器
func WithTaskPlanner(planner builtin_tools.TaskPlanner) Option {
	return func(c *AgentConfig) {
		c.TaskPlanner = planner
	}
}

// WithHistoryCompressKeepLastRounds 设置历史压缩保留轮数
func WithHistoryCompressKeepLastRounds(n int) Option {
	return func(c *AgentConfig) {
		c.HistoryCompressKeepLastRounds = n
	}
}

// WithHistoryCompressor 设置历史压缩器
func WithHistoryCompressor(compressor HistoryCompressor) Option {
	return func(c *AgentConfig) {
		c.HistoryCompressor = compressor
	}
}

// WithSessionID 设置会话ID
func WithSessionID(sessionID string) Option {
	return func(c *AgentConfig) {
		if c == nil {
			return
		}
		c.SessionID = sessionID
	}
}

// WithAgentID 设置AgentID
func WithAgentID(agentID string) Option {
	return func(c *AgentConfig) {
		if c == nil {
			return
		}
		c.AgentID = agentID
	}
}

// WithOnHumanInput 设置人工输入回调
func WithOnHumanInput(fn builtin_tools.OnHumanInputFunc) Option {
	return func(c *AgentConfig) {
		c.OnHumanInput = fn
	}
}

// WithEmitter 设置事件发射器
func WithEmitter(emitter *Emitter) Option {
	return func(c *AgentConfig) {
		c.Emitter = emitter
	}
}

func WithSkillsPromptProvider(provider SkillsPromptProvider) Option {
	return func(c *AgentConfig) {
		if c == nil || provider == nil {
			return
		}
		c.SkillsPromptProvider = provider
	}
}

func WithSkillCatalog(catalog SkillsCatalog, allowedSkillNames []string) Option {
	return func(c *AgentConfig) {
		if c == nil || catalog == nil {
			return
		}
		c.SkillsPromptProvider = NewSkillsPromptProviderFromCatalog(catalog, allowedSkillNames)
	}
}

func WithMCPManager(manager MCPManagerForPrompt) Option {
	return func(c *AgentConfig) {
		if c == nil || manager == nil {
			return
		}
		c.MCPManager = manager
	}
}

func WithStructuredOutputConfig(cfg StructuredOutputConfig) Option {
	return func(c *AgentConfig) {
		if c == nil {
			return
		}
		c.StructuredOutput = structuredoutput.NormalizeConfig(cfg)
	}
}

func WithStructuredOutputRetryCount(n int) Option {
	return func(c *AgentConfig) {
		if c == nil || n <= 0 {
			return
		}
		cfg := structuredoutput.NormalizeConfig(c.StructuredOutput)
		cfg.RetryCount = n
		c.StructuredOutput = cfg
	}
}

// BashToolConfig bash 工具配置；仅当此字段非 nil 时，NewReActAgent 才会注册 BashTool。
type BashToolConfig struct {
	PermCtx   *builtin_tools.BashPermissionContext
	SessionAL *builtin_tools.SessionAllowlist
}

// WithBashTool 启用 bash 工具。
func WithBashTool(cfg *BashToolConfig) Option {
	return func(c *AgentConfig) {
		if c == nil || cfg == nil {
			return
		}
		c.BashTool = cfg
	}
}

func WithPromptCacheConfig(cfg *ai.PromptCacheConfig) Option {
	return func(c *AgentConfig) {
		if c == nil {
			return
		}
		c.PromptCacheConfig = cfg
	}
}

func WithPromptManager(manager PromptManager) Option {
	return func(c *AgentConfig) {
		if c == nil || manager == nil {
			return
		}
		c.PromptManager = manager
	}
}

func WithDefaultToolTimeout(ms int64) Option {
	return func(c *AgentConfig) {
		if c == nil || ms <= 0 {
			return
		}
		c.DefaultToolTimeoutMs = ms
	}
}

func (c *AgentConfig) resolveToolTimeout(args map[string]any) time.Duration {
	ms := c.resolveToolTimeoutMs(args)
	return time.Duration(ms) * time.Millisecond
}

func (c *AgentConfig) resolveToolTimeoutMs(args map[string]any) int64 {
	if v, ok := args["timeout_ms"]; ok && v != nil {
		if ms := toInt64(v); ms > 0 {
			if ms > maxToolTimeoutMs {
				return maxToolTimeoutMs
			}
			return ms
		}
	}
	if c != nil && c.DefaultToolTimeoutMs > 0 {
		return c.DefaultToolTimeoutMs
	}
	return defaultToolTimeoutMs
}

func toInt64(v any) int64 {
	switch n := v.(type) {
	case int:
		return int64(n)
	case int64:
		return n
	case float64:
		return int64(n)
	case float32:
		return int64(n)
	case int32:
		return int64(n)
	case json.Number:
		if i, err := n.Int64(); err == nil {
			return i
		}
		if f, err := n.Float64(); err == nil {
			return int64(f)
		}
		return 0
	case string:
		s := strings.TrimSpace(n)
		if s == "" {
			return 0
		}
		if i, err := strconv.ParseInt(s, 10, 64); err == nil {
			return i
		}
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			return int64(f)
		}
		return 0
	default:
		return 0
	}
}
