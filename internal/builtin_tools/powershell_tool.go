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

// PowerShellTool is an independent tool for Windows-native PowerShell operations.
// It provides version-aware prompt engineering and AST-based safety checks.
type PowerShellTool struct {
	ctx        ToolContext
	permCtx    *BashPermissionContext
	sessionAL  *SessionAllowlist
	mu         sync.Mutex
	confirming bool
	initOnce   sync.Once
	psPath     string
	psEdition  PSEdition
}

func NewPowerShellTool(ctx ToolContext, permCtx *BashPermissionContext, sessionAL *SessionAllowlist) *PowerShellTool {
	return &PowerShellTool{
		ctx:       ctx,
		permCtx:   permCtx,
		sessionAL: sessionAL,
	}
}

func (t *PowerShellTool) init() {
	t.initOnce.Do(func() {
		t.psPath, t.psEdition = FindPowerShell()
	})
}

func (t *PowerShellTool) Name() string { return PowerShellToolName }

func (t *PowerShellTool) Description() string {
	t.init()
	base := "执行 PowerShell 命令。专用于 Windows 原生操作：注册表、服务管理、WMI 查询、.NET 调用、组策略、Windows API。通用文件操作、构建、测试、git 等应使用 bash 工具。"
	return base + "\n\n" + t.buildPSGuidance()
}

func (t *PowerShellTool) buildPSGuidance() string {
	var sb strings.Builder

	switch t.psEdition {
	case PSEditionCore:
		sb.WriteString("当前 PowerShell 版本：Core 7+ (pwsh)\n")
		sb.WriteString("[OK] 可用 && 和 || 链接命令（优先于 ;）\n")
		sb.WriteString("[OK] 可用三元运算符 ($x ? $a : $b)、null 合并 (??)、null 条件 (?.)\n")
		sb.WriteString("默认编码 UTF-8 without BOM\n")
		sb.WriteString("ConvertFrom-Json 支持 -AsHashtable\n")
	case PSEditionDesktop:
		sb.WriteString("当前 PowerShell 版本：Desktop 5.1\n")
		sb.WriteString("[禁止] && 和 ||（语法错误），用 ; 或 if ($?) {...}\n")
		sb.WriteString("[禁止] 三元 ?:、null 合并 ??、null 条件 ?.\n")
		sb.WriteString("[注意] 避免 2>&1（stderr 处理 bug，native exe 写 stderr 会导致 $?=false）\n")
		sb.WriteString("默认编码 UTF-16 LE with BOM\n")
		sb.WriteString("ConvertFrom-Json 返回 PSCustomObject 而非 hashtable\n")
	default:
		sb.WriteString("PowerShell 版本未知，使用保守语法（兼容 5.1）\n")
		sb.WriteString("[禁止] && 和 ||，用 ; 或 if ($?) {...}\n")
		sb.WriteString("[禁止] 三元 ?:、null 合并 ??、null 条件 ?.\n")
	}

	sb.WriteString("\n通用规则：\n")
	sb.WriteString("- 用 Get-ChildItem 不用 ls（alias 在非交互模式可能不可用）\n")
	sb.WriteString("- 用 Select-String 不用 grep；管道传递对象不是文本\n")
	sb.WriteString("- 退出码：native exe 用 $LASTEXITCODE，cmdlet 用 $?\n")
	sb.WriteString("- 变量展开用双引号，字面量用单引号\n")
	sb.WriteString("- 禁止 Read-Host、Get-Credential 等交互式命令\n")

	return sb.String()
}

func (t *PowerShellTool) Parameters() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command": map[string]any{
				"type":        "string",
				"description": "要执行的 PowerShell 命令",
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
				"description": "风险声明：low=仅本地只读/查询；high=涉及系统修改、提权、远程访问等；uncertain=无法确定",
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

