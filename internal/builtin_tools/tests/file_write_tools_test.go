package builtin_tools_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	. "aster/internal/builtin_tools"
)

func TestReadFileRecordsFullTextObservation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.txt")
	mustWriteFile(t, path, "hello\n")

	store := NewFileObservationStore()
	tool := NewReadFileToolWithObservations(store)
	if _, err := tool.Execute(context.Background(), map[string]any{"path": path, "limit": 10}); err != nil {
		t.Fatalf("read_file: %v", err)
	}

	obs, ok := store.Get(path)
	if !ok {
		t.Fatal("expected observation to be recorded")
	}
	if !obs.IsFullRead() || obs.Content != "hello\n" {
		t.Fatalf("unexpected observation: %+v", obs)
	}
}

func TestReadFileDoesNotRecordPartialObservation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.txt")
	mustWriteFile(t, path, "a\nb\n")

	store := NewFileObservationStore()
	tool := NewReadFileToolWithObservations(store)
	if _, err := tool.Execute(context.Background(), map[string]any{"path": path, "limit": 1}); err != nil {
		t.Fatalf("read_file: %v", err)
	}
	if _, ok := store.Get(path); ok {
		t.Fatal("partial read must not be a write credential")
	}
}

func TestWriteCreatesAndRequiresReadBeforeOverwrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "x.txt")
	store := NewFileObservationStore()
	writeTool := NewWriteTool(store)

	if _, err := writeTool.Execute(context.Background(), map[string]any{
		"file_path": path,
		"content":   "one\n",
	}); err != nil {
		t.Fatalf("write create: %v", err)
	}
	if got := mustReadFile(t, path); got != "one\n" {
		t.Fatalf("created content = %q", got)
	}

	_, err := writeTool.Execute(context.Background(), map[string]any{
		"file_path": path,
		"content":   "two\n",
	})
	if err == nil || !strings.Contains(err.Error(), "Read it first") {
		t.Fatalf("expected read-before-write error, got %v", err)
	}

	readTool := NewReadFileToolWithObservations(store)
	if _, err := readTool.Execute(context.Background(), map[string]any{"path": path, "limit": 10}); err != nil {
		t.Fatalf("read before overwrite: %v", err)
	}
	if _, err := writeTool.Execute(context.Background(), map[string]any{
		"file_path": path,
		"content":   "two\n",
	}); err != nil {
		t.Fatalf("write overwrite: %v", err)
	}
	if got := mustReadFile(t, path); got != "two\n" {
		t.Fatalf("overwritten content = %q", got)
	}
}

func TestWriteRejectsExternalModification(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.txt")
	mustWriteFile(t, path, "one\n")

	store := NewFileObservationStore()
	readTool := NewReadFileToolWithObservations(store)
	writeTool := NewWriteTool(store)
	if _, err := readTool.Execute(context.Background(), map[string]any{"path": path, "limit": 10}); err != nil {
		t.Fatalf("read: %v", err)
	}
	time.Sleep(5 * time.Millisecond)
	mustWriteFile(t, path, "external\n")

	_, err := writeTool.Execute(context.Background(), map[string]any{
		"file_path": path,
		"content":   "two\n",
	})
	if err == nil || !strings.Contains(err.Error(), "modified since read") {
		t.Fatalf("expected stale-write error, got %v", err)
	}
}

func TestWriteAllowsEmptyContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.txt")
	store := NewFileObservationStore()
	writeTool := NewWriteTool(store)

	if _, err := writeTool.Execute(context.Background(), map[string]any{
		"file_path": path,
		"content":   "",
	}); err != nil {
		t.Fatalf("write empty content: %v", err)
	}
	if got := mustReadFile(t, path); got != "" {
		t.Fatalf("created content = %q", got)
	}
}

func TestWriteOverwriteUsesExplicitContentLineEndings(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.txt")
	mustWriteFile(t, path, "one\r\n")
	store := NewFileObservationStore()
	readTool := NewReadFileToolWithObservations(store)
	writeTool := NewWriteTool(store)

	if _, err := readTool.Execute(context.Background(), map[string]any{"path": path, "limit": 10}); err != nil {
		t.Fatalf("read: %v", err)
	}
	if _, err := writeTool.Execute(context.Background(), map[string]any{
		"file_path": path,
		"content":   "two\n",
	}); err != nil {
		t.Fatalf("write overwrite: %v", err)
	}
	if got := mustReadFile(t, path); got != "two\n" {
		t.Fatalf("overwritten content = %q", got)
	}
}

