package tui

import (
	"strings"
	"testing"
	"time"
)

// TestInlineStep_StreamBuffersIsolatedByStepID 红线：同 agentName 不同 stepID 必须
// 独立 buffer。修复前所有流共享 streamingByAgent[agentName]，peer 内容混入主路径流。
func TestInlineStep_StreamBuffersIsolatedByStepID(t *testing.T) {
	m := NewChatModel()
	m.SetSize(80, 24)

	m.AppendStream("pentest", "main-1 ", "")
	m.AppendStream("pentest", "peer-p1-1 ", "p1")
	m.AppendStream("pentest", "peer-p2-1 ", "p2")
	m.AppendStream("pentest", "main-2", "")
	m.AppendStream("pentest", "peer-p1-2", "p1")

	if got := m.StreamContent("pentest", ""); got != "main-1 main-2" {
		t.Errorf("main path content=%q, want %q", got, "main-1 main-2")
	}
	if got := m.StreamContent("pentest", "p1"); got != "peer-p1-1 peer-p1-2" {
		t.Errorf("p1 content=%q, want %q", got, "peer-p1-1 peer-p1-2")
	}
	if got := m.StreamContent("pentest", "p2"); got != "peer-p2-1 " {
		t.Errorf("p2 content=%q, want %q", got, "peer-p2-1 ")
	}
}

// TestInlineStep_FlushStreamCarriesStepID FlushStream(p2) 产出的 TextPart 必须带 StepID
// 字段 + 加入 idxByStepID 索引 → PartsForStep(p2) 拿到该 TextPart。
func TestInlineStep_FlushStreamCarriesStepID(t *testing.T) {
	m := NewChatModel()
	m.SetSize(80, 24)

	m.AppendStream("pentest", "p2 output", "p2")
	if !m.FlushStream("pentest", "p2") {
		t.Fatal("expected FlushStream(pentest, p2) to flush content")
	}

	var p2TextIdx = -1
	for i, p := range m.store.parts {
		if p.Type == PartTypeText && p.Text != nil && p.Text.StepID == "p2" {
			p2TextIdx = i
			break
		}
	}
	if p2TextIdx == -1 {
		t.Fatalf("expected TextPart with StepID=p2 to exist in store")
	}
	idxList := m.store.PartsForStep("p2")
	found := false
	for _, i := range idxList {
		if i == p2TextIdx {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("idxByStepID[p2] missing TextPart at idx=%d (got %v)", p2TextIdx, idxList)
	}
}

// TestFilterMainParts_SkipsStepIDChildrenAndCard 红线（变体 C 移植后）：
//   - 带 StepID 的 ThinkingPart / ToolPart / TextPart 不进主流（归到 inline_step 子序列）
//   - InlineStepPart **本体也不进主流**（由下区 tile bar 负责呈现，plan v2 §Step 3）
func TestFilterMainParts_SkipsStepIDChildrenAndCard(t *testing.T) {
	m := NewChatModel()
	m.SetSize(80, 24)
	m.AddPart(DisplayPart{
		Type: PartTypeUser,
		Time: time.Now(),
		User: &UserPart{Content: "go"},
	})
	m.AddPart(DisplayPart{
		Type: PartTypeInlineStep,
		Time: time.Now(),
		InlineStep: &InlineStepPart{
			StepID:      "p2",
			Description: "peer task",
			Status:      "running",
			StartedAt:   time.Now(),
		},
	})
	m.AddPart(DisplayPart{
		Type: PartTypeThinking,
		Time: time.Now(),
		Thinking: &ThinkingPart{
			Content: "secret peer thinking",
			StepID:  "p2",
		},
	})
	m.AddPart(DisplayPart{
		Type: PartTypeTool,
		Time: time.Now(),
		Tool: &ToolPart{
			Name:   "bash",
			CallID: "c1",
			State:  "running",
			StepID: "p2",
		},
	})
	m.AddPart(DisplayPart{
		Type: PartTypeText,
		Time: time.Now(),
		Text: &TextPart{Content: "peer stream out", StepID: "p2"},
	})

	m.refreshContent()
	rendered := m.viewport.View()

	// 子 part 不进主流
	if strings.Contains(rendered, "secret peer thinking") {
		t.Errorf("main timeline must NOT contain peer thinking; got:\n%s", rendered)
	}
	if strings.Contains(rendered, "peer stream out") {
		t.Errorf("main timeline must NOT contain peer stream text; got:\n%s", rendered)
	}
	// 卡片本体也不进主流（C 范式下由 tile bar 渲染）
	if strings.Contains(rendered, "peer task") {
		t.Errorf("main timeline must NOT contain InlineStep description (now in tile bar); got:\n%s", rendered)
	}
}