func (t *PowerShellTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	t.init()

	if t.ctx == nil {
		return "", fmt.Errorf("tool context is nil")
	}
	if t.permCtx == nil {
		return "", fmt.Errorf("powershell tool: permission context not configured")
	}
	if t.psPath == "" {
		return "", fmt.Errorf("powershell tool: PowerShell not found on this system")
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

	modelRisk := ParseModelRisk(argx.OptionalText(args, "risk"))
	modelReason := strings.TrimSpace(argx.OptionalText(args, "reason"))
	if modelReason == "" {
		modelReason = "(未提供)"
		if modelRisk == RiskLevelLow {
			modelRisk = RiskLevelUncertain
		}
	}

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

	workDir := t.resolveWorkDir(cwd)

	// AST-based safety check
	var dangerFlags []PSDangerFlag
	parsed, _ := ParsePSCommandAST(ctx, t.psPath, command)
	if parsed == nil {
		dangerFlags = append(dangerFlags, PSDangerFlag{
			Category: "parse_failure",
			Command:  command,
			Reason:   "AST parser returned nil result, cannot verify safety",
		})
	} else {
		switch parsed.Status {
		case "ok":
			dangerFlags = DetectPSDangerousPatterns(parsed)
		case "unsupported":
			dangerFlags = append(dangerFlags, PSDangerFlag{
				Category: "unsupported_construct",
				Command:  command,
				Reason:   "command contains dynamic constructs that cannot be statically analyzed",
			})
		case "parse_failed", "parse_errors":
			dangerFlags = append(dangerFlags, PSDangerFlag{
				Category: "parse_failure",
				Command:  command,
				Reason:   fmt.Sprintf("AST parsing returned status %q, cannot verify safety", parsed.Status),
			})
		}
	}

	astForcesConfirmation := len(dangerFlags) > 0

	riskReasons := []string{modelReason}
	for _, f := range dangerFlags {
		riskReasons = append(riskReasons, fmt.Sprintf("[AST] %s: %s (%s)", f.Category, f.Command, f.Reason))
	}

	// Permission decision
	persistRules, _ := LoadPersistAllowlist(t.permCtx.ProjectPath)
	normalized := NormalizeCommand(command, ShellFamilyPowerShell)
	var matchedRule *AllowlistRule
	for _, rule := range t.sessionAL.Rules() {
		if rule.Tool == PowerShellToolName && MatchRule(normalized, ShellFamilyPowerShell, rule) {
			matchedRule = rule
			break
		}
	}
	if matchedRule == nil {
		for _, rule := range persistRules {
			if rule.Tool == PowerShellToolName && MatchRule(normalized, ShellFamilyPowerShell, rule) {
				matchedRule = rule
				break
			}
		}
	}
	allowlistHit := matchedRule != nil

	requiresConfirmation := ResolvePermissionDecision(t.permCtx.Mode, modelRisk, allowlistHit)
	if astForcesConfirmation && t.permCtx.Mode != PermissionModeYOLO {
		requiresConfirmation = true
	}

	grantType := GrantTypeAuto
	if requiresConfirmation {
		gt, err := t.requestUserConfirmation(ctx, command, desc, modelRisk, strings.Join(riskReasons, "; "), ShellFamilyPowerShell)
		if err != nil {
			return "", err
		}
		grantType = gt
		if grantType == GrantTypeReject {
			out := &BashToolOutput{
				Executed:                 false,
				Shell:                    t.psPath,
				ShellFamily:              ShellFamilyPowerShell,
				Cwd:                      workDir,
				PermissionMode:           t.permCtx.Mode,
				RiskLevel:                modelRisk,
				RiskReasons:              riskReasons,
				RequiresUserConfirmation: true,
				GrantType:                GrantTypeReject,
			}
			return out.JSON(), nil
		}
	}

	// Execute
	EmitToolRuntimeProgress(ctx, "execute", fmt.Sprintf("running (powershell): %s", command), map[string]any{
		"command":    command,
		"cwd":        workDir,
		"shell":      t.psPath,
		"ps_edition": string(t.psEdition),
		"risk_level": string(modelRisk),
		"grant_type": string(grantType),
	})

	exe, shellArgs := BuildShellCommand(t.psPath, ShellFamilyPowerShell, command)
	timeout := time.Duration(timeoutMs) * time.Millisecond
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	res := RunCommandLimited(execCtx, workDir, exe, shellArgs, bashCaptureMaxStdout, bashCaptureMaxStderr, bashDefaultWaitDelay)

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
		Shell:                    t.psPath,
		ShellFamily:              ShellFamilyPowerShell,
		Cwd:                      workDir,
		PermissionMode:           t.permCtx.Mode,
		RiskLevel:                modelRisk,
		RiskReasons:              riskReasons,
		RequiresUserConfirmation: requiresConfirmation,
		GrantType:                grantType,
		MatchedRule:              matchedRule,
		Truncated:                truncated,
		ReturnCodeInterpretation: interpretation,
	}

	return out.JSON(), nil
}

