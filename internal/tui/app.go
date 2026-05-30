package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"aster/internal/ai"
	aiusage "aster/internal/ai/usage"
	"aster/internal/builtin_tools"
	"aster/internal/mcp"
	"aster/internal/provider"
	"aster/internal/selfupdate"
	"aster/internal/service"
	tuicontext "aster/internal/tui/context"
	tuiui "aster/internal/tui/ui"
)

const sidebarWidth = 42

type FocusTarget int

const (
	FocusInput FocusTarget = iota
	FocusSidebar
	FocusSubAgents
	FocusChat
)

type renderTickMsg struct{}
type sessionRestoreMsg struct{}

// MCPStatusChangedMsg 由 MCPBridge 在 MCP manager 状态迁移时推送，触发侧边栏/footer 刷新。
type MCPStatusChangedMsg struct {
	Name   string
	Status string
}

const renderInterval = 33 * time.Millisecond

func renderTickCmd() tea.Cmd {
	return tea.Tick(renderInterval, func(time.Time) tea.Msg {
		return renderTickMsg{}
	})
}

type pendingProviderSetup struct {
	ProviderID string
}

type retryState struct {
	Message     string
	Attempt     int
	MaxAttempts int
	Next        time.Time
}

type Model struct {
	width  int
	height int
	store  *SessionStore

	chat            ChatModel
	input           InputModel
	sidebar         SidebarModel
	subAgentPanel   SubAgentPanel
	thinkingPanel   ThinkingPanelModel
	agentCtx        *AgentExecContext
	humanBridge     *HumanInputBridge
	agentRunning    bool
	statusText      string
	retryState      *retryState
	profileRegistry *ProfileRegistry

	skillService    *service.SkillService
	mcpManager      *mcp.Manager
	registry        *provider.Registry
	credStore       *CredentialStore
	appCfg          *AppConfig
	providerCfg     *ProviderState
	pendingProvider *pendingProviderSetup
	pendingPrompt   string

	currentSessionID        string
	sessionMaterialized     bool
	sessionMeta             SessionMeta
	focus                   FocusTarget
	runStartTime            time.Time
	hadStreamDuringRun      bool
	hadStreamByAgent        map[string]bool
	hadFinalAnswerDuringRun bool
	externalInterrupt       *builtin_tools.ExternalInterrupt
	pendingInterrupt        *builtin_tools.PendingInterrupt
	runtimePhase            string
	runtimeProgress         int
	runtimeGoal             string
	runtimeWarnings         []string
	replanThinkBuf          *strings.Builder
	renderScheduled         bool
	sessionRestoredOnce     bool
	mcpLastLogged           map[string]string

	layoutChatWidth    int
	layoutChatHeight   int
	layoutMainHeight   int
	layoutInputHeight  int
	layoutContentWidth int

	sessionUsage   ai.TokenUsage
	sessionCost    float64
	turnStartUsage ai.TokenUsage
	turnStartCost  float64

	currentVersion string
	updateChecker  *selfupdate.UpdateChecker

	syncStore       *tuicontext.SyncStore
	themeProvider   *tuicontext.ThemeProvider
	keybindProvider *tuicontext.KeybindProvider
	localProvider   *tuicontext.LocalProvider
	exitProvider    *tuicontext.ExitProvider
	dialogStack     *tuiui.DialogStack
	toastManager    *tuiui.ToastManager
	spinner         *tuiui.Spinner
	footer          FooterModel
	commandPicker   *tuiui.CommandPickerModel
	filePicker      *tuiui.FilePickerModel
	selection       SelectionModel
}

type ModelDeps struct {
	Store           *SessionStore
	AgentCtx        *AgentExecContext
	HumanBridge     *HumanInputBridge
	ProfileRegistry *ProfileRegistry
	SkillService    *service.SkillService
	MCPManager      *mcp.Manager
	Registry        *provider.Registry
	CredStore       *CredentialStore
	AppCfg          *AppConfig
	ProviderCfg     *ProviderState
	LocalProv       *tuicontext.LocalProvider
	SyncStore       *tuicontext.SyncStore
	CurrentVersion  string
	UpdateChecker   *selfupdate.UpdateChecker
}

func NewModel(deps ModelDeps) Model {
	localProv := deps.LocalProv
	if localProv == nil {
		localProv = tuicontext.NewLocalProvider()
	}
	m := Model{
		store:           deps.Store,
		chat:            NewChatModel(),
		input:           NewInputModel(),
		sidebar:         NewSidebarModel(),
		subAgentPanel:   NewSubAgentPanel(),
		thinkingPanel:   NewThinkingPanelModel(),
		agentCtx:        deps.AgentCtx,
		humanBridge:     deps.HumanBridge,
		profileRegistry: deps.ProfileRegistry,
		skillService:    deps.SkillService,
		mcpManager:      deps.MCPManager,
		registry:        deps.Registry,
		credStore:       deps.CredStore,
		appCfg:          deps.AppCfg,
		providerCfg:     deps.ProviderCfg,
		replanThinkBuf:  &strings.Builder{},
		statusText:      "ready",
		focus:           FocusInput,

		currentVersion: deps.CurrentVersion,
		updateChecker:  deps.UpdateChecker,

		syncStore:       deps.SyncStore,
		themeProvider:   tuicontext.NewThemeProvider(),
		keybindProvider: tuicontext.NewKeybindProvider(),
		localProvider:   localProv,
		exitProvider:    tuicontext.NewExitProvider(),
		dialogStack:     tuiui.NewDialogStack(),
		toastManager:    tuiui.NewToastManager(5),
		spinner:         tuiui.NewSpinner("thinking..."),
	}
	if deps.AgentCtx != nil {
		m.chat.rootAgentName = deps.AgentCtx.Definition.Name
	}
	if savedTheme := localProv.Get().ThemeName; savedTheme != "" {
		m.themeProvider.SetByName(savedTheme)
	}
	return m
}

func interruptNoticeText(info *builtin_tools.ExternalInterrupt) string {
	if info == nil || info.Retryable {
		return ""
	}
	message := strings.TrimSpace(info.UserMessage)
	if message == "" {
		return ""
	}
	if next := firstNonEmptyInterruptAction(info.SuggestedActions); next != "" {
		return message + " 建议：" + next + "。"
	}
	return message
}

func firstNonEmptyInterruptAction(actions []string) string {
	for _, item := range actions {
		item = strings.TrimSpace(item)
		if item != "" {
			return item
		}
	}
	return ""
}

