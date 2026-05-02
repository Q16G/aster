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

	"aster/internal/ai"
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
	profileRegistry *ProfileRegistry

	skillService *service.SkillService
	mcpManager   *mcp.Manager
	appCfg       *AppConfig
	providerCfg  *ProviderState

	currentSessionID string
	sessionMeta      SessionMeta
	focus            FocusTarget

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
				m.statusText = "cancelling..."
				return m, nil
			}
			if m.exitProvider.RequestQuit() {
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
			m.chat.AddMessage(ChatMessage{Role: "system", Content: "Failed to create session. Please try again."})
			return m, m.input.Focus()
		}
		chatMsg := ChatMessage{Role: "user", Content: msg.Text, Time: time.Now()}
		m.chat.AddMessage(chatMsg)
		m.appendMessage(chatMsg)
		m.input.SetEnabled(false)
		m.agentRunning = true
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
		hadStream := m.chat.FlushStream()
		m.dialogStack.Clear()
		m.agentRunning = false
		m.spinner.Stop()
		m.input.SetEnabled(true)
		if msg.Err != nil {
			sysMsg := ChatMessage{Role: "system", Content: fmt.Sprintf("Error: %v", msg.Err)}
			m.chat.AddMessage(sysMsg)
			m.appendMessage(sysMsg)
			m.statusText = "error"
		} else if msg.Result != nil && !msg.Result.Success {
			sysMsg := ChatMessage{Role: "system", Content: fmt.Sprintf("Agent failed: %s", msg.Result.Error)}
			m.chat.AddMessage(sysMsg)
			m.appendMessage(sysMsg)
			m.statusText = "failed"
		} else {
			if m.agentCtx != nil {
				m.agentCtx.InitialHistory = ai.NormalizeMsgInfoSlice(msg.History)
			}
			if !hadStream && msg.Result != nil && msg.Result.Result != "" {
				aiMsg := ChatMessage{Role: "assistant", Content: msg.Result.Result}
				m.chat.AddMessage(aiMsg)
				m.appendMessage(aiMsg)
			} else if hadStream {
				msgs := m.chat.Messages()
				if len(msgs) > 0 {
					m.appendMessage(msgs[len(msgs)-1])
				}
			}
			m.statusText = "ready"
		}
		m.persistSessionSummary()
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
		case "theme-select":
			if msg.Value != "" {
				m.themeProvider.SetByName(msg.Value)
				m.sessionMeta.Theme = m.themeProvider.Get().Name
				m.persistSessionMeta()
				m.statusText = fmt.Sprintf("theme: %s", m.themeProvider.Get().Name)
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
		m.agentCtx.Definition = msg.Definition
		m.statusText = fmt.Sprintf("switched to %s", msg.Definition.Name)
		if m.currentSessionID != "" {
			m.updateSessionAgent(msg.Definition.Name)
		}
		return m, m.input.Focus()

	case StatusTextMsg:
		m.statusText = msg.Text
		return m, nil

	case ModelPickerLoadedMsg:
		if len(msg.Models) == 0 {
			m.chat.AddMessage(ChatMessage{Role: "system", Content: "no models available from current provider"})
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
			m.chat.AddMessage(ChatMessage{Role: "system", Content: fmt.Sprintf("load models failed: %v\nUsage: /model <model_id>", msg.Err)})
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

	chatHeight := mainHeight - inputHeight
	if chatHeight < 1 {
		chatHeight = 1
	}

	if m.sidebarVisible() {
		m.sidebar.SetSize(sidebarWidth, mainHeight)
	}
	m.chat.SetSize(chatWidth-2, chatHeight)
	m.input.SetWidth(chatWidth)
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

	chatHeight := mainHeight - inputHeight
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
		BorderStyle(lipgloss.NormalBorder()).
		BorderTop(true).
		BorderForeground(inputBorderColor).
		Padding(0, 1)

	inputView := inputStyle.Render(m.input.View())

	pickerView := ""
	if m.commandPicker != nil {
		m.commandPicker.SetWidth(chatWidth)
		pickerView = m.commandPicker.View()
	} else if m.filePicker != nil {
		m.filePicker.SetWidth(chatWidth)
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

	spinnerView := ""
	if m.spinner != nil && m.spinner.IsVisible() {
		spinnerView = m.spinner.View()
	}
	focusHint := ""
	switch m.focus {
	case FocusChat:
		focusHint = "[chat]"
	}

	m.footer.SetStatus(m.statusText, spinnerView, focusHint)
	footerView := m.footer.View(th)

	if m.toastManager != nil && !m.toastManager.IsEmpty() {
		m.toastManager.SetWidth(m.width)
		toastView := m.toastManager.View()
		return lipgloss.JoinVertical(lipgloss.Left, mainArea, toastView, footerView)
	}
	return lipgloss.JoinVertical(lipgloss.Left, mainArea, footerView)
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
