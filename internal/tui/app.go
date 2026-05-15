package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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
	"aster/internal/service"
	tuicontext "aster/internal/tui/context"
	tuiui "aster/internal/tui/ui"
)

const sidebarWidth = 42

type FocusTarget int

const (
	FocusInput FocusTarget = iota
	FocusSidebar
	FocusChat
)

type renderTickMsg struct{}
type sessionRestoreMsg struct{}

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
	sessionMeta             SessionMeta
	focus                   FocusTarget
	runStartTime            time.Time
	hadStreamDuringRun      bool
	hadFinalAnswerDuringRun bool
	externalInterrupt       *builtin_tools.ExternalInterrupt
	pendingInterrupt        *builtin_tools.PendingInterrupt
	runtimePhase            string
	runtimeProgress         int
	runtimeGoal             string
	runtimeWarnings         []string
	renderScheduled         bool
	sessionRestoredOnce     bool

	layoutChatWidth    int
	layoutChatHeight   int
	layoutMainHeight   int
	layoutInputHeight  int
	layoutContentWidth int

	sessionUsage ai.TokenUsage
	sessionCost  float64

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
}

func NewModel(
	store *SessionStore,
	agentCtx *AgentExecContext,
	humanBridge *HumanInputBridge,
	profileRegistry *ProfileRegistry,
	skillService *service.SkillService,
	mcpManager *mcp.Manager,
	reg *provider.Registry,
	credStore *CredentialStore,
	appCfg *AppConfig,
	providerCfg *ProviderState,
	localProv *tuicontext.LocalProvider,
	syncStore *tuicontext.SyncStore,
) Model {
	if localProv == nil {
		localProv = tuicontext.NewLocalProvider()
	}
	m := Model{
		store:           store,
		chat:            NewChatModel(),
		input:           NewInputModel(),
		sidebar:         NewSidebarModel(),
		thinkingPanel:   NewThinkingPanelModel(),
		agentCtx:        agentCtx,
		humanBridge:     humanBridge,
		profileRegistry: profileRegistry,
		skillService:    skillService,
		mcpManager:      mcpManager,
		registry:        reg,
		credStore:       credStore,
		appCfg:          appCfg,
		providerCfg:     providerCfg,
		statusText:      "ready",
		focus:           FocusInput,

		syncStore:       syncStore,
		themeProvider:   tuicontext.NewThemeProvider(),
		keybindProvider: tuicontext.NewKeybindProvider(),
		localProvider:   localProv,
		exitProvider:    tuicontext.NewExitProvider(),
		dialogStack:     tuiui.NewDialogStack(),
		toastManager:    tuiui.NewToastManager(5),
		spinner:         tuiui.NewSpinner("thinking..."),
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
		m.updateLayout()
		return m, nil

	case tea.MouseMsg:
		// Mouse support is enabled by default (see cmd/aster/main.go), but we only
		// act on wheel events to avoid focus surprises.
		me := tea.MouseEvent(msg)
		if me.IsWheel() {
			if !m.dialogStack.IsEmpty() {
				cmd := m.dialogStack.Update(msg)
				if cmd != nil {
					cmds = append(cmds, cmd)
				}
			} else {
				var cmd tea.Cmd
				m.chat, cmd = m.chat.Update(msg)
				if cmd != nil {
					cmds = append(cmds, cmd)
				}
			}
		}
		// Let the rest of the model (spinner/toasts/etc) update as usual.

	case sessionRestoreMsg:
		if !m.sessionRestoredOnce {
			m.sessionRestoredOnce = true
			m.restoreLatestSession()
			m.updateLayout()
		}
		return m, nil

	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			if m.agentRunning && m.agentCtx != nil {
				m.agentCtx.CancelTurn()
				m.clearRetryState()
				m.statusText = "cancelling..."
				return m, nil
			}
			if m.exitProvider.RequestQuit() {
				m.persistCurrentSession()
				return m, tea.Quit
			}
			m.statusText = "press Ctrl+C again to quit"
			return m, nil
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
				return m, m.input.Focus()
			default:
				var cmd tea.Cmd
				m.input, cmd = m.input.Update(msg)
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
				return m, m.input.Focus()
			default:
				var cmd tea.Cmd
				m.input, cmd = m.input.Update(msg)
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
				m.updateLayout()
				return m, nil
			case tuicontext.KeyActionOpenModels:
				if !m.agentRunning {
					return m.handleSlashCommand("/model")
				}
				return m, nil
			case tuicontext.KeyActionCycleFocus:
				if !m.agentRunning {
					m.cycleFocus()
					return m, m.focusCmd()
				}
				return m, nil
			case tuicontext.KeyActionEscape:
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
		m.hadFinalAnswerDuringRun = false
		m.externalInterrupt = nil
		m.thinkingPanel.Reset()
		m.clearRetryState()
		m.runStartTime = time.Now()
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
		return m, nil

	case AgentDoneMsg:
		m.thinkingPanel.Hide()
		m.updateLayout()
		m.chat.FlushThinking()
		hadStream := m.flushStreamAndPersist()
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
			m.chat.AddPart(DisplayPart{
				Type: PartTypeSummary,
				Time: time.Now(),
				Summary: &SummaryPart{
					AgentName: agentName,
					ModelID:   modelID,
					Duration:  time.Since(m.runStartTime),
					Success:   success,
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
			m.hadFinalAnswerDuringRun = false
			m.externalInterrupt = nil
			m.thinkingPanel.Reset()
			m.clearRetryState()
			m.runStartTime = time.Now()
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
				active := stringsContains(m.sessionMeta.ActiveMCPServers, msg.Value)
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
		return m, m.input.Focus()

	case SlashCommandMsg:
		return m.handleSlashCommand(msg.Command)

	case AgentSwitchMsg:
		if m.agentCtx == nil {
			return m, nil
		}
		m.agentCtx.Definition = msg.Definition
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

		if m.agentCtx != nil {
			m.agentCtx.Definition.ModelID = state.ModelID
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

		m.rememberRecentModel(state.ModelID)
		m.persistSessionMeta()
		m.refreshSidebarData()
		m.statusText = fmt.Sprintf("provider: %s (model: %s)", msg.Name, state.ModelID)

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
		} else {
			m.setFocus(FocusChat)
		}
	case FocusSidebar:
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
	m.focus = target
	m.sidebar.SetFocused(target == FocusSidebar)
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
		TokenCount:   m.formatTokenCount(),
		CostEstimate: m.formatCostEstimate(),
		RunStatus:    "idle",
	}

	if m.currentSessionID != "" && m.store != nil {
		if rec, err := m.store.Get(m.currentSessionID); err == nil {
			snap.SessionTitle = rec.Title
			snap.AgentName = rec.AgentName
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
		for _, e := range m.mcpManager.ServerEntries() {
			if e == nil {
				continue
			}
			active := false
			for _, name := range m.sessionMeta.ActiveMCPServers {
				if name == e.Name {
					active = true
					break
				}
			}
			snap.MCPServers = append(snap.MCPServers, MCPStatusEntry{
				Name:      e.Name,
				Status:    string(e.Status),
				ToolCount: e.ToolCount,
				Active:    active,
			})
		}
	}

	for _, p := range m.chat.Parts() {
		if p.Type == PartTypePlan && p.Plan != nil {
			snap.PlanItems = p.Plan.Items
		}
	}

	snap.ActiveSkills = m.effectiveActiveSkillNames()
	snap.ActiveMCPs = m.sessionMeta.ActiveMCPServers
	snap.DismissedGettingStarted = m.localProvider.Get().DismissedGettingStarted
	snap.Workdir = m.footer.Workdir()

	return snap
}

func (m *Model) formatTokenCount() string {
	total := m.sessionUsage.ContextCountTokens()
	if total == 0 {
		return "--"
	}
	switch {
	case total >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(total)/1_000_000)
	case total >= 1_000:
		return fmt.Sprintf("%.1fk", float64(total)/1_000)
	default:
		return strconv.Itoa(total)
	}
}

func (m *Model) formatCostEstimate() string {
	if m.sessionCost <= 0 {
		return "--"
	}
	if m.sessionCost < 0.01 {
		return fmt.Sprintf("$%.4f", m.sessionCost)
	}
	return fmt.Sprintf("$%.2f", m.sessionCost)
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

// --- Layout ---

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
	inputHeight := 3

	sbWidth := 0
	if m.sidebarVisible() {
		sbWidth = sidebarWidth + 1
	}

	chatWidth := m.width - sbWidth
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
	m.chat.SetSize(contentWidth, chatHeight)
	m.thinkingPanel.SetWidth(contentWidth)
	m.input.SetWidth(contentWidth)
	m.footer.SetWidth(m.width)
	m.dialogStack.SetSize(m.width, m.height)
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

	chatContent := m.chat.View()
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

	var mainArea string
	if m.sidebarVisible() {
		sidebarView := m.sidebar.View()
		mainArea = lipgloss.JoinHorizontal(lipgloss.Top, leftPane, sidebarView)
	} else {
		mainArea = leftPane
	}
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
	case FocusChat:
		focusHint = "[chat]"
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
