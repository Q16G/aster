package tui

import (
	"testing"
	"time"
)

// TestPartsStore_PartsForStep_StepIDIndex 验证 commit 11 加的 idxByStepID:
//   - Append 一组 ToolPart with StepID → PartsForStep 返回所有下标
//   - InlineStepPart 本体也归属自己的 stepID
//   - 不同 stepID 的 parts 互不串
func TestPartsStore_PartsForStep_StepIDIndex(t *testing.T) {
	s := NewPartsStore()

	// step-a 卡片 + 2 个 tool
	s.Append(DisplayPart{Type: PartTypeInlineStep, InlineStep: &InlineStepPart{StepID: "step-a", Status: "running"}})
	s.Append(DisplayPart{Type: PartTypeTool, Tool: &ToolPart{Name: "bash", CallID: "ca1", StepID: "step-a"}})
	s.Append(DisplayPart{Type: PartTypeTool, Tool: &ToolPart{Name: "rg", CallID: "ca2", StepID: "step-a"}})

	// step-b 卡片 + 1 个 tool
	s.Append(DisplayPart{Type: PartTypeInlineStep, InlineStep: &InlineStepPart{StepID: "step-b", Status: "running"}})
	s.Append(DisplayPart{Type: PartTypeTool, Tool: &ToolPart{Name: "read", CallID: "cb1", StepID: "step-b"}})

	// 一个无 StepID 的主路径 tool（不应进任何 step 的索引）
	s.Append(DisplayPart{Type: PartTypeTool, Tool: &ToolPart{Name: "ls", CallID: "main1"}})

	gotA := s.PartsForStep("step-a")
	if len(gotA) != 3 { // 1 卡 + 2 tool
		t.Fatalf("step-a 应有 3 个 parts，got %d (%v)", len(gotA), gotA)
	}

	gotB := s.PartsForStep("step-b")
	if len(gotB) != 2 { // 1 卡 + 1 tool
		t.Fatalf("step-b 应有 2 个 parts，got %d (%v)", len(gotB), gotB)
	}

	gotEmpty := s.PartsForStep("")
	if gotEmpty != nil {
		t.Fatalf("PartsForStep('') 应返回 nil，got %v", gotEmpty)
	}

	gotMissing := s.PartsForStep("step-z")
	if gotMissing != nil {
		t.Fatalf("PartsForStep(missing) 应返回 nil，got %v", gotMissing)
	}
}

// TestPartsStore_InlineStepsThisTurn_PerTurnReset 验证 UserPart 重置 inlineStepIdxThisTurn。
func TestPartsStore_InlineStepsThisTurn_PerTurnReset(t *testing.T) {
	s := NewPartsStore()
	s.Append(DisplayPart{Type: PartTypeUser, User: &UserPart{Content: "turn1"}})
	s.Append(DisplayPart{Type: PartTypeInlineStep, InlineStep: &InlineStepPart{StepID: "s1", Status: "running"}})
	s.Append(DisplayPart{Type: PartTypeInlineStep, InlineStep: &InlineStepPart{StepID: "s2", Status: "running"}})

	turn1 := s.InlineStepsThisTurn()
	if len(turn1) != 2 {
		t.Fatalf("turn1 应有 2 个 inline_step，got %d", len(turn1))
	}

	// 新 turn 边界：UserPart 入栈后应清空 inlineStepIdxThisTurn
	s.Append(DisplayPart{Type: PartTypeUser, User: &UserPart{Content: "turn2"}})
	if got := s.InlineStepsThisTurn(); got != nil {
		t.Fatalf("UserPart 重置后应返回 nil，got %v", got)
	}

	s.Append(DisplayPart{Type: PartTypeInlineStep, InlineStep: &InlineStepPart{StepID: "s3", Status: "running"}})
	turn2 := s.InlineStepsThisTurn()
	if len(turn2) != 1 || turn2[0].StepID != "s3" {
		t.Fatalf("turn2 应只含 s3，got %v", turn2)
	}
}

// TestPartsStore_RebuildIndex_RecoversInlineStepIndexes 验证 RebuildIndex（SetParts 切换会话路径）
// 会重建 idxByStepID + inlineStepIdxThisTurn——commit 11 沿用 1ec66e58 fix 模式。
func TestPartsStore_RebuildIndex_RecoversInlineStepIndexes(t *testing.T) {
	s := NewPartsStore()
	// 模拟 SetAll 加载老 session
	parts := []DisplayPart{
		{Type: PartTypeUser, User: &UserPart{Content: "ask"}},
		{Type: PartTypeInlineStep, Time: time.Now(), InlineStep: &InlineStepPart{StepID: "x", Status: "running"}},
		{Type: PartTypeTool, Tool: &ToolPart{Name: "bash", CallID: "tc1", StepID: "x"}},
	}
	s.SetAll(parts)

	if idx, ok := s.IndexByInlineStepID("x"); !ok || idx != 1 {
		t.Fatalf("IndexByInlineStepID(x) = (%d, %v), want (1, true)", idx, ok)
	}
	if got := s.PartsForStep("x"); len(got) != 2 {
		t.Fatalf("PartsForStep(x) 应有 2 项 (卡 + tool)，got %v", got)
	}
	if got := s.InlineStepsThisTurn(); len(got) != 1 {
		t.Fatalf("InlineStepsThisTurn 应有 1 项，got %d", len(got))
	}
}
