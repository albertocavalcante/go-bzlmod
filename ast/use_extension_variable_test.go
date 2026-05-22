package ast

import "testing"

// Capturing the LHS variable name of a use_extension assignment is
// required for downstream linking with ExtensionTagCall.Extension.
// Without this field, consumers can't tell which use_extension a
// proxy.tag(...) call belongs to.
//
// Test case: when the LHS variable differs from the extension's
// second argument (the extension name within its .bzl), the parser
// must still record the LHS so callers don't guess.
func TestParseContent_UseExtension_RecordsLHSVariable(t *testing.T) {
	const src = `
module(name = "x", version = "1.0")

# LHS variable "gosdk_alias" differs from extension name "go_sdk"
gosdk_alias = use_extension("@rules_go//go:extensions.bzl", "go_sdk")
gosdk_alias.download(version = "1.22.5")
`
	result, err := ParseContent("MODULE.bazel", []byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	var ue *UseExtension
	for _, stmt := range result.File.Statements {
		if u, ok := stmt.(*UseExtension); ok {
			ue = u
			break
		}
	}
	if ue == nil {
		t.Fatal("no UseExtension statement found")
	}
	if ue.Variable != "gosdk_alias" {
		t.Errorf("Variable = %q, want %q (the LHS of the use_extension assignment)",
			ue.Variable, "gosdk_alias")
	}
	if ue.ExtensionName.String() != "go_sdk" {
		t.Errorf("ExtensionName = %q, want go_sdk (independent of Variable)",
			ue.ExtensionName.String())
	}
}

// When use_extension is at the top level without an assignment (rare
// but valid Starlark), Variable should be empty — not error.
func TestParseContent_UseExtension_NoAssignmentEmptyVariable(t *testing.T) {
	const src = `
module(name = "x", version = "1.0")
use_extension("@x//:y.bzl", "z")
`
	result, err := ParseContent("MODULE.bazel", []byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	for _, stmt := range result.File.Statements {
		if u, ok := stmt.(*UseExtension); ok {
			if u.Variable != "" {
				t.Errorf("Variable = %q, want empty for unassigned use_extension",
					u.Variable)
			}
			return
		}
	}
	t.Fatal("no UseExtension found")
}
