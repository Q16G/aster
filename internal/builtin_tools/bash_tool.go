package builtin_tools

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"aster/internal/utils/argx"

	"github.com/google/uuid"
)

const (
	bashDefaultTimeoutMs   int64 = 120_000
	bashBuildTestTimeoutMs int64 = 300_000
	bashMaxTimeoutMs       int64 = 900_000
	bashCaptureMaxStdout   int64 = 1 * 1024 * 1024 // 1 MiB
	bashCaptureMaxStderr   int64 = 1 * 1024 * 1024 // 1 MiB
	bashTruncateHeadRatio        = 0.2
)

// BashTool bash 命令执行工具
type BashTool struct {
	ctx        ToolContext
	permCtx    *BashPermissionContext
	sessionAL  *SessionAllowlist
	mu         sync.Mutex
	confirming bool // 标记是否有命令正在等待确认
}

// NewBashTool 创建 bash 工具实例
func NewBashTool(ctx ToolContext, permCtx *BashPermissionContext, sessionAL *SessionAllowlist) *BashTool {
	return &BashTool{
		ctx:       ctx,
		permCtx:   permCtx,
		sessionAL: sessionAL,
	}
}

func (t *BashTool) Name() string { return BashToolName }

func (t *BashTool) Description() string {
	return "执行 shell 命令。用于构建、测试、脚本、git 和系统命令。搜索代码和读文件应优先使用 rg 和 read_file。"
}

func (t *BashTool) Parameters() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command": map[string]any{
				"type":        "string",
				"description": "要执行的 shell 命令",
			},
			"cwd": map[string]any{
				"type":        "string",
				"description": "可选：命令工作目录",
			},
			"timeout_ms": map[string]any{
				"type":        "integer",
				"description": "可选：超时毫秒（默认 120000，最大 900000）",
			},
			"description": map[string]any{
				"type":        "string",
				"description": "可选：简短业务说明",
			},
			"risk": map[string]any{
				"type":        "string",
				"enum":        []string{"low", "high", "uncertain"},
				"description": "风险声明：low=仅本地只读/构建/测试；high=涉及远程访问、提权、系统级修改、磁盘写入等；uncertain=无法确定",
			},
			"reason": map[string]any{
				"type":        "string",
				"description": "风险判断的简短可读理由",
			},
		},
		"required":             []string{"command", "risk", "reason"},
		"additionalProperties": false,
	}
}

