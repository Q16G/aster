package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Skill keeps the runtime-visible skill fields. It intentionally omits any
// database model concerns so the first-stage migration stays memory-only.
type Skill struct {
	Name         string   `json:"name,omitempty"`
	Description  string   `json:"description,omitempty"`
	Instructions string   `json:"instructions,omitempty"`
	Version      string   `json:"version,omitempty"`
	Tags         []string `json:"tags,omitempty"`
	Enabled      bool     `json:"enabled,omitempty"`

	Agent         string   `json:"agent,omitempty"`
	WhenToUse     string   `json:"when_to_use,omitempty"`
	UserInvocable bool     `json:"user_invocable"`
	Arguments     []string `json:"arguments,omitempty"`
	ArgumentHint  string   `json:"argument_hint,omitempty"`
	AllowedTools  []string `json:"allowed_tools,omitempty"`
	MCP           []string `json:"mcp,omitempty"`
	Context       string   `json:"context,omitempty"`
	Source        string   `json:"source,omitempty"`
	SkillDir      string   `json:"skill_dir,omitempty"`
}

type SkillFilter struct {
	Name    string
	Enabled *bool
	Tags    []string
}

// ModelDTO 保留模型查询所需最小字段，供上层配置与测试使用。
type ModelDTO struct {
	ModelID       string
	ModelType     string
	ModelName     string
	URL           string
	APIKey        string
	Proxy         string
	TimeoutSecs   int
	ExtraBodyJSON string
	ModelInfoJSON string
	Remark        string
	Enabled       bool
	Online        bool
	CallCount     int64
	IsBuiltin     bool
}

// MCPSkill 对应 skills/ 下的 SKILL 元数据结构。
type MCPSkill struct {
	Name         string
	Description  string
	Instructions string
	Version      string
	Tags         []string
	Metadata     map[string]any
	Enabled      *bool

	Agent         string
	WhenToUse     string
	UserInvocable bool
	Arguments     []string
	ArgumentHint  string
	AllowedTools  []string
	MCP           []string
	Context       string
	Source        string
	SkillDir      string
}

// SkillService 提供技能导入与读取能力。
type SkillService struct {
	memory *skillMemoryStore
}

func NewSkillServiceWithMemory() *SkillService {
	return &SkillService{memory: newSkillMemoryStore()}
}

