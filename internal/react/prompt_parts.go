package react

import (
	"strings"

	"aster/internal/ai"
)

// PromptParts 按稳定性分层承载一次 LLM 调用的 prompt：
//   - SystemRules：通用规则（system block1，跨任务静态，条件变体内字节稳定）；
//   - SystemAgent：Agent 身份 + env 块（system block2，per-run 字节稳定）；
//   - User：任务动态输入，作为首条 user message。
//
// 五个 ReAct 形态 prompt（task_planner/think_act/step_replan/final_answer/
// intent_classification）必须同时产出非空 SystemRules 与非空 User。
type PromptParts struct {
	SystemRules string
	SystemAgent string
	User        string
}

// SystemBlocks 返回非空的 system block 列表，每个 block 对应一条 system MsgInfo。
func (p PromptParts) SystemBlocks() []string {
	blocks := make([]string, 0, 2)
	if s := strings.TrimSpace(p.SystemRules); s != "" {
		blocks = append(blocks, s)
	}
	if s := strings.TrimSpace(p.SystemAgent); s != "" {
		blocks = append(blocks, s)
	}
	return blocks
}

// SystemJoined 返回 system 全文，作为缓存 key 的哈希来源。
func (p PromptParts) SystemJoined() string {
	return strings.TrimSpace(strings.Join(p.SystemBlocks(), "\n\n"))
}

// Joined 返回完整 prompt 文本，用于 token 估算与日志（非出站消息形态）。
func (p PromptParts) Joined() string {
	parts := p.SystemBlocks()
	if u := strings.TrimSpace(p.User); u != "" {
		parts = append(parts, u)
	}
	return strings.TrimSpace(strings.Join(parts, "\n\n"))
}

// buildOutboundMsgs 组装出站消息：system block×N + 首条 user message + 阶段内 transcript。
func buildOutboundMsgs(parts PromptParts, stepHistory []*ai.MsgInfo) []*ai.MsgInfo {
	systemBlocks := parts.SystemBlocks()
	msgs := make([]*ai.MsgInfo, 0, len(systemBlocks)+1+len(stepHistory))
	for _, block := range systemBlocks {
		msgs = append(msgs, ai.NewSystemMsgInfo(block))
	}
	if user := strings.TrimSpace(parts.User); user != "" {
		msgs = append(msgs, ai.NewUserMsgInfo(user))
	}
	msgs = append(msgs, stepHistory...)
	return msgs
}
