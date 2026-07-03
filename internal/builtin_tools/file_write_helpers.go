package builtin_tools

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"
)

const (
	fileUnexpectedlyModifiedError = "File has been modified since read, either by the user or by a linter. Read it again before attempting to write it."
	fileNotReadError              = "File has not been read yet. Read it first before writing to it."
)

type fileTextSnapshot struct {
	Content  string
	ModTime  time.Time
	Encoding string
	Newline  string
	Exists   bool
}

func rawStringArg(args map[string]any, key string, required bool) (string, error) {
	if args == nil {
		if required {
			return "", fmt.Errorf("%s is required", key)
		}
		return "", nil
	}
	value, ok := args[key]
	if !ok || value == nil {
		if required {
			return "", fmt.Errorf("%s is required", key)
		}
		return "", nil
	}
	switch v := value.(type) {
	case string:
		if required && v == "" {
			return "", fmt.Errorf("%s is required", key)
		}
		return v, nil
	case []byte:
		s := string(v)
		if required && s == "" {
			return "", fmt.Errorf("%s is required", key)
		}
		return s, nil
	default:
		s := fmt.Sprint(value)
		if required && s == "" {
			return "", fmt.Errorf("%s is required", key)
		}
		return s, nil
	}
}

func optionalBoolArg(args map[string]any, key string) bool {
	if args == nil {
		return false
	}
	switch v := args[key].(type) {
	case bool:
		return v
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "1", "true", "yes", "y", "on":
			return true
		default:
			return false
		}
	default:
		return false
	}
}

func readTextFileSnapshot(path string) (fileTextSnapshot, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fileTextSnapshot{Encoding: "utf8", Newline: "LF", Exists: false}, nil
		}
		return fileTextSnapshot{}, fmt.Errorf("read file: %w", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return fileTextSnapshot{}, fmt.Errorf("stat file: %w", err)
	}
	encoding := "utf8"
	if len(data) >= 2 && data[0] == 0xff && data[1] == 0xfe {
		encoding = "utf16le"
	}
	if encoding == "utf8" && bytes.IndexByte(data, 0) >= 0 {
		return fileTextSnapshot{}, fmt.Errorf("binary file is not supported")
	}
	content := string(data)
	if encoding == "utf16le" {
		content = decodeUTF16LE(data[2:])
	}
	return fileTextSnapshot{
		Content:  normalizeCRLF(content),
		ModTime:  info.ModTime(),
		Encoding: encoding,
		Newline:  detectLineEnding(content),
		Exists:   true,
	}, nil
}

func writeTextFile(path string, content string, encoding string, newline string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create parent directory: %w", err)
	}
	out := content
	if newline == "CRLF" {
		out = strings.ReplaceAll(strings.ReplaceAll(out, "\r\n", "\n"), "\n", "\r\n")
	}
	var data []byte
	if encoding == "utf16le" {
		data = append([]byte{0xff, 0xfe}, encodeUTF16LE(out)...)
	} else {
		data = []byte(out)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write file: %w", err)
	}
	return nil
}

func validateReadBeforeWrite(store *FileObservationStore, path string, snap fileTextSnapshot) error {
	if !snap.Exists {
		return nil
	}
	obs, ok := store.Get(path)
	if !ok || !obs.IsFullRead() || !obs.ReadCredential {
		return fmt.Errorf(fileNotReadError)
	}
	lastWrite := snap.ModTime.UnixMilli()
	if lastWrite > obs.ModTimeMillis && snap.Content != obs.Content {
		return fmt.Errorf(fileUnexpectedlyModifiedError)
	}
	return nil
}

func recordPostWrite(store *FileObservationStore, path string, content string) {
	if store == nil {
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		return
	}
	previous, hadPrevious := store.Get(path)
	store.Record(FileObservation{
		Path:           path,
		Content:        normalizeCRLF(content),
		ModTime:        info.ModTime(),
		ModTimeMillis:  info.ModTime().UnixMilli(),
		ReadCredential: hadPrevious && previous.IsFullRead() && previous.ReadCredential,
	})
}

func normalizeCRLF(s string) string {
	return strings.ReplaceAll(s, "\r\n", "\n")
}

func detectLineEnding(s string) string {
	if strings.Contains(s, "\r\n") {
		return "CRLF"
	}
	return "LF"
}

func decodeUTF16LE(data []byte) string {
	u16 := make([]uint16, 0, len(data)/2)
	for i := 0; i+1 < len(data); i += 2 {
		u16 = append(u16, uint16(data[i])|uint16(data[i+1])<<8)
	}
	return string(utf16.Decode(u16))
}

func encodeUTF16LE(s string) []byte {
	u16 := utf16.Encode([]rune(s))
	out := make([]byte, 0, len(u16)*2)
	for _, v := range u16 {
		out = append(out, byte(v), byte(v>>8))
	}
	return out
}