func (m Model) Init() tea.Cmd {
	if wd, err := os.Getwd(); err == nil {
		home, _ := os.UserHomeDir()
		if home != "" {
			rel, relErr := filepath.Rel(home, wd)
			if relErr == nil && len(rel) < len(wd) {
				wd = "~/" + rel
			}
		}
		m.footer.SetWorkdir(wd)
	}

	defaultProvider := ""
	if m.providerCfg != nil {
		defaultProvider = m.providerCfg.Name
	}
	m.localProvider.MigrateRecentModels(defaultProvider)

	return tea.Batch(m.input.Focus(), m.refreshSidebarCmd(), func() tea.Msg { return sessionRestoreMsg{} })
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.selection.Clear()
		m.updateLayout()
		return m, nil

	case tea.MouseMsg:
		me := tea.MouseEvent(msg)
		switch {
		case me.Button == tea.MouseButtonLeft:
			return m.handleLeftClick(me)
		case me.IsWheel():
			return m.handleWheel(me, msg)
		default:
			if me.Action == tea.MouseActionRelease && m.selection.state == SelectionInProgress {
				return m.handleLeftClick(me)
			}
		}

	case sessionRestoreMsg:
		if !m.sessionRestoredOnce {
			m.sessionRestoredOnce = true
			if m.newSession() {
				m.updateLayout()
			}
		}
		return m, nil

	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			if !copyOnSelect && m.selection.HasSelection() {
				cmd := m.copySelectionCmd()
				m.selection.Clear()
				return m, cmd
			}
			m.selection.Clear()
			if m.agentRunning && m.agentCtx != nil {
				m.agentCtx.CancelTurn()
				m.clearRetryState()
				m.statusText = "cancelling..."
				return m, nil
			}
			if m.focus == FocusInput && strings.TrimSpace(m.input.Value()) != "" {
				m.input.Clear()
				m.syncInputLayout()
				return m, nil
			}
			return m.handleQuitRequest("ctrl+c")
		}

		if msg.String() == "ctrl+d" && m.dialogStack.IsEmpty() {
			return m.handleQuitRequest("ctrl+d")
		}

		if m.selection.state != SelectionNone {
			m.selection.Clear()
		}

		m.exitProvider.Reset()

		if !m.dialogStack.IsEmpty() {
			cmd := m.dialogStack.Update(msg)
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
			return m, tea.Batch(cmds...)
		}

		if m.commandPicker != nil {
			switch msg.String() {
			case "up", "down", "ctrl+p", "ctrl+n", "enter", "tab":
				cmd := m.commandPicker.Update(msg)
				if cmd != nil {
					cmds = append(cmds, cmd)
				}
				return m, tea.Batch(cmds...)
			case "esc":
				m.commandPicker = nil
				m.input.Clear()
				m.syncInputLayout()
				return m, m.input.Focus()
			default:
				var cmd tea.Cmd
				m.input, cmd = m.input.Update(msg)
				m.syncInputLayout()
				if cmd != nil {
					cmds = append(cmds, cmd)
				}
				val := m.input.Value()
				if val == "" || !strings.HasPrefix(val, "/") {
					m.commandPicker = nil
				} else {
					m.commandPicker.SetFilter(val)
				}
				return m, tea.Batch(cmds...)
			}
		}

		if m.filePicker != nil {
			switch msg.String() {
			case "up", "down", "ctrl+p", "ctrl+n", "enter", "tab":
				cmd := m.filePicker.Update(msg)
				if cmd != nil {
					cmds = append(cmds, cmd)
				}
				return m, tea.Batch(cmds...)
			case "esc":
				m.filePicker = nil
				m.input.Clear()
				m.syncInputLayout()
				return m, m.input.Focus()
			default:
				var cmd tea.Cmd
				m.input, cmd = m.input.Update(msg)
				m.syncInputLayout()
				if cmd != nil {
					cmds = append(cmds, cmd)
				}
				val := m.input.Value()
				if val == "" || !strings.HasPrefix(val, "@") {
					m.filePicker = nil
				} else {
					m.filePicker.SetFilter(val)
				}
				return m, tea.Batch(cmds...)
			}
		}

		// In-place sub-agent transcript: left/esc exits back to the panel,
		// before keybinds (esc-to-input) or focus routing can claim the key.
		if m.chat.ViewingChild() != "" {
			switch msg.String() {
			case "left", "h", "esc":
				m.chat.ExitChild()
				m.refreshSidebarData()
				m.setFocus(FocusSubAgents)
				return m, nil
			}
		}

		if action, ok := m.keybindProvider.Resolve(msg.String()); ok {
			switch action {
			case tuicontext.KeyActionOpenAgents:
				if !m.agentRunning && m.profileRegistry != nil {
					m.showAgentSelector()
				}
				return m, nil
			case tuicontext.KeyActionNewSession:
				if !m.agentRunning {
					if !m.newSession() {
						return m, nil
					}
					m.statusText = fmt.Sprintf("new session: %s", shortSessionID(m.currentSessionID))
					return m, tea.Batch(m.input.Focus(), m.refreshSidebarCmd())
				}
				return m, nil
			case tuicontext.KeyActionOpenSessions:
				if !m.agentRunning {
					return m.openSessionSelector()
				}
				return m, nil
			case tuicontext.KeyActionClearChat:
				m.chat = NewChatModel()
				if m.agentCtx != nil {
					m.chat.rootAgentName = m.agentCtx.Definition.Name
				}
				m.updateLayout()
				return m, nil
			case tuicontext.KeyActionOpenModels:
				if !m.agentRunning {
					return m.handleSlashCommand("/model")
				}
				return m, nil
			case tuicontext.KeyActionCycleFocus:
				// Allowed during a run too: inspecting sub-agents while they
				// execute is the whole point of the panel.
				m.cycleFocus()
				return m, m.focusCmd()
			case tuicontext.KeyActionEscape:
				if m.agentRunning && m.agentCtx != nil {
					m.agentCtx.CancelTurn()
					m.clearRetryState()
					m.statusText = "cancelling..."
					return m, nil
				}
				if m.focus != FocusInput {
					m.setFocus(FocusInput)
					return m, m.focusCmd()
				}
				return m, nil
			case tuicontext.KeyActionToggleSidebar:
				m.toggleSidebar()
				return m, nil
			}
		}

		switch m.focus {
		case FocusSidebar:
			var cmd tea.Cmd
			m.sidebar, cmd = m.sidebar.Update(msg)
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
			return m, tea.Batch(cmds...)
		case FocusSubAgents:
			switch msg.String() {
			case "up", "k":
				m.subAgentPanel.MoveUp()
			case "down", "j":
				m.subAgentPanel.MoveDown()
			case "enter", "right", "l", " ":
				if it, ok := m.subAgentPanel.Selected(); ok {
					if m.chat.EnterChild(it.CallID) {
						m.refreshSidebarData()
						m.setFocus(FocusChat)
					}
				}
			case "left", "h", "esc":
				m.setFocus(FocusChat)
			}
			return m, tea.Batch(cmds...)
		case FocusChat:
			var cmd tea.Cmd
			m.chat, cmd = m.chat.Update(msg)
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
			return m, tea.Batch(cmds...)
		default:
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			m.syncInputLayout()
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
			return m, tea.Batch(cmds...)
		}

	case UserSubmitMsg:
		if m.isFirstTimeUser() {
			ret, cmd := m.tryProviderGuardedSubmit(msg.Text)
			if cmd != nil || ret != nil {
				return ret, cmd
			}
		}
		if !m.ensureSession() {
			m.chat.AddPart(DisplayPart{Type: PartTypeSystem, Time: time.Now(), System: &SystemPart{Content: "Failed to create session. Please try again."}})
			return m, m.input.Focus()
		}
		userPart := DisplayPart{Type: PartTypeUser, Time: time.Now(), User: &UserPart{Content: msg.Text}}
		m.chat.AddPart(userPart)
		m.appendPart(userPart)
		m.input.SetEnabled(false)
		m.agentRunning = true
		m.hadStreamDuringRun = false
		m.hadStreamByAgent = nil
		m.hadFinalAnswerDuringRun = false
		m.externalInterrupt = nil
		m.thinkingPanel.Reset()
		m.clearRetryState()
		m.runStartTime = time.Now()
		m.turnStartUsage = m.sessionUsage
		m.turnStartCost = m.sessionCost
		m.statusText = "thinking..."
		m.refreshSidebarData()
		spinnerCmd := m.spinner.Start()
		fileRefs := extractFileRefs(msg.Text)
		if len(fileRefs) > 0 {
			if fileCtx := buildFileContext(fileRefs); fileCtx != "" {
				return m, tea.Batch(m.agentCtx.ExecuteCmdWithExtra(msg.Text, fileCtx), spinnerCmd)
			}
		}
		return m, tea.Batch(m.agentCtx.ExecuteCmd(msg.Text), spinnerCmd)

	case AgentEventMsg:
		m.handleAgentEvent(msg.Event)
		if !m.renderScheduled && (m.chat.IsDirty() || m.thinkingPanel.IsDirty()) {
			m.renderScheduled = true
			return m, renderTickCmd()
		}
		return m, nil

	case renderTickMsg:
		m.renderScheduled = false
		m.chat.FlushRender()
		m.thinkingPanel.FlushRender()
		if m.subAgentPanelVisible() {
			m.refreshSubAgentPanel()
		}
		return m, nil

	case MCPStatusChangedMsg:
		m.refreshSidebarData()
		if msg.Status == string(mcp.MCPStatusError) {
			m.logMCPError(msg.Name)
		}
		return m, nil

	case AgentDoneMsg:
		m.thinkingPanel.Hide()
		m.updateLayout()
		m.chat.FlushThinking()
		hadStream := m.flushAllStreamsAndPersist()
		m.chat.FlushRender()
		m.renderScheduled = false
		m.clearRetryState()
		m.dialogStack.Clear()
		m.agentRunning = false
		m.spinner.Stop()
		m.input.SetEnabled(true)
		m.pendingInterrupt = nil
		history := ai.NormalizeMsgInfoSlice(msg.History)
		if len(history) > 0 {
			if m.agentCtx != nil {
				m.agentCtx.InitialHistory = history
			}
			m.recalcUsageFromHistory(history)
		}
		turnTokens := m.sessionUsage.ContextCountTokens() - m.turnStartUsage.ContextCountTokens()
		if turnTokens < 0 {
			turnTokens = 0
		}
		turnCost := m.sessionCost - m.turnStartCost
		if turnCost < 0 {
			turnCost = 0
		}
		turnStatus := ""
		if msg.Result != nil {
			turnStatus = strings.TrimSpace(msg.Result.TurnStatus)
		}

		if msg.Err != nil {
			m.chat.AddPart(DisplayPart{Type: PartTypeSystem, Time: time.Now(), System: &SystemPart{Content: fmt.Sprintf("Error: %v", msg.Err)}})
			m.statusText = "error"
		} else if turnStatus == "interrupted" && msg.Result != nil && msg.Result.PendingInterrupt != nil {
			// Human-in-the-loop: the turn ends in WAITING_FOR_HUMAN instead of "failed".
			m.pendingInterrupt = msg.Result.PendingInterrupt
			m.statusText = "waiting for your input..."
			m.input.SetEnabled(false)
			prompt := tuiui.NewPromptDialog(m.pendingInterrupt.InterruptID, "Agent needs your input", m.pendingInterrupt.Question)
			if len(m.pendingInterrupt.Options) > 0 {
				prompt.WithOptions(m.pendingInterrupt.Options)
			}
			m.dialogStack.Push(prompt, nil)
			m.dialogStack.SetSize(m.width, m.height)
		} else if turnStatus == "cancelled" {
			m.statusText = "cancelled"
		} else if msg.Result != nil && !msg.Result.Success {
			m.chat.AddPart(DisplayPart{Type: PartTypeSystem, Time: time.Now(), System: &SystemPart{Content: fmt.Sprintf("Agent failed: %s", msg.Result.Error)}})
			m.statusText = "failed"
		} else {
			if !hadStream && !m.hadStreamDuringRun && !m.hadFinalAnswerDuringRun && msg.Result != nil && msg.Result.Result != "" {
				m.chat.AddPart(DisplayPart{
					Type: PartTypeText,
					Time: time.Now(),
					Text: &TextPart{Content: msg.Result.Result},
				})
			}
			m.statusText = "ready"
		}
		if notice := interruptNoticeText(m.externalInterrupt); notice != "" {
			m.chat.AddPart(DisplayPart{
				Type:   PartTypeSystem,
				Time:   time.Now(),
				System: &SystemPart{Content: notice},
			})
		}
		// Run summary (skip for "interrupted/cancelled" turns; they are not user-facing completion).
		if msg.Err == nil && turnStatus != "interrupted" && turnStatus != "cancelled" {
			success := msg.Result == nil || msg.Result.Success
			agentName := ""
			modelID := ""
			if m.agentCtx != nil {
				agentName = m.agentCtx.Definition.Name
			}
			if m.providerCfg != nil {
				modelID = m.providerCfg.ModelID
			}
			turnTokenStr := formatTokenValue(turnTokens)
			if turnTokenStr == "" {
				turnTokenStr = "--"
			}
			turnCostStr := formatCostUSD(turnCost)
			m.chat.AddPart(DisplayPart{
				Type: PartTypeSummary,
				Time: time.Now(),
				Summary: &SummaryPart{
					AgentName:    agentName,
					ModelID:      modelID,
					Duration:     time.Since(m.runStartTime),
					Success:      success,
					TokenCount:   turnTokenStr,
					CostEstimate: turnCostStr,
				},
			})
		}
		m.persistCurrentSession()
		if turnStatus == "interrupted" {
			return m, m.refreshSidebarCmd()
		}
		return m, tea.Batch(m.input.Focus(), m.refreshSidebarCmd())

	case HumanRequestMsg:
		prompt := tuiui.NewPromptDialog(msg.RequestID, "Agent needs your input", msg.Question)
		if len(msg.Options) > 0 {
			prompt.WithOptions(msg.Options)
		}
		m.dialogStack.Push(prompt, nil)
		m.dialogStack.SetSize(m.width, m.height)
		return m, nil

	case tuiui.PromptResultMsg:
		m.dialogStack.Pop()
		if strings.HasPrefix(msg.DialogID, "provider-") {
			return m.handleProviderPromptResult(msg)
		}
		// Durable HIL: resolving the pending interrupt resumes the same session via V2 snapshot/events.
		if m.pendingInterrupt != nil && msg.DialogID == m.pendingInterrupt.InterruptID && m.agentCtx != nil {
			m.agentRunning = true
			m.hadStreamDuringRun = false
			m.hadStreamByAgent = nil
			m.hadFinalAnswerDuringRun = false
			m.externalInterrupt = nil
			m.thinkingPanel.Reset()
			m.clearRetryState()
			m.runStartTime = time.Now()
			m.turnStartUsage = m.sessionUsage
			m.turnStartCost = m.sessionCost
			m.statusText = "thinking..."
			m.input.SetEnabled(false)
			m.refreshSidebarData()
			spinnerCmd := m.spinner.Start()

			interruptID := m.pendingInterrupt.InterruptID
			m.pendingInterrupt = nil

			if msg.Cancelled {
				return m, tea.Batch(m.agentCtx.CancelInterruptCmd(interruptID), spinnerCmd)
			}
			return m, tea.Batch(m.agentCtx.ResolveInterruptCmd(interruptID, msg.Value), spinnerCmd)
		}
		if m.humanBridge != nil {
			if msg.Cancelled {
				m.humanBridge.Cancel(msg.DialogID)
			} else {
				m.humanBridge.Respond(msg.DialogID, msg.Value)
			}
		}
		return m, nil

	case tuiui.AlertDismissedMsg:
		m.dialogStack.Pop()
		return m, nil

	case tuiui.ConfirmResultMsg:
		m.dialogStack.Pop()
		return m, nil

	case tuiui.SelectResultMsg:
		m.dialogStack.Pop()
		if msg.Cancelled {
			if msg.DialogID == "provider-select" || msg.DialogID == "onboarding-model-select" {
				m.pendingPrompt = ""
			}
			return m, nil
		}
		switch msg.DialogID {
		case "agent-select":
			if m.profileRegistry != nil {
				profiles := m.profileRegistry.List()
				if msg.Index >= 0 && msg.Index < len(profiles) {
					return m, func() tea.Msg { return AgentSwitchMsg{Definition: profiles[msg.Index]} }
				}
			}
		case "model-select":
			if msg.Value != "" {
				return m, func() tea.Msg { return ModelSwitchMsg{ModelID: msg.Value} }
			}
		case "onboarding-model-select":
			if msg.Value != "" {
				m.providerCfg.ModelID = msg.Value
				if m.agentCtx != nil {
					m.agentCtx.Definition.ModelID = msg.Value
				}
				m.localProvider.SetPreferredModel(m.providerCfg.Name, msg.Value)
				m.rememberRecentModel(msg.Value)
				m.persistSessionMeta()
				m.refreshSidebarData()
				m.statusText = fmt.Sprintf("model: %s", msg.Value)
			}
			if m.pendingPrompt != "" {
				prompt := m.pendingPrompt
				m.pendingPrompt = ""
				return m, func() tea.Msg { return UserSubmitMsg{Text: prompt} }
			}
		case "session-select":
			if msg.Value != "" {
				return m, func() tea.Msg { return SessionSwitchMsg{SessionID: msg.Value} }
			}
		case "mode-select":
			if msg.Value != "" {
				if mode, ok := parsePermissionModeArg(msg.Value); ok {
					return m.applyPermissionMode(mode)
				}
			}
		case "theme-select":
			if msg.Value != "" {
				m.themeProvider.SetByName(msg.Value)
				m.sessionMeta.Theme = m.themeProvider.Get().Name
				m.localProvider.SetThemeName(m.themeProvider.Get().Name)
				m.persistSessionMeta()
				m.statusText = fmt.Sprintf("theme: %s", m.themeProvider.Get().Name)
			}
		case "skill-select":
			if msg.Value != "" {
				active := false
				if set := m.effectiveActiveSkillSet(); set != nil {
					_, active = set[msg.Value]
				}
				m.toggleSessionSkill(msg.Value, !active)
				return m.openSkillSelector()
			}
		case "mcp-select":
			if msg.Value != "" {
				active := stringsContains(m.desiredMCPNames(), msg.Value)
				m.toggleSessionMCP(msg.Value, !active)
				return m.openMCPSelector()
			}
		case "provider-select":
			if msg.Value != "" {
				cfgP := m.appCfg.Providers[msg.Value]
				cfgKey := ""
				if cfgP != nil {
					cfgKey = cfgP.APIKey
				}
				apiKey := m.registry.ResolveAPIKey(msg.Value, cfgKey)
				envVar := m.registry.ProviderEnvVar(msg.Value)
				if apiKey == "" && envVar != "" {
					pInfo, _ := m.registry.GetProvider(msg.Value)
					pName := msg.Value
					if pInfo != nil {
						pName = pInfo.Name
					}
					m.pendingProvider = &pendingProviderSetup{ProviderID: msg.Value}
					prompt := tuiui.NewPromptDialog(
						"provider-apikey:"+msg.Value,
						fmt.Sprintf("Configure %s", pName),
						fmt.Sprintf("Enter your %s API key:", pName),
					).WithMasked().WithPlaceholder("sk-...")
					m.dialogStack.Push(prompt, nil)
					m.dialogStack.SetSize(m.width, m.height)
					return m, nil
				}
				return m, func() tea.Msg { return ProviderSwitchMsg{Name: msg.Value} }
			}
		}
		return m, nil

	case EnterSubAgentMsg:
		if m.chat.EnterChild(msg.CallID) {
			m.refreshSidebarData()
			m.setFocus(FocusChat)
		}
		return m, nil

	case clipboardCopiedMsg:
		if msg.text != "" {
			return m, m.toastManager.Push("copied to clipboard", tuiui.ToastInfo, 2*time.Second)
		}
		return m, nil

	case pasteResultMsg:
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		m.syncInputLayout()
		return m, cmd

	case tuiui.ToastExpiredMsg:
		if m.toastManager != nil {
			m.toastManager.Remove(msg.ID)
		}
		return m, nil

	case CommandPickerRequestMsg:
		m.commandPicker = tuiui.NewCommandPickerModel(slashCommands, m.width)
		return m, nil

	case tuiui.CommandPickerResultMsg:
		m.commandPicker = nil
		if !msg.Cancelled && msg.Command != "" {
			m.input.Clear()
			m.syncInputLayout()
			return m.handleSlashCommand(msg.Command)
		}
		return m, m.input.Focus()

	case FilePickerRequestMsg:
		wd, _ := os.Getwd()
		m.filePicker = tuiui.NewFilePickerModel(wd, m.width)
		return m, nil

	case tuiui.FilePickerResultMsg:
		m.filePicker = nil
		if !msg.Cancelled && msg.Path != "" {
			m.input.SetValue("@" + msg.Path + " ")
		} else {
			m.input.Clear()
		}
		m.syncInputLayout()
		return m, m.input.Focus()

	case SlashCommandMsg:
		return m.handleSlashCommand(msg.Command)

	case AgentSwitchMsg:
		if m.agentCtx == nil {
			return m, nil
		}
		m.agentCtx.Definition = msg.Definition
		m.chat.rootAgentName = msg.Definition.Name
		m.localProvider.SetPreferredAgent(msg.Definition.Name)
		if m.sessionMeta.PermissionMode != "" {
			if mode, ok := parsePermissionModeArg(m.sessionMeta.PermissionMode); ok {
				if m.agentCtx.Definition.Policies.BashPermissionContext != nil &&
					m.agentCtx.Definition.Policies.BashPermissionContext.PermCtx != nil {
					m.agentCtx.Definition.Policies.BashPermissionContext.PermCtx.Mode = mode
				}
			}
		}
		m.statusText = fmt.Sprintf("switched to %s", msg.Definition.Name)
		if m.currentSessionID != "" {
			m.updateSessionAgent(msg.Definition.Name)
		}
		m.applySessionRuntimeState()
		m.refreshSidebarData()
		return m, m.input.Focus()

	case ProviderSwitchMsg:
		if m.appCfg == nil || m.providerCfg == nil {
			return m, nil
		}

		state := m.appCfg.ResolveProviderState(msg.Name, "", "", "", m.registry, m.credStore)
		if state == nil {
			m.chat.AddPart(DisplayPart{Type: PartTypeSystem, Time: time.Now(), System: &SystemPart{Content: "unknown provider: " + msg.Name}})
			return m, nil
		}
		envVar := m.registry.ProviderEnvVar(msg.Name)
		if state.APIKey == "" && envVar != "" {
			pInfo, _ := m.registry.GetProvider(msg.Name)
			pName := msg.Name
			if pInfo != nil {
				pName = pInfo.Name
			}
			m.chat.AddPart(DisplayPart{Type: PartTypeSystem, Time: time.Now(), System: &SystemPart{Content: fmt.Sprintf("provider %s requires %s to be set", pName, envVar)}})
			return m, nil
		}

		*m.providerCfg = *state

		if preferred := m.localProvider.PreferredModelForProvider(msg.Name); preferred != "" {
			var cfgVariants map[string]map[string]any
			if pc := m.appCfg.Providers[msg.Name]; pc != nil {
				cfgVariants = pc.Variants
			}
			base, variant, vopts := ParseModelVariant(preferred, m.registry, msg.Name, cfgVariants)
			m.providerCfg.ModelID = base
			m.providerCfg.Variant = variant
			m.providerCfg.VariantOptions = vopts
		}

		if m.agentCtx != nil {
			m.agentCtx.Definition.ModelID = m.providerCfg.ModelID
			if m.agentCtx.RebuildClient != nil {
				m.agentCtx.RebuildClient(m.providerCfg)
			}
		}

		m.appCfg.DefaultProvider = msg.Name
		if err := SaveConfig(DefaultConfigPath(), func(cfg *AppConfig) {
			cfg.DefaultProvider = msg.Name
			if cfg.Providers == nil {
				cfg.Providers = make(map[string]*ProviderConfig)
			}
			if _, exists := cfg.Providers[msg.Name]; !exists {
				if rp, ok := m.registry.GetProvider(msg.Name); ok {
					pc := &ProviderConfig{BaseURL: rp.BaseURL}
					if len(rp.EnvVars) > 0 {
						pc.APIKey = fmt.Sprintf("${%s}", rp.EnvVars[0])
					}
					cfg.Providers[msg.Name] = pc
				}
			}
		}); err != nil {
			m.chat.AddPart(DisplayPart{Type: PartTypeSystem, Time: time.Now(), System: &SystemPart{Content: fmt.Sprintf("provider switched but config save failed: %v", err)}})
		}

		if m.appCfg.Providers == nil {
			m.appCfg.Providers = make(map[string]*ProviderConfig)
		}
		if _, exists := m.appCfg.Providers[msg.Name]; !exists {
			m.appCfg.Providers[msg.Name] = &ProviderConfig{
				BaseURL:      state.BaseURL,
				DefaultModel: state.ModelID,
			}
		}

		m.rememberRecentModel(m.providerCfg.ModelID)
		m.persistSessionMeta()
		m.refreshSidebarData()
		m.statusText = fmt.Sprintf("provider: %s (model: %s)", msg.Name, m.providerCfg.ModelID)

		if m.pendingPrompt != "" {
			return m.openModelSelectorAfterProviderSetup()
		}
		return m, nil

	case StatusTextMsg:
		m.statusText = msg.Text
		return m, nil

	case MultiProviderModelPickerLoadedMsg:
		totalModels := 0
		for _, models := range msg.ModelsByProvider {
			totalModels += len(models)
		}
		if totalModels == 0 {
			m.chat.AddPart(DisplayPart{Type: PartTypeSystem, Time: time.Now(), System: &SystemPart{Content: "no models available from configured providers"}})
			m.statusText = "no models"
			return m, nil
		}
		currentProviderID := ""
		currentModelID := ""
		if m.providerCfg != nil {
			currentProviderID = m.providerCfg.Name
			currentModelID = m.providerCfg.ModelID
		}
		prefs := m.localProvider.Get()
		providerOrder := m.configuredProviderIDs()
		options := buildMultiProviderModelSelectOptions(
			msg.ModelsByProvider,
			providerOrder,
			currentProviderID, currentModelID,
			prefs.FavoriteModels, prefs.RecentModels,
		)
		dialog := tuiui.NewSelectDialog("model-select", "Select Model", options)
		dialog.OnFavoriteToggle = func(value string) {
			pid, mid := decodeModelValue(value)
			if pid == "" {
				pid = currentProviderID
			}
			m.localProvider.ToggleFavoriteModel(pid, mid)
		}
		m.dialogStack.Push(dialog, nil)
		m.dialogStack.SetSize(m.width, m.height)
		m.statusText = "select model"
		return m, nil

	case ModelPickerFailedMsg:
		if msg.Err != nil {
			m.chat.AddPart(DisplayPart{Type: PartTypeSystem, Time: time.Now(), System: &SystemPart{Content: fmt.Sprintf("load models failed: %v\nUsage: /model <model_id>", msg.Err)}})
			m.statusText = "model list failed"
		}
		return m, nil

	case SessionSwitchMsg:
		m.switchSession(msg.SessionID)
		return m, m.input.Focus()

	case SessionCreateMsg:
		if !m.agentRunning {
			if !m.newSession() {
				return m, nil
			}
			m.statusText = fmt.Sprintf("new session: %s", shortSessionID(m.currentSessionID))
			return m, tea.Batch(m.input.Focus(), m.refreshSidebarCmd())
		}
		return m, nil

	case SkillToggleMsg:
		m.toggleSessionSkill(msg.Name, msg.Enabled)
		return m, nil

	case MCPToggleMsg:
		m.toggleSessionMCP(msg.Name, msg.Connect)
		return m, nil

	case RefreshSidebarMsg:
		m.refreshSidebarData()
		return m, nil

	case ThemeToggleMsg:
		m.themeProvider.Toggle()
		m.sessionMeta.Theme = m.themeProvider.Get().Name
		m.localProvider.SetThemeName(m.themeProvider.Get().Name)
		m.persistSessionMeta()
		return m, m.toastManager.Push(fmt.Sprintf("theme: %s", m.themeProvider.Get().Name), tuiui.ToastInfo, 2*time.Second)

	case ModelSwitchMsg:
		if m.providerCfg == nil || msg.ModelID == "" {
			return m, nil
		}

		targetProvider, rawModelID := decodeModelValue(msg.ModelID)
		if targetProvider == "" {
			targetProvider = m.providerCfg.Name
		}

		if targetProvider != m.providerCfg.Name {
			if err := m.switchProviderInline(targetProvider); err != nil {
				m.chat.AddPart(DisplayPart{Type: PartTypeSystem, Time: time.Now(), System: &SystemPart{Content: fmt.Sprintf("provider switch failed: %v", err)}})
				return m, nil
			}
		}

		var cfgVariants map[string]map[string]any
		if m.appCfg != nil {
			if pc := m.appCfg.Providers[m.providerCfg.Name]; pc != nil {
				cfgVariants = pc.Variants
			}
		}
		baseModel, variant, variantOpts := ParseModelVariant(rawModelID, m.registry, m.providerCfg.Name, cfgVariants)
		m.providerCfg.ModelID = baseModel
		m.providerCfg.Variant = variant
		m.providerCfg.VariantOptions = variantOpts
		if m.agentCtx != nil {
			m.agentCtx.Definition.ModelID = baseModel
		}
		m.localProvider.SetPreferredModel(m.providerCfg.Name, rawModelID)
		m.rememberRecentModel(rawModelID)
		m.persistSessionMeta()
		m.refreshSidebarData()
		label := baseModel
		if variant != "" {
			label += " (" + variant + ")"
		}
		m.statusText = fmt.Sprintf("model: %s", label)
		return m, nil

	case BatchedEventsMsg:
		m.handleBatchedEvents(msg.Events)
		return m, nil
	}

	if m.spinner != nil {
		if cmd := m.spinner.Update(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	return m, tea.Batch(cmds...)
}

func (m *Model) switchProviderInline(providerID string) error {
	state := m.appCfg.ResolveProviderState(providerID, "", "", "", m.registry, m.credStore)
	if state == nil {
		return fmt.Errorf("unknown provider: %s", providerID)
	}
	envVar := m.registry.ProviderEnvVar(providerID)
	if state.APIKey == "" && envVar != "" {
		return fmt.Errorf("provider %s requires %s", providerID, envVar)
	}

	*m.providerCfg = *state

	if m.agentCtx != nil && m.agentCtx.RebuildClient != nil {
		m.agentCtx.RebuildClient(m.providerCfg)
	}

	m.appCfg.DefaultProvider = providerID
	_ = SaveConfig(DefaultConfigPath(), func(cfg *AppConfig) {
		cfg.DefaultProvider = providerID
	})

	return nil
}

// --- Focus Management ---

func (m *Model) cycleFocus() {
	switch m.focus {
	case FocusInput:
		if m.sidebarVisible() {
			m.setFocus(FocusSidebar)
		} else if m.subAgentPanelVisible() {
			m.setFocus(FocusSubAgents)
		} else {
			m.setFocus(FocusChat)
		}
	case FocusSidebar:
		if m.subAgentPanelVisible() {
			m.setFocus(FocusSubAgents)
		} else {
			m.setFocus(FocusChat)
		}
	case FocusSubAgents:
		m.setFocus(FocusChat)
	case FocusChat:
		m.setFocus(FocusInput)
	default:
		m.setFocus(FocusInput)
	}
}

func (m *Model) setFocus(target FocusTarget) {
	if target == FocusSidebar && !m.sidebarVisible() {
		target = FocusChat
	}
	if target == FocusSubAgents && !m.subAgentPanelVisible() {
		target = FocusChat
	}
	m.focus = target
	m.sidebar.SetFocused(target == FocusSidebar)
	m.subAgentPanel.SetFocused(target == FocusSubAgents)
	m.chat.SetFocused(target == FocusChat)
	m.input.SetEnabled(target == FocusInput)
}

func (m *Model) focusCmd() tea.Cmd {
	if m.focus == FocusInput {
		return m.input.Focus()
	}
	return nil
}

// --- Sidebar ---

func (m *Model) refreshSidebarData() {
	snap := m.buildSidebarSnapshot()
	m.sidebar.SetSnapshot(snap)

	if m.mcpManager != nil {
		entries := m.mcpManager.ServerEntries()
		total := len(entries)
		connected := 0
		for _, e := range entries {
			if e != nil && e.Status == mcp.MCPStatusConnected {
				connected++
			}
		}
		m.footer.SetMCPStatus(total, connected)
	}
}

func (m *Model) buildSidebarSnapshot() SidebarSnapshot {
	snap := SidebarSnapshot{
		TokenCount:       m.formatTokenCount(),
		CostEstimate:     m.formatCostEstimate(),
		RunStatus:        "idle",
		InputTokens:      formatTokenValue(m.sessionUsage.InputTokens),
		OutputTokens:     formatTokenValue(m.sessionUsage.OutputTokens),
		ReasoningTokens:  formatTokenValue(m.sessionUsage.ReasoningTokens),
		CacheReadTokens:  formatTokenValue(m.sessionUsage.CacheReadTokens),
		CacheWriteTokens: formatTokenValue(m.sessionUsage.CacheWriteTokens),
	}

	if m.currentSessionID != "" && m.store != nil {
		if m.sessionMaterialized {
			if rec, err := m.store.Get(m.currentSessionID); err == nil {
				snap.SessionTitle = rec.Title
				snap.AgentName = rec.AgentName
			}
		} else if m.agentCtx != nil {
			snap.AgentName = m.agentCtx.Definition.Name
		}
	}

	if m.providerCfg != nil {
		snap.ProviderName = m.providerCfg.Name
		snap.ModelID = m.providerCfg.ModelID
		snap.HasProvider = m.providerCfg.Name != ""
	}

	if m.agentRunning {
		snap.RunStatus = "running"
	}

	if m.mcpManager != nil {
		desiredMCPs := m.desiredMCPNames()
		desiredSet := make(map[string]struct{}, len(desiredMCPs))
		for _, name := range desiredMCPs {
			if name == "" {
				continue
			}
			desiredSet[name] = struct{}{}
		}

		entries := m.mcpManager.ServerEntries()
		byName := make(map[string]*mcp.MCPServerEntry, len(entries))
		for _, e := range entries {
			if e == nil {
				continue
			}
			byName[strings.TrimSpace(e.Name)] = e
		}

		for _, e := range entries {
			if e == nil {
				continue
			}
			_, active := desiredSet[e.Name]
			snap.MCPServers = append(snap.MCPServers, MCPStatusEntry{
				Name:      e.Name,
				Status:    string(e.Status),
				ToolCount: e.ToolCount,
				Active:    active,
			})
		}

		// Active: show only desired MCPs that are connected.
		for _, name := range desiredMCPs {
			entry := byName[name]
			if entry == nil || entry.Status != mcp.MCPStatusConnected {
				continue
			}
			snap.ActiveMCPs = append(snap.ActiveMCPs, name)
		}
	}

	latestPlans := map[string]*PlanPart{}
	for _, p := range m.chat.Parts() {
		if p.Type == PartTypePlan && p.Plan != nil {
			latestPlans[p.Plan.AgentName] = p.Plan
		}
	}
	childrenByParentStep := map[string][]*PlanPart{}
	var rootPlan *PlanPart
	for _, plan := range latestPlans {
		if m.chat.isRootAgentPlan(plan) {
			rootPlan = plan
		} else {
			childrenByParentStep[plan.ParentStepID] = append(childrenByParentStep[plan.ParentStepID], plan)
		}
	}
	for _, children := range childrenByParentStep {
		sort.Slice(children, func(i, j int) bool {
			return children[i].AgentName < children[j].AgentName
		})
	}
	// Collect IDs and normalized step texts from all child plans
	// so we can deduplicate when the root plan replans and copies sub-agent items.
	childItemIDs := map[string]bool{}
	childStepNorm := map[string]bool{}
	for _, children := range childrenByParentStep {
		for _, childPlan := range children {
			for _, item := range childPlan.Items {
				if item.ID != "" {
					childItemIDs[item.ID] = true
				}
				childStepNorm[normalizeStepText(item.Step)] = true
			}
		}
	}
	visited := map[string]bool{}
	var flattenPlan func(plan *PlanPart, depth int, dedup bool)
	flattenPlan = func(plan *PlanPart, depth int, dedup bool) {
		agentLabel := ""
		if depth > 0 {
			agentLabel = plan.AgentName
		}
		for _, item := range plan.Items {
			if dedup && len(childrenByParentStep[item.ID]) == 0 {
				if childItemIDs[item.ID] || childStepNorm[normalizeStepText(item.Step)] {
					continue
				}
			}
			item.Depth = depth
			item.AgentName = agentLabel
			snap.PlanItems = append(snap.PlanItems, item)
			for _, childPlan := range childrenByParentStep[item.ID] {
				if !visited[childPlan.AgentName] {
					visited[childPlan.AgentName] = true
					flattenPlan(childPlan, depth+1, false)
				}
			}
		}
	}
	if childCallID := m.chat.ViewingChild(); childCallID != "" {
		// 下钻中：只扁平化该子 agent 的整棵子树（depth 从 0 起，不对 root 去重）。
		if childPlan := m.chat.PlanForChild(childCallID); childPlan != nil {
			flattenPlan(childPlan, 0, false)
		}
	} else if rootPlan != nil {
		flattenPlan(rootPlan, 0, true)
		for _, orphan := range childrenByParentStep[""] {
			flattenPlan(orphan, 1, false)
		}
	} else {
		for _, plan := range latestPlans {
			flattenPlan(plan, 0, false)
		}
	}

	snap.ActiveSkills = m.effectiveActiveSkillNames()
	snap.DismissedGettingStarted = m.localProvider.Get().DismissedGettingStarted
	snap.Workdir = m.footer.Workdir()

	if m.currentVersion != "" {
		snap.Version = m.currentVersion
	}
	if m.updateChecker != nil && m.updateChecker.IsUpdateAvailable() {
		if rel := m.updateChecker.Latest(); rel != nil {
			snap.UpdateAvailable = rel.TagName
		}
	}

	return snap
}

func normalizeStepText(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}

func formatTokenValue(count int) string {
	if count <= 0 {
		return ""
	}
	switch {
	case count >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(count)/1_000_000)
	case count >= 1_000:
		return fmt.Sprintf("%.1fk", float64(count)/1_000)
	default:
		return strconv.Itoa(count)
	}
}

