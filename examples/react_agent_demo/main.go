package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"aster/internal/ai/openai"
	"aster/internal/builtin_tools"
	"aster/internal/react"
	"aster/internal/service"
)

func main() {
	ctx := context.Background()

	opts := []openai.Option{
		openai.WithAPIKey(os.Getenv("OPENAI_API_KEY")),
		openai.WithModel(os.Getenv("OPENAI_MODEL")),
		openai.WithStream(false),
	}
	if baseURL := os.Getenv("OPENAI_BASE_URL"); baseURL != "" {
		opts = append(opts, openai.WithURL(baseURL))
	}
	aiClient := openai.NewClient(opts...)

	skillService := service.NewSkillServiceWithMemory()
	if _, err := skillService.ImportEmbeddedSkills(ctx); err != nil {
		log.Fatal(err)
	}

	emitter := react.NewEmitter("demo-session", "demo-agent", func(e *react.AgentOutputEvent) error {
		if e == nil {
			return nil
		}
		if e.Content != "" {
			fmt.Printf("[%s] %s\n", e.Type, e.Content)
		}
		return nil
	})

	// --- Demo 1: AgentFactory + AgentDefinition (recommended) ---
	fmt.Println("=== Demo 1: Factory-based Agent (analysis type) ===")

	registry := react.NewDefaultToolRegistry()
	registry.Register(builtin_tools.ListSkillsToolName, func(_ builtin_tools.ToolContext) react.Tool {
		return builtin_tools.NewListSkillsTool(skillService)
	})
	registry.Register(builtin_tools.LoadSkillsToolName, func(_ builtin_tools.ToolContext) react.Tool {
		return builtin_tools.NewLoadSkillsTool(skillService)
	})

	factory := react.NewAgentFactory(
		react.WithFactoryDefaultAIClient(aiClient),
		react.WithFactoryToolRegistry(registry),
		react.WithFactorySkillsCatalog(skillService),
		react.WithFactoryEmitter(emitter),
	)

	analysisDef := react.AgentDefinition{
		Name:        "analysis-agent",
		Role:        "你是一个项目分析 Agent，专注于理解项目结构和定位入口。",
		Instruction: "优先使用现有上下文和工具完成定位，输出简洁的分析结论。",
		ToolNames:   []string{"list_files", "read_file", "rg", "list_skills", "load_skills"},
		Policies: react.AgentPolicies{
			MaxIterations: 8,
		},
		Context: []react.TaskContextEntry{
			{Label: "分析目标", Value: "项目结构概览", Description: "本次分析的主要聚焦方向"},
		},
	}

	agent, err := factory.Build(analysisDef)
	if err != nil {
		log.Fatal(err)
	}

	workspaceRoot, err := os.MkdirTemp("", "aster-demo-*")
	if err != nil {
		log.Fatal(err)
	}

	result, err := agent.Execute(
		ctx,
		"请用一句话介绍 ReAct Agent。",
		react.WithWorkspaceSession("demo-session", workspaceRoot),
		react.WithTaskContext(&react.TaskContextData{
			Entries: []react.TaskContextEntry{
				{Label: "分析目标", Value: "项目结构概览"},
			},
		}),
	)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("success=%v result=%s\n\n", result.Success, result.Result)

	// --- Demo 2: Different agent type via definition ---
	fmt.Println("=== Demo 2: Factory-based Agent (execution type) ===")

	executionDef := react.AgentDefinition{
		Name:        "executor-agent",
		Role:        "你是一个执行型 Agent，负责按照指令完成具体操作。",
		Background:  "本次任务场景为自动化运维。",
		Instruction: "直接执行指令，输出操作结果，不做多余分析。",
		Policies: react.AgentPolicies{
			MaxIterations: 5,
		},
	}

	executor, err := factory.Build(executionDef)
	if err != nil {
		log.Fatal(err)
	}

	result2, err := executor.Execute(
		ctx,
		"请用一句话说明你的角色。",
		react.WithWorkspaceSession("demo-session-2", workspaceRoot),
	)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("success=%v result=%s\n", result2.Success, result2.Result)
}
