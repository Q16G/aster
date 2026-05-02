package builtin_tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"aster/internal/service"
)

type DeleteSkillTool struct {
	skillService *service.SkillService
}

func NewDeleteSkillTool(skillService *service.SkillService) *DeleteSkillTool {
	return &DeleteSkillTool{skillService: skillService}
}

func (t *DeleteSkillTool) Name() string { return DeleteSkillToolName }

func (t *DeleteSkillTool) Description() string {
	return "[DEPRECATED] 删除指定技能。"
}

func (t *DeleteSkillTool) Parameters() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{
				"type":        "string",
				"description": "Skill name to delete.",
			},
		},
		"required":             []string{"name"},
		"additionalProperties": false,
	}
}

func (t *DeleteSkillTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	if t == nil || t.skillService == nil {
		return "", fmt.Errorf("skill service is nil")
	}
	name, _ := args["name"].(string)
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("name is required")
	}
	if err := t.skillService.DeleteSkillByName(ctx, name); err != nil {
		return "", fmt.Errorf("delete skill failed: %w", err)
	}

	out, err := json.Marshal(map[string]any{
		"ok":   true,
		"name": name,
	})
	if err != nil {
		return "", fmt.Errorf("marshal result failed: %w", err)
	}
	return string(out), nil
}
