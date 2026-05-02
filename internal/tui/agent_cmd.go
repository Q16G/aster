package tui

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"aster/internal/ai"
	"aster/internal/builtin_tools"
	"aster/internal/mcp"
	"aster/internal/react"
)

type AgentExecContext struct {
	Factory        *react.AgentFactory
	Definition     react.AgentDefinition
	MCPManager     *mcp.Manager
	ProjectRoot    string
	SessionID      string
	SessionDir     string // ~/.aster/sessions/<id>
	InitialState   builtin_tools.StateSnapshot
	InitialHistory []*ai.MsgInfo
	cancelMu       sync.Mutex
	cancelFunc     context.CancelFunc
}

func (c *AgentExecContext) ExecuteCmd(input string) tea.Cmd {
	return func() tea.Msg { return c.executeInternal(input, "") }
}

func (c *AgentExecContext) ExecuteCmdWithExtra(input, extraText string) tea.Cmd {
	return func() tea.Msg { return c.executeInternal(input, extraText) }
}

func (c *AgentExecContext) executeInternal(input, extraText string) tea.Msg {
	def := c.Definition
	if def.Policies.AllowBash && def.Policies.BashPermissionContext != nil {
		if def.Policies.BashPermissionContext.PermCtx != nil && c.ProjectRoot != "" {
			copied := *def.Policies.BashPermissionContext
			copiedCtx := *copied.PermCtx
			copiedCtx.ProjectPath = c.ProjectRoot
			copied.PermCtx = &copiedCtx
			def.Policies.BashPermissionContext = &copied
		}
	}

	runID := fmt.Sprintf("tui-%d", time.Now().UnixNano())
	if c.SessionID != "" && c.SessionDir != "" {
		baseDir := filepath.Dir(c.SessionDir)
		_ = appendSessionRunEvent(baseDir, c.SessionID, persistedRunEvent{
			RunID: runID,
			Event: "started",
			Input: input,
			Time:  time.Now(),
		})
	}

	c.ensureSessionMCPConnections()

	agent, err := c.Factory.Build(def)
	if err != nil {
		c.persistSessionRunArtifacts(runID, nil, err, nil)
		return AgentDoneMsg{RunID: runID, Err: err}
	}
	if len(c.InitialHistory) > 0 {
		agent.SetHistory(ai.NormalizeMsgInfoSlice(c.InitialHistory))
	}
	ctx, cancel := context.WithCancel(context.Background())
	c.cancelMu.Lock()
	c.cancelFunc = cancel
	c.cancelMu.Unlock()
	defer c.Cancel()

	var execOpts []react.ExecuteOption
	if c.SessionID != "" && c.SessionDir != "" {
		workspaceRoot := c.SessionDir + "/workspace"
		execOpts = append(execOpts, react.WithWorkspaceSession(c.SessionID, workspaceRoot))
	} else if c.ProjectRoot != "" {
		execOpts = append(execOpts, react.WithWorkspaceSession("", c.ProjectRoot))
	}
	execOpts = append(execOpts, react.WithExecuteRunID(runID))
	if len(c.InitialState.ActiveSkillNames) > 0 || len(c.InitialState.ActiveMCPServers) > 0 {
		execOpts = append(execOpts, react.WithInitialStateBootstrap(c.InitialState))
	}
	if extraText != "" {
		execOpts = append(execOpts, react.WithExtraText(extraText))
	}

	result, err := agent.Execute(ctx, input, execOpts...)
	history := agent.History()
	c.persistSessionRunArtifacts(runID, result, err, history)
	return AgentDoneMsg{Result: result, RunID: runID, History: history, Err: err}
}

func (c *AgentExecContext) Cancel() {
	c.cancelMu.Lock()
	defer c.cancelMu.Unlock()
	if c.cancelFunc != nil {
		c.cancelFunc()
		c.cancelFunc = nil
	}
}

func (c *AgentExecContext) ensureSessionMCPConnections() {
	if c == nil || c.MCPManager == nil || len(c.InitialState.ActiveMCPServers) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	for _, name := range c.InitialState.ActiveMCPServers {
		if name == "" {
			continue
		}
		_, _ = c.MCPManager.Connect(ctx, name)
	}
}

func (c *AgentExecContext) persistSessionRunArtifacts(runID string, result *builtin_tools.RunResult, runErr error, history []*ai.MsgInfo) {
	if c == nil || c.SessionID == "" || c.SessionDir == "" {
		return
	}
	baseDir := filepath.Dir(c.SessionDir)
	errText := ""
	success := false
	if runErr != nil {
		errText = runErr.Error()
	} else if result != nil {
		success = result.Success
		if !result.Success {
			errText = result.Error
		}
	}
	_ = appendSessionRunEvent(baseDir, c.SessionID, persistedRunEvent{
		RunID:   runID,
		Event:   "finished",
		Success: success,
		Error:   errText,
		Time:    time.Now(),
	})
	if len(history) > 0 {
		_ = saveSessionAIHistory(baseDir, c.SessionID, history)
	}
}
