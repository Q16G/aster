package tui

import (
	"strings"
	"testing"
)

// TestThinking_MainAndPeersIsolatedByStepID 红线（与截图症状对齐）：
// 同 agentName 下 main（stepID=""）与多个 peer（stepID="p1"/"p2"）的活体 thinking
// 必须独立 buffer，互不串扰。修复前所有 thinking 共享单 agentName key 的 buffer，
// peer reasoning content 也算 root thinking 被 rootThinkingState 选中显示在主屏
// renderThinkingStream（截图里 InlineStep 卡片下方那一大段 `| Thinking: ...` 漏渲）。
func TestThinking_MainAndPeersIsolatedByStepID(t *testing.T) {
	m := NewChatModel()
	m.SetSize(80, 24)
	m.rootAgentName = "pentest"

	m.AppendThinkingForAgent("pentest", "main reasoning chunk 1 ", "g-main", "")
	m.AppendThinkingForAgent("pentest", "peer-p1 reasoning ", "g-p1", "p1")
	m.AppendThinkingForAgent("pentest", "peer-p2 reasoning ", "g-p2", "p2")
	m.AppendThinkingForAgent("pentest", "main reasoning chunk 2", "g-main", "")

	// rootThinkingState 必须只返回 main 路径 buffer（stepID=""），不含 peer 内容。
	root := m.rootThinkingState()
	if root == nil {
		t.Fatal("expected root thinking buffer to be present")
	}
	got := root.buf.String()
	want := "main reasoning chunk 1 main reasoning chunk 2"
	if got != want {
		t.Errorf("root thinking content=%q, want %q", got, want)
	}
	if strings.Contains(got, "peer-p1") || strings.Contains(got, "peer-p2") {
		t.Errorf("root thinking must NOT contain peer reasoning; got %q", got)
	}
}

// TestThinking_PeerStreamNotInMainLiveRender 红线：peer 活体 thinking 不出现在
// renderThinkingStream 输出（renderContent 1015 行调用），即 InlineStep 卡片下方
// 主屏幕区域不应能看到 peer reasoning 文本。
func TestThinking_PeerStreamNotInMainLiveRender(t *testing.T) {
	m := NewChatModel()
	m.SetSize(80, 24)
	m.rootAgentName = "pentest"

	m.AppendThinkingForAgent("pentest", "PEER_SECRET_REASONING_MARKER", "g-p1", "p1")

	// 只有 peer 在 reasoning，无 main —— rootThinkingState 必须为 nil
	if m.rootThinkingState() != nil {
		t.Fatal("rootThinkingState should be nil when only peer is reasoning")
	}

	// renderContent 走 if m.rootThinkingState() != nil 才会调 renderThinkingStream。
	// 验证 fullContent 不含 peer marker。
	m.refreshContent()
	if strings.Contains(m.fullContent, "PEER_SECRET_REASONING_MARKER") {
		t.Errorf("main live render must NOT contain peer reasoning; got:\n%s", m.fullContent)
	}
}

// TestThinking_FlushThinkingForAgentFlushesAllStepBuckets 旧 API 兼容：调
// FlushThinkingForAgent(agentName) 应把该 agent 下所有 (agentName, *) 桶全部 flush。
func TestThinking_FlushThinkingForAgentFlushesAllStepBuckets(t *testing.T) {
	m := NewChatModel()
	m.SetSize(80, 24)
	m.rootAgentName = "pentest"

	m.AppendThinkingForAgent("pentest", "main ", "g-main", "")
	m.AppendThinkingForAgent("pentest", "peer1 ", "g-p1", "p1")
	m.AppendThinkingForAgent("pentest", "peer2", "g-p2", "p2")

	if !m.FlushThinkingForAgent("pentest") {
		t.Fatal("FlushThinkingForAgent should flush something")
	}
	if m.anyThinking() {
		t.Errorf("anyThinking should be false after FlushThinkingForAgent flushes all buckets")
	}

	// 验证落盘的 ThinkingPart 各自带正确 StepID
	gotByStep := map[string]string{}
	for _, p := range m.store.parts {
		if p.Type == PartTypeThinking && p.Thinking != nil {
			gotByStep[p.Thinking.StepID] = p.Thinking.Content
		}
	}
	if gotByStep[""] != "main " {
		t.Errorf("main flushed=%q, want %q", gotByStep[""], "main ")
	}
	if gotByStep["p1"] != "peer1 " {
		t.Errorf("p1 flushed=%q, want %q", gotByStep["p1"], "peer1 ")
	}
	if gotByStep["p2"] != "peer2" {
		t.Errorf("p2 flushed=%q, want %q", gotByStep["p2"], "peer2")
	}
}

// TestThinking_GroupIDSwitchWithinPeerBucket 同一 peer 桶内 groupID 切换触发 flush，
// 不应影响其他桶。
func TestThinking_GroupIDSwitchWithinPeerBucket(t *testing.T) {
	m := NewChatModel()
	m.SetSize(80, 24)
	m.rootAgentName = "pentest"

	m.AppendThinkingForAgent("pentest", "peer1-block1 ", "g1", "p1")
	// 同 peer，新 groupID → 前一个 group 被 flush 成 ThinkingPart，新 buffer 开始
	m.AppendThinkingForAgent("pentest", "peer1-block2", "g2", "p1")
	// main 路径未受影响
	m.AppendThinkingForAgent("pentest", "main-alive", "g-main", "")

	if root := m.rootThinkingState(); root == nil || root.buf.String() != "main-alive" {
		var got string
		if root != nil {
			got = root.buf.String()
		}
		t.Errorf("main buffer should hold only main delta=%q", got)
	}
}
