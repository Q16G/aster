package tui

import (
	"strings"
	"testing"
	"time"
)

// 变体 C inline_step 详情态红线测试（plan v2 §测试覆盖）：
//   - TestStepDetail_EnterTriggersDetailMode
//   - TestStepDetail_RendersChildrenForStep
//   - TestStepDetail_EscReturnsToSplit
//   - TestStepDetail_RejectedWhenViewingChild ([v2-fix P5] 优先级守卫)
//   - TestStepDetail_SetViewingStepIDSwitchesPeer (h/l 切 peer)

// helper：创建 InlineStep + 一组子 part（Thinking + Tool）
func seedStepWithChildren(m *ChatModel, stepID, desc string) {
	m.AddPart(DisplayPart{
		Type: PartTypeInlineStep,
		Time: time.Now(),
		InlineStep: &InlineStepPart{
			StepID:      stepID,
			Description: desc,
			Status:      "running",
			StartedAt:   time.Now(),
		},
	})
	m.AddPart(DisplayPart{
		Type: PartTypeThinking,
		Time: time.Now(),
		Thinking: &ThinkingPart{
			Content: "MARKER_" + stepID + "_THINK",
			StepID:  stepID,
		},
	})
	m.AddPart(DisplayPart{
		Type: PartTypeTool,
		Time: time.Now(),
		Tool: &ToolPart{
			Name:   "bash",
			CallID: "call-" + stepID,
			State:  "completed",
			Result: "MARKER_" + stepID + "_RESULT",
			StepID: stepID,
		},
	})
}

func TestStepDetail_EnterTriggersDetailMode(t *testing.T) {
	m := NewChatModel()
	m.SetSize(80, 24)
	m.AddPart(DisplayPart{Type: PartTypeUser, Time: time.Now(), User: &UserPart{Content: "go"}})
	seedStepWithChildren(&m, "p1", "task 1")

	if ok := m.EnterStepDetail("p1"); !ok {
		t.Fatal("EnterStepDetail(p1) should succeed")
	}
	if got := m.ViewingStepID(); got != "p1" {
		t.Errorf("ViewingStepID=%q, want p1", got)
	}
}

func TestStepDetail_RendersChildrenForStep(t *testing.T) {
	m := NewChatModel()
	m.SetSize(80, 24)
	m.AddPart(DisplayPart{Type: PartTypeUser, Time: time.Now(), User: &UserPart{Content: "go"}})
	seedStepWithChildren(&m, "p1", "task 1")

	if ok := m.EnterStepDetail("p1"); !ok {
		t.Fatal("EnterStepDetail(p1) should succeed")
	}
	rendered := m.viewport.View()
	if !strings.Contains(rendered, "MARKER_p1_THINK") {
		t.Errorf("detail view should contain p1's thinking marker; got:\n%s", rendered)
	}
	// 工具卡片在折叠态只显示 name + duration，不显示 result 全文；只断言 name 出现
	if !strings.Contains(rendered, "bash") {
		t.Errorf("detail view should contain p1's tool name; got:\n%s", rendered)
	}
}

func TestStepDetail_EscReturnsToSplit(t *testing.T) {
	m := NewChatModel()
	m.SetSize(80, 24)
	m.AddPart(DisplayPart{Type: PartTypeUser, Time: time.Now(), User: &UserPart{Content: "go"}})
	seedStepWithChildren(&m, "p1", "task 1")

	if ok := m.EnterStepDetail("p1"); !ok {
		t.Fatal("EnterStepDetail(p1) should succeed")
	}
	m.ExitStepDetail()
	if got := m.ViewingStepID(); got != "" {
		t.Errorf("after ExitStepDetail ViewingStepID=%q, want \"\"", got)
	}
}

// TestStepDetail_RejectedWhenViewingChild 红线 [v2-fix P5]：sub-agent transcript
// 下用户进 inline_step 详情应被拒绝。
func TestStepDetail_RejectedWhenViewingChild(t *testing.T) {
	m := NewChatModel()
	m.SetSize(80, 24)
	m.AddPart(DisplayPart{Type: PartTypeUser, Time: time.Now(), User: &UserPart{Content: "go"}})
	// 模拟有一个 sub-agent
	m.AddPart(DisplayPart{
		Type: PartTypeSubAgent,
		Time: time.Now(),
		SubAgent: &SubAgentPart{
			AgentName: "child",
			CallID:    "call-child",
			Status:    "running",
		},
	})
	m.store.spawnByCallID["call-child"] = agentSpawnInfo{CallID: "call-child", SubScheme: true}
	seedStepWithChildren(&m, "p1", "task 1")

	if ok := m.EnterChild("call-child"); !ok {
		t.Fatal("EnterChild(call-child) should succeed")
	}
	if ok := m.EnterStepDetail("p1"); ok {
		t.Error("EnterStepDetail should return false when viewingChild != \"\"")
	}
	if got := m.ViewingStepID(); got != "" {
		t.Errorf("ViewingStepID should remain empty when blocked, got %q", got)
	}
}

// TestStepDetail_SetViewingStepIDSwitchesPeer 红线 [v2-fix P4]：detail 模式下用
// SetViewingStepID 切到下一个 peer，应当切换内容（不需要先 ExitStepDetail）。
func TestStepDetail_SetViewingStepIDSwitchesPeer(t *testing.T) {
	m := NewChatModel()
	m.SetSize(80, 24)
	m.AddPart(DisplayPart{Type: PartTypeUser, Time: time.Now(), User: &UserPart{Content: "go"}})
	seedStepWithChildren(&m, "p1", "task 1")
	seedStepWithChildren(&m, "p2", "task 2")

	if ok := m.EnterStepDetail("p1"); !ok {
		t.Fatal("EnterStepDetail(p1) should succeed")
	}
	if !strings.Contains(m.viewport.View(), "MARKER_p1_THINK") {
		t.Fatalf("initial detail should show p1 markers; got:\n%s", m.viewport.View())
	}
	m.SetViewingStepID("p2")
	if got := m.ViewingStepID(); got != "p2" {
		t.Errorf("after SetViewingStepID(p2) ViewingStepID=%q", got)
	}
	rendered := m.viewport.View()
	if !strings.Contains(rendered, "MARKER_p2_THINK") {
		t.Errorf("after switch view should contain p2 markers; got:\n%s", rendered)
	}
	if strings.Contains(rendered, "MARKER_p1_THINK") {
		t.Errorf("after switch view should NOT contain p1 markers; got:\n%s", rendered)
	}
}

// TestStepDetail_EnterRejectsUnknownStep 不存在的 stepID 应当拒绝。
func TestStepDetail_EnterRejectsUnknownStep(t *testing.T) {
	m := NewChatModel()
	m.SetSize(80, 24)
	m.AddPart(DisplayPart{Type: PartTypeUser, Time: time.Now(), User: &UserPart{Content: "go"}})

	if ok := m.EnterStepDetail("nope"); ok {
		t.Error("EnterStepDetail(nope) should return false for unknown stepID")
	}
	if ok := m.EnterStepDetail(""); ok {
		t.Error("EnterStepDetail(\"\") should return false for empty stepID")
	}
}
