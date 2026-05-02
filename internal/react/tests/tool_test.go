package react_test

import (
	. "aster/internal/react"
	"testing"
)

func TestParseToolArguments_Nil(t *testing.T) {
	args, err := ParseToolArguments(nil)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(args) != 0 {
		t.Fatalf("args = %#v", args)
	}
}

func TestParseToolArguments_MapClone(t *testing.T) {
	in := map[string]any{"a": 1, "b": "x"}
	out, err := ParseToolArguments(in)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if out["a"] != 1 || out["b"] != "x" {
		t.Fatalf("out = %#v", out)
	}
	out["a"] = 2
	if in["a"] != 1 {
		t.Fatalf("in should not change: %#v", in)
	}
}

func TestParseToolArgumentsString_QuotedJSON(t *testing.T) {
	out, err := ParseToolArgumentsString("\"{\\\"a\\\": 1}\"")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if out["a"] != float64(1) {
		t.Fatalf("out = %#v", out)
	}
}

func TestParseToolArgumentsString_Repair(t *testing.T) {
	out, err := ParseToolArgumentsString("{\"a\": 1")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if out["a"] != float64(1) {
		t.Fatalf("out = %#v", out)
	}
}

func TestParseToolArgumentsString_ExtractorWrappedJSON(t *testing.T) {
	out, err := ParseToolArgumentsString("参数如下：```json\n{\"path\":\"/tmp/a\"}\n```")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if out["path"] != "/tmp/a" {
		t.Fatalf("out = %#v", out)
	}
}
