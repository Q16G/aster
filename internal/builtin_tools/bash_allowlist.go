package builtin_tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const (
	maxSessionAllowlistRules = 200
	maxPersistAllowlistRules = 500
	persistAllowlistDir      = ".sastpro"
	persistAllowlistFile     = "bash_allowlist.json"
)

// ------- 命令规范化 -------

// NormalizeCommand 规范化命令字符串用于规则匹配
func NormalizeCommand(command string, family ShellFamily) string {
	cmd := strings.TrimSpace(command)
	// 折叠连续空白为单个空格
	cmd = collapseWhitespace(cmd)
	// 简单引号归一：仅处理纯语法等价的引号包裹
	cmd = normalizeSimpleQuotes(cmd)
	// Windows 下大小写不敏感
	if family == ShellFamilyPowerShell || family == ShellFamilyCmd {
		cmd = strings.ToLower(cmd)
	}
	return cmd
}

func collapseWhitespace(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	prevSpace := false
	for _, r := range s {
		if r == ' ' || r == '\t' {
			if !prevSpace {
				b.WriteByte(' ')
			}
			prevSpace = true
		} else {
			b.WriteRune(r)
			prevSpace = false
		}
	}
	return b.String()
}

// normalizeSimpleQuotes 去除纯语法等价的简单引号
// 例如 `"npm" run build` → `npm run build`
// 不处理含变量展开或转义的引号
func normalizeSimpleQuotes(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	i := 0
	for i < len(s) {
		ch := s[i]
		if (ch == '"' || ch == '\'') && canStripQuote(s, i) {
			// 找到配对引号
			quote := ch
			end := strings.IndexByte(s[i+1:], quote)
			if end >= 0 {
				inner := s[i+1 : i+1+end]
				// 仅当内容不含特殊字符时才剥离
				if !containsShellSpecial(inner) {
					b.WriteString(inner)
					i = i + 1 + end + 1
					continue
				}
			}
		}
		b.WriteByte(ch)
		i++
	}
	return b.String()
}

func canStripQuote(s string, pos int) bool {
	if pos > 0 && s[pos-1] == '\\' {
		return false
	}
	return true
}

func containsShellSpecial(s string) bool {
	for _, ch := range s {
		switch ch {
		case '$', '`', '\\', '!', '*', '?', '[', ']', '{', '}', '~', '(', ')', '<', '>', '|', '&', ';', '\n':
			return true
		}
	}
	return false
}

// ------- 规则匹配 -------

// MatchRule 检查命令是否匹配单条规则
func MatchRule(normalized string, family ShellFamily, rule *AllowlistRule) bool {
	if rule.Tool != BashToolName {
		return false
	}
	if rule.ShellFamily != family {
		return false
	}
	switch rule.Kind {
	case AllowlistRuleExact:
		return normalized == rule.Pattern
	case AllowlistRulePrefix:
		if !strings.HasPrefix(normalized, rule.Pattern) {
			return false
		}
		// 后继字符必须是空白或命令结束
		if len(normalized) == len(rule.Pattern) {
			return true
		}
		next := normalized[len(rule.Pattern)]
		return next == ' ' || next == '\t'
	}
	return false
}

// ------- 规则生成 -------

// GenerateRule 从命令中生成 allowlist 规则
// 复合命令不生成持久规则
func GenerateRule(command string, family ShellFamily, isCompound bool) *AllowlistRule {
	normalized := NormalizeCommand(command, family)
	if normalized == "" {
		return nil
	}

	// 尝试提取稳定前缀
	prefix := extractStablePrefix(normalized)
	if prefix != "" && !isCompound {
		return &AllowlistRule{
			Tool:        BashToolName,
			ShellFamily: family,
			Kind:        AllowlistRulePrefix,
			Pattern:     prefix,
		}
	}

	// 退化为 exact 规则
	if isCompound {
		return nil // 复合命令不生成持久规则
	}
	return &AllowlistRule{
		Tool:        BashToolName,
		ShellFamily: family,
		Kind:        AllowlistRuleExact,
		Pattern:     normalized,
	}
}