func TestEditUniqueReplaceAndReplaceAll(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.txt")
	mustWriteFile(t, path, "alpha\nbeta\nalpha\n")
	store := NewFileObservationStore()
	readTool := NewReadFileToolWithObservations(store)
	editTool := NewEditTool(store)

	if _, err := readTool.Execute(context.Background(), map[string]any{"path": path, "limit": 10}); err != nil {
		t.Fatalf("read: %v", err)
	}
	_, err := editTool.Execute(context.Background(), map[string]any{
		"file_path":  path,
		"old_string": "alpha",
		"new_string": "omega",
	})
	if err == nil || !strings.Contains(err.Error(), "Found 2 matches") {
		t.Fatalf("expected multi-match error, got %v", err)
	}

	if _, err := editTool.Execute(context.Background(), map[string]any{
		"file_path":   path,
		"old_string":  "alpha",
		"new_string":  "omega",
		"replace_all": true,
	}); err != nil {
		t.Fatalf("replace_all edit: %v", err)
	}
	if got := mustReadFile(t, path); got != "omega\nbeta\nomega\n" {
		t.Fatalf("edited content = %q", got)
	}
}

func TestEditRejectsNotebook(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.ipynb")
	mustWriteFile(t, path, "{}")
	store := NewFileObservationStore()
	editTool := NewEditTool(store)
	_, err := editTool.Execute(context.Background(), map[string]any{
		"file_path":  path,
		"old_string": "{}",
		"new_string": "{}\n",
	})
	if err == nil || !strings.Contains(err.Error(), NotebookEditToolName) {
		t.Fatalf("expected notebook_edit guidance, got %v", err)
	}
}

func TestEditNormalizesQuotesAfterNonASCIIPrefix(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.txt")
	mustWriteFile(t, path, "前缀 “hello”\n")
	store := NewFileObservationStore()
	readTool := NewReadFileToolWithObservations(store)
	editTool := NewEditTool(store)

	if _, err := readTool.Execute(context.Background(), map[string]any{"path": path, "limit": 10}); err != nil {
		t.Fatalf("read: %v", err)
	}
	if _, err := editTool.Execute(context.Background(), map[string]any{
		"file_path":  path,
		"old_string": `"hello"`,
		"new_string": `"bye"`,
	}); err != nil {
		t.Fatalf("quote-normalized edit: %v", err)
	}
	if got := mustReadFile(t, path); got != "前缀 “bye”\n" {
		t.Fatalf("edited content = %q", got)
	}
}

func TestNotebookEditReplaceClearsCodeOutputs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "n.ipynb")
	mustWriteFile(t, path, `{
 "cells": [
  {
   "cell_type": "code",
   "execution_count": 7,
   "id": "abc",
   "metadata": {},
   "outputs": [{"name":"stdout"}],
   "source": "print(1)"
  }
 ],
 "metadata": {"language_info": {"name": "python"}},
 "nbformat": 4,
 "nbformat_minor": 5
}`)
	store := NewFileObservationStore()
	readTool := NewReadFileToolWithObservations(store)
	nbTool := NewNotebookEditTool(store)
	if _, err := readTool.Execute(context.Background(), map[string]any{"path": path, "limit": 100}); err != nil {
		t.Fatalf("read notebook: %v", err)
	}
	out, err := nbTool.Execute(context.Background(), map[string]any{
		"notebook_path": path,
		"cell_id":       "abc",
		"new_source":    "print(2)",
	})
	if err != nil {
		t.Fatalf("notebook edit: %v", err)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if result["edit_mode"] != "replace" {
		t.Fatalf("unexpected result: %s", out)
	}

	var notebook map[string]any
	if err := json.Unmarshal([]byte(mustReadFile(t, path)), &notebook); err != nil {
		t.Fatalf("decode notebook: %v", err)
	}
	cell := notebook["cells"].([]any)[0].(map[string]any)
	if cell["source"] != "print(2)" {
		t.Fatalf("source = %v", cell["source"])
	}
	if cell["execution_count"] != nil {
		t.Fatalf("execution_count = %v", cell["execution_count"])
	}
	if outputs := cell["outputs"].([]any); len(outputs) != 0 {
		t.Fatalf("outputs = %+v", outputs)
	}
}

func mustReadFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}
