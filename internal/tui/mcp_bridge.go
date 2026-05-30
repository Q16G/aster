package tui

import (
	"sync/atomic"

	tea "github.com/charmbracelet/bubbletea"

	"aster/internal/mcp"
)

// MCPBridge 把 MCP manager 后台 goroutine 的状态变更推送进 Bubble Tea 的 Update 循环，
// 用于事件驱动刷新侧边栏/footer，替代有界轮询。
type MCPBridge struct {
	program atomic.Pointer[tea.Program]
}

func NewMCPBridge() *MCPBridge {
	return &MCPBridge{}
}

func (b *MCPBridge) Bind(p *tea.Program) {
	b.program.Store(p)
}

func (b *MCPBridge) NotifyStatusChanged(name string, status mcp.MCPServerStatus) {
	if p := b.program.Load(); p != nil {
		// tea.Program.msgs is unbuffered, so a synchronous Send from the Update
		// goroutine (e.g. a reconcile-triggered Disconnect) would self-deadlock.
		// Decouple delivery so the manager never blocks its caller.
		go p.Send(MCPStatusChangedMsg{Name: name, Status: string(status)})
	}
}
