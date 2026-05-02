package builtin_tools

import "strings"

// ParseModelRisk 校验模型声明的 risk 值；无效或缺失时返回 uncertain。
func ParseModelRisk(raw string) RiskLevel {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "low":
		return RiskLevelLow
	case "high":
		return RiskLevelHigh
	case "uncertain":
		return RiskLevelUncertain
	default:
		return RiskLevelUncertain
	}
}

// ResolvePermissionDecision 根据权限模式、allowlist 命中和模型声明风险判断是否需要人工审核。
//
// 决策顺序（PRD §7）：
//  1. allowlist 命中 → 直接放行（所有模式）
//  2. YOLO → 直接放行
//  3. MANUAL → 人工审核
//  4. AI → risk=low 放行，risk=high / uncertain 人工审核
func ResolvePermissionDecision(mode PermissionMode, modelRisk RiskLevel, allowlistHit bool) bool {
	if allowlistHit {
		return false
	}
	switch mode {
	case PermissionModeYOLO:
		return false
	case PermissionModeManual:
		return true
	case PermissionModeAI:
		return modelRisk != RiskLevelLow
	default:
		return true
	}
}

// IsCompoundCommand 检测命令是否包含 shell 复合操作符（引号内除外）。
// 用于 allowlist 规则生成：复合命令不提供 allow_rule 选项（PRD §6.5）。
func IsCompoundCommand(command string) bool {
	cmd := strings.TrimSpace(command)
	inSingle, inDouble := false, false
	for i := 0; i < len(cmd); i++ {
		ch := cmd[i]
		if ch == '\'' && !inDouble {
			inSingle = !inSingle
			continue
		}
		if ch == '"' && !inSingle {
			inDouble = !inDouble
			continue
		}
		if inSingle || inDouble {
			continue
		}
		remaining := cmd[i:]
		for _, op := range compoundOperators {
			if strings.HasPrefix(remaining, op) {
				return true
			}
		}
		if strings.HasPrefix(remaining, "<<") {
			return true
		}
		if strings.HasPrefix(remaining, "<(") || strings.HasPrefix(remaining, ">(") {
			return true
		}
	}
	return false
}

var compoundOperators = []string{"&&", "||", "|", ";", "&", "$(", "`"}