func (t *BashTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	if t == nil || t.ctx == nil {
		return "", fmt.Errorf("tool context is nil")
	}
	if t.permCtx == nil {
		return "", fmt.Errorf("bash tool: permission context not configured")
	}

	command, err := argx.RequiredText(args, "command")
	if err != nil {
		return "", err
	}
	command = strings.TrimSpace(command)
	if command == "" {
		return "", fmt.Errorf("command cannot be empty")
	}

	cwd := argx.OptionalText(args, "cwd")
	desc := argx.OptionalText(args, "description")
	timeoutMs := t.resolveTimeout(args, command)

	// 解析模型声明的 risk / reason；缺失时按 uncertain 处理（PRD §5.3）
	modelRisk := ParseModelRisk(argx.OptionalText(args, "risk"))
	modelReason := strings.TrimSpace(argx.OptionalText(args, "reason"))
	if modelReason == "" {
		modelReason = "(未提供)"
		if modelRisk == RiskLevelLow {
			modelRisk = RiskLevelUncertain
		}
	}

	// 串行检查：是否有命令正在等待确认
	t.mu.Lock()
	if t.confirming {
		t.mu.Unlock()
		out := &BashToolOutput{
			Executed:       false,
			PermissionMode: t.permCtx.Mode,
			RiskLevel:      modelRisk,
			RiskReasons:    []string{modelReason},
			GrantType:      GrantTypePendingConfirmation,
		}
		return out.JSON(), nil
	}
	t.mu.Unlock()

	shellPath, shellFamily := DetectShell()
	workDir := t.resolveWorkDir(cwd)

	// --- 权限决策（PRD §7）---

	// 1. 规范化 → 2. 检查 allowlist
	persistRules, _ := LoadPersistAllowlist(t.permCtx.ProjectPath)
	normalized := NormalizeCommand(command, shellFamily)
	var matchedRule *AllowlistRule
	for _, rule := range t.sessionAL.Rules() {
		if MatchRule(normalized, shellFamily, rule) {
			matchedRule = rule
			break
		}
	}
	if matchedRule == nil {
		for _, rule := range persistRules {
			if MatchRule(normalized, shellFamily, rule) {
				matchedRule = rule
				break
			}
		}
	}

	allowlistHit := matchedRule != nil

	// 3-6. 模式分流
	requiresConfirmation := ResolvePermissionDecision(t.permCtx.Mode, modelRisk, allowlistHit)

	grantType := GrantTypeAuto

	if requiresConfirmation {
		gt, err := t.requestUserConfirmation(ctx, command, desc, modelRisk, modelReason, shellFamily)
		if err != nil {
			return "", err
		}
		grantType = gt
		if grantType == GrantTypeReject {
			out := &BashToolOutput{
				Executed:                 false,
				Shell:                    shellPath,
				ShellFamily:              shellFamily,
				Cwd:                      workDir,
				PermissionMode:           t.permCtx.Mode,
				RiskLevel:                modelRisk,
				RiskReasons:              []string{modelReason},
				RequiresUserConfirmation: true,
				GrantType:                GrantTypeReject,
			}
			return out.JSON(), nil
		}
	}

	// --- 执行命令 ---
	EmitToolRuntimeProgress(ctx, "execute", fmt.Sprintf("running: %s", command), map[string]any{
		"command":    command,
		"cwd":        workDir,
		"shell":      shellPath,
		"risk_level": string(modelRisk),
		"grant_type": string(grantType),
	})

	exe, shellArgs := BuildShellCommand(shellPath, shellFamily, command)
	timeout := time.Duration(timeoutMs) * time.Millisecond
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	res := RunCommandLimited(execCtx, workDir, exe, shellArgs, bashCaptureMaxStdout, bashCaptureMaxStderr)

	stdout := res.Stdout
	stderr := res.Stderr
	truncated := false

	if res.StdoutTruncated {
		stdout, _ = TruncateHeadTail(stdout, bashCaptureMaxStdout, bashTruncateHeadRatio)
		truncated = true
	}
	if res.StderrTruncated {
		stderr, _ = TruncateHeadTail(stderr, bashCaptureMaxStderr, bashTruncateHeadRatio)
		truncated = true
	}

	interrupted := execCtx.Err() == context.DeadlineExceeded

	interpretation := InterpretExitCode(command, res.ExitCode)

	out := &BashToolOutput{
		Executed:                 true,
		Stdout:                   stdout,
		Stderr:                   stderr,
		ExitCode:                 res.ExitCode,
		Interrupted:              interrupted,
		Shell:                    shellPath,
		ShellFamily:              shellFamily,
		Cwd:                      workDir,
		PermissionMode:           t.permCtx.Mode,
		RiskLevel:                modelRisk,
		RiskReasons:              []string{modelReason},
		RequiresUserConfirmation: requiresConfirmation,
		GrantType:                grantType,
		MatchedRule:              matchedRule,
		Truncated:                truncated,
		ReturnCodeInterpretation: interpretation,
	}

	return out.JSON(), nil
}

// resolveTimeout 解析超时，支持构建命令自动提升
func (t *BashTool) resolveTimeout(args map[string]any, command string) int64 {
	if v, ok := args["timeout_ms"]; ok && v != nil {
		if ms, ok := asInt64Any(v); ok && ms > 0 {
			if ms > bashMaxTimeoutMs {
				return bashMaxTimeoutMs
			}
			return ms
		}
	}
	base := extractCommandBase(command)
	if isBuildTestCommand(base, command) {
		return bashBuildTestTimeoutMs
	}
	return bashDefaultTimeoutMs
}

func isBuildTestCommand(base, command string) bool {
	buildBases := map[string]bool{
		"go": true, "make": true, "cargo": true, "mvn": true, "gradle": true, "dotnet": true,
	}
	if buildBases[base] {
		return true
	}
	if (base == "npm" || base == "pnpm" || base == "yarn") &&
		(strings.Contains(command, " run ") || strings.Contains(command, " test") || strings.Contains(command, " build")) {
		return true
	}
	return false
}

