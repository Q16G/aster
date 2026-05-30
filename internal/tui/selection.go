package tui

import (
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"
)

type SelectionState int

const (
	SelectionNone SelectionState = iota
	SelectionInProgress
	SelectionDone
)

type CellPos struct {
	X, Y int
}

type SelectionModel struct {
	state        SelectionState
	start        CellPos
	end          CellPos
	text         string
	startYOffset int

	lastClickTime time.Time
	lastClickPos  CellPos
	clickCount    int
}

const doubleClickThreshold = 400 * time.Millisecond

func (s *SelectionModel) DetectMultiClick(x, y int) {
	now := time.Now()
	if now.Sub(s.lastClickTime) < doubleClickThreshold &&
		s.lastClickPos.X == x && s.lastClickPos.Y == y {
		s.clickCount++
		if s.clickCount > 3 {
			s.clickCount = 1
		}
	} else {
		s.clickCount = 1
	}
	s.lastClickTime = now
	s.lastClickPos = CellPos{X: x, Y: y}
}

func (s *SelectionModel) Start(x, y int) {
	s.state = SelectionInProgress
	s.start = CellPos{X: x, Y: y}
	s.end = CellPos{X: x, Y: y}
	s.text = ""
}

func (s *SelectionModel) Update(x, y int) {
	s.end = CellPos{X: x, Y: y}
}

func (s *SelectionModel) Finish(x, y int) {
	s.end = CellPos{X: x, Y: y}
	if s.start.X == s.end.X && s.start.Y == s.end.Y {
		s.state = SelectionNone
		return
	}
	s.state = SelectionDone
}

func (s *SelectionModel) Clear() {
	s.state = SelectionNone
	s.start = CellPos{}
	s.end = CellPos{}
	s.text = ""
	s.startYOffset = 0
}

func (s *SelectionModel) HasSelection() bool {
	return s.state == SelectionDone && s.text != ""
}

func (s *SelectionModel) NormalizedRange() (start, end CellPos) {
	start, end = s.start, s.end
	if start.Y > end.Y || (start.Y == end.Y && start.X > end.X) {
		start, end = end, start
	}
	return
}

func cellToRuneIndex(runes []rune, cellPos int) int {
	cells := 0
	for i, r := range runes {
		w := runewidth.RuneWidth(r)
		if cells+w > cellPos {
			return i
		}
		cells += w
	}
	return len(runes)
}

func runesToCellWidth(runes []rune, start, end int) int {
	w := 0
	for i := start; i < end && i < len(runes); i++ {
		w += runewidth.RuneWidth(runes[i])
	}
	return w
}

func (s *SelectionModel) SelectWord(contentLine int, contentCol int, lines []string) {
	if contentLine < 0 || contentLine >= len(lines) {
		s.state = SelectionNone
		return
	}
	plain := ansi.Strip(lines[contentLine])
	runes := []rune(plain)
	runeIdx := cellToRuneIndex(runes, contentCol)
	if runeIdx < 0 || runeIdx >= len(runes) {
		s.state = SelectionNone
		return
	}

	isWordChar := func(r rune) bool {
		return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
	}

	left := runeIdx
	for left > 0 && isWordChar(runes[left-1]) {
		left--
	}
	right := runeIdx
	for right < len(runes)-1 && isWordChar(runes[right+1]) {
		right++
	}

	leftCellDelta := runesToCellWidth(runes, left, runeIdx)
	rightCellDelta := runesToCellWidth(runes, runeIdx, right+1)
	startX := s.lastClickPos.X - leftCellDelta
	if startX < 1 {
		startX = 1
	}
	s.start = CellPos{X: startX, Y: s.lastClickPos.Y}
	s.end = CellPos{X: s.lastClickPos.X + rightCellDelta, Y: s.lastClickPos.Y}
	s.state = SelectionDone
	s.text = string(runes[left : right+1])
}

func (s *SelectionModel) SelectLine(contentLine int, lines []string) {
	if contentLine < 0 || contentLine >= len(lines) {
		s.state = SelectionNone
		return
	}
	plain := ansi.Strip(lines[contentLine])
	s.start = CellPos{X: 1, Y: s.lastClickPos.Y}
	s.end = CellPos{X: ansi.StringWidth(plain) + 1, Y: s.lastClickPos.Y}
	s.state = SelectionDone
	s.text = plain
}

func ApplySelectionHighlight(viewLines []string, sel *SelectionModel) []string {
	if sel.state == SelectionNone {
		return viewLines
	}
	start, end := sel.NormalizedRange()
	if start.Y > end.Y {
		return viewLines
	}

	result := make([]string, len(viewLines))
	const reverseOn = "\x1b[7m"
	const reverseOff = "\x1b[27m"

	for i, line := range viewLines {
		if i < start.Y || i > end.Y {
			result[i] = line
			continue
		}

		lineWidth := ansi.StringWidth(line)

		colStart := 0
		colEnd := lineWidth
		if i == start.Y {
			colStart = start.X - 1
		}
		if i == end.Y {
			colEnd = end.X - 1
		}
		if colStart < 0 {
			colStart = 0
		}
		if colEnd > lineWidth {
			colEnd = lineWidth
		}
		if colStart >= colEnd {
			result[i] = line
			continue
		}

		var sb strings.Builder
		if colStart > 0 {
			sb.WriteString(ansi.Cut(line, 0, colStart))
		}
		sb.WriteString(reverseOn)
		sb.WriteString(ansi.Strip(ansi.Cut(line, colStart, colEnd)))
		sb.WriteString(reverseOff)
		if colEnd < lineWidth {
			sb.WriteString(ansi.Cut(line, colEnd, lineWidth))
		}
		result[i] = sb.String()
	}
	return result
}

func ExtractSelectedText(lines []string, startLine, startCellCol, endLine, endCellCol int) string {
	if startLine < 0 || endLine >= len(lines) || startLine > endLine {
		return ""
	}

	var sb strings.Builder
	for i := startLine; i <= endLine; i++ {
		plain := ansi.Strip(lines[i])
		runes := []rune(plain)
		lineLen := len(runes)

		runeStart := 0
		runeEnd := lineLen
		if i == startLine {
			runeStart = cellToRuneIndex(runes, startCellCol)
		}
		if i == endLine {
			runeEnd = cellToRuneIndex(runes, endCellCol+1)
		}
		if runeStart < 0 {
			runeStart = 0
		}
		if runeEnd > lineLen {
			runeEnd = lineLen
		}
		if runeStart > runeEnd {
			continue
		}

		sb.WriteString(stripLeftGutter(string(runes[runeStart:runeEnd])))
		if i < endLine {
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

// stripLeftGutter removes the decorative left border that chat parts render
// via lipgloss BorderLeft + PaddingLeft(1). When a selection starts at column
// 0 the box-drawing glyph "│" (U+2502) and its padding space land in the
// copied text; they are never meaningful content, so drop them. The ASCII pipe
// "|" used by Markdown tables is left untouched.
func stripLeftGutter(segment string) string {
	rest := strings.TrimLeft(segment, " ")
	glyph := lipgloss.NormalBorder().Left
	if glyph == "" || !strings.HasPrefix(rest, glyph) {
		return segment
	}
	rest = rest[len(glyph):]
	return strings.TrimPrefix(rest, " ")
}
