package jsonextractor_test

import (
	. "aster/internal/jsonextractor"
	"encoding/json"
	"testing"
)

func TestExtractJSONWithRaw_ExtractsWrappedJSON(t *testing.T) {
	stdjsons, raw := ExtractJSONWithRaw("前言\n```json\n{\"a\":1,\"b\":\"x\"}\n```\n后记")
	if len(stdjsons) != 1 {
		t.Fatalf("expected 1 valid json, got %d (%#v)", len(stdjsons), stdjsons)
	}
	if stdjsons[0] != "{\"a\":1,\"b\":\"x\"}" {
		t.Fatalf("unexpected json candidate: %q", stdjsons[0])
	}
	if len(raw) != 0 {
		t.Fatalf("expected no raw candidates, got %#v", raw)
	}
}

func TestExtractJSONWithRaw_CollectsInvalidCandidateAsRaw(t *testing.T) {
	stdjsons, raw := ExtractJSONWithRaw("prefix {a:1} suffix")
	if len(stdjsons) != 0 {
		t.Fatalf("expected no valid json, got %#v", stdjsons)
	}
	if len(raw) != 1 || raw[0] != "{a:1}" {
		t.Fatalf("unexpected raw candidates: %#v", raw)
	}
}

func TestExtractJSONWithRaw_ExtractsMultipleCandidates(t *testing.T) {
	stdjsons, raw := ExtractJSONWithRaw(`prefix [{"a":1}] suffix {"b":2}`)
	if len(raw) != 0 {
		t.Fatalf("expected no raw candidates, got %#v", raw)
	}
	if len(stdjsons) != 2 {
		t.Fatalf("expected 2 json candidates, got %#v", stdjsons)
	}
	if stdjsons[0] != `[{"a":1}]` || stdjsons[1] != `{"b":2}` {
		t.Fatalf("unexpected candidates: %#v", stdjsons)
	}
}

func TestFixJson_RepairsHexEscape(t *testing.T) {
	fixed := FixJson([]byte(`{"value":"\x41"}`))
	if !json.Valid(fixed) {
		t.Fatalf("expected valid json after fix, got %q", fixed)
	}
	if string(fixed) != `{"value":"\u0041"}` {
		t.Fatalf("unexpected fixed json: %q", fixed)
	}
}
