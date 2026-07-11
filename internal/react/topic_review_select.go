package react

import (
	"strings"

	"aster/internal/builtin_tools"
)

// nextReviewableTopic 选下一个「已静默且自其边界后有新 terminal step」的 active topic 做局部 review。
// 返回该 topic 的 id 与其最新 terminal step id（review 成功后写回 lastReplanBoundaryByTopic，
// 标记该静默点已复核，防重复 review）。无可 review 的 topic 时返回空串。
//
// 判据：T ∈ QuiescedActiveTopics（active + 已解锁 + 全 step terminal + ≥1 step）；且
// boundaryByTopic[T] != T 的最新 step id（否则该静默点已 review 过，跳过）。
// 用于第四阶段 currentPhase 双触发路由的「局部 review」分支（Inc3/Inc4 接线）。
func nextReviewableTopic(plan []*builtin_tools.PlanItem, phases []*builtin_tools.AnalysisTopic, boundaryByTopic map[string]string) (topicID, latestStepID string) {
	for _, ph := range builtin_tools.QuiescedActiveTopics(plan, phases) {
		if ph == nil {
			continue
		}
		latest := latestStepIDOfTopic(plan, ph.ID)
		if latest == "" {
			continue // 0-step 守卫已在 QuiescedActiveTopics 保证，此处兜底
		}
		if strings.TrimSpace(boundaryByTopic[ph.ID]) == latest {
			continue // 该静默点已 review 过
		}
		return ph.ID, latest
	}
	return "", ""
}

// scopeActiveTopicsToTopic 把 active phase 集收窄为仅目标 topic（局部 review 只判该 topic 的
// 视角 A+C）。目标不在 active 集时返回 nil（退化为 reducer 态，视角 B 全半径仍生效）。
func scopeActiveTopicsToTopic(active []*builtin_tools.AnalysisTopic, topicID string) []*builtin_tools.AnalysisTopic {
	topicID = strings.TrimSpace(topicID)
	for _, ph := range active {
		if ph != nil && strings.TrimSpace(ph.ID) == topicID {
			return []*builtin_tools.AnalysisTopic{ph}
		}
	}
	return nil
}

// latestStepIDOfTopic 返回 plan 顺序中属于该 topic 的最后一个 step 的 id（topic 已静默时即其
// review 边界的右端）。无该 topic 的 step 时返回空。
func latestStepIDOfTopic(plan []*builtin_tools.PlanItem, topicID string) string {
	topicID = strings.TrimSpace(topicID)
	latest := ""
	for _, it := range plan {
		if it != nil && strings.TrimSpace(it.TopicID) == topicID {
			latest = strings.TrimSpace(it.ID)
		}
	}
	return latest
}