func (s *SkillService) LoadSkills(ctx context.Context, names []string) ([]*Skill, error) {
	if s == nil || s.memory == nil {
		return nil, fmt.Errorf("skill service is nil")
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("skill names are empty")
	}

	normalized := normalizeSkillNames(names)
	if len(normalized) == 0 {
		return nil, fmt.Errorf("skill names are empty")
	}

	result := make([]*Skill, 0, len(normalized))
	for _, name := range normalized {
		item, err := s.memory.GetSkillByName(ctx, name)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, nil
}

func (s *SkillService) ImportSkill(ctx context.Context, input *MCPSkill) error {
	if s == nil || s.memory == nil {
		return fmt.Errorf("skill service is nil")
	}
	if input == nil {
		return fmt.Errorf("skill is nil")
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return fmt.Errorf("skill name is required")
	}
	instructions := strings.TrimSpace(input.Instructions)
	if instructions == "" {
		return fmt.Errorf("skill instructions are required")
	}

	enabled := true
	if input.Enabled != nil {
		enabled = *input.Enabled
	}

	skillContext := strings.TrimSpace(input.Context)
	if skillContext == "" {
		skillContext = "inline"
	}

	skill := &Skill{
		Name:         name,
		Description:  firstNonEmpty(strings.TrimSpace(input.Description), name),
		Instructions: instructions,
		Version:      firstNonEmpty(strings.TrimSpace(input.Version), "1.0.0"),
		Tags:         cloneStrings(input.Tags),
		Enabled:      enabled,

		Agent:         firstNonEmpty(strings.TrimSpace(input.Agent), "all"),
		WhenToUse:     strings.TrimSpace(input.WhenToUse),
		UserInvocable: input.UserInvocable,
		Arguments:     cloneStrings(input.Arguments),
		ArgumentHint:  strings.TrimSpace(input.ArgumentHint),
		AllowedTools:  cloneStrings(input.AllowedTools),
		MCP:           cloneStrings(input.MCP),
		Context:       skillContext,
		Source:        strings.TrimSpace(input.Source),
		SkillDir:      strings.TrimSpace(input.SkillDir),
	}
	return s.memory.SaveSkill(ctx, skill)
}

func (s *SkillService) ImportMcpSkills(ctx context.Context, skills []*MCPSkill) error {
	for _, skill := range skills {
		if err := s.ImportSkill(ctx, skill); err != nil {
			return err
		}
	}
	return nil
}

func (s *SkillService) ListSkills(ctx context.Context, filter *SkillFilter) ([]*Skill, error) {
	if s == nil || s.memory == nil {
		return nil, fmt.Errorf("skill service is nil")
	}
	return s.memory.ListSkills(ctx, filter)
}

func (s *SkillService) DeleteSkillByName(ctx context.Context, name string) error {
	if s == nil || s.memory == nil {
		return fmt.Errorf("skill service is nil")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("skill name is required")
	}
	return s.memory.DeleteSkillByName(ctx, name)
}

func (s *SkillService) SetSkillEnabledByName(ctx context.Context, name string, enabled bool) error {
	if s == nil || s.memory == nil {
		return fmt.Errorf("skill service is nil")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("skill name is required")
	}

	item, err := s.memory.GetSkillByName(ctx, name)
	if err != nil {
		return err
	}
	item.Enabled = enabled
	return s.memory.SaveSkill(ctx, item)
}

func normalizeSkillNames(names []string) []string {
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

func cloneStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}

func cloneSkill(input *Skill) *Skill {
	if input == nil {
		return nil
	}
	out := *input
	out.Tags = cloneStrings(input.Tags)
	out.Arguments = cloneStrings(input.Arguments)
	out.AllowedTools = cloneStrings(input.AllowedTools)
	out.MCP = cloneStrings(input.MCP)
	return &out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

type skillMemoryStore struct {
	skills map[string]*Skill
	mu     sync.RWMutex
}

func newSkillMemoryStore() *skillMemoryStore {
	return &skillMemoryStore{skills: make(map[string]*Skill)}
}

func (s *skillMemoryStore) SaveSkill(_ context.Context, skill *Skill) error {
	if skill == nil {
		return fmt.Errorf("skill is nil")
	}
	name := strings.TrimSpace(skill.Name)
	if name == "" {
		return fmt.Errorf("skill name is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	stored := cloneSkill(skill)
	stored.Name = name
	s.skills[name] = stored
	return nil
}

func (s *skillMemoryStore) GetSkillByName(_ context.Context, name string) (*Skill, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	name = strings.TrimSpace(name)
	item, ok := s.skills[name]
	if !ok {
		return nil, fmt.Errorf("skill not found: %s", name)
	}
	return cloneSkill(item), nil
}

func (s *skillMemoryStore) ListSkills(_ context.Context, filter *SkillFilter) ([]*Skill, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]*Skill, 0, len(s.skills))
	for _, item := range s.skills {
		if !matchesSkillFilter(item, filter) {
			continue
		}
		out = append(out, cloneSkill(item))
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out, nil
}

func (s *skillMemoryStore) DeleteSkillByName(_ context.Context, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	name = strings.TrimSpace(name)
	if _, ok := s.skills[name]; !ok {
		return fmt.Errorf("skill not found: %s", name)
	}
	delete(s.skills, name)
	return nil
}

func matchesSkillFilter(item *Skill, filter *SkillFilter) bool {
	if item == nil {
		return false
	}
	if filter == nil {
		return true
	}
	if filter.Enabled != nil && item.Enabled != *filter.Enabled {
		return false
	}
	if filter.Name != "" {
		name := strings.ToLower(strings.TrimSpace(item.Name))
		want := strings.ToLower(strings.TrimSpace(filter.Name))
		if !strings.Contains(name, want) {
			return false
		}
	}
	if len(filter.Tags) > 0 {
		tagSet := make(map[string]struct{}, len(item.Tags))
		for _, tag := range item.Tags {
			tag = strings.ToLower(strings.TrimSpace(tag))
			if tag == "" {
				continue
			}
			tagSet[tag] = struct{}{}
		}
		for _, tag := range filter.Tags {
			tag = strings.ToLower(strings.TrimSpace(tag))
			if tag == "" {
				continue
			}
			if _, ok := tagSet[tag]; !ok {
				return false
			}
		}
	}
	return true
}
