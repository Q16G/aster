package react

import (
	"context"
	"strings"
	"testing"

	"aster/internal/ai"
)

// spyStepHistoryCompactor 记录 Compact 是否被调用，返回可继续的压缩结果。
type spyStepHistoryCompactor struct{ calls int }

func (s *spyStepHistoryCompactor) Compact(_ context.Context, _ ai.ChatClient, _ string, _ string, stepHistory []*ai.MsgInfo) (*StepHistoryCompactionResult, error) {
	s.calls++
	keep := stepHistory
	if len(keep) > 2 {
		keep = keep[len(keep)-2:]
	}
	return &StepHistoryCompactionResult{StepHistory: keep, DidCompact: true, CanContinue: true}, nil
}

// TestLayerB_CompactionReachableInFinalAnswerPhase（T6-E）验证 Layer B 历史压缩对非 Step
// 阶段（final_answer，promptFamily 非 think_act）无门控可达：stepHistory 超触发阈值时
// AICallProxy 调用 StepHistoryCompactor.Compact；且无 step 归属（stepHistoryStepID 空）时
// 不落 step transcript blob（T5 收口）。
func TestLayerB_CompactionReachableInFinalAnswerPhase(t *testing.T) {
	spy := &spyStepHistoryCompactor{}
	client := &stepHistoryCompactionTestClient{inputLimit: 300, outputLimit: 60}
	agent, err := NewReActAgent("layerb", client, WithEmitter(NewDummyEmitter()))
	if err != nil {
		t.Fatalf("NewReActAgent: %v", err)
	}
	agent.cfg.StepHistoryCompactor = spy
	agent.cfg.StepHistoryCompressTriggerRatio = 0.90

	// 非 Step 阶段：无 step 归属。
	agent.stepHistoryStepID = ""
	long := strings.Repeat("很长的工具输出内容行，需要触发历史压缩。", 40)
	for i := 0; i < 30; i++ {
		agent.stepHistory = append(agent.stepHistory, makeToolRound("c"+strings.Repeat("x", i+1), long)...)
	}

	parts := PromptParts{SystemRules: "rules", User: "final answer input"}
	if _, err := agent.AICallProxy(context.Background(), nil, 0, client, parts, promptFamilyFinalAnswer); err != nil {
		t.Fatalf("AICallProxy: %v", err)
	}

	if spy.calls == 0 {
		t.Fatalf("Layer B 应在 final_answer 阶段触发压缩（无阶段门控），got calls=0")
	}
	if agent.lastStepTranscriptBlobRef != "" {
		t.Fatalf("无 step 归属的转录不应落 step blob，got ref=%q", agent.lastStepTranscriptBlobRef)
	}
}
