package ui

import (
	"strings"
	"testing"
)

func TestCommandPickerHeightMatchesRenderedLines(t *testing.T) {
	picker := NewCommandPickerModel(nil, 40)
	view := picker.View()
	if lines := strings.Count(view, "\n") + 1; lines != picker.Height() {
		t.Fatalf("expected %d lines, got %d", picker.Height(), lines)
	}
}

func TestCommandPickerLongDescriptionDoesNotWrap(t *testing.T) {
	picker := NewCommandPickerModel([]CommandEntry{
		{Name: "/agent", Description: strings.Repeat("verylongdescription", 8)},
	}, 24)
	view := picker.View()
	if lines := strings.Count(view, "\n") + 1; lines != picker.Height() {
		t.Fatalf("expected %d lines, got %d", picker.Height(), lines)
	}
}

func TestFilePickerHeightMatchesRenderedLines(t *testing.T) {
	picker := &FilePickerModel{width: 40}
	view := picker.View()
	if lines := strings.Count(view, "\n") + 1; lines != picker.Height() {
		t.Fatalf("expected %d lines, got %d", picker.Height(), lines)
	}
}

func TestFilePickerLongPathDoesNotWrap(t *testing.T) {
	picker := &FilePickerModel{
		width:    24,
		filtered: []int{0},
		files: []FileEntry{{
			RelPath: "some/really/long/path/to/a/file.txt",
		}},
	}
	view := picker.View()
	if lines := strings.Count(view, "\n") + 1; lines != picker.Height() {
		t.Fatalf("expected %d lines, got %d", picker.Height(), lines)
	}
}
