package tuicontext

import (
	"sync"
	"testing"
	"time"
)

func TestSyncStore_NewSyncStore(t *testing.T) {
	s := NewSyncStore()
	if s.Status != SyncStatusLoading {
		t.Fatalf("expected SyncStatusLoading, got %d", s.Status)
	}
	if s.Sessions != nil {
		t.Fatal("expected nil Sessions initially")
	}
	if s.Messages == nil || s.Parts == nil || s.MCP == nil {
		t.Fatal("maps should be initialized")
	}
}

func TestSyncStore_SetGetStatus(t *testing.T) {
	s := NewSyncStore()
	s.SetStatus(SyncStatusComplete)
	if s.GetStatus() != SyncStatusComplete {
		t.Fatal("expected SyncStatusComplete")
	}
}

func TestSyncStore_Sessions(t *testing.T) {
	s := NewSyncStore()
	sessions := []SessionEntry{
		{ID: "s1", Title: "Session 1"},
		{ID: "s2", Title: "Session 2"},
	}
	s.SetSessions(sessions)

	s.SetCurrentSession("s2")

	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, sess := range s.Sessions {
		if sess.ID == "s2" && !sess.IsCurrent {
			t.Fatal("expected s2 to be current")
		}
		if sess.ID == "s1" && sess.IsCurrent {
			t.Fatal("expected s1 to not be current")
		}
	}
}

func TestSyncStore_Messages(t *testing.T) {
	s := NewSyncStore()
	s.AppendMessage("sess-1", MessageEntry{Role: "user", Content: "hello"})
	s.AppendMessage("sess-1", MessageEntry{Role: "assistant", Content: "hi"})

	msgs := s.GetMessages("sess-1")
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].Content != "hello" || msgs[1].Content != "hi" {
		t.Fatal("unexpected message content")
	}

	empty := s.GetMessages("nonexistent")
	if len(empty) != 0 {
		t.Fatal("expected empty for nonexistent session")
	}
}

func TestSyncStore_SetMessages(t *testing.T) {
	s := NewSyncStore()
	s.AppendMessage("sess-1", MessageEntry{Content: "old"})
	s.SetMessages("sess-1", []MessageEntry{{Content: "new"}})

	msgs := s.GetMessages("sess-1")
	if len(msgs) != 1 || msgs[0].Content != "new" {
		t.Fatal("SetMessages should replace")
	}
}

func TestSyncStore_MCP(t *testing.T) {
	s := NewSyncStore()
	s.SetMCPStatus("rg", MCPEntry{Name: "rg", Status: "connected", ToolCount: 3})

	s.mu.RLock()
	entry, ok := s.MCP["rg"]
	s.mu.RUnlock()

	if !ok || entry.ToolCount != 3 {
		t.Fatal("expected MCP entry with 3 tools")
	}
}

func TestSyncStore_Config(t *testing.T) {
	s := NewSyncStore()
	s.SetConfig(ConfigState{CurrentProvider: "openai", CurrentModel: "gpt-4o"})

	cfg := s.GetConfig()
	if cfg.CurrentProvider != "openai" || cfg.CurrentModel != "gpt-4o" {
		t.Fatalf("unexpected config: %+v", cfg)
	}
}

func TestSyncStore_EnqueueAndFlush(t *testing.T) {
	s := NewSyncStore()

	var flushed []any
	var mu sync.Mutex
	s.SetFlushCallback(func(events []any) {
		mu.Lock()
		flushed = append(flushed, events...)
		mu.Unlock()
	})

	s.Enqueue("event-1")
	s.Enqueue("event-2")
	s.Enqueue("event-3")

	s.batchMu.Lock()
	if len(s.eventQueue) != 3 {
		t.Fatalf("expected 3 events in queue, got %d", len(s.eventQueue))
	}
	s.batchMu.Unlock()

	s.flush()

	s.batchMu.Lock()
	if len(s.eventQueue) != 0 {
		t.Fatal("expected empty queue after flush")
	}
	s.batchMu.Unlock()

	mu.Lock()
	if len(flushed) != 3 {
		t.Fatalf("expected 3 flushed events, got %d", len(flushed))
	}
	mu.Unlock()
}

func TestSyncStore_Close(t *testing.T) {
	s := NewSyncStore()
	s.Enqueue("event-1")
	s.Close()

	s.Enqueue("event-2")

	s.batchMu.Lock()
	defer s.batchMu.Unlock()
	if len(s.eventQueue) != 1 {
		t.Fatalf("expected 1 event (enqueued before close), got %d", len(s.eventQueue))
	}
}

func TestSyncStore_ConcurrentEnqueue(t *testing.T) {
	s := NewSyncStore()
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(v int) {
			defer wg.Done()
			s.Enqueue(v)
		}(i)
	}

	wg.Wait()

	s.batchMu.Lock()
	count := len(s.eventQueue)
	s.batchMu.Unlock()

	if count != 100 {
		t.Fatalf("expected 100 events, got %d", count)
	}

	s.Close()
}

func TestSyncStore_BatchTimerFires(t *testing.T) {
	s := NewSyncStore()
	s.Enqueue("event-1")

	// Wait for timer to fire
	time.Sleep(batchInterval + 10*time.Millisecond)

	s.batchMu.Lock()
	qLen := len(s.eventQueue)
	timer := s.batchTimer
	s.batchMu.Unlock()

	if qLen != 0 {
		t.Fatalf("expected queue flushed by timer, got %d events", qLen)
	}
	if timer != nil {
		t.Fatal("expected timer to be nil after flush")
	}

	s.Close()
}
