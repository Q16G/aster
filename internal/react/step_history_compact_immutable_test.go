package react

import (
	"strings"
	"testing"

	"aster/internal/ai"
)

// TestShortenOldToolResults_DoesNotMutateSharedMsgInfo 红线 (R1-4)：
// shortenOldToolResults 必须用 clone + 替换 slice 元素的方式重写过长 tool result，
// 绝不就地改 *MsgInfo.Content。
//
// 背景：peer 桶 seed 来自 `append([]*ai.MsgInfo(nil), a.stepHistory...)`——slice header
// 是新数组，但元素是 *MsgInfo 指针，桶与主 history 共享同一组结构体。若 compaction
// in-place 改 msg.Content，peer 与主路径会跨桶看见中间态（R1-4 跨桶污染）。
//
// 本测试模拟「主 history 中有一条长 tool_result，peer 桶拿到同指针后跑 compaction」
// 场景：保留原指针快照，跑过 shortenOldToolResults 后断言原 *MsgInfo.Content 不变。
func TestShortenOldToolResults_DoesNotMutateSharedMsgInfo(t *testing.T) {
	longBody := strings.Repeat("a", 500)

	// 主 history 模拟：两轮 tool round。
	mainHistory := make([]*ai.MsgInfo, 0, 4)
	mainHistory = append(mainHistory, makeToolRound("call-1", longBody)...)
	mainHistory = append(mainHistory, makeToolRound("call-2", "short")...)

	// peer 桶 seed：复制 slice header（新底层数组），元素仍是同 *MsgInfo 指针。
	// 与 spawnInlinePeer 的 `append([]*ai.MsgInfo(nil), a.stepHistory...)` 一致。
	peerBucket := append([]*ai.MsgInfo(nil), mainHistory...)

	// 取主 history 那条长 tool_result 的指针快照，用于断言其 Content 不被改。
	// makeToolRound 产出 [assistant, tool]，所以 call-1 的 tool 在 idx=1。
	mainToolMsg := mainHistory[1]
	if mainToolMsg == nil || strings.TrimSpace(mainToolMsg.Role) != "tool" {
		t.Fatalf("test setup wrong: mainHistory[1] not a tool msg")
	}
	originalContent, ok := mainToolMsg.Content.(string)
	if !ok {
		t.Fatalf("test setup wrong: expected string content")
	}
	if originalContent != longBody {
		t.Fatalf("test setup wrong: original content mismatch")
	}

	// 在 peer 桶上跑 layer1 截断，maxRunes 远小于 longBody → 必触发截断。
	out, did := shortenOldToolResults(peerBucket, 1, 64)
	if !did {
		t.Fatalf("expected shortenOldToolResults to shorten, but did=false")
	}

	// 红线断言 1：主 history 那条 *MsgInfo 的 Content 未被改。
	// 如果 R1-4 修复回退（重新出现 `msg.Content = next`），这里会失败。
	gotMain, _ := mainToolMsg.Content.(string)
	if gotMain != originalContent {
		t.Errorf("shared *MsgInfo.Content was mutated in-place by shortenOldToolResults (R1-4 regression)\nwant len=%d (unchanged), got len=%d", len(originalContent), len(gotMain))
	}

	// 红线断言 2：peer 桶返回值的对应位置确实拿到截断后的新 *MsgInfo。
	// 截断 = clone 替换 slice 元素，所以 out[1] 应是 NEW pointer，且 Content 已变短。
	if out[1] == mainToolMsg {
		t.Errorf("expected out[1] to be a NEW *MsgInfo pointer after clone+replace, got same pointer as original")
	}
	gotPeerContent, _ := out[1].Content.(string)
	if gotPeerContent == originalContent {
		t.Errorf("expected peer bucket to see shortened content, but still sees original (len=%d)", len(gotPeerContent))
	}
}

// TestShortenOldToolResults_ChatContextSliceVariant 红线 (R1-4 second branch)：
// []*ai.ChatContext 分支也走 clone + 替换路径，原 *MsgInfo.Content 不动。
func TestShortenOldToolResults_ChatContextSliceVariant(t *testing.T) {
	longText := strings.Repeat("b", 500)
	tcAssistant := ai.NewAIMsgInfo("")
	tcAssistant.ToolCalls = []*ai.FunctionTool{{
		Id:       "call-x",
		Type:     "function",
		Function: &ai.FunctionDetail{Name: "demo", Arguments: "{}"},
	}}
	toolMsg := ai.NewToolCallResultMsgInfo("", "call-x")
	toolMsg.Content = []*ai.ChatContext{
		{Type: "text", Text: longText},
	}

	mainHistory := []*ai.MsgInfo{
		tcAssistant,
		toolMsg,
		// 第二轮短的，确保第一轮在 keepLastRounds=1 之外
		tcAssistant,
		ai.NewToolCallResultMsgInfo("short", "call-x"),
	}
	peerBucket := append([]*ai.MsgInfo(nil), mainHistory...)

	originalContent := toolMsg.Content

	out, did := shortenOldToolResults(peerBucket, 1, 64)
	if !did {
		t.Fatalf("expected shortenOldToolResults to shorten")
	}

	// 主 history 那条 *MsgInfo.Content 未被改：必须仍是同一切片对象、文本仍是原始 longText。
	gotMain, ok := toolMsg.Content.([]*ai.ChatContext)
	if !ok {
		t.Fatalf("shared *MsgInfo.Content was replaced with different type (in-place mutation)")
	}
	if len(gotMain) != 1 || gotMain[0].Text != longText {
		t.Errorf("shared *MsgInfo.Content was mutated in-place (R1-4 regression)\nwant original longText, got %#v", gotMain)
	}
	_ = originalContent

	// peer 桶返回值是新 *MsgInfo。
	if out[1] == toolMsg {
		t.Errorf("expected out[1] to be a NEW *MsgInfo pointer after clone+replace")
	}
}