func (m *Model) formatTokenCount() string {
	total := m.sessionUsage.ContextCountTokens()
	v := formatTokenValue(total)
	if v == "" {
		return "--"
	}
	return v
}

func formatCostUSD(cost float64) string {
	if cost <= 0 {
		return "--"
	}
	if cost < 0.01 {
		return fmt.Sprintf("$%.4f", cost)
	}
	return fmt.Sprintf("$%.2f", cost)
}

func (m *Model) formatCostEstimate() string {
	return formatCostUSD(m.sessionCost)
}

func (m *Model) recalcUsageFromHistory(history []*ai.MsgInfo) {
	var merged ai.TokenUsage
	for _, msg := range history {
		if msg == nil || msg.Usage == nil {
			continue
		}
		merged.InputTokens += msg.Usage.InputTokens
		merged.OutputTokens += msg.Usage.OutputTokens
		merged.ReasoningTokens += msg.Usage.ReasoningTokens
		merged.CacheReadTokens += msg.Usage.CacheReadTokens
		merged.CacheWriteTokens += msg.Usage.CacheWriteTokens
	}
	merged.NormalizeInPlace()

	// history (skeleton) 中的消息可能不携带 Usage（Usage 在 stepHistory 上），
	// 此时 merged 全零。如果 StepFinish 事件已累加了有效值，不要用零覆盖。
	if merged.ContextCountTokens() == 0 && m.sessionUsage.ContextCountTokens() > 0 {
		if m.sessionCost <= 0 {
			pricing := aiusage.PricingModel{}
			if m.agentCtx != nil && m.agentCtx.Factory != nil {
				if client := m.agentCtx.Factory.DefaultClient(); client != nil {
					type pricingProvider interface {
						UsagePricingModel() aiusage.PricingModel
					}
					if pp, ok := client.(pricingProvider); ok {
						pricing = pp.UsagePricingModel()
					}
				}
			}
			if result := aiusage.Summarize(pricing, &m.sessionUsage); result.Cost > 0 {
				m.sessionCost = result.Cost
			}
		}
		return
	}

	m.sessionUsage = merged

	pricing := aiusage.PricingModel{}
	if m.agentCtx != nil && m.agentCtx.Factory != nil {
		if client := m.agentCtx.Factory.DefaultClient(); client != nil {
			type pricingProvider interface {
				UsagePricingModel() aiusage.PricingModel
			}
			if pp, ok := client.(pricingProvider); ok {
				pricing = pp.UsagePricingModel()
			}
		}
	}
	result := aiusage.Summarize(pricing, &merged)
	m.sessionCost = result.Cost
}

