package react

import (
	"context"
	"strings"

	"aster/internal/builtin_tools"
)

type SkillsPromptContext struct {
	Table    string
	Injected string
}

func (c *SkillsPromptContext) HasVisibleData() bool {
	return c != nil && (c.HasTable() || c.HasInjected())
}

func (c *SkillsPromptContext) HasTable() bool {
	return c != nil && strings.TrimSpace(c.Table) != ""
}

func (c *SkillsPromptContext) HasInjected() bool {
	return c != nil && strings.TrimSpace(c.Injected) != ""
}

type SkillsPromptProvider interface {
	BuildSkillsPrompt(ctx context.Context, agentName string, snapshot builtin_tools.StateSnapshot) (*SkillsPromptContext, error)
}

type SkillsPromptProviderFunc func(ctx context.Context, agentName string, snapshot builtin_tools.StateSnapshot) (*SkillsPromptContext, error)

func (fn SkillsPromptProviderFunc) BuildSkillsPrompt(ctx context.Context, agentName string, snapshot builtin_tools.StateSnapshot) (*SkillsPromptContext, error) {
	if fn == nil {
		return nil, nil
	}
	return fn(ctx, agentName, snapshot)
}

type SkillsCatalog interface {
	BuildSkillsTableWithStatus(ctx context.Context, agentName string, activeSkillNames []string) (string, error)
	BuildInjectedSkillsSection(ctx context.Context, names []string) (string, error)
}

func NewSkillsPromptProviderFromCatalog(catalog SkillsCatalog) SkillsPromptProvider {
	if catalog == nil {
		return nil
	}
	return SkillsPromptProviderFunc(func(ctx context.Context, agentName string, snapshot builtin_tools.StateSnapshot) (*SkillsPromptContext, error) {
		table, err := catalog.BuildSkillsTableWithStatus(ctx, agentName, snapshot.ActiveSkillNames)
		if err != nil {
			return nil, err
		}
		injected, err := catalog.BuildInjectedSkillsSection(ctx, snapshot.ActiveSkillNames)
		if err != nil {
			return nil, err
		}
		result := &SkillsPromptContext{
			Table:    strings.TrimSpace(table),
			Injected: strings.TrimSpace(injected),
		}
		if !result.HasVisibleData() {
			return nil, nil
		}
		return result, nil
	})
}
