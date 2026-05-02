package tui

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestStore(t *testing.T) *SessionStore {
	t.Helper()
	dir := t.TempDir()
	store, err := NewSessionStore(filepath.Join(dir, "test.db"), filepath.Join(dir, "sessions"))
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })
	return store
}

func TestNewSessionStore_WALMode(t *testing.T) {
	store := newTestStore(t)
	var mode string
	err := store.db.QueryRow("PRAGMA journal_mode").Scan(&mode)
	require.NoError(t, err)
	assert.Equal(t, "wal", mode)
}

func TestNewSessionStore_CreatesTable(t *testing.T) {
	store := newTestStore(t)
	var name string
	err := store.db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='sessions'").Scan(&name)
	require.NoError(t, err)
	assert.Equal(t, "sessions", name)
}

func TestSessionStore_CreateAndGet(t *testing.T) {
	store := newTestStore(t)
	rec := &SessionRecord{
		ID:           "test-1",
		Title:        "Test Session",
		AgentName:    "security-agent",
		ProviderName: "openai",
		ModelID:      "gpt-4o",
		Metadata:     `{"key":"value"}`,
	}
	require.NoError(t, store.Create(rec))

	got, err := store.Get("test-1")
	require.NoError(t, err)
	assert.Equal(t, "test-1", got.ID)
	assert.Equal(t, "Test Session", got.Title)
	assert.Equal(t, "active", got.Status)
	assert.Equal(t, "security-agent", got.AgentName)
	assert.Equal(t, "openai", got.ProviderName)
	assert.Equal(t, "gpt-4o", got.ModelID)
	assert.Equal(t, `{"key":"value"}`, got.Metadata)
	assert.WithinDuration(t, time.Now(), got.CreatedAt, 5*time.Second)
	assert.WithinDuration(t, time.Now(), got.UpdatedAt, 5*time.Second)
}

func TestSessionStore_CreateAutoID(t *testing.T) {
	store := newTestStore(t)
	rec := &SessionRecord{Title: "Auto ID"}
	require.NoError(t, store.Create(rec))
	assert.NotEmpty(t, rec.ID)

	got, err := store.Get(rec.ID)
	require.NoError(t, err)
	assert.Equal(t, "Auto ID", got.Title)
}

func TestSessionStore_CreateMakesDir(t *testing.T) {
	store := newTestStore(t)
	rec := &SessionRecord{ID: "dir-test"}
	require.NoError(t, store.Create(rec))

	info, err := os.Stat(filepath.Join(store.baseDir, "dir-test"))
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

func TestSessionStore_List_OrderByUpdatedDesc(t *testing.T) {
	store := newTestStore(t)

	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	for i, title := range []string{"first", "second", "third"} {
		rec := &SessionRecord{ID: title, Title: title}
		require.NoError(t, store.Create(rec))
		// manually set updated_at to control ordering
		_, err := store.db.Exec(
			`UPDATE sessions SET updated_at=? WHERE id=?`,
			base.Add(time.Duration(i)*time.Hour).Format(sqliteTimeFormat), title,
		)
		require.NoError(t, err)
	}

	list, err := store.List()
	require.NoError(t, err)
	require.Len(t, list, 3)
	assert.Equal(t, "third", list[0].ID)
	assert.Equal(t, "second", list[1].ID)
	assert.Equal(t, "first", list[2].ID)
}

func TestSessionStore_Update(t *testing.T) {
	store := newTestStore(t)
	rec := &SessionRecord{ID: "upd-1", Title: "Original"}
	require.NoError(t, store.Create(rec))

	rec.Title = "Updated"
	rec.AgentName = "new-agent"
	require.NoError(t, store.Update(rec))

	got, err := store.Get("upd-1")
	require.NoError(t, err)
	assert.Equal(t, "Updated", got.Title)
	assert.Equal(t, "new-agent", got.AgentName)
}

func TestSessionStore_Delete(t *testing.T) {
	store := newTestStore(t)
	rec := &SessionRecord{ID: "del-1"}
	require.NoError(t, store.Create(rec))

	require.NoError(t, store.Delete("del-1"))

	_, err := store.Get("del-1")
	assert.ErrorIs(t, err, sql.ErrNoRows)

	_, err = os.Stat(filepath.Join(store.baseDir, "del-1"))
	assert.True(t, os.IsNotExist(err))
}

func TestSessionStore_UpdateSummary(t *testing.T) {
	store := newTestStore(t)
	rec := &SessionRecord{ID: "sum-1"}
	require.NoError(t, store.Create(rec))

	require.NoError(t, store.UpdateSummary("sum-1", 5, "last message content"))

	got, err := store.Get("sum-1")
	require.NoError(t, err)
	assert.Equal(t, 5, got.MessageCount)
	assert.Equal(t, "last message content", got.LastMessage)
}

func TestSessionStore_GetNotFound(t *testing.T) {
	store := newTestStore(t)
	_, err := store.Get("nonexistent")
	assert.ErrorIs(t, err, sql.ErrNoRows)
}