func (m *Model) refreshSidebarCmd() tea.Cmd {
	return func() tea.Msg { return RefreshSidebarMsg{} }
}

// logMCPError 在某个 MCP 进入 error 状态时向 chat 写一次错误（按错误文本去重）。
func (m *Model) logMCPError(name string) {
	if m == nil || m.mcpManager == nil {
		return
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}

	errText := ""
	for _, e := range m.mcpManager.ServerEntries() {
		if e != nil && strings.TrimSpace(e.Name) == name {
			errText = strings.TrimSpace(e.Error)
			break
		}
	}

	if m.mcpLastLogged == nil {
		m.mcpLastLogged = make(map[string]string)
	}
	logKey := "error:" + errText
	if m.mcpLastLogged[name] == logKey {
		return
	}
	m.mcpLastLogged[name] = logKey

	msg := fmt.Sprintf("MCP %q error (try /mcp list)", name)
	if errText != "" {
		msg = fmt.Sprintf("MCP %q error: %s (try /mcp list)", name, truncateOneLine(errText, 200))
	}
	m.chat.AddPart(DisplayPart{Type: PartTypeSystem, Time: time.Now(), System: &SystemPart{Content: msg}})
}

// --- Layout ---

// subAgentPanelVisible reports whether the right-side sub-agent panel should
// render: only while at least one sub-agent is still running.
func (m *Model) subAgentPanelVisible() bool {
	return m.chat.HasRunningSubAgents()
}

