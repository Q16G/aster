package react

import (
	"strings"
	"testing"
)

// TestRenderPassthrough_FivePhases（T6-D）验证：各阶段经 PromptContext 投影的注入字段内容
// 原样穿透进渲染输出，且同输入两次构建逐字节相等（渲染确定性）。这是「数据底座切到
// PreviewField 不改 prompt 语义」的渲染侧证据（字段级 .Text 值等价见 prompt_context 测试）。
func TestRenderPassthrough_FivePhases(t *testing.T) {
	manager, err := newDefaultPromptManager()
	if err != nil {
		t.Fatalf("newDefaultPromptManager: %v", err)
	}
	const gu = "SENTINEL_GOAL_核心目标_XYZ"
	const tl = "SENTINEL_TIMELINE_输入_XYZ"

	cases := []struct {
		name   string
		build  func() (PromptParts, error)
		wantIn []string
	}{
		{
			name: "planner",
			build: func() (PromptParts, error) {
				return manager.BuildTaskPlannerPrompt(TaskPlannerPromptInput{
					Input:             tl,
					GoalUnderstanding: gu,
					TaskContextBoard:  "SENTINEL_BOARD_planner",
				})
			},
			wantIn: []string{gu, tl, "SENTINEL_BOARD_planner"},
		},
		{
			name: "step_replan",
			build: func() (PromptParts, error) {
				return manager.BuildStepReplanPrompt(StepReplanPromptInput{
					GoalUnderstanding: gu,
					InputTimeline:     tl,
					OpenItemsLedger:   "SENTINEL_LEDGER_replan",
					PlannerJournal:    "SENTINEL_JOURNAL_replan",
					PlanOverview:      "SENTINEL_PLAN_replan",
				})
			},
			wantIn: []string{gu, tl, "SENTINEL_LEDGER_replan", "SENTINEL_JOURNAL_replan", "SENTINEL_PLAN_replan"},
		},
		{
			name: "final_answer",
			build: func() (PromptParts, error) {
				return manager.BuildFinalAnswerPrompt(FinalAnswerPromptInput{
					GoalUnderstanding: gu,
					InputTimeline:     tl,
					OpenItemsLedger:   "SENTINEL_LEDGER_final",
					PlanItems:         "SENTINEL_PLAN_final",
				})
			},
			wantIn: []string{gu, tl, "SENTINEL_LEDGER_final", "SENTINEL_PLAN_final"},
		},
		{
			name: "think_act",
			build: func() (PromptParts, error) {
				return manager.BuildThinkActPrompt(ThinkActPromptInput{
					GoalUnderstanding: gu,
					CurrentStep:       map[string]any{"id": "s1", "step": "SENTINEL_STEP_think"},
					HasCurrentStep:    true,
				})
			},
			wantIn: []string{gu, "SENTINEL_STEP_think"},
		},
		{
			name: "intent_classification",
			build: func() (PromptParts, error) {
				return manager.BuildIntentClassificationPrompt(IntentClassificationPromptInput{
					GoalUnderstanding: gu,
					InputTimeline:     tl,
					Status:            "running",
				})
			},
			wantIn: []string{gu, tl},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p1, err := tc.build()
			if err != nil {
				t.Fatalf("build %s: %v", tc.name, err)
			}
			joined := p1.SystemJoined() + "\n" + p1.User
			for _, want := range tc.wantIn {
				if !strings.Contains(joined, want) {
					t.Errorf("%s 渲染输出应含注入内容 %q（穿透失败）", tc.name, want)
				}
			}
			p2, err := tc.build()
			if err != nil {
				t.Fatalf("rebuild %s: %v", tc.name, err)
			}
			if p1.SystemJoined() != p2.SystemJoined() || p1.User != p2.User {
				t.Errorf("%s 同输入两次渲染应逐字节相等（确定性）", tc.name)
			}
		})
	}
}
