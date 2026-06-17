package tui

import "strings"

// PartsStore is the authoritative owner of a ChatModel's timeline state. It
// holds the part slice plus all the ancillary maps that used to live as loose
// fields on ChatModel (streaming buffers, sub-agent spawn metadata) so the
// renderer and event handlers have a single, narrow API to write through.
//
// This commit introduces the type as a transparent wrapper: methods are 1:1
// with the previous direct slice/map access so chat.go can migrate one call
// site at a time in C2.3 / C2.4. Index maintenance and dirty-set tracking
// are introduced later in Stage 3; the wrapper here intentionally has no
// indexing logic so the semantics are byte-for-byte identical to the
// pre-Stage-2 ChatModel.
type PartsStore struct {
	parts []DisplayPart

	streamingByAgent map[string]*strings.Builder
	streamingOrder   []string

	spawnByCallID map[string]agentSpawnInfo
	agentParent   map[string]agentSpawnInfo

	// nextID seeds DisplayPart.ID assignment. 0 is the unassigned sentinel;
	// the first allocated ID is 1. Owned by the store so PartsStore.Append
	// can mint identities without bouncing back through ChatModel once the
	// migration completes.
	nextID uint64
}

// NewPartsStore constructs an empty store with allocated index maps. Callers
// keep a *PartsStore to share mutations across the model.
func NewPartsStore() *PartsStore {
	return &PartsStore{
		streamingByAgent: make(map[string]*strings.Builder),
		spawnByCallID:    make(map[string]agentSpawnInfo),
		agentParent:      make(map[string]agentSpawnInfo),
	}
}

// Len returns the number of parts currently stored.
func (s *PartsStore) Len() int { return len(s.parts) }

// At returns the part at index i. Callers should bounds-check before calling.
func (s *PartsStore) At(i int) DisplayPart { return s.parts[i] }

// Snapshot returns the underlying parts slice. The slice is not copied — this
// matches the pre-migration `m.parts` semantics. Mutating callers must go
// through Replace/Append to keep future index/dirty tracking honest.
func (s *PartsStore) Snapshot() []DisplayPart { return s.parts }

// Append adds a DisplayPart and assigns a fresh ID if the caller did not
// provide one. The assigned ID is returned for convenience.
func (s *PartsStore) Append(p DisplayPart) uint64 {
	if p.ID == 0 {
		s.nextID++
		p.ID = s.nextID
	} else if p.ID > s.nextID {
		s.nextID = p.ID
	}
	if p.Version == 0 {
		p.Version = 1
	}
	s.parts = append(s.parts, p)
	return p.ID
}

// Replace mutates the part at index i via the provided closure. The closure
// receives a pointer so it can update fields in place; Version is left to the
// caller (Stage 3 will start auto-bumping it from typed setter methods).
func (s *PartsStore) Replace(i int, fn func(*DisplayPart)) {
	if i < 0 || i >= len(s.parts) {
		return
	}
	fn(&s.parts[i])
}

// SetAll replaces the entire slice, used by SetParts on session load. ID/
// Version back-fill is the caller's responsibility (ChatModel.SetParts does
// it before handing the slice over).
func (s *PartsStore) SetAll(parts []DisplayPart) {
	s.parts = parts
	var maxID uint64
	for _, p := range parts {
		if p.ID > maxID {
			maxID = p.ID
		}
	}
	if maxID > s.nextID {
		s.nextID = maxID
	}
}

// StreamingBuilder returns the in-progress text stream buffer for agentName,
// creating it on first use. Mirrors the pre-migration streamBuilder helper.
func (s *PartsStore) StreamingBuilder(agentName string) *strings.Builder {
	b, ok := s.streamingByAgent[agentName]
	if !ok {
		b = &strings.Builder{}
		s.streamingByAgent[agentName] = b
		s.streamingOrder = append(s.streamingOrder, agentName)
	}
	return b
}

// StreamingLookup returns the existing builder for agentName without creating
// one. The second return is false when no builder exists.
func (s *PartsStore) StreamingLookup(agentName string) (*strings.Builder, bool) {
	b, ok := s.streamingByAgent[agentName]
	return b, ok
}

// StreamingDrop removes agentName's stream buffer and de-registers it from the
// ordered list. No-op when there is no buffer for agentName.
func (s *PartsStore) StreamingDrop(agentName string) {
	if _, ok := s.streamingByAgent[agentName]; !ok {
		return
	}
	delete(s.streamingByAgent, agentName)
	for i, name := range s.streamingOrder {
		if name == agentName {
			s.streamingOrder = append(s.streamingOrder[:i], s.streamingOrder[i+1:]...)
			break
		}
	}
}

// StreamingOrder returns the agents with active stream buffers in arrival
// order. A copy is returned so callers may mutate the slice freely.
func (s *PartsStore) StreamingOrder() []string {
	return append([]string(nil), s.streamingOrder...)
}

// StreamingHasContent reports whether any stream buffer currently has bytes.
func (s *PartsStore) StreamingHasContent() bool {
	for _, b := range s.streamingByAgent {
		if b.Len() > 0 {
			return true
		}
	}
	return false
}

// SpawnByCallID returns the sub-agent spawn metadata indexed by the spawning
// tool's call_id. Returns the zero value when not found.
func (s *PartsStore) SpawnByCallID(callID string) (agentSpawnInfo, bool) {
	info, ok := s.spawnByCallID[callID]
	return info, ok
}

// SetSpawnByCallID records spawn metadata for callID. Used by tool_start
// handlers and SetParts rebuild path.
func (s *PartsStore) SetSpawnByCallID(callID string, info agentSpawnInfo) {
	s.spawnByCallID[callID] = info
}

// SpawnByCallIDAll returns the raw map for iteration. Callers must not retain
// the reference beyond a single scope; future indexing work may swap the
// representation.
func (s *PartsStore) SpawnByCallIDAll() map[string]agentSpawnInfo {
	return s.spawnByCallID
}

// AgentParent returns the parent spawn info for a child agent name.
func (s *PartsStore) AgentParent(child string) (agentSpawnInfo, bool) {
	info, ok := s.agentParent[child]
	return info, ok
}

// SetAgentParent records the parent spawn info for a child agent name.
func (s *PartsStore) SetAgentParent(child string, info agentSpawnInfo) {
	s.agentParent[child] = info
}

// AgentParentAll returns the raw parent map for iteration.
func (s *PartsStore) AgentParentAll() map[string]agentSpawnInfo {
	return s.agentParent
}