// refreshSubAgentPanel rebuilds the panel's snapshot from the current sub-agent
// cards (timeline order), keeping live elapsed times up to date.
func (m *Model) refreshSubAgentPanel() {
	sums := m.chat.SubAgentSummaries()
	items := make([]subAgentPanelItem, 0, len(sums))
	for i := range sums {
		sa := sums[i]
		title := sa.AgentName
		if title == "" {
			title = "sub_agent"
		}
		items = append(items, subAgentPanelItem{
			CallID:      sa.CallID,
			Title:       title,
			Description: sa.Description,
			Status:      sa.Status,
			Elapsed:     subAgentElapsed(&sa),
			Running:     sa.Status == "running",
		})
	}
	m.subAgentPanel.SetSnapshot(items)
}

func (m *Model) sidebarVisible() bool {
	mode := m.localProvider.Get().SidebarMode
	switch mode {
	case "hide":
		return false
	case "show":
		return true
	default:
		return m.width >= 100
	}
}

func (m *Model) toggleSidebar() {
	pref := m.localProvider.Get()
	switch pref.SidebarMode {
	case "hide":
		pref.SidebarMode = "show"
	case "show":
		pref.SidebarMode = "hide"
	default:
		if m.sidebarVisible() {
			pref.SidebarMode = "hide"
		} else {
			pref.SidebarMode = "show"
		}
	}
	m.localProvider.Set(pref)
	m.updateLayout()
	m.refreshSidebarData()
}