func (t *PowerShellTool) resolveTimeout(args map[string]any, command string) int64 {
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

func (t *PowerShellTool) resolveWorkDir(explicitCwd string) string {
	if explicitCwd != "" {
		return explicitCwd
	}
	if t.permCtx != nil && t.permCtx.ProjectPath != "" {
		return t.permCtx.ProjectPath
	}
	return "."
}

func (t *PowerShellTool) requestUserConfirmation(ctx context.Context, command, desc string, risk RiskLevel, reason string, family ShellFamily) (GrantType, error) {
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

	isCompound := IsCompoundCommand(command)
	options := []string{"allow_once", "allow_session", "allow_rule", "reject"}
	if isCompound || GenerateRule(command, family, isCompound, PowerShellToolName) == nil {
		options = []string{"allow_once", "reject"}
	}

	question := fmt.Sprintf("是否允许执行该 PowerShell 命令？\n\n命令：%s", command)
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
			"shell_confirm": true,
		},
	}

	t.ctx.GetEmitter().EmitHumanRequest(iteration, requestID, question, humanInputRequest)

	snap := t.ctx.UpdateTaskStatus(TaskStatusUpdate{
		Task:     "等待 PowerShell 命令确认",
		Status:   TaskStatusPaused,
		Message:  fmt.Sprintf("等待确认命令: %s", command),
		Progress: -1,
	})
	t.ctx.GetEmitter().EmitStateChange(snap)

	ctxMap := map[string]any{
		"request_id":   requestID,
		"shell_confirm": true,
		"input_type":   "single_choice",
		"options":      options,
	}

	answer, err := onHumanInput(ctx, question, ctxMap)
	if err != nil {
		snap := t.ctx.UpdateTaskStatus(TaskStatusUpdate{
			Task:     "PowerShell 命令确认失败",
			Status:   TaskStatusRunning,
			Message:  err.Error(),
			Progress: -1,
		})
		t.ctx.GetEmitter().EmitStateChange(snap)
		return GrantTypeReject, err
	}

	answer = strings.TrimSpace(strings.ToLower(answer))

	snap = t.ctx.UpdateTaskStatus(TaskStatusUpdate{
		Task:     "收到 PowerShell 命令确认",
		Status:   TaskStatusRunning,
		Message:  fmt.Sprintf("确认结果: %s", answer),
		Progress: -1,
	})
	t.ctx.GetEmitter().EmitStateChange(snap)

	switch answer {
	case "allow_once":
		return GrantTypeAllowOnce, nil
	case "allow_session":
		rule := GenerateRule(command, family, isCompound, PowerShellToolName)
		if rule != nil {
			_ = t.sessionAL.Add(rule)
		}
		return GrantTypeAllowSession, nil
	case "allow_rule":
		rule := GenerateRule(command, family, isCompound, PowerShellToolName)
		if rule != nil {
			_ = t.sessionAL.Add(rule)
			_ = AddPersistRule(t.permCtx.ProjectPath, rule)
		}
		return GrantTypeAllowRule, nil
	default:
		return GrantTypeReject, nil
	}
}
