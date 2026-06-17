package tui

import (
	"strings"
	"time"
)

// thinkingKey identifies an in-flight ThinkingPart by its owning agent and
// optional group ID. Both fields are required to disambiguate concurrent
// sub-agents that might happen to share a group ID across model boundaries.
type thinkingKey struct {
	agent string
	group string
}

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

	// idxByCallID maps a Tool or SubAgent part's CallID to its parts index.
	// Maintained on every write that introduces or replaces such a part so
	// UpdateToolByCallID / future lookup paths run in O(1) instead of the
	// pre-Stage-3 reverse scan over the whole timeline.
	idxByCallID map[string]int

	// idxByThinkingGroup maps (agent, group) to the parts index of that
	// agent's currently-open ThinkingPart in the named group. Streaming
	// thinking deltas append into the existing part via this index in O(1)
	// instead of the pre-Stage-3 reverse scan.
	idxByThinkingGroup map[thinkingKey]int

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
		streamingByAgent:   make(map[string]*strings.Builder),
		spawnByCallID:      make(map[string]agentSpawnInfo),
		agentParent:        make(map[string]agentSpawnInfo),
		idxByCallID:        make(map[string]int),
		idxByThinkingGroup: make(map[thinkingKey]int),
	}
}

// partCallID returns the CallID a part identifies itself by, or "" when the
// part type has no CallID concept. Centralised so index maintenance and
// lookups agree on which fields carry the join key.
func partCallID(p DisplayPart) string {
	switch p.Type {
	case PartTypeTool:
		if p.Tool != nil {
			return p.Tool.CallID
		}
	case PartTypeSubAgent:
		if p.SubAgent != nil {
			return p.SubAgent.CallID
		}
	}
	return ""
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
// provide one. The assigned ID is returned for convenience. Side indexes
// (idxByCallID) are maintained synchronously so subsequent lookups are O(1).
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
	idx := len(s.parts)
	s.parts = append(s.parts, p)
	if cid := partCallID(p); cid != "" {
		s.idxByCallID[cid] = idx
	}
	if p.Type == PartTypeThinking && p.Thinking != nil && p.Thinking.GroupID != "" {
		s.idxByThinkingGroup[thinkingKey{p.Thinking.AgentName, p.Thinking.GroupID}] = idx
	}
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
// it before handing the slice over). Side indexes are rebuilt from scratch
// so a freshly loaded session has identical lookup semantics to a live one.
func (s *PartsStore) SetAll(parts []DisplayPart) {
	s.parts = parts
	s.idxByCallID = make(map[string]int, len(parts))
	s.idxByThinkingGroup = make(map[thinkingKey]int)
	var maxID uint64
	for i, p := range parts {
		if p.ID > maxID {
			maxID = p.ID
		}
		if cid := partCallID(p); cid != "" {
			s.idxByCallID[cid] = i
		}
		if p.Type == PartTypeThinking && p.Thinking != nil && p.Thinking.GroupID != "" {
			s.idxByThinkingGroup[thinkingKey{p.Thinking.AgentName, p.Thinking.GroupID}] = i
		}
	}
	if maxID > s.nextID {
		s.nextID = maxID
	}
}

// IndexByCallID returns the parts index for the most recent part registered
// under callID. The second return is false when no Tool or SubAgent part with
// that CallID has been recorded.
func (s *PartsStore) IndexByCallID(callID string) (int, bool) {
	i, ok := s.idxByCallID[callID]
	return i, ok
}

// RebuildIndex rebuilds the side indexes from the current parts slice.
// Production code keeps the indexes in sync via Append/Replace/SetAll; tests
// that mutate parts directly (e.g. seed a timeline by assigning store.parts)
// must call this before any index-driven lookup or update.
func (s *PartsStore) RebuildIndex() {
	s.idxByCallID = make(map[string]int, len(s.parts))
	s.idxByThinkingGroup = make(map[thinkingKey]int)
	for i, p := range s.parts {
		if cid := partCallID(p); cid != "" {
			s.idxByCallID[cid] = i
		}
		if p.Type == PartTypeThinking && p.Thinking != nil && p.Thinking.GroupID != "" {
			s.idxByThinkingGroup[thinkingKey{p.Thinking.AgentName, p.Thinking.GroupID}] = i
		}
	}
}

// AppendThinkingDelta extends an in-flight ThinkingPart belonging to (agent,
// groupID) by delta. When no part with that key exists (or groupID is empty),
// a new ThinkingPart is created with the provided timestamp. Returns the part
// ID and a created flag indicating whether a fresh part was added (true) or
// an existing one was extended (false).
//
// The Version of an extended part is bumped so the Renderer's fragment cache
// can invalidate just that fragment on the next Render call.
func (s *PartsStore) AppendThinkingDelta(agentName, groupID, delta string, now time.Time) (id uint64, created bool) {
	if groupID != "" {
		if idx, ok := s.idxByThinkingGroup[thinkingKey{agentName, groupID}]; ok && idx >= 0 && idx < len(s.parts) {
			p := &s.parts[idx]
			if p.Type == PartTypeThinking && p.Thinking != nil {
				p.Thinking.Content += delta
				p.Version++
				return p.ID, false
			}
		}
	}
	id = s.Append(DisplayPart{
		Type:     PartTypeThinking,
		Time:     now,
		Thinking: &ThinkingPart{Content: delta, GroupID: groupID, AgentName: agentName},
	})
	return id, true
}

// UpdateToolByCallID mutates the ToolPart belonging to callID via the closure.
// Returns true on success, false when no Tool part is registered under that
// callID. The caller is responsible for any Version bump on the part (Stage 3
// keeps this manual; Stage 4 will fold it into Renderer invalidation).
func (s *PartsStore) UpdateToolByCallID(callID string, fn func(*ToolPart)) bool {
	idx, ok := s.idxByCallID[callID]
	if !ok || idx < 0 || idx >= len(s.parts) {
		return false
	}
	p := &s.parts[idx]
	if p.Type != PartTypeTool || p.Tool == nil {
		return false
	}
	fn(p.Tool)
	return true
}

// UpdateSubAgentByCallID mutates the SubAgentPart belonging to callID via the
// closure. Returns the parts index on success and -1 when no SubAgent part is
// registered under that callID. The index is returned so callers can do
// follow-up reads (duration calc, summary lookup) without re-scanning.
func (s *PartsStore) UpdateSubAgentByCallID(callID string, fn func(*SubAgentPart)) int {
	idx, ok := s.idxByCallID[callID]
	if !ok || idx < 0 || idx >= len(s.parts) {
		return -1
	}
	p := &s.parts[idx]
	if p.Type != PartTypeSubAgent || p.SubAgent == nil {
		return -1
	}
	fn(p.SubAgent)
	return idx
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
