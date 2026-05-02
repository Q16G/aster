package tuicontext

import (
	"sync"
	"time"
)

const batchInterval = 16 * time.Millisecond

type SyncStatus int

const (
	SyncStatusLoading SyncStatus = iota
	SyncStatusPartial
	SyncStatusComplete
	SyncStatusFailed
)

type ProviderEntry struct {
	Name         string
	BaseURL      string
	DefaultModel string
	IsCurrent    bool
}

type AgentEntry struct {
	Name      string
	Role      string
	IsCurrent bool
}

type SessionEntry struct {
	ID           string
	Title        string
	Status       string
	AgentName    string
	ModelID      string
	MessageCount int
	LastMessage  string
	UpdatedAt    time.Time
	IsCurrent    bool
}

type MessageEntry struct {
	Role    string
	Content string
	Time    time.Time
}

type PartEntry struct {
	MessageID string
	Type      string
	Name      string
	Content   string
	Time      time.Time
}

type MCPEntry struct {
	Name      string
	Status    string
	ToolCount int
	IsActive  bool
}

type ConfigState struct {
	CurrentProvider string
	CurrentModel    string
	Theme           string
	Phase           string
}

type SyncStore struct {
	mu     sync.RWMutex
	Status SyncStatus

	Providers     []ProviderEntry
	Agents        []AgentEntry
	Sessions      []SessionEntry
	SessionStatus map[string]string
	Messages      map[string][]MessageEntry
	Parts         map[string][]PartEntry
	MCP           map[string]MCPEntry
	Config        ConfigState

	onFlush    func(events []any)
	eventQueue []any
	batchTimer *time.Timer
	batchMu    sync.Mutex
	closed     bool
}

func NewSyncStore() *SyncStore {
	return &SyncStore{
		SessionStatus: make(map[string]string),
		Messages:      make(map[string][]MessageEntry),
		Parts:         make(map[string][]PartEntry),
		MCP:           make(map[string]MCPEntry),
	}
}

func (s *SyncStore) SetFlushCallback(fn func(events []any)) {
	s.batchMu.Lock()
	defer s.batchMu.Unlock()
	s.onFlush = fn
}

func (s *SyncStore) Enqueue(event any) {
	s.batchMu.Lock()
	defer s.batchMu.Unlock()

	if s.closed {
		return
	}

	s.eventQueue = append(s.eventQueue, event)
	if s.batchTimer == nil {
		s.batchTimer = time.AfterFunc(batchInterval, s.flush)
	}
}

func (s *SyncStore) flush() {
	s.batchMu.Lock()
	events := s.eventQueue
	s.eventQueue = nil
	s.batchTimer = nil
	fn := s.onFlush
	s.batchMu.Unlock()

	if len(events) > 0 && fn != nil {
		fn(events)
	}
}

func (s *SyncStore) Close() {
	s.batchMu.Lock()
	defer s.batchMu.Unlock()
	s.closed = true
	if s.batchTimer != nil {
		s.batchTimer.Stop()
		s.batchTimer = nil
	}
}

func (s *SyncStore) SetStatus(status SyncStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Status = status
}

func (s *SyncStore) GetStatus() SyncStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Status
}

func (s *SyncStore) SetSessions(sessions []SessionEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Sessions = sessions
}

func (s *SyncStore) SetCurrentSession(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.Sessions {
		s.Sessions[i].IsCurrent = s.Sessions[i].ID == id
	}
}

func (s *SyncStore) AppendMessage(sessionID string, msg MessageEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Messages[sessionID] = append(s.Messages[sessionID], msg)
}

func (s *SyncStore) SetMessages(sessionID string, msgs []MessageEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Messages[sessionID] = msgs
}

func (s *SyncStore) GetMessages(sessionID string) []MessageEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	msgs := s.Messages[sessionID]
	cp := make([]MessageEntry, len(msgs))
	copy(cp, msgs)
	return cp
}

func (s *SyncStore) SetMCPStatus(name string, entry MCPEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.MCP[name] = entry
}

func (s *SyncStore) SetAgents(agents []AgentEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Agents = agents
}

func (s *SyncStore) SetConfig(cfg ConfigState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Config = cfg
}

func (s *SyncStore) GetConfig() ConfigState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Config
}
