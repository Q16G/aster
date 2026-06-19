package tui

import (
	"testing"
	"time"
)

func TestPartsStore_SubAgentsThisTurn_FiltersRemoteStep(t *testing.T) {
	s := NewPartsStore()
	// 加 1 个 sub_agent（Kind="" 默认）+ 1 个 remote_step + 1 个 sub_agent（显式 Kind）
	s.Append(DisplayPart{
		Type: PartTypeSubAgent,
		Time: time.Now(),
		SubAgent: &SubAgentPart{
			AgentName: "sub-1",
			CallID:    "sub-1",
			Status:    "running",
		},
	})
	s.Append(DisplayPart{
		Type: PartTypeSubAgent,
		Time: time.Now(),
		SubAgent: &SubAgentPart{
			AgentName: "step-b",
			CallID:    "step-b",
			Kind:      subAgentPartKindRemoteStep,
			Status:    "running",
		},
	})
	s.Append(DisplayPart{
		Type: PartTypeSubAgent,
		Time: time.Now(),
		SubAgent: &SubAgentPart{
			AgentName: "sub-2",
			CallID:    "sub-2",
			Kind:      subAgentPartKindSubAgent,
			Status:    "completed",
		},
	})

	got := s.SubAgentsThisTurn()
	if len(got) != 2 {
		t.Fatalf("expected 2 sub_agent entries (remote_step filtered), got %d: %+v", len(got), got)
	}
	for _, sa := range got {
		if sa.Kind == subAgentPartKindRemoteStep {
			t.Fatalf("remote_step leaked into panel: %+v", sa)
		}
		if sa.CallID == "step-b" {
			t.Fatalf("remote_step step-b should be filtered, got it in panel")
		}
	}
}