func (m *Model) inlineLoading() bool {
	return m.agentRunning && m.spinner != nil && m.spinner.IsVisible()
}

func (m *Model) pickerHeight(chatWidth int) int {
	if chatWidth < 1 {
		chatWidth = 1
	}
	if m.commandPicker != nil {
		m.commandPicker.SetWidth(chatWidth)
		return m.commandPicker.Height()
	}
	if m.filePicker != nil {
		m.filePicker.SetWidth(chatWidth)
		return m.filePicker.Height()
	}
	return 0
}

func (m *Model) clearRetryState() {
	m.retryState = nil
}

func (m Model) handleQuitRequest(key string) (tea.Model, tea.Cmd) {
	if m.exitProvider.RequestQuit(key) {
		if !m.sessionMaterialized {
			m.cleanupUnmaterializedSession()
		} else {
			m.persistCurrentSession()
		}
		return m, tea.Quit
	}
	m.statusText = fmt.Sprintf("press %s again to quit", key)
	return m, nil
}

func (m *Model) loadingLabel(maxWidth int) string {
	if m.retryState != nil {
		label := formatRetryLabel(m.retryState, maxWidth)
		if label != "" {
			return label
		}
	}
	label := strings.TrimSpace(m.statusText)
	if label == "" || label == "ready" {
		return "thinking..."
	}
	return truncateDisplayWidth(label, maxWidth)
}

