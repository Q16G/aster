package jsonextractor

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/tidwall/gjson"
)

var (
	reHexQuoted = regexp.MustCompile(`(?P<quoted>(\\x[0-9a-fA-F]{2}))`)
)

type stringStack struct {
	items []string
}

func (s *stringStack) Push(value string) {
	s.items = append(s.items, value)
}

func (s *stringStack) Pop() (string, bool) {
	if len(s.items) == 0 {
		return "", false
	}
	idx := len(s.items) - 1
	value := s.items[idx]
	s.items = s.items[:idx]
	return value, true
}

func (s *stringStack) Peek() (string, bool) {
	if len(s.items) == 0 {
		return "", false
	}
	return s.items[len(s.items)-1], true
}

// FixJson repairs common malformed escape sequences in otherwise recoverable JSON.
func FixJson(data []byte) []byte {
	return reHexQuoted.ReplaceAllFunc(data, func(fragment []byte) []byte {
		raw, err := strconv.Unquote(`"` + string(fragment) + `"`)
		if err != nil || len(raw) == 0 {
			return fragment
		}
		return []byte(fmt.Sprintf(`\u%04x`, raw[0]))
	})
}

// JsonValidObject returns a valid JSON object/array if the input can be accepted directly
// or trivially normalized by gjson.
func JsonValidObject(data []byte) ([]byte, bool) {
	if gjson.ValidBytes(data) {
		return data, true
	}

	result := gjson.ParseBytes(data)
	if result.IsArray() {
		return []byte(result.Raw), true
	}

	if !result.IsObject() {
		return nil, false
	}

	fields := make([]string, 0, len(result.Map()))
	for key, value := range result.Map() {
		keyJSON, _ := json.Marshal(key)
		fields = append(fields, fmt.Sprintf("%s: %s", keyJSON, value.Raw))
	}
	if len(fields) == 0 {
		return nil, false
	}
	sort.Strings(fields)
	return []byte("{" + strings.Join(fields, ", ") + "}"), true
}

const (
	stateSingleQuoteString = "single-quote"
	stateDoubleQuoteString = "double-quote"
	stateJSONObj           = "json-object"
	stateJSONArray         = "json-array"
	stateData              = "data"
	stateReset             = "reset"
	stateQuote             = "quote"
)

