package tui

import (
	"testing"
	"time"
)

// TestParts_PeerToolGroupedByStepID（fix/05 P1-7 红线——补 commit message 声称却漏写的测试）：
// peer 桶的 tool 通过 emitter 带 step_id → event_handlers 填 ToolPart.StepID →
// PartsStore.PartsForStep(peerID) 能查到。验证 commit 11 加的 idxByStepID 真激活。
func TestParts_PeerToolGroupedByStepID(t *testing.T) {
	s := NewPartsStore()

	// 模拟主路径 tool（StepID 空）
	s.Append(DisplayPart{
		Type: PartTypeTool,
		Time: time.Now(),
		Tool: &ToolPart{
			Name:   "bash",
			CallID: "main-1",
			State:  "running",
			// StepID 空
		},
	})

	// 模拟 peer 桶 tool（StepID = "peer-a"）
	s.Append(DisplayPart{
		Type: PartTypeTool,
		Time: time.Now(),
		Tool: &ToolPart{
			Name:   "rg",
			CallID: "peer-1",
			State:  "running",
			StepID: "peer-a",
		},
	})
	s.Append(DisplayPart{
		Type: PartTypeTool,
		Time: time.Now(),
		Tool: &ToolPart{
			Name:   "read_file",
			CallID: "peer-2",
			State:  "running",
			StepID: "peer-a",
		},
	})

	// 模拟另一个 peer 桶 tool（StepID = "peer-b"）
	s.Append(DisplayPart{
		Type: PartTypeTool,
		Time: time.Now(),
		Tool: &ToolPart{
			Name:   "ls",
			CallID: "peer-3",
			State:  "running",
			StepID: "peer-b",
		},
	})

	// peer-a 应有 2 个 part
	gotA := s.PartsForStep("peer-a")
	if len(gotA) != 2 {
		t.Fatalf("peer-a 应有 2 个 tool parts, got %d (%v)", len(gotA), gotA)
	}

	// peer-b 应有 1 个 part
	gotB := s.PartsForStep("peer-b")
	if len(gotB) != 1 {
		t.Fatalf("peer-b 应有 1 个 tool part, got %d (%v)", len(gotB), gotB)
	}

	// 主路径（StepID 空）不进 idxByStepID
	gotMain := s.PartsForStep("")
	if gotMain != nil {
		t.Fatalf("空 stepID 应返回 nil (主路径 tool 不进索引), got %v", gotMain)
	}

	// 未注册 stepID 返回 nil
	if got := s.PartsForStep("nonexistent"); got != nil {
		t.Fatalf("未注册 stepID 应返回 nil, got %v", got)
	}
}

// TestParts_PeerThinkingGroupedByStepID（fix/06 P1-7 补完红线）：
// ThinkingPart.StepID 在 fix/06 真激活——peer 桶 think 块通过 partStepID 进 idxByStepID。
func TestParts_PeerThinkingGroupedByStepID(t *testing.T) {
	s := NewPartsStore()

	// peer 桶 thinking
	s.Append(DisplayPart{
		Type: PartTypeThinking,
		Time: time.Now(),
		Thinking: &ThinkingPart{
			Content:   "peer think 1",
			GroupID:   "g1",
			AgentName: "root",
			StepID:    "peer-a",
		},
	})

	// 同 peer 的 tool
	s.Append(DisplayPart{
		Type: PartTypeTool,
		Time: time.Now(),
		Tool: &ToolPart{
			Name:   "bash",
			CallID: "ca1",
			State:  "running",
			StepID: "peer-a",
		},
	})

	got := s.PartsForStep("peer-a")
	if len(got) != 2 {
		t.Fatalf("peer-a 应有 think + tool 2 个 parts, got %d", len(got))
	}
}
