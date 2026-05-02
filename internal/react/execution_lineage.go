package react

import (
	"fmt"
	"strings"
	"time"

	"aster/internal/builtin_tools"
)

type executionContextCard struct {
	ContextKey      string   `json:"context_key"`
	Namespace       string   `json:"namespace,omitempty"`
	StepID          string   `json:"step_id"`
	ShortSummary    string   `json:"short_summary,omitempty"`
	KeyFacts        []string `json:"key_facts,omitempty"`
	ToolCallsDigest []string `json:"tool_calls_digest,omitempty"`
	SummaryFile     string   `json:"summary_file,omitempty"`
	ResultFile      string   `json:"result_file,omitempty"`
	ResultKeys      []string `json:"result_keys,omitempty"`
	References      []string `json:"references,omitempty"`
}

type frozenStepLineage struct {
	PlanVersion          int
	StepID               string
	Namespace            string
	InheritedContextKeys []string
	InheritedRefIDs      []string
	Cards                []executionContextCard
	FrozenAt             time.Time
}

func frozenLineageKey(planVersion int, stepID string) string {
	stepID = strings.TrimSpace(stepID)
	if planVersion <= 0 {
		planVersion = 1
	}
	return fmt.Sprintf("%d:%s", planVersion, stepID)
}

func (a *Agent) ensureFrozenStepLineage(snapshot builtin_tools.StateSnapshot) (*frozenStepLineage, error) {
	if a == nil {
		return nil, nil
	}
	current := snapshot.CurrentStep()
	if current == nil || strings.TrimSpace(current.ID) == "" {
		return nil, nil
	}
	stepID := strings.TrimSpace(current.ID)
	planVersion := snapshot.PlanVersion
	key := frozenLineageKey(planVersion, stepID)
	if a.frozenLineageByStep == nil {
		a.frozenLineageByStep = make(map[string]*frozenStepLineage)
	} else if cached := a.frozenLineageByStep[key]; cached != nil {
		return cached, nil
	}

	workspaceRoot := strings.TrimSpace(a.workspaceRootDir)
	if workspaceRoot == "" {
		// No workspace session root => no lineage to load.
		lineage := &frozenStepLineage{
			PlanVersion: planVersion,
			StepID:      stepID,
			Namespace:   builtin_tools.NormalizeWorkspaceNamespace(a.workspaceNamespace),
			FrozenAt:    time.Now(),
		}
		a.frozenLineageByStep[key] = lineage
		return lineage, nil
	}

	records, err := builtin_tools.LoadWorkspaceStepContextRecords(workspaceRoot, 800)
	if err != nil {
		return nil, err
	}

	namespace := builtin_tools.NormalizeWorkspaceNamespace(a.workspaceNamespace)
	contextKeys := selectDirectInheritedContextKeys(records, namespace, current, a)
	cardByKey := make(map[string]executionContextCard, len(contextKeys))
	refIDs := make([]string, 0, 8)

	recordByKey := make(map[string]*builtin_tools.StepContextRecord, len(records))
	for _, rec := range records {
		if rec == nil {
			continue
		}
		ck := strings.TrimSpace(rec.ContextKey)
		if ck == "" {
			continue
		}
		recordByKey[ck] = rec
	}

	cards := make([]executionContextCard, 0, len(contextKeys))
	for _, ck := range contextKeys {
		ck = strings.TrimSpace(ck)
		if ck == "" {
			continue
		}
		if _, exists := cardByKey[ck]; exists {
			continue
		}
		rec := recordByKey[ck]
		if rec == nil {
			continue
		}
		card := executionContextCard{
			ContextKey:      strings.TrimSpace(rec.ContextKey),
			Namespace:       strings.TrimSpace(rec.Namespace),
			StepID:          strings.TrimSpace(rec.StepID),
			ShortSummary:    strings.TrimSpace(rec.ShortSummary),
			KeyFacts:        builtin_tools.CloneStringSlice(rec.KeyFacts),
			ToolCallsDigest: builtin_tools.CloneStringSlice(rec.ToolCallsDigest),
			SummaryFile:     strings.TrimSpace(rec.SummaryFile),
			ResultFile:      strings.TrimSpace(rec.ResultFile),
			ResultKeys:      builtin_tools.CloneStringSlice(rec.ResultKeys),
			References:      builtin_tools.CloneStringSlice(rec.References),
		}
		if len(card.KeyFacts) == 0 {
			card.KeyFacts = nil
		}
		if len(card.ToolCallsDigest) == 0 {
			card.ToolCallsDigest = nil
		}
		if len(card.ResultKeys) == 0 {
			card.ResultKeys = nil
		}
		if len(card.References) == 0 {
			card.References = nil
		}
		cards = append(cards, card)
		cardByKey[ck] = card
		refIDs = mergeUniqueStrings(refIDs, card.References)
	}

	lineage := &frozenStepLineage{
		PlanVersion:          planVersion,
		StepID:               stepID,
		Namespace:            namespace,
		InheritedContextKeys: contextKeys,
		InheritedRefIDs:      refIDs,
		Cards:                cards,
		FrozenAt:             time.Now(),
	}
	a.frozenLineageByStep[key] = lineage
	return lineage, nil
}

