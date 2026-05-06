package tui

import (
	"context"
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
	"aster/internal/builtin_tools"
	"aster/internal/mcp"
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
	agentCtx        *AgentExecContext
	humanBridge     *HumanInputBridge
	agentRunning    bool
	statusText      string
	retryState      *retryState
	profileRegistry *ProfileRegistry

	skillService    *service.SkillService
	mcpManager      *mcp.Manager
	appCfg          *AppConfig
	providerCfg     *ProviderState
	pendingProvider *pendingProviderSetup

	currentSessionID        string
	sessionMeta             SessionMeta
	focus                   FocusTarget
	runStartTime            time.Time
	hadStreamDuringRun      bool
	hadFinalAnswerDuringRun bool
	externalInterrupt       *builtin_tools.ExternalInterrupt

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
	appCfg *AppConfig,
	providerCfg *ProviderState,
	syncStore *tuicontext.SyncStore,
) Model {
	return Model{
		store:           store,
		chat:            NewChatModel(),
		input:           NewInputModel(),
		sidebar:         NewSidebarModel(),
		agentCtx:        agentCtx,
		humanBridge:     humanBridge,
		profileRegistry: profileRegistry,
		skillService:    skillService,
		mcpManager:      mcpManager,
		appCfg:          appCfg,
		providerCfg:     providerCfg,
		statusText:      "ready",
		focus:           FocusInput,

		syncStore:       syncStore,
		themeProvider:   tuicontext.NewThemeProvider(),
		keybindProvider: tuicontext.NewKeybindProvider(),
		localProvider:   tuicontext.NewLocalProvider(),
		exitProvider:    tuicontext.NewExitProvider(),
		dialogStack:     tuiui.NewDialogStack(),
		toastManager:    tuiui.NewToastManager(5),
		spinner:         tuiui.NewSpinner("thinking..."),
	}
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
	return tea.Batch(m.input.Focus(), m.refreshSidebarCmd())
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.updateLayout()
		return m, nil

	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			if m.agentRunning && m.agentCtx != nil {
				m.agentCtx.Cancel()
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
		m.clearRetryState()
		m.runStartTime = time.Now()
		m.statusText = "thinking..."
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
		return m, nil

	case AgentDoneMsg:
		m.chat.FlushThinking()
		hadStream := m.flushStreamAndPersist()
		m.clearRetryState()
		m.dialogStack.Clear()
		m.agentRunning = false
		m.spinner.Stop()
		m.input.SetEnabled(true)
		if msg.Err != nil {
			m.chat.AddPart(DisplayPart{Type: PartTypeSystem, Time: time.Now(), System: &SystemPart{Content: fmt.Sprintf("Error: %v", msg.Err)}})
			m.statusText = "error"
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
			if m.agentCtx != nil {
				m.agentCtx.InitialHistory = ai.NormalizeMsgInfoSlice(msg.History)
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
		// Run summary
		success := msg.Err == nil && (msg.Result == nil || msg.Result.Success)
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
		m.persistCurrentSession()
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
				active := stringsContains(m.sessionMeta.ActiveSkillNames, msg.Value)
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
				if bp, ok := GetBuiltinProvider(msg.Value); ok {
					cfgP := m.appCfg.Providers[msg.Value]
					apiKey := resolveAPIKey(bp, "")
					if cfgP != nil {
						apiKey = resolveAPIKey(bp, cfgP.APIKey)
					}
					if apiKey == "" && bp.APIKeyEnvVar != "" {
						m.pendingProvider = &pendingProviderSetup{ProviderID: msg.Value}
						prompt := tuiui.NewPromptDialog(
							"provider-apikey:"+msg.Value,
							fmt.Sprintf("Configure %s", bp.Name),
							fmt.Sprintf("Enter your %s API key:", bp.Name),
						).WithMasked().WithPlaceholder("sk-...")
						m.dialogStack.Push(prompt, nil)
						m.dialogStack.SetSize(m.width, m.height)
						return m, nil
					}
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

		var baseURL, apiKey, model string

		if bp, ok := GetBuiltinProvider(msg.Name); ok {
			cfgP := m.appCfg.Providers[msg.Name]
			apiKey = resolveAPIKey(bp, "")
			if cfgP != nil {
				apiKey = resolveAPIKey(bp, cfgP.APIKey)
			}
			if apiKey == "" && bp.APIKeyEnvVar != "" {
				m.chat.AddPart(DisplayPart{Type: PartTypeSystem, Time: time.Now(), System: &SystemPart{Content: fmt.Sprintf("provider %s requires %s to be set", bp.Name, bp.APIKeyEnvVar)}})
				return m, nil
			}
			baseURL = bp.BaseURL
			model = bp.DefaultModel
			if cfgP != nil {
				if cfgP.BaseURL != "" {
					baseURL = cfgP.BaseURL
				}
				if cfgP.DefaultModel != "" {
					model = cfgP.DefaultModel
				}
			}
		} else if p, ok := m.appCfg.Providers[msg.Name]; ok {
			baseURL = p.BaseURL
			apiKey = p.APIKey
			model = p.DefaultModel
		} else {
			m.chat.AddPart(DisplayPart{Type: PartTypeSystem, Time: time.Now(), System: &SystemPart{Content: "unknown provider: " + msg.Name}})
			return m, nil
		}

		m.providerCfg.Name = msg.Name
		m.providerCfg.BaseURL = baseURL
		m.providerCfg.APIKey = apiKey
		m.providerCfg.ModelID = model

		if m.agentCtx != nil {
			m.agentCtx.Definition.ModelID = model
			if m.agentCtx.RebuildClient != nil {
				m.agentCtx.RebuildClient(baseURL, apiKey, model)
			}
		}

		m.appCfg.DefaultProvider = msg.Name
		if err := SaveConfig(DefaultConfigPath(), func(cfg *AppConfig) {
			cfg.DefaultProvider = msg.Name
			if cfg.Providers == nil {
				cfg.Providers = make(map[string]*ProviderConfig)
			}
			if _, exists := cfg.Providers[msg.Name]; !exists {
				if bp, ok := GetBuiltinProvider(msg.Name); ok {
					cfg.Providers[msg.Name] = &ProviderConfig{
						BaseURL:      bp.BaseURL,
						APIKey:       fmt.Sprintf("${%s}", bp.APIKeyEnvVar),
						DefaultModel: bp.DefaultModel,
					}
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
				BaseURL:      baseURL,
				DefaultModel: model,
			}
		}

		m.rememberRecentModel(model)
		m.persistSessionMeta()
		m.statusText = fmt.Sprintf("provider: %s (model: %s)", msg.Name, model)
		return m, nil

	case StatusTextMsg:
		m.statusText = msg.Text
		return m, nil

	case ModelPickerLoadedMsg:
		if len(msg.Models) == 0 {
			m.chat.AddPart(DisplayPart{Type: PartTypeSystem, Time: time.Now(), System: &SystemPart{Content: "no models available from current provider"}})
			m.statusText = "no models"
			return m, nil
		}
		currentModelID := ""
		if m.providerCfg != nil {
			currentModelID = m.providerCfg.ModelID
		}
		recentIDs := m.localProvider.Get().RecentModelIDs
		options := buildModelSelectOptions(msg.Models, currentModelID, recentIDs)
		dialog := tuiui.NewSelectDialog("model-select", "Select Model", options)
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
		if m.providerCfg != nil && msg.ModelID != "" {
			m.providerCfg.ModelID = msg.ModelID
			if m.agentCtx != nil {
				m.agentCtx.Definition.ModelID = msg.ModelID
			}
			m.rememberRecentModel(msg.ModelID)
			m.persistSessionMeta()
			m.statusText = fmt.Sprintf("model: %s", msg.ModelID)
		}
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

// --- Focus Management ---

func (m *Model) cycleFocus() {
	switch m.focus {
	case FocusInput:
		m.setFocus(FocusChat)
	case FocusChat:
		m.setFocus(FocusInput)
	default:
		m.setFocus(FocusInput)
	}
}

func (m *Model) setFocus(target FocusTarget) {
	if target == FocusSidebar {
		target = FocusChat
	}
	m.focus = target
	m.sidebar.SetFocused(false)
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
	if m.skillService != nil {
		skills, _ := m.skillService.ListSkills(context.Background(), nil)
		m.sidebar.SetSkills(skills)
	}
	if m.mcpManager != nil {
		m.sidebar.SetMCPEntries(m.mcpManager.ServerEntries())
	}

	activeSkills := make(map[string]bool)
	for _, name := range m.sessionMeta.ActiveSkillNames {
		activeSkills[name] = true
	}
	m.sidebar.SetActiveSkillNames(activeSkills)

	activeMCP := make(map[string]bool)
	for _, name := range m.sessionMeta.ActiveMCPServers {
		activeMCP[name] = true
	}
	m.sidebar.SetActiveMCPServers(activeMCP)

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

func (m *Model) refreshSidebarCmd() tea.Cmd {
	return func() tea.Msg { return RefreshSidebarMsg{} }
}

// --- Layout ---

func (m *Model) sidebarVisible() bool {
	return false
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
	chatHeight := mainHeight - inputHeight - pickerHeight
	if chatHeight < 1 {
		chatHeight = 1
	}

	if m.sidebarVisible() {
		m.sidebar.SetSize(sidebarWidth, mainHeight)
	}
	m.chat.SetSize(chatWidth-2, chatHeight)
	m.input.SetWidth(chatWidth - 2)
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
	chatHeight := mainHeight - inputHeight - pickerHeight
	if chatHeight < 1 {
		chatHeight = 1
	}

	chatContentWidth := chatWidth - 2
	if chatContentWidth < 1 {
		chatContentWidth = 1
	}

	m.chat.SetSize(chatContentWidth, chatHeight)

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

	m.input.SetWidth(chatWidth - 2)
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
	if pickerView != "" {
		leftPane = lipgloss.JoinVertical(lipgloss.Left, chatView, pickerView, inputView)
	} else {
		leftPane = lipgloss.JoinVertical(lipgloss.Left, chatView, inputView)
	}

	var mainArea string
	if m.sidebarVisible() {
		m.sidebar.SetSize(sidebarWidth, mainHeight)
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