// extractStablePrefix 提取命令的稳定前缀用于生成 allowlist 规则。
// 对于解释器命令，前缀包含脚本路径以避免过度放行。
func extractStablePrefix(normalized string) string {
	fields := strings.Fields(normalized)
	if len(fields) < 2 {
		return ""
	}

	base := strings.ToLower(fields[0])

	// 子命令级前缀：命令 + 子命令
	subcommandPrefixes := map[string][]string{
		"go":     {"go test", "go build", "go run", "go vet", "go fmt", "go mod"},
		"npm":    {"npm run", "npm test", "npm start"},
		"pnpm":   {"pnpm run", "pnpm test", "pnpm build"},
		"yarn":   {"yarn run", "yarn test", "yarn build"},
		"make":   {"make"},
		"git":    {"git status", "git diff", "git log", "git show", "git branch", "git stash"},
		"cargo":  {"cargo test", "cargo build", "cargo run", "cargo check"},
		"dotnet": {"dotnet build", "dotnet test", "dotnet run"},
	}

	if prefixes, ok := subcommandPrefixes[base]; ok {
		for _, p := range prefixes {
			if strings.HasPrefix(normalized, p) {
				return p
			}
		}
		// 基础命令名 + 子命令
		if len(fields) >= 2 {
			return fields[0] + " " + fields[1]
		}
		return fields[0]
	}

	// 解释器：前缀必须包含脚本路径（command + arg1），防止裸命令名放行整个解释器。
	// 例如 "python tools/lint.py --fix" → prefix "python tools/lint.py"
	interpreters := map[string]bool{
		"python": true, "python3": true, "node": true, "java": true,
	}
	if interpreters[base] && len(fields) >= 2 {
		return fields[0] + " " + fields[1]
	}

	// 项目脚本 ./scripts/xxx.sh
	if strings.HasPrefix(normalized, "./") {
		return fields[0]
	}

	return ""
}

// ------- 会话级 Allowlist -------

// SessionAllowlist 会话级 allowlist，线程安全
type SessionAllowlist struct {
	mu    sync.RWMutex
	rules []*AllowlistRule
}

func NewSessionAllowlist() *SessionAllowlist {
	return &SessionAllowlist{}
}

func (a *SessionAllowlist) Rules() []*AllowlistRule {
	if a == nil {
		return nil
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	out := make([]*AllowlistRule, len(a.rules))
	copy(out, a.rules)
	return out
}

func (a *SessionAllowlist) Add(rule *AllowlistRule) error {
	if a == nil || rule == nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.rules) >= maxSessionAllowlistRules {
		return fmt.Errorf("session allowlist limit reached (%d rules)", maxSessionAllowlistRules)
	}
	// 去重
	for _, existing := range a.rules {
		if existing.Kind == rule.Kind && existing.Pattern == rule.Pattern && existing.ShellFamily == rule.ShellFamily {
			return nil
		}
	}
	a.rules = append(a.rules, rule)
	return nil
}

func (a *SessionAllowlist) Remove(idx int) bool {
	if a == nil {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if idx < 0 || idx >= len(a.rules) {
		return false
	}
	a.rules = append(a.rules[:idx], a.rules[idx+1:]...)
	return true
}

// ------- 持久 Allowlist -------

// LoadPersistAllowlist 从项目本地配置加载持久 allowlist
func LoadPersistAllowlist(projectPath string) ([]*AllowlistRule, error) {
	fp := filepath.Join(projectPath, persistAllowlistDir, persistAllowlistFile)
	data, err := os.ReadFile(fp)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read persist allowlist: %w", err)
	}
	var rules []*AllowlistRule
	if err := json.Unmarshal(data, &rules); err != nil {
		return nil, fmt.Errorf("parse persist allowlist: %w", err)
	}
	return rules, nil
}

// SavePersistAllowlist 保存持久 allowlist 到项目本地配置
func SavePersistAllowlist(projectPath string, rules []*AllowlistRule) error {
	dir := filepath.Join(projectPath, persistAllowlistDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create persist allowlist dir: %w", err)
	}
	data, err := json.MarshalIndent(rules, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal persist allowlist: %w", err)
	}
	fp := filepath.Join(dir, persistAllowlistFile)
	return os.WriteFile(fp, data, 0o644)
}

// AddPersistRule 向持久 allowlist 追加规则
func AddPersistRule(projectPath string, rule *AllowlistRule) error {
	rules, err := LoadPersistAllowlist(projectPath)
	if err != nil {
		return err
	}
	if len(rules) >= maxPersistAllowlistRules {
		return fmt.Errorf("persist allowlist limit reached (%d rules)", maxPersistAllowlistRules)
	}
	// 去重
	for _, existing := range rules {
		if existing.Kind == rule.Kind && existing.Pattern == rule.Pattern && existing.ShellFamily == rule.ShellFamily {
			return nil
		}
	}
	rules = append(rules, rule)
	return SavePersistAllowlist(projectPath, rules)
}

// RemovePersistRule 从持久 allowlist 删除规则
func RemovePersistRule(projectPath string, idx int) error {
	rules, err := LoadPersistAllowlist(projectPath)
	if err != nil {
		return err
	}
	if idx < 0 || idx >= len(rules) {
		return fmt.Errorf("rule index %d out of range", idx)
	}
	rules = append(rules[:idx], rules[idx+1:]...)
	return SavePersistAllowlist(projectPath, rules)
}
