package builtin_tools

import (
	"context"
	"encoding/json"
	"fmt"

	"aster/internal/service"
)

type LoadSkillsTool struct {
	skillService *service.SkillService
}

func NewLoadSkillsTool(skillService *service.SkillService) *LoadSkillsTool {
	return &LoadSkillsTool{skillService: skillService}
}

func (t *LoadSkillsTool) Name() string {
	return LoadSkillsToolName
}

func (t *LoadSkillsTool) Description() string {
	return "[DEPRECATED: 使用 skill 工具替代] 按名称加载技能内容。"
}

func (t *LoadSkillsTool) Parameters() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"names": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "string",
				},
				"description": "Skill names to load (e.g., ['spring-boot-entry-points', 'mybatis-sql-injection'])",
			},
		},
		"required":             []string{"names"},
		"additionalProperties": false,
	}
}

func (t *LoadSkillsTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	if t == nil || t.skillService == nil {
		return "", fmt.Errorf("skill service is nil")
	}
	namesRaw, ok := args["names"]
	if !ok {
		return "", fmt.Errorf("missing required parameter: names")
	}

	namesSlice, ok := namesRaw.([]interface{})
	if !ok {
		return "", fmt.Errorf("names must be an array")
	}

	names := make([]string, 0, len(namesSlice))
	for _, n := range namesSlice {
		if str, ok := n.(string); ok {
			names = append(names, str)
		}
	}

	if len(names) == 0 {
		return "", fmt.Errorf("names array is empty")
	}

	skills, err := t.skillService.LoadSkills(ctx, names)
	if err != nil {
		return "", fmt.Errorf("failed to load skills: %w", err)
	}

	result := make([]map[string]any, 0, len(skills))
	for _, skill := range skills {
		result = append(result, formatSkill(skill))
	}

	out, err := json.Marshal(map[string]any{
		"ok":     true,
		"count":  len(result),
		"skills": result,
	})
	if err != nil {
		return "", fmt.Errorf("failed to marshal result: %w", err)
	}

	return string(out), nil
}

func formatSkill(skill *service.Skill) map[string]any {
	return map[string]any{
		"name":         skill.Name,
		"description":  skill.Description,
		"version":      skill.Version,
		"instructions": skill.Instructions,
		"enabled":      skill.Enabled,
	}
}
