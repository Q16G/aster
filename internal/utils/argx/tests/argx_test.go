package argx_test

import (
	. "aster/internal/utils/argx"
	"fmt"
	"testing"
)

type testStringer string

func (s testStringer) String() string { return string(s) }

func TestText(t *testing.T) {
	if got := Text(nil); got != "" {
		t.Fatalf("Text(nil) = %q", got)
	}

	var p *int
	if got := Text(p); got != "" {
		t.Fatalf("Text(typed nil) = %q", got)
	}

	if got := Text("  hello  "); got != "hello" {
		t.Fatalf("Text(string) = %q", got)
	}

	if got := Text("<nil>"); got != "" {
		t.Fatalf("Text(<nil>) = %q", got)
	}

	if got := Text([]byte("  bytes  ")); got != "bytes" {
		t.Fatalf("Text([]byte) = %q", got)
	}
	if got := Text([]byte("<nil>")); got != "" {
		t.Fatalf("Text([]byte <nil>) = %q", got)
	}

	if got := Text(testStringer("  world  ")); got != "world" {
		t.Fatalf("Text(Stringer) = %q", got)
	}

	if got := Text(123); got != "123" {
		t.Fatalf("Text(number) = %q", got)
	}
}

func TestOptionalAndRequiredText(t *testing.T) {
	args := map[string]any{
		"a": "  x  ",
		"b": "<nil>",
	}

	if got := OptionalText(nil, "a"); got != "" {
		t.Fatalf("OptionalText(nil) = %q", got)
	}
	if got := OptionalText(args, "missing"); got != "" {
		t.Fatalf("OptionalText(missing) = %q", got)
	}
	if got := OptionalText(args, "a"); got != "x" {
		t.Fatalf("OptionalText(a) = %q", got)
	}
	if got := OptionalText(args, "b"); got != "" {
		t.Fatalf("OptionalText(b) = %q", got)
	}

	if _, err := RequiredText(args, "missing"); err == nil {
		t.Fatalf("RequiredText(missing) should error")
	}
	if _, err := RequiredText(args, "b"); err == nil {
		t.Fatalf("RequiredText(<nil>) should error")
	}
	if got, err := RequiredText(args, "a"); err != nil || got != "x" {
		t.Fatalf("RequiredText(a) = %q, %v", got, err)
	}
}

func TestStringSlice(t *testing.T) {
	if got := StringSlice(nil); got != nil {
		t.Fatalf("StringSlice(nil) = %#v", got)
	}

	anySlice := []any{" a ", nil, "<nil>", "b", 123}
	got := StringSlice(anySlice)
	if fmt.Sprint(got) != "[a b 123]" {
		t.Fatalf("StringSlice([]any) = %#v", got)
	}

	stringSlice := []string{" a ", "", "<nil>", "b"}
	got = StringSlice(stringSlice)
	if fmt.Sprint(got) != "[a b]" {
		t.Fatalf("StringSlice([]string) = %#v", got)
	}

	got = StringSlice("  x  ")
	if fmt.Sprint(got) != "[x]" {
		t.Fatalf("StringSlice(string) = %#v", got)
	}

	got = StringSlice("[\"java\", \"go\"]")
	if fmt.Sprint(got) != "[java go]" {
		t.Fatalf("StringSlice(json array string) = %#v", got)
	}

	got = StringSlice("[]")
	if got != nil {
		t.Fatalf("StringSlice(empty json array string) = %#v", got)
	}
}

func TestArrayArgs(t *testing.T) {
	args := map[string]any{
		"items": []any{1},
		"empty": []any{},
		"bad":   "nope",
	}

	if _, err := RequiredArray(nil, "items"); err == nil {
		t.Fatalf("RequiredArray(nil) should error")
	}
	if _, err := RequiredArray(args, "missing"); err == nil {
		t.Fatalf("RequiredArray(missing) should error")
	}
	if _, err := RequiredArray(args, "empty"); err == nil {
		t.Fatalf("RequiredArray(empty) should error")
	}
	if _, err := RequiredArray(args, "bad"); err == nil {
		t.Fatalf("RequiredArray(bad) should error")
	}
	if got, err := RequiredArray(args, "items"); err != nil || got == nil {
		t.Fatalf("RequiredArray(items) = %#v, %v", got, err)
	}

	if _, ok, err := OptionalArray(args, "missing"); err != nil || ok {
		t.Fatalf("OptionalArray(missing) = ok:%v err:%v", ok, err)
	}
	if _, ok, err := OptionalArray(args, "empty"); err != nil || ok {
		t.Fatalf("OptionalArray(empty) = ok:%v err:%v", ok, err)
	}
	if _, ok, err := OptionalArray(args, "bad"); err == nil || ok {
		t.Fatalf("OptionalArray(bad) should error, got ok:%v err:%v", ok, err)
	}
	if _, ok, err := OptionalArray(args, "items"); err != nil || !ok {
		t.Fatalf("OptionalArray(items) = ok:%v err:%v", ok, err)
	}
}

func TestOptionalObject(t *testing.T) {
	args := map[string]any{
		"obj": map[string]any{"k": "v"},
		"bad": []any{1},
	}

	if _, ok, err := OptionalObject(nil, "obj"); err != nil || ok {
		t.Fatalf("OptionalObject(nil) = ok:%v err:%v", ok, err)
	}
	if _, ok, err := OptionalObject(args, "missing"); err != nil || ok {
		t.Fatalf("OptionalObject(missing) = ok:%v err:%v", ok, err)
	}
	if _, ok, err := OptionalObject(args, "obj"); err != nil || !ok {
		t.Fatalf("OptionalObject(obj) = ok:%v err:%v", ok, err)
	}
	if _, ok, err := OptionalObject(args, "bad"); err == nil || ok {
		t.Fatalf("OptionalObject(bad) should error, got ok:%v err:%v", ok, err)
	}
}
