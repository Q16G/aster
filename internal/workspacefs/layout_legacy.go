package workspacefs

import "fmt"

// 旧路径读回退：namespace 归一（顶层 = artifacts/）之前，顶层 runtime 的
// Namespace()=="root"，checkpoint 实际写在 artifacts/root/… 子树。
// 存量 session resume 时，新路径缺失需回退读这些字面路径；final 序号推导
// 需取新旧两目录的 max。
//
// 仅顶层（Namespace==""）返回非空：子 agent namespace 的新旧两套规则本就一致，
// 不存在旧子树。全部为只读回退用途，禁止作为写入路径。

func (l Layout) LegacyPlanCurrentRel() string {
	if l.Namespace != "" {
		return ""
	}
	return "artifacts/root/plan/current.json"
}
func (l Layout) LegacyPlanCurrent() string { return l.abs(l.LegacyPlanCurrentRel()) }

func (l Layout) LegacyPlanHistoryRel(planVersion int) string {
	if l.Namespace != "" {
		return ""
	}
	return fmt.Sprintf("artifacts/root/plan/history/%d.json", planVersion)
}
func (l Layout) LegacyPlanHistory(planVersion int) string {
	return l.abs(l.LegacyPlanHistoryRel(planVersion))
}

func (l Layout) LegacyFinalRootRel() string {
	if l.Namespace != "" {
		return ""
	}
	return "artifacts/root/final"
}
func (l Layout) LegacyFinalRoot() string { return l.abs(l.LegacyFinalRootRel()) }

func (l Layout) LegacyFinalDirRel(finalSeq int) string {
	if l.Namespace != "" {
		return ""
	}
	return fmt.Sprintf("artifacts/root/final/%d", finalSeq)
}
func (l Layout) LegacyFinalDir(finalSeq int) string { return l.abs(l.LegacyFinalDirRel(finalSeq)) }