// resolveWorkDir 解析工作目录
func (t *BashTool) resolveWorkDir(explicitCwd string) string {
	if explicitCwd != "" {
		return explicitCwd
	}
	if t.permCtx != nil && t.permCtx.ProjectPath != "" {
		return t.permCtx.ProjectPath
	}
	return "."
}

// requestUserConfirmation 请求用户确认
func (t *BashTool) requestUserConfirmation(ctx context.Context, command, desc string, risk RiskLevel, reason string, family ShellFamily) (GrantType, error) {
	onHumanInput := t.ctx.GetOnHumanInput()
	if onHumanInput == nil {
		return GrantTypeReject, fmt.Errorf("human input callback not configured")
	}

	t.mu.Lock()
	t.confirming = true
	t.mu.Unlock()
	defer func() {
		t.mu.Lock()
		t.confirming = false
		t.mu.Unlock()
	}()

	// 构建确认选项：复合命令或无法生成规则时退化为 allow_once / reject
	isCompound := IsCompoundCommand(command)
	options := []string{"allow_once", "allow_session", "allow_rule", "reject"}
	if isCompound || GenerateRule(command, family, isCompound) == nil {
		options = []string{"allow_once", "reject"}
	}

	question := fmt.Sprintf("是否允许执行该命令？\n\n命令：%s", command)
	if desc != "" {
		question += fmt.Sprintf("\n说明：%s", desc)
	}
	question += fmt.Sprintf("\n风险等级：%s", risk)
	question += fmt.Sprintf("\n风险原因：%s", reason)

	requestID := uuid.NewString()
	iteration := t.ctx.Snapshot().Iteration

	humanInputRequest := map[string]any{
		"request_id": requestID,
		"question":   question,
		"input_type": "single_choice",
		"options":    options,
		"context": map[string]any{
			"command":      command,
			"risk_level":   string(risk),
			"risk_reasons": []string{reason},
			"bash_confirm": true,
		},
	}

	t.ctx.GetEmitter().EmitHumanRequest(iteration, requestID, question, humanInputRequest)

	snap := t.ctx.UpdateTaskStatus(TaskStatusUpdate{
		Task:     "等待命令确认",
		Status:   TaskStatusPaused,
		Message:  fmt.Sprintf("等待确认命令: %s", command),
		Progress: -1,
	})
	t.ctx.GetEmitter().EmitStateChange(snap)

	ctxMap := map[string]any{
		"request_id":   requestID,
		"bash_confirm": true,
		"input_type":   "single_choice",
		"options":      options,
	}

	answer, err := onHumanInput(ctx, question, ctxMap)
	if err != nil {
		snap := t.ctx.UpdateTaskStatus(TaskStatusUpdate{
			Task:     "命令确认失败",
			Status:   TaskStatusRunning,
			Message:  err.Error(),
			Progress: -1,
		})
		t.ctx.GetEmitter().EmitStateChange(snap)
		return GrantTypeReject, err
	}

	answer = strings.TrimSpace(strings.ToLower(answer))

	snap = t.ctx.UpdateTaskStatus(TaskStatusUpdate{
		Task:     "收到命令确认",
		Status:   TaskStatusRunning,
		Message:  fmt.Sprintf("确认结果: %s", answer),
		Progress: -1,
	})
	t.ctx.GetEmitter().EmitStateChange(snap)

	switch answer {
	case "allow_once":
		return GrantTypeAllowOnce, nil
	case "allow_session":
		rule := GenerateRule(command, family, isCompound)
		if rule != nil {
			_ = t.sessionAL.Add(rule)
		}
		return GrantTypeAllowSession, nil
	case "allow_rule":
		rule := GenerateRule(command, family, isCompound)
		if rule != nil {
			_ = t.sessionAL.Add(rule)
			_ = AddPersistRule(t.permCtx.ProjectPath, rule)
		}
		return GrantTypeAllowRule, nil
	default:
		return GrantTypeReject, nil
	}
}