func (a *Agent) consumeFrozenStepLineage(planVersion int, stepID string) *frozenStepLineage {
	if a == nil || a.frozenLineageByStep == nil {
		return nil
	}
	key := frozenLineageKey(planVersion, stepID)
	lineage := a.frozenLineageByStep[key]
	delete(a.frozenLineageByStep, key)
	return lineage
}

func (a *Agent) executionContextsForPrompt(snapshot builtin_tools.StateSnapshot) []executionContextCard {
	if a == nil {
		return nil
	}
	current := snapshot.CurrentStep()
	if current == nil || strings.TrimSpace(current.ID) == "" {
		return nil
	}
	key := frozenLineageKey(snapshot.PlanVersion, strings.TrimSpace(current.ID))
	if a.frozenLineageByStep == nil {
		return nil
	}
	lineage := a.frozenLineageByStep[key]
	if lineage == nil || len(lineage.Cards) == 0 {
		return nil
	}
	return lineage.Cards
}

func selectDirectInheritedContextKeys(records []*builtin_tools.StepContextRecord, namespace string, currentStep *builtin_tools.PlanItem, agent *Agent) []string {
	namespace = builtin_tools.NormalizeWorkspaceNamespace(namespace)
	if currentStep == nil {
		return nil
	}

	out := make([]string, 0, 4)
	add := func(ck string) {
		ck = strings.TrimSpace(ck)
		if ck == "" {
			return
		}
		for _, existing := range out {
			if existing == ck {
				return
			}
		}
		out = append(out, ck)
	}

	// 1) Prefer the latest completed context in the same namespace (runtime progressive chain).
	for i := len(records) - 1; i >= 0; i-- {
		rec := records[i]
		if rec == nil {
			continue
		}
		if builtin_tools.NormalizeWorkspaceNamespace(rec.Namespace) != namespace {
			continue
		}
		add(rec.ContextKey)
		break
	}

	// 2) Optionally include direct depends_on step contexts (still "direct" if injected into prompt).
	for _, depID := range currentStep.DependencyIDs() {
		for i := len(records) - 1; i >= 0; i-- {
			rec := records[i]
			if rec == nil {
				continue
			}
			if builtin_tools.NormalizeWorkspaceNamespace(rec.Namespace) != namespace {
				continue
			}
			if strings.TrimSpace(rec.StepID) != strings.TrimSpace(depID) {
				continue
			}
			add(rec.ContextKey)
			break
		}
	}

	// 3) If this is a child agent and there is no local context yet, inherit the parent step context.
	if len(out) == 0 && strings.HasPrefix(namespace, "agents/") && agent != nil {
		parentContextKey := agent.parentContextKeyForChildNamespace(namespace)
		add(parentContextKey)
	}

	if len(out) == 0 {
		return nil
	}
	return out
}

func (a *Agent) parentContextKeyForChildNamespace(namespace string) string {
	if a == nil {
		return ""
	}
	namespace = builtin_tools.NormalizeWorkspaceNamespace(namespace)
	if !strings.HasPrefix(namespace, "agents/") {
		return ""
	}
	workspaceRoot := strings.TrimSpace(a.workspaceRootDir)
	if workspaceRoot == "" {
		return ""
	}

	rootRuntime, err := newLocalWorkspaceRuntime(a.workspaceSessionID, workspaceRoot, "root")
	if err != nil {
		return ""
	}
	rootWriter, err := newArtifactWriter(rootRuntime)
	if err != nil {
		return ""
	}
	rootState, err := rootWriter.LoadWorkspaceState()
	if err != nil || rootState == nil || len(rootState.ChildAgents) == 0 {
		return ""
	}

	childKey := strings.TrimPrefix(namespace, "agents/")
	childKey = strings.Trim(childKey, "/")
	if childKey == "" {
		childKey = "unknown"
	}
	childPtr := rootState.ChildAgents[childKey]
	if childPtr == nil {
		return ""
	}
	parentStepKey := strings.TrimSpace(childPtr.ParentStepKey)
	if parentStepKey == "" || rootState.LatestStepOutcomes == nil {
		return ""
	}
	parentOutcome := rootState.LatestStepOutcomes[parentStepKey]
	if parentOutcome == nil {
		return latestRootContextKey(workspaceRoot)
	}
	if ck := strings.TrimSpace(parentOutcome.ContextKey); ck != "" {
		return ck
	}
	// Parent step may still be in-progress (no context_key yet). Fallback to the latest
	// completed root context so child agents still get a usable coarse result.
	return latestRootContextKey(workspaceRoot)
}

func latestRootContextKey(workspaceRoot string) string {
	records, err := builtin_tools.LoadWorkspaceStepContextRecords(workspaceRoot, 800)
	if err != nil || len(records) == 0 {
		return ""
	}
	for i := len(records) - 1; i >= 0; i-- {
		rec := records[i]
		if rec == nil {
			continue
		}
		if builtin_tools.NormalizeWorkspaceNamespace(rec.Namespace) != "root" {
			continue
		}
		return strings.TrimSpace(rec.ContextKey)
	}
	return ""
}
