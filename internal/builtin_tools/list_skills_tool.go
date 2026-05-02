package builtin_tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"aster/internal/service"
	"aster/internal/utils/argx"
)

type ListSkillsTool struct {
	skillService *service.SkillService
}

func NewListSkillsTool(skillService *service.SkillService) *ListSkillsTool {
	return &ListSkillsTool{skillService: skillService}
}

func (t *ListSkillsTool) Name() string { return ListSkillsToolName }

func (t *ListSkillsTool) Description() string {
	return "按条件列出技能。"
}

func (t *ListSkillsTool) Parameters() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{
				"type":        "string",
				"description": "Optional fuzzy name filter (LIKE %name%).",
			},
			"enabled": map[string]any{
				"type":        "boolean",
				"description": "Optional enabled filter.",
			},
			"agent": map[string]any{
				"type":        "string",
				"description": "Optional filter by agent (case-insensitive).",
			},
		},
		"additionalProperties": false,
	}
}

func (t *ListSkillsTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	if t == nil || t.skillService == nil {
		return "", fmt.Errorf("skill service is nil")
	}
	filter := &service.SkillFilter{}

	filter.Name = argx.OptionalText(args, "name")
	if v, ok := args["enabled"].(bool); ok {
		filter.Enabled = &v
	}

	agentFilter := strings.TrimSpace(argx.OptionalText(args, "agent"))

	skills, err := t.skillService.ListSkills(ctx, filter)
	if err != nil {
		return "", err
	}

	items := make([]map[string]any, 0, len(skills))
	for _, skill := range skills {
		if skill == nil {
			continue
		}
		skillAgent := strings.TrimSpace(skill.Agent)
		if skillAgent == "" {
			skillAgent = "all"
		}
		if agentFilter != "" && !strings.EqualFold(skillAgent, agentFilter) {
			continue
		}

		item := map[string]any{
			"name":           skill.Name,
			"description":    skill.Description,
			"agent":          skillAgent,
			"when_to_use":    skill.WhenToUse,
			"context":        skill.Context,
			"user_invocable": skill.UserInvocable,
			"enabled":        skill.Enabled,
		}
		if len(skill.Arguments) > 0 {
			item["arguments"] = skill.Arguments
		}
		if skill.ArgumentHint != "" {
			item["argument_hint"] = skill.ArgumentHint
		}
		if len(skill.AllowedTools) > 0 {
			item["allowed_tools"] = skill.AllowedTools
		}
		items = append(items, item)
	}

	out, err := json.Marshal(map[string]any{
		"ok":     true,
		"count":  len(items),
		"skills": items,
	})
	if err != nil {
		return "", fmt.Errorf("marshal result failed: %w", err)
	}
	return string(out), nil
}
