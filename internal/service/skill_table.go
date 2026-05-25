package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

type SkillIndexRow struct {
	Name        string
	Description string
	WhenToUse   string
	Context     string
	Status      string
	SkillDir    string
}

func (s *SkillService) BuildSkillsTableWithStatus(ctx context.Context, agentName string, allowedSkillNames []string, activeSkillNames []string) (string, error) {
	if s == nil {
		return "", fmt.Errorf("skill service is nil")
	}
	agentName = strings.TrimSpace(agentName)
	if agentName == "" {
		return "", fmt.Errorf("agent name is required")
	}
	allowedSet := toStringSet(allowedSkillNames)
	activeSet := make(map[string]struct{}, len(activeSkillNames))
	for _, raw := range activeSkillNames {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		activeSet[name] = struct{}{}
	}

	enabled := true
	skills, err := s.ListSkills(ctx, &SkillFilter{Enabled: &enabled})
	if err != nil {
		return "", err
	}

	rows := make([]SkillIndexRow, 0, len(skills))
	for _, item := range skills {
		if item == nil {
			continue
		}
		name := strings.TrimSpace(item.Name)
		if len(allowedSet) > 0 {
			if _, ok := allowedSet[name]; !ok {
				continue
			}
		}
		skillAgent := firstNonEmpty(strings.TrimSpace(item.Agent), "all")
		if !skillVisibleToAgent(skillAgent, agentName) {
			continue
		}

		rows = append(rows, SkillIndexRow{
			Name:        name,
			Description: strings.TrimSpace(item.Description),
			WhenToUse:   strings.TrimSpace(item.WhenToUse),
			Context:     firstNonEmpty(strings.TrimSpace(item.Context), "inline"),
			Status:      skillStatus(name, activeSet),
			SkillDir:    strings.TrimSpace(item.SkillDir),
		})
	}

	sort.SliceStable(rows, func(i, j int) bool {
		return rows[i].Name < rows[j].Name
	})

	return formatSkillsTable(rows), nil
}

func (s *SkillService) BuildInjectedSkillsSection(ctx context.Context, allowedSkillNames []string, names []string) (string, error) {
	if s == nil {
		return "", fmt.Errorf("skill service is nil")
	}
	normalized := normalizeSkillNamesForTable(names)
	if len(normalized) == 0 {
		return "", nil
	}
	allowedSet := toStringSet(allowedSkillNames)
	skills, err := s.LoadSkills(ctx, normalized)
	if err != nil {
		return "", err
	}
	sections := make([]string, 0, len(skills))
	for _, skill := range skills {
		if skill == nil {
			continue
		}
		name := strings.TrimSpace(skill.Name)
		if name == "" {
			continue
		}
		if len(allowedSet) > 0 {
			if _, ok := allowedSet[name]; !ok {
				continue
			}
		}
		desc := strings.TrimSpace(skill.Description)
		if desc == "" {
			desc = "-"
		}
		version := strings.TrimSpace(skill.Version)
		if version == "" {
			version = "-"
		}
		sections = append(sections, strings.TrimSpace(fmt.Sprintf(
			"#### %s\n- description: %s\n- version: %s\n\n%s",
			name,
			desc,
			version,
			strings.TrimSpace(skill.Instructions),
		)))
	}
	return strings.TrimSpace(strings.Join(sections, "\n\n")), nil
}

func toStringSet(names []string) map[string]struct{} {
	if len(names) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(names))
	for _, raw := range names {
		name := strings.TrimSpace(raw)
		if name != "" {
			set[name] = struct{}{}
		}
	}
	if len(set) == 0 {
		return nil
	}
	return set
}

func skillVisibleToAgent(skillAgent string, agentName string) bool {
	skillAgent = strings.TrimSpace(skillAgent)
	agentName = strings.TrimSpace(agentName)
	if agentName == "" {
		return false
	}
	switch strings.ToLower(agentName) {
	case "all", "*":
		return true
	}
	switch strings.ToLower(skillAgent) {
	case "all", "*":
		return true
	default:
		return strings.EqualFold(skillAgent, agentName)
	}
}

const skillsTablePathThreshold = 20

func formatSkillsTable(rows []SkillIndexRow) string {
	if len(rows) == 0 {
		return ""
	}

	lines := make([]string, 0, len(rows)+4)

	if len(rows) > skillsTablePathThreshold {
		lines = append(lines, "> 可用技能较多，请通过 read_file 读取对应 SKILL.md 获取完整指令，不要一次性加载所有技能。")
		lines = append(lines, "")
	}
	lines = append(lines, "| name | description | when-to-use | path | context | status |")
	lines = append(lines, "| --- | --- | --- | --- | --- | --- |")

	for _, row := range rows {
		name := sanitizeTableCell(row.Name)
		desc := sanitizeTableCell(row.Description)
		if desc == "" {
			desc = "-"
		}
		whenToUse := sanitizeTableCell(row.WhenToUse)
		if whenToUse == "" {
			whenToUse = "-"
		}
		ctx := sanitizeTableCell(row.Context)
		if ctx == "" {
			ctx = "inline"
		}
		status := sanitizeTableCell(row.Status)
		if status == "" {
			status = "available"
		}
		skillPath := "-"
		if row.SkillDir != "" {
			skillPath = sanitizeTableCell(row.SkillDir + "/SKILL.md")
		}
		lines = append(lines, fmt.Sprintf("| %s | %s | %s | %s | %s | %s |", name, desc, whenToUse, skillPath, ctx, status))
	}
	return strings.Join(lines, "\n")
}

func skillStatus(name string, activeSet map[string]struct{}) string {
	if _, exists := activeSet[name]; exists {
		return "loaded"
	}
	return "available"
}

func normalizeSkillNamesForTable(names []string) []string {
	if len(names) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(names))
	out := make([]string, 0, len(names))
	for _, raw := range names {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func sanitizeTableCell(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "|", "\\|")
	s = strings.Join(strings.Fields(s), " ")
	return s
}
