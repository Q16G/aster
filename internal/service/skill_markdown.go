package service

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

type flexStrings []string

func (f *flexStrings) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode {
		for _, s := range strings.Split(value.Value, ",") {
			if t := strings.TrimSpace(s); t != "" {
				*f = append(*f, t)
			}
		}
		return nil
	}
	var list []string
	if err := value.Decode(&list); err != nil {
		return err
	}
	*f = list
	return nil
}

type skillFrontmatter struct {
	Name            string         `yaml:"name"`
	Description     string         `yaml:"description"`
	Version         string         `yaml:"version"`
	Author          string         `yaml:"author"`
	Category        string         `yaml:"category"`
	Type            string         `yaml:"type"`
	Tags            flexStrings    `yaml:"tags"`
	Tools           flexStrings    `yaml:"tools"`
	Enabled         *bool          `yaml:"enabled"`
	Priority        int            `yaml:"priority"`
	TriggerKeywords flexStrings    `yaml:"trigger_keywords"`
	Meta            map[string]any `yaml:"meta"`
	Metadata        map[string]any `yaml:"metadata"`

	WhenToUse     string      `yaml:"when-to-use"`
	UserInvocable *bool       `yaml:"user-invocable"`
	Arguments     flexStrings `yaml:"arguments"`
	ArgumentHint  string      `yaml:"argument-hint"`
	AllowedTools  flexStrings `yaml:"allowed-tools"`
	MCP           flexStrings `yaml:"mcp"`
	Context       string      `yaml:"context"`
	Agent         string      `yaml:"agent"`
}

func ParseSkillMarkdownFile(filePath string) (*MCPSkill, error) {
	raw, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	return ParseSkillMarkdown(string(raw))
}

func ParseSkillMarkdown(markdown string) (*MCPSkill, error) {
	scanner := bufio.NewScanner(strings.NewReader(markdown))
	scanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)

	inFrontmatter := false
	frontmatterDone := false

	frontmatterLines := make([]string, 0, 32)
	instructionsLines := make([]string, 0, 256)

	for scanner.Scan() {
		line := scanner.Text()

		if line == "---" {
			if !inFrontmatter && !frontmatterDone {
				inFrontmatter = true
				continue
			}
			if inFrontmatter {
				inFrontmatter = false
				frontmatterDone = true
				continue
			}
		}

		if inFrontmatter {
			frontmatterLines = append(frontmatterLines, line)
			continue
		}
		if frontmatterDone {
			instructionsLines = append(instructionsLines, line)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	yamlText := strings.TrimSpace(strings.Join(frontmatterLines, "\n"))
	if yamlText == "" {
		return nil, fmt.Errorf("missing frontmatter")
	}

	var fm skillFrontmatter
	if err := yaml.Unmarshal([]byte(yamlText), &fm); err != nil {
		return nil, fmt.Errorf("unmarshal frontmatter yaml failed: %w", err)
	}

	agent := strings.TrimSpace(fm.Agent)
	if agent == "" {
		if fm.Meta != nil {
			if v, ok := fm.Meta["agent"]; ok && v != nil {
				agent = strings.TrimSpace(fmt.Sprint(v))
			}
		}
	}
	if agent == "" {
		agent = "all"
	}

	triggerKeywords := make([]string, 0, len(fm.TriggerKeywords))
	for _, it := range fm.TriggerKeywords {
		kw := strings.TrimSpace(it)
		if kw == "" {
			continue
		}
		triggerKeywords = append(triggerKeywords, kw)
	}

	whenToUse := strings.TrimSpace(fm.WhenToUse)
	if whenToUse == "" && len(triggerKeywords) > 0 {
		whenToUse = strings.Join(triggerKeywords, ", ")
	}

	allowedTools := fm.AllowedTools
	if len(allowedTools) == 0 {
		allowedTools = fm.Tools
	}

	skillContext := strings.TrimSpace(fm.Context)
	if skillContext == "" {
		skillContext = "inline"
	}

	userInvocable := true
	if fm.UserInvocable != nil {
		userInvocable = *fm.UserInvocable
	}

	return &MCPSkill{
		Name:          strings.TrimSpace(fm.Name),
		Description:   strings.TrimSpace(fm.Description),
		Version:       strings.TrimSpace(fm.Version),
		Tags:          fm.Tags,
		Enabled:       fm.Enabled,
		Instructions:  strings.TrimSpace(strings.Join(instructionsLines, "\n")),
		Agent:         agent,
		WhenToUse:     whenToUse,
		UserInvocable: userInvocable,
		Arguments:     fm.Arguments,
		ArgumentHint:  strings.TrimSpace(fm.ArgumentHint),
		AllowedTools:  allowedTools,
		MCP:           fm.MCP,
		Context:       skillContext,
	}, nil
}