// ExtractObjectIndexes returns all object/array boundaries discovered in the input.
func ExtractObjectIndexes(content string) [][2]int {
	scanner := bufio.NewScanner(bytes.NewBufferString(content))
	scanner.Split(bufio.ScanBytes)

	index := -1
	objectDepth := 0
	objectDepthIndexTable := make(map[int]int)
	arrayDepth := 0
	arrayDepthIndexTable := make(map[int]int)

	results := make([][2]int, 0)
	stack := &stringStack{}
	pushState := func(value string) {
		if value == stateJSONObj {
			objectDepth++
			if _, exists := objectDepthIndexTable[objectDepth]; !exists {
				objectDepthIndexTable[objectDepth] = index
			}
		} else if value == stateJSONArray {
			arrayDepth++
			if _, exists := arrayDepthIndexTable[arrayDepth]; !exists {
				arrayDepthIndexTable[arrayDepth] = index
			}
		}
		stack.Push(value)
	}
	popState := func() {
		value, ok := stack.Pop()
		if !ok {
			return
		}
		switch value {
		case stateJSONObj:
			if start, exists := objectDepthIndexTable[objectDepth]; exists && start >= 0 {
				results = append(results, [2]int{start, index + 1})
			}
			delete(objectDepthIndexTable, objectDepth)
			if objectDepth == 0 {
				objectDepthIndexTable = make(map[int]int)
			}
			objectDepth--
		case stateJSONArray:
			if start, exists := arrayDepthIndexTable[arrayDepth]; exists && start >= 0 {
				results = append(results, [2]int{start, index + 1})
			}
			delete(arrayDepthIndexTable, arrayDepth)
			if arrayDepth == 0 {
				arrayDepthIndexTable = make(map[int]int)
			}
			arrayDepth--
		}
	}
	currentState := func() string {
		if value, ok := stack.Peek(); ok {
			return value
		}
		return stateReset
	}

	pushState(stateData)
	for scanner.Scan() {
		index++
		bytes := scanner.Bytes()
		if len(bytes) == 0 {
			break
		}
		ch := bytes[0]

		switch currentState() {
		case stateData:
			switch ch {
			case '{':
				pushState(stateJSONObj)
			case '[':
				pushState(stateJSONArray)
			case '"':
				pushState(stateDoubleQuoteString)
			case '\'':
				pushState(stateSingleQuoteString)
			}
		case stateJSONObj:
			switch ch {
			case '{':
				pushState(stateJSONObj)
			case '[':
				pushState(stateJSONArray)
			case '"':
				pushState(stateDoubleQuoteString)
			case '\'':
				pushState(stateSingleQuoteString)
			case '}':
				popState()
			}
		case stateJSONArray:
			switch ch {
			case '{':
				pushState(stateJSONObj)
			case '[':
				pushState(stateJSONArray)
			case '"':
				pushState(stateDoubleQuoteString)
			case '\'':
				pushState(stateSingleQuoteString)
			case ']':
				popState()
			}
		case stateDoubleQuoteString:
			switch ch {
			case '\\':
				pushState(stateQuote)
			case '"':
				popState()
			}
		case stateSingleQuoteString:
			switch ch {
			case '\\':
				pushState(stateQuote)
			case '\'':
				popState()
			}
		case stateQuote:
			popState()
		case stateReset:
			pushState(stateData)
		}
	}

	blocks := make([][2]int, 0, len(results))
	current := [2]int{-1, -1}
	sort.SliceStable(results, func(i, j int) bool {
		return results[i][0] < results[j][0]
	})
	currentBlockIsJSON := func() bool {
		if current[0] < 0 || current[1] <= current[0] {
			return false
		}
		return json.Valid([]byte(content[current[0]:current[1]]))
	}
	for _, result := range results {
		raw := content[result[0]:result[1]]
		_, valid := JsonValidObject([]byte(raw))
		if current[0] < 0 {
			current = result
			continue
		}
		if result[0] >= current[0] && result[1] <= current[1] && currentBlockIsJSON() {
			continue
		}
		blocks = append(blocks, current)
		if valid {
			current = result
			continue
		}
		blocks = append(blocks, result)
		current = [2]int{-1, -1}
	}
	if current[0] >= 0 {
		blocks = append(blocks, current)
	}
	return blocks
}

// ExtractJSONWithRaw returns valid JSON snippets first, and raw candidates second.
func ExtractJSONWithRaw(raw string) (results []string, rawStr []string) {
	defer func() {
		_ = recover()
	}()

	extraValid := make([]string, 0)
	for _, pair := range ExtractObjectIndexes(raw) {
		jsonStr := strings.TrimSpace(raw[pair[0]:pair[1]])
		if jsonStr == "" {
			continue
		}
		if repaired, ok := JsonValidObject([]byte(jsonStr)); ok {
			if !json.Valid([]byte(jsonStr)) {
				rawStr = append(rawStr, jsonStr)
				extraValid = append(extraValid, string(repaired))
			} else {
				results = append(results, jsonStr)
			}
			continue
		}
		rawStr = append(rawStr, jsonStr)
	}
	if len(extraValid) > 0 {
		results = append(results, extraValid...)
	}
	return results, rawStr
}

// ExtractStandardJSON returns all valid JSON snippets discovered in the input.
func ExtractStandardJSON(raw string) []string {
	results, _ := ExtractJSONWithRaw(raw)
	return results
}

// ExtractObjectsOnly keeps compatibility with the yaklang helper by returning
// object-shaped JSON values even when they are nested inside arrays.
func ExtractObjectsOnly(raw string) []string {
	results := make([]string, 0)
	for _, candidate := range ExtractStandardJSON(raw) {
		result := gjson.Parse(candidate)
		switch {
		case result.IsObject():
			results = append(results, result.Raw)
		case result.IsArray():
			result.ForEach(func(_, value gjson.Result) bool {
				if value.IsObject() {
					results = append(results, value.Raw)
				}
				return true
			})
		}
	}
	return results
}