func countOccurrences(s, substr string) int {
	if substr == "" {
		return 0
	}
	count := 0
	start := 0
	for {
		idx := strings.Index(s[start:], substr)
		if idx < 0 {
			return count
		}
		count++
		start += idx + len(substr)
	}
}

func applyEditToFile(originalContent, oldString, newString string, replaceAll bool) string {
	replace := func(content, search, repl string) string {
		if replaceAll {
			return strings.ReplaceAll(content, search, repl)
		}
		return strings.Replace(content, search, repl, 1)
	}
	if newString != "" {
		return replace(originalContent, oldString, newString)
	}
	stripTrailingNewline := !strings.HasSuffix(oldString, "\n") && strings.Contains(originalContent, oldString+"\n")
	if stripTrailingNewline {
		return replace(originalContent, oldString+"\n", newString)
	}
	return replace(originalContent, oldString, newString)
}

func normalizeQuotes(s string) string {
	replacer := strings.NewReplacer(
		"‘", "'",
		"’", "'",
		"“", "\"",
		"”", "\"",
	)
	return replacer.Replace(s)
}

func findActualString(fileContent, searchString string) string {
	if strings.Contains(fileContent, searchString) {
		return searchString
	}
	normalizedSearch := normalizeQuotes(searchString)
	normalizedFile := normalizeQuotes(fileContent)
	idx := strings.Index(normalizedFile, normalizedSearch)
	if idx < 0 {
		return ""
	}
	runeStart := utf8.RuneCountInString(normalizedFile[:idx])
	runes := []rune(fileContent)
	searchRunes := []rune(searchString)
	if runeStart+len(searchRunes) > len(runes) {
		return ""
	}
	return string(runes[runeStart : runeStart+len(searchRunes)])
}

func preserveQuoteStyle(oldString, actualOldString, newString string) string {
	if oldString == actualOldString {
		return newString
	}
	hasDouble := strings.ContainsAny(actualOldString, "“”")
	hasSingle := strings.ContainsAny(actualOldString, "‘’")
	out := newString
	if hasDouble {
		out = applyCurlyDoubleQuotes(out)
	}
	if hasSingle {
		out = applyCurlySingleQuotes(out)
	}
	return out
}

func applyCurlyDoubleQuotes(s string) string {
	runes := []rune(s)
	var b strings.Builder
	for i, r := range runes {
		if r == '"' {
			if isOpeningQuoteContext(runes, i) {
				b.WriteRune('“')
			} else {
				b.WriteRune('”')
			}
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func applyCurlySingleQuotes(s string) string {
	runes := []rune(s)
	var b strings.Builder
	for i, r := range runes {
		if r == '\'' {
			prevIsLetter := i > 0 && unicode.IsLetter(runes[i-1])
			nextIsLetter := i < len(runes)-1 && unicode.IsLetter(runes[i+1])
			if prevIsLetter && nextIsLetter {
				b.WriteRune('’')
			} else if isOpeningQuoteContext(runes, i) {
				b.WriteRune('‘')
			} else {
				b.WriteRune('’')
			}
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func isOpeningQuoteContext(runes []rune, index int) bool {
	if index == 0 {
		return true
	}
	switch runes[index-1] {
	case ' ', '\t', '\n', '\r', '(', '[', '{', '—', '–':
		return true
	default:
		return false
	}
}

type structuredPatchHunk struct {
	OldStart int      `json:"oldStart"`
	OldLines int      `json:"oldLines"`
	NewStart int      `json:"newStart"`
	NewLines int      `json:"newLines"`
	Lines    []string `json:"lines"`
}

func buildSimplePatch(oldContent, newContent string) []structuredPatchHunk {
	if oldContent == newContent {
		return nil
	}
	oldLines := splitPatchLines(oldContent)
	newLines := splitPatchLines(newContent)
	lines := make([]string, 0, len(oldLines)+len(newLines))
	for _, line := range oldLines {
		lines = append(lines, "-"+line)
	}
	for _, line := range newLines {
		lines = append(lines, "+"+line)
	}
	return []structuredPatchHunk{{
		OldStart: 1,
		OldLines: len(oldLines),
		NewStart: 1,
		NewLines: len(newLines),
		Lines:    lines,
	}}
}

func splitPatchLines(s string) []string {
	if s == "" {
		return nil
	}
	lines := strings.Split(strings.TrimSuffix(s, "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil
	}
	return lines
}

func jsonResult(payload any) (string, error) {
	out, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func parseCellID(cellID string) (int, bool) {
	cellID = strings.TrimSpace(cellID)
	if cellID == "" {
		return 0, false
	}
	re := regexp.MustCompile(`^cell-(\d+)$`)
	match := re.FindStringSubmatch(cellID)
	if len(match) != 2 {
		return 0, false
	}
	var idx int
	if _, err := fmt.Sscanf(match[1], "%d", &idx); err != nil {
		return 0, false
	}
	return idx, true
}
