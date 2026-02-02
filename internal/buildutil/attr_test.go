package buildutil

import (
	"testing"

	"github.com/albertocavalcante/go-bzlmod/third_party/buildtools/build"
)

func parseCall(t *testing.T, content string) *build.CallExpr {
	t.Helper()
	f, err := build.ParseModule("test.bzl", []byte(content))
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if len(f.Stmt) == 0 {
		t.Fatal("no statements parsed")
	}
	call, ok := f.Stmt[0].(*build.CallExpr)
	if !ok {
		t.Fatalf("expected CallExpr, got %T", f.Stmt[0])
	}
	return call
}

func TestString(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		attrName string
		want     string
	}{
		{
			name:     "named string attribute",
			input:    `foo(name = "bar")`,
			attrName: "name",
			want:     "bar",
		},
		{
			name:     "missing attribute",
			input:    `foo(other = "value")`,
			attrName: "name",
			want:     "",
		},
		{
			name:     "non-string attribute",
			input:    `foo(name = 123)`,
			attrName: "name",
			want:     "",
		},
		{
			name:     "first positional when name empty",
			input:    `foo("positional")`,
			attrName: "",
			want:     "positional",
		},
		{
			name:     "empty call with empty name",
			input:    `foo()`,
			attrName: "",
			want:     "",
		},
		{
			name:     "multiple attributes",
			input:    `foo(a = "first", b = "second", c = "third")`,
			attrName: "b",
			want:     "second",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			call := parseCall(t, tt.input)
			got := String(call, tt.attrName)
			if got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestInt(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		attrName string
		want     int
	}{
		{
			name:     "integer attribute",
			input:    `foo(count = 42)`,
			attrName: "count",
			want:     42,
		},
		{
			name:     "zero value",
			input:    `foo(count = 0)`,
			attrName: "count",
			want:     0,
		},
		{
			name:     "missing attribute",
			input:    `foo(other = 1)`,
			attrName: "count",
			want:     0,
		},
		{
			name:     "string instead of int",
			input:    `foo(count = "42")`,
			attrName: "count",
			want:     0,
		},
		{
			name:     "negative number returns 0",
			input:    `foo(count = -5)`,
			attrName: "count",
			want:     0, // buildtools parses -5 as UnaryExpr, not LiteralExpr
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			call := parseCall(t, tt.input)
			got := Int(call, tt.attrName)
			if got != tt.want {
				t.Errorf("Int() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestBool(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		attrName string
		want     bool
	}{
		{
			name:     "True value",
			input:    `foo(enabled = True)`,
			attrName: "enabled",
			want:     true,
		},
		{
			name:     "False value",
			input:    `foo(enabled = False)`,
			attrName: "enabled",
			want:     false,
		},
		{
			name:     "missing attribute",
			input:    `foo(other = True)`,
			attrName: "enabled",
			want:     false,
		},
		{
			name:     "string instead of bool",
			input:    `foo(enabled = "True")`,
			attrName: "enabled",
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			call := parseCall(t, tt.input)
			got := Bool(call, tt.attrName)
			if got != tt.want {
				t.Errorf("Bool() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStringList(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		attrName string
		want     []string
	}{
		{
			name:     "simple string list",
			input:    `foo(items = ["a", "b", "c"])`,
			attrName: "items",
			want:     []string{"a", "b", "c"},
		},
		{
			name:     "empty list",
			input:    `foo(items = [])`,
			attrName: "items",
			want:     []string{},
		},
		{
			name:     "missing attribute",
			input:    `foo(other = ["x"])`,
			attrName: "items",
			want:     nil,
		},
		{
			name:     "not a list",
			input:    `foo(items = "single")`,
			attrName: "items",
			want:     nil,
		},
		{
			name:     "mixed types skips non-strings",
			input:    `foo(items = ["a", 1, "b"])`,
			attrName: "items",
			want:     []string{"a", "b"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			call := parseCall(t, tt.input)
			got := StringList(call, tt.attrName)
			if tt.want == nil {
				if got != nil {
					t.Errorf("StringList() = %v, want nil", got)
				}
				return
			}
			if len(got) != len(tt.want) {
				t.Errorf("StringList() len = %d, want %d", len(got), len(tt.want))
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("StringList()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestPositionalStrings(t *testing.T) {
	tests := []struct {
		name  string
		input string
		skip  int
		want  []string
	}{
		{
			name:  "all positional strings",
			input: `foo("a", "b", "c")`,
			skip:  0,
			want:  []string{"a", "b", "c"},
		},
		{
			name:  "skip first",
			input: `foo("skip", "a", "b")`,
			skip:  1,
			want:  []string{"a", "b"},
		},
		{
			name:  "mixed positional and named",
			input: `foo("a", "b", name = "named")`,
			skip:  0,
			want:  []string{"a", "b"},
		},
		{
			name:  "skip all",
			input: `foo("a", "b")`,
			skip:  5,
			want:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			call := parseCall(t, tt.input)
			got := PositionalStrings(call, tt.skip)
			if len(got) != len(tt.want) {
				t.Errorf("PositionalStrings() = %v, want %v", got, tt.want)
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("PositionalStrings()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestExtractValue(t *testing.T) {
	tests := []struct {
		name  string
		input string // We'll parse this as a call and extract first arg
		want  any
	}{
		{
			name:  "string value",
			input: `foo("hello")`,
			want:  "hello",
		},
		{
			name:  "integer value",
			input: `foo(42)`,
			want:  42,
		},
		{
			name:  "True boolean",
			input: `foo(True)`,
			want:  true,
		},
		{
			name:  "False boolean",
			input: `foo(False)`,
			want:  false,
		},
		{
			name:  "None value",
			input: `foo(None)`,
			want:  nil,
		},
		{
			name:  "identifier",
			input: `foo(some_var)`,
			want:  "some_var",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			call := parseCall(t, tt.input)
			if len(call.List) == 0 {
				t.Fatal("no arguments in call")
			}
			got := ExtractValue(call.List[0])
			switch want := tt.want.(type) {
			case nil:
				if got != nil {
					t.Errorf("ExtractValue() = %v, want nil", got)
				}
			case bool:
				if gotBool, ok := got.(bool); !ok || gotBool != want {
					t.Errorf("ExtractValue() = %v, want %v", got, want)
				}
			case int:
				if gotInt, ok := got.(int); !ok || gotInt != want {
					t.Errorf("ExtractValue() = %v, want %v", got, want)
				}
			case string:
				if gotStr, ok := got.(string); !ok || gotStr != want {
					t.Errorf("ExtractValue() = %v, want %q", got, want)
				}
			}
		})
	}
}

func TestFuncName(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "simple function",
			input: `foo()`,
			want:  "foo",
		},
		{
			name:  "function with args",
			input: `bazel_dep(name = "test")`,
			want:  "bazel_dep",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			call := parseCall(t, tt.input)
			got := FuncName(call)
			if got != tt.want {
				t.Errorf("FuncName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIsFuncCall(t *testing.T) {
	call := parseCall(t, `module(name = "test")`)

	if !IsFuncCall(call, "module") {
		t.Error("IsFuncCall(call, \"module\") = false, want true")
	}
	if IsFuncCall(call, "bazel_dep") {
		t.Error("IsFuncCall(call, \"bazel_dep\") = true, want false")
	}
}
