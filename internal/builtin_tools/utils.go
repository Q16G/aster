package builtin_tools

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"aster/internal/utils/argx"
)

var nonPlanIDCharRE = regexp.MustCompile(`[^a-z0-9_-]+`)

func CloneAnyMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func ClonePlanItems(items []*PlanItem) []*PlanItem {
	if len(items) == 0 {
		return nil
	}
	out := make([]*PlanItem, 0, len(items))
	for _, it := range items {
		if it == nil {
			continue
		}
		id := strings.TrimSpace(it.ID)
		step := strings.TrimSpace(it.Step)
		status := PlanStepStatus(strings.TrimSpace(string(it.Status)))
		out = append(out, &PlanItem{
			ID:        id,
			Step:      step,
			Status:    status,
			DependsOn: CloneStringSlice(it.DependsOn),
		})
	}
	if len(out) == 0 {
		return nil
	}
	HydratePlanRelations(out)
	return out
}

func CloneStringSlice(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	for _, item := range in {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		out = append(out, item)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func CloneReplanContext(in *ReplanContext) *ReplanContext {
	if in == nil {
		return nil
	}
	out := *in
	out.SourceStepID = strings.TrimSpace(in.SourceStepID)
	out.Reason = strings.TrimSpace(in.Reason)
	out.NextGoal = strings.TrimSpace(in.NextGoal)
	out.MissingItems = CloneStringSlice(in.MissingItems)
	out.Warnings = CloneStringSlice(in.Warnings)
	return &out
}

func CloneExternalInterrupt(in *ExternalInterrupt) *ExternalInterrupt {
	if in == nil {
		return nil
	}
	out := *in
	out.ReasonCode = strings.TrimSpace(in.ReasonCode)
	out.Error = strings.TrimSpace(in.Error)
	out.UserMessage = strings.TrimSpace(in.UserMessage)
	out.SuggestedActions = CloneStringSlice(in.SuggestedActions)
	return &out
}

func NormalizePlanItems(items []*PlanItem, requireStatus bool) ([]*PlanItem, error) {
	return normalizePlanItems(items, requireStatus)
}

func HydratePlanRelations(plan []*PlanItem) {
	if len(plan) == 0 {
		return
	}

	itemByID := make(map[string]*PlanItem, len(plan))
	for _, item := range plan {
		if item == nil {
			continue
		}
		itemByID[strings.TrimSpace(item.ID)] = item
	}

	for _, item := range plan {
		if item == nil {
			continue
		}
		dependencyIDs := item.DependencyIDs()
		if len(dependencyIDs) == 0 {
			item.ResolvedDependsOn = nil
			continue
		}
		resolved := make([]*PlanItem, 0, len(dependencyIDs))
		for _, depID := range dependencyIDs {
			resolved = append(resolved, itemByID[depID])
		}
		item.ResolvedDependsOn = resolved
	}
}

func normalizePlanItems(items []*PlanItem, requireStatus bool) ([]*PlanItem, error) {
	if len(items) == 0 {
		return nil, nil
	}

	out := make([]*PlanItem, 0, len(items))
	usedIDs := make(map[string]int, len(items))
	stepToID := make(map[string]string, len(items))
	inProgress := 0

	for idx, item := range items {
		if item == nil {
			continue
		}

		step := strings.TrimSpace(item.Step)
		if step == "" {
			return nil, fmt.Errorf("plan.step is required")
		}

		status := PlanStepStatus(strings.TrimSpace(string(item.Status)))
		if requireStatus {
			switch status {
			case PlanStepPending, PlanStepInProgress, PlanStepCompleted, PlanStepFailed, PlanStepSkipped:
			default:
				return nil, fmt.Errorf("invalid plan.status: %s", status)
			}
			if status == PlanStepInProgress {
				inProgress++
			}
		} else if status != "" {
			switch status {
			case PlanStepPending, PlanStepInProgress, PlanStepCompleted, PlanStepFailed, PlanStepSkipped:
			default:
				return nil, fmt.Errorf("invalid plan.status: %s", status)
			}
		}

		id := canonicalPlanItemID(item.ID, idx, usedIDs)
		norm := &PlanItem{
			ID:                id,
			Step:              step,
			Status:            status,
			DependsOn:         CloneStringSlice(item.DependsOn),
			OutputContractRef: strings.TrimSpace(item.OutputContractRef),
		}
		out = append(out, norm)
		if _, exists := stepToID[step]; !exists {
			stepToID[step] = id
		}
	}

	if requireStatus && inProgress > 1 {
		return nil, fmt.Errorf("plan must have at most one in_progress")
	}

	idSet := make(map[string]struct{}, len(out))
	for _, item := range out {
		if item == nil {
			continue
		}
		idSet[item.ID] = struct{}{}
	}

	for _, item := range out {
		if item == nil {
			continue
		}
		deps := make([]string, 0, len(item.DependsOn))
		seen := make(map[string]struct{}, len(item.DependsOn))
		for _, dep := range item.DependsOn {
			dep = strings.TrimSpace(dep)
			if dep == "" {
				continue
			}

			canonical := dep
			if _, ok := idSet[canonical]; !ok {
				if resolved, ok := stepToID[dep]; ok {
					canonical = resolved
				} else {
					return nil, fmt.Errorf("unknown dependency %q for plan item %q", dep, item.Step)
				}
			}
			if canonical == item.ID {
				continue
			}
			if _, exists := seen[canonical]; exists {
				continue
			}
			seen[canonical] = struct{}{}
			deps = append(deps, canonical)
		}
		item.DependsOn = deps
	}

	if err := validatePlanDependencyGraph(out); err != nil {
		return nil, err
	}
	HydratePlanRelations(out)

	return out, nil
}

func canonicalPlanItemID(raw string, index int, used map[string]int) string {
	candidate := strings.ToLower(strings.TrimSpace(raw))
	candidate = strings.ReplaceAll(candidate, " ", "-")
	candidate = nonPlanIDCharRE.ReplaceAllString(candidate, "-")
	candidate = strings.Trim(candidate, "-_")
	if candidate == "" {
		candidate = fmt.Sprintf("task-%d", index+1)
	}
	if count := used[candidate]; count > 0 {
		used[candidate] = count + 1
		return fmt.Sprintf("%s-%d", candidate, count+1)
	}
	used[candidate] = 1
	return candidate
}

func validatePlanDependencyGraph(items []*PlanItem) error {
	if len(items) == 0 {
		return nil
	}

	itemByID := make(map[string]*PlanItem, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		itemByID[item.ID] = item
	}

	visiting := make(map[string]bool, len(items))
	visited := make(map[string]bool, len(items))
	var visit func(string) error
	visit = func(id string) error {
		if visited[id] {
			return nil
		}
		if visiting[id] {
			return fmt.Errorf("plan dependencies contain cycle at %s", id)
		}
		visiting[id] = true
		item := itemByID[id]
		if item != nil {
			for _, dep := range item.DependsOn {
				if _, ok := itemByID[dep]; !ok {
					return fmt.Errorf("unknown dependency %q for plan item %q", dep, item.Step)
				}
				if err := visit(dep); err != nil {
					return err
				}
			}
		}
		visiting[id] = false
		visited[id] = true
		return nil
	}

	for _, item := range items {
		if item == nil {
			continue
		}
		if err := visit(item.ID); err != nil {
			return err
		}
	}

	return nil
}

func planReadyDependencies(plan []*PlanItem) map[string]bool {
	completed := make(map[string]bool, len(plan))
	for _, item := range plan {
		if item == nil {
			continue
		}
		if item.Status == PlanStepCompleted && strings.TrimSpace(item.ID) != "" {
			completed[strings.TrimSpace(item.ID)] = true
		}
	}
	return completed
}

func planItemDependenciesSatisfied(item *PlanItem, completed map[string]bool) bool {
	if item == nil {
		return false
	}
	dependencyItems := item.DependencyItems()
	for _, dep := range dependencyItems {
		if !completed[strings.TrimSpace(dep.ID)] {
			return false
		}
	}
	if dependencyItems != nil {
		return true
	}
	for _, dep := range item.DependencyIDs() {
		dep = strings.TrimSpace(dep)
		if dep == "" {
			continue
		}
		if !completed[dep] {
			return false
		}
	}
	return true
}

func findFirstReadyPendingPlanIndex(plan []*PlanItem) int {
	completed := planReadyDependencies(plan)
	for idx, item := range plan {
		if item == nil {
			continue
		}
		if item.Status != PlanStepPending {
			continue
		}
		if planItemDependenciesSatisfied(item, completed) {
			return idx
		}
	}
	return -1
}

func currentPlanStep(plan []*PlanItem, currentStepID string) *PlanItem {
	if item := planItemByID(plan, currentStepID); item != nil {
		return item
	}
	for _, item := range plan {
		if item == nil {
			continue
		}
		if item.Status == PlanStepInProgress {
			return item
		}
	}
	if nextID := NextRunnablePlanStepID(plan); nextID != "" {
		return planItemByID(plan, nextID)
	}
	return nil
}

func NextRunnablePlanStepID(plan []*PlanItem) string {
	if idx := findFirstReadyPendingPlanIndex(plan); idx >= 0 && idx < len(plan) && plan[idx] != nil {
		return strings.TrimSpace(plan[idx].ID)
	}
	return ""
}

func AllPlanStepsTerminal(plan []*PlanItem) bool {
	for _, item := range plan {
		if item == nil {
			continue
		}
		switch item.Status {
		case PlanStepCompleted, PlanStepFailed, PlanStepSkipped:
		default:
			return false
		}
	}
	return true
}

func AllPlanStepsCompleted(plan []*PlanItem) bool {
	for _, item := range plan {
		if item == nil {
			continue
		}
		if item.Status != PlanStepCompleted {
			return false
		}
	}
	return true
}

func PlanProgress(plan []*PlanItem) int {
	total := 0
	completed := 0
	for _, item := range plan {
		if item == nil {
			continue
		}
		total++
		if item.Status == PlanStepCompleted {
			completed++
		}
	}
	if total == 0 {
		return 0
	}
	return completed * 100 / total
}

// PropagateSkippedPlanSteps marks blocked pending steps as skipped.
//
// A plan item becomes "skipped" when any of its dependencies is failed or skipped.
// The propagation walks the dependency graph transitively so downstream nodes are
// also skipped. It never changes completed/failed steps, and avoids touching
// in_progress steps (which should already be dependency-satisfied).
func PropagateSkippedPlanSteps(plan []*PlanItem) (changed bool) {
	if len(plan) == 0 {
		return false
	}

	blocked := make(map[string]struct{}, len(plan))
	for _, item := range plan {
		if item == nil {
			continue
		}
		id := strings.TrimSpace(item.ID)
		if id == "" {
			continue
		}
		switch item.Status {
		case PlanStepFailed, PlanStepSkipped:
			blocked[id] = struct{}{}
		}
	}
	if len(blocked) == 0 {
		return false
	}

	for {
		progressed := false
		for _, item := range plan {
			if item == nil {
				continue
			}
			if item.Status != PlanStepPending {
				continue
			}
			if len(item.DependsOn) == 0 {
				continue
			}
			for _, dep := range item.DependsOn {
				dep = strings.TrimSpace(dep)
				if dep == "" {
					continue
				}
				if _, ok := blocked[dep]; !ok {
					continue
				}

				item.Status = PlanStepSkipped
				if id := strings.TrimSpace(item.ID); id != "" {
					blocked[id] = struct{}{}
				}
				changed = true
				progressed = true
				break
			}
		}
		if !progressed {
			break
		}
	}
	return changed
}

func planItemByID(plan []*PlanItem, id string) *PlanItem {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	for _, item := range plan {
		if item == nil {
			continue
		}
		if strings.TrimSpace(item.ID) == id {
			return item
		}
	}
	return nil
}

func normalizeToolTextOrJSON(v any) (string, error) {
	if argx.IsTypedNil(v) {
		return "", nil
	}
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t), nil
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return "", err
		}
		s := strings.TrimSpace(string(b))
		if s == "null" {
			return "", nil
		}
		return s, nil
	}
}