func formatRetryLabel(state *retryState, maxWidth int) string {
	if state == nil {
		return ""
	}
	message := strings.TrimSpace(state.Message)
	if message == "" {
		message = "Retrying"
	}
	suffix := fmt.Sprintf(" [retrying attempt #%d]", state.Attempt)
	if remaining := time.Until(state.Next); remaining > 0 {
		seconds := int((remaining + time.Second - 1) / time.Second)
		if seconds > 0 {
			suffix = fmt.Sprintf(" [retrying in %ds attempt #%d]", seconds, state.Attempt)
		}
	}
	if maxWidth <= 0 {
		return message + suffix
	}
	suffixWidth := runewidth.StringWidth(suffix)
	if suffixWidth >= maxWidth {
		return truncateDisplayWidth(strings.TrimSpace(message+suffix), maxWidth)
	}
	message = truncateDisplayWidth(message, maxWidth-suffixWidth)
	return message + suffix
}

func truncateDisplayWidth(s string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}
	if runewidth.StringWidth(s) <= maxWidth {
		return s
	}
	if maxWidth == 1 {
		return "…"
	}

	var b strings.Builder
	width := 0
	for _, r := range s {
		rw := runewidth.RuneWidth(r)
		if width+rw > maxWidth-1 {
			break
		}
		b.WriteRune(r)
		width += rw
	}
	return b.String() + "…"
}

