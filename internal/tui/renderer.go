package tui

import "strings"

// renderedFragment is one part's cached visual contribution to the timeline
// string. Cached by (id, version, width); any of those changing invalidates
// the entry. lineCount is precomputed so partLineOffsets accumulation skips a
// strings.Count pass.
type renderedFragment struct {
	version   uint32
	width     int
	content   string
	lineCount int
}

// Renderer caches per-part rendered strings keyed by DisplayPart.ID. The cache
// is populated lazily on miss and invalidated by drainStore() on the store at
// the start of each Render call. A width change empties the entire cache: most
// rendered output is width-sensitive (lipgloss border, padding, wrap), so a
// per-part width compare every iteration is wasteful when a resize hits.
//
// Cursor/focus changes are handled outside this struct: ChatModel.setCursor /
// setFocused mark the involved part IDs dirty in the store so DrainDirty
// surfaces them next pass. The Renderer itself has no view-state coupling.
type Renderer struct {
	width int
	frag  map[uint64]renderedFragment
}

// NewRenderer constructs an empty fragment cache.
func NewRenderer() *Renderer {
	return &Renderer{frag: make(map[uint64]renderedFragment)}
}

// invalidate evicts the cached fragment for id, if any.
func (r *Renderer) invalidate(id uint64) {
	delete(r.frag, id)
}

// invalidateAll drops the entire cache. Used on width change.
func (r *Renderer) invalidateAll() {
	if len(r.frag) > 0 {
		r.frag = make(map[uint64]renderedFragment)
	}
}

// fragmentFor returns the rendered string and line count for part p at the
// given width. Cache hit when (id, version, width) match; otherwise miss is
// invoked to produce the fresh rendering and the result is cached.
//
// Caching note: callers are responsible for marking the part dirty when any
// rendering input outside (id, version, width) changes — selection state
// (cursor + focused) is the typical example, handled by ChatModel.setCursor.
func (r *Renderer) fragmentFor(p DisplayPart, width int, miss func() string) (content string, lineCount int) {
	if width != r.width {
		r.width = width
		r.invalidateAll()
	}
	if f, ok := r.frag[p.ID]; ok && f.version == p.Version && f.width == width {
		return f.content, f.lineCount
	}
	content = miss()
	if content == "" {
		lineCount = 0
	} else {
		lineCount = strings.Count(content, "\n") + 1
	}
	r.frag[p.ID] = renderedFragment{
		version:   p.Version,
		width:     width,
		content:   content,
		lineCount: lineCount,
	}
	return content, lineCount
}

// drainStore evicts every dirty part ID from the cache. Called at the top of
// each refreshContent pass so subsequent fragmentFor lookups hit only fresh
// entries. Width-change invalidation is handled inside fragmentFor.
func (r *Renderer) drainStore(s *PartsStore) {
	for _, id := range s.DrainDirty() {
		r.invalidate(id)
	}
}