func ParsePlanItems(raw any) ([]*PlanItem, error) {
	list, ok := raw.([]any)
	if !ok {
		if raw == nil {
			return nil, fmt.Errorf("plan is required")
		}
		return nil, fmt.Errorf("plan must be array")
	}

	items := make([]*PlanItem, 0, len(list))
	for _, entry := range list {
		m, ok := entry.(map[string]any)
		if !ok {
			if cast, ok := entry.(map[string]interface{}); ok {
				m = make(map[string]any, len(cast))
				for k, v := range cast {
					m[k] = v
				}
			} else {
				return nil, fmt.Errorf("plan item must be object")
			}
		}

		step := ToolRuntimeValue(m["step"])
		status := PlanStepStatus(ToolRuntimeValue(m["status"]))
		id := ToolRuntimeValue(m["id"])
		dependsOn, err := parseStringArray(m["depends_on"])
		if err != nil {
			return nil, fmt.Errorf("plan.depends_on must be array of strings: %w", err)
		}

		outputContractRef := ToolRuntimeValue(m["output_contract_ref"])

		items = append(items, &PlanItem{
			ID:                id,
			Step:              step,
			Status:            status,
			DependsOn:         dependsOn,
			OutputContractRef: outputContractRef,
		})
	}

	return normalizePlanItems(items, true)
}

func parseStringArray(raw any) ([]string, error) {
	if raw == nil {
		return nil, nil
	}
	list, ok := raw.([]any)
	if !ok {
		if arr, ok := raw.([]string); ok {
			return CloneStringSlice(arr), nil
		}
		return nil, fmt.Errorf("not an array")
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		value := ToolRuntimeValue(item)
		if value == "" {
			continue
		}
		out = append(out, value)
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

func asInt64Any(v any) (int64, bool) {
	switch n := v.(type) {
	case int:
		return int64(n), true
	case int32:
		return int64(n), true
	case int64:
		return n, true
	case float32:
		return int64(n), true
	case float64:
		return int64(n), true
	case string:
		s := argx.Text(n)
		if s == "" {
			return 0, false
		}
		parsed, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return 0, false
		}
		return parsed, true
	default:
		s := argx.Text(v)
		if s == "" {
			return 0, false
		}
		parsed, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return 0, false
		}
		return parsed, true
	}
}