func (m *Model) updateLayout() {
	if m.width == 0 || m.height == 0 {
		return
	}
	footerHeight := 1
	inputLines := m.input.DesiredHeight()
	inputHeight := inputLines + 2
	if inputHeight < 3 {
		inputHeight = 3
	}

	sbWidth := 0
	if m.sidebarVisible() {
		sbWidth = sidebarWidth + 1
	}

	sapWidth := 0
	if m.subAgentPanelVisible() {
		sapWidth = subAgentPanelWidth + 1
	}

	chatWidth := m.width - sbWidth - sapWidth
	if chatWidth < 1 {
		chatWidth = 1
	}

	mainHeight := m.height - footerHeight
	if mainHeight < 1 {
		mainHeight = 1
	}

	pickerHeight := m.pickerHeight(chatWidth)
	panelHeight := m.thinkingPanel.Height()
	chatHeight := mainHeight - inputHeight - pickerHeight - panelHeight
	if chatHeight < 1 {
		chatHeight = 1
	}

	contentWidth := chatWidth - 2
	if contentWidth < 1 {
		contentWidth = 1
	}

	m.layoutChatWidth = chatWidth
	m.layoutChatHeight = chatHeight
	m.layoutMainHeight = mainHeight
	m.layoutInputHeight = inputHeight
	m.layoutContentWidth = contentWidth

	if m.sidebarVisible() {
		m.sidebar.SetSize(sidebarWidth, mainHeight)
	}
	if m.subAgentPanelVisible() {
		m.subAgentPanel.SetSize(subAgentPanelWidth, mainHeight)
		m.refreshSubAgentPanel()
	}
	m.chat.SetSize(contentWidth, chatHeight)
	m.thinkingPanel.SetWidth(contentWidth)
	m.input.SetWidth(contentWidth)
	m.input.SetHeight(inputLines)
	m.footer.SetWidth(m.width)
	m.dialogStack.SetSize(m.width, m.height)
}

func (m *Model) syncInputLayout() {
	newH := m.input.DesiredHeight() + 2
	if newH < 3 {
		newH = 3
	}
	if newH != m.layoutInputHeight {
		m.updateLayout()
	}
}

// --- View ---

func (m Model) View() string {
	if m.width == 0 {
		return "Loading..."
	}

	if !m.dialogStack.IsEmpty() {
		return m.dialogStack.View()
	}

	th := m.themeProvider.Get()

	chatWidth := m.layoutChatWidth
	chatHeight := m.layoutChatHeight
	mainHeight := m.layoutMainHeight
	inputHeight := m.layoutInputHeight
	chatContentWidth := m.layoutContentWidth

	chatBorderColor := th.BorderColor
	if m.focus == FocusChat {
		chatBorderColor = th.FocusBorderColor
	}
	_ = chatBorderColor

	chatStyle := lipgloss.NewStyle().
		Width(chatWidth).
		Height(chatHeight).
		Padding(0, 1)

	chatContent := m.chat.ViewWithSelection(&m.selection)
	if !m.chat.HasContent() {
		chatContent = m.renderHomeView(chatContentWidth, chatHeight)
	}
	chatView := chatStyle.Render(chatContent)

	inputBorderColor := th.BorderColor
	if m.focus == FocusInput {
		inputBorderColor = th.FocusBorderColor
	}

	inputStyle := lipgloss.NewStyle().
		Width(chatWidth).
		Height(inputHeight-1).
		BorderStyle(lipgloss.NormalBorder()).
		BorderTop(true).
		BorderForeground(inputBorderColor).
		Padding(0, 1)

	inlineLoading := m.inlineLoading()
	var inputContent string
	if inlineLoading {
		inputContentWidth := chatWidth - 4
		if inputContentWidth < 1 {
			inputContentWidth = 1
		}
		m.spinner.SetLabel(m.loadingLabel(inputContentWidth))
		inputContent = m.spinner.View()
	} else {
		inputContent = m.input.View()
	}
	inputView := inputStyle.Render(inputContent)

	pickerView := ""
	if m.commandPicker != nil {
		pickerView = m.commandPicker.View()
	} else if m.filePicker != nil {
		pickerView = m.filePicker.View()
	}

	var leftPane string
	panelView := m.thinkingPanel.View()
	if pickerView != "" {
		if panelView != "" {
			leftPane = lipgloss.JoinVertical(lipgloss.Left, chatView, panelView, pickerView, inputView)
		} else {
			leftPane = lipgloss.JoinVertical(lipgloss.Left, chatView, pickerView, inputView)
		}
	} else {
		if panelView != "" {
			leftPane = lipgloss.JoinVertical(lipgloss.Left, chatView, panelView, inputView)
		} else {
			leftPane = lipgloss.JoinVertical(lipgloss.Left, chatView, inputView)
		}
	}

	cols := []string{leftPane}
	if m.subAgentPanelVisible() {
		(&m).refreshSubAgentPanel()
		if v := m.subAgentPanel.View(); v != "" {
			cols = append(cols, v)
		}
	}
	if m.sidebarVisible() {
		if v := m.sidebar.View(); v != "" {
			cols = append(cols, v)
		}
	}
	mainArea := lipgloss.JoinHorizontal(lipgloss.Top, cols...)
	mainArea = lipgloss.NewStyle().Width(m.width).Height(mainHeight).Render(mainArea)

	spinnerView := ""
	statusText := m.statusText
	if inlineLoading {
		statusText = ""
	} else if m.spinner != nil && m.spinner.IsVisible() {
		spinnerView = m.spinner.View()
	}
	focusHint := ""
	switch m.focus {
	case FocusInput:
		focusHint = "Tab 切换焦点"
	case FocusSidebar:
		focusHint = "侧栏 · ↑↓ 选择 · Tab 切换"
	case FocusSubAgents:
		focusHint = "子Agent · ↑↓ 选择 · Enter 进入 · Tab 切换"
	case FocusChat:
		focusHint = "聊天 · Tab 切换"
	}
	if m.chat.ViewingChild() != "" {
		focusHint = "子Agent 详情 · ↑↓ 滚动 · ← 返回"
	}

	m.footer.SetModeIndicator(string(m.currentPermissionMode()))
	m.footer.SetSidebarShown(m.sidebarVisible())
	m.footer.SetStatus(statusText, spinnerView, focusHint)
	footerView := m.footer.View(th)

	if m.toastManager != nil && !m.toastManager.IsEmpty() {
		m.toastManager.SetWidth(m.width)
		toastView := m.toastManager.View()
		return lipgloss.NewStyle().
			Width(m.width).
			Height(m.height).
			Render(lipgloss.JoinVertical(lipgloss.Left, mainArea, toastView, footerView))
	}
	return lipgloss.NewStyle().
		Width(m.width).
		Height(m.height).
		Render(lipgloss.JoinVertical(lipgloss.Left, mainArea, footerView))
}

var fileRefPattern = regexp.MustCompile(`@([\w./_-]+[\w./_-])(?:#(\d+)(?:-(\d+))?)?`)

type fileRef struct {
	Path      string
	StartLine int // 0 means "from beginning"
	EndLine   int // 0 means "to end"
}

func extractFileRefs(text string) []fileRef {
	matches := fileRefPattern.FindAllStringSubmatch(text, -1)
	var refs []fileRef
	seen := make(map[string]bool)
	for _, m := range matches {
		p := m[1]
		key := p
		var start, end int
		if m[2] != "" {
			start, _ = strconv.Atoi(m[2])
			key += "#" + m[2]
		}
		if m[3] != "" {
			end, _ = strconv.Atoi(m[3])
			key += "-" + m[3]
		} else if start > 0 {
			end = start
		}
		if !seen[key] {
			seen[key] = true
			refs = append(refs, fileRef{Path: p, StartLine: start, EndLine: end})
		}
	}
	return refs
}

func buildFileContext(refs []fileRef) string {
	var sb strings.Builder
	const maxSize = 20 * 1024
	for _, ref := range refs {
		data, err := os.ReadFile(ref.Path)
		if err != nil {
			continue
		}
		content := string(data)
		label := ref.Path

		if ref.StartLine > 0 {
			lines := strings.Split(content, "\n")
			start := ref.StartLine - 1
			if start < 0 {
				start = 0
			}
			if start >= len(lines) {
				continue
			}
			end := ref.EndLine
			if end <= 0 || end > len(lines) {
				end = len(lines)
			}
			content = strings.Join(lines[start:end], "\n")
			label = fmt.Sprintf("%s#%d-%d", ref.Path, ref.StartLine, end)
		} else if len(content) > maxSize {
			content = content[:maxSize] + "\n... (truncated)"
		}

		sb.WriteString(fmt.Sprintf("\n--- File: %s ---\n%s\n", label, content))
	}
	return sb.String()
}
