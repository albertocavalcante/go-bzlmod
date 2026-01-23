package ast

import (
	"testing"
)

func TestParseContent_Module(t *testing.T) {
	content := `module(
    name = "my_module",
    version = "1.0.0",
    compatibility_level = 1,
    repo_name = "custom_repo",
)
`
	result, err := ParseContent("MODULE.bazel", []byte(content))
	if err != nil {
		t.Fatalf("ParseContent error: %v", err)
	}

	if result.HasErrors() {
		for _, e := range result.Errors {
			t.Errorf("Parse error: %s", e.Error())
		}
		return
	}

	var module *ModuleDecl
	for _, stmt := range result.File.Statements {
		if m, ok := stmt.(*ModuleDecl); ok {
			module = m
			break
		}
	}

	if module == nil {
		t.Fatal("No module declaration found")
	}

	if module.Name.String() != "my_module" {
		t.Errorf("module.Name = %q, want 'my_module'", module.Name.String())
	}
	if module.Version.String() != "1.0.0" {
		t.Errorf("module.Version = %q, want '1.0.0'", module.Version.String())
	}
	if module.CompatibilityLevel != 1 {
		t.Errorf("module.CompatibilityLevel = %d, want 1", module.CompatibilityLevel)
	}
	if module.RepoName.String() != "custom_repo" {
		t.Errorf("module.RepoName = %q, want 'custom_repo'", module.RepoName.String())
	}
}

func TestParseContent_BazelDep(t *testing.T) {
	content := `bazel_dep(name = "rules_go", version = "0.50.1")
bazel_dep(name = "gazelle", version = "0.38.0", dev_dependency = True)
bazel_dep(name = "rules_python", version = "0.35.0", repo_name = "py_rules")
`
	result, err := ParseContent("MODULE.bazel", []byte(content))
	if err != nil {
		t.Fatalf("ParseContent error: %v", err)
	}

	deps := make([]*BazelDep, 0)
	for _, stmt := range result.File.Statements {
		if d, ok := stmt.(*BazelDep); ok {
			deps = append(deps, d)
		}
	}

	if len(deps) != 3 {
		t.Fatalf("Expected 3 dependencies, got %d", len(deps))
	}

	// First dep
	if deps[0].Name.String() != "rules_go" {
		t.Errorf("deps[0].Name = %q, want 'rules_go'", deps[0].Name.String())
	}
	if deps[0].Version.String() != "0.50.1" {
		t.Errorf("deps[0].Version = %q, want '0.50.1'", deps[0].Version.String())
	}
	if deps[0].DevDependency {
		t.Error("deps[0] should not be a dev dependency")
	}

	// Second dep (dev)
	if !deps[1].DevDependency {
		t.Error("deps[1] should be a dev dependency")
	}

	// Third dep (repo_name)
	if deps[2].RepoName.String() != "py_rules" {
		t.Errorf("deps[2].RepoName = %q, want 'py_rules'", deps[2].RepoName.String())
	}
}

func TestParseContent_Overrides(t *testing.T) {
	content := `single_version_override(
    module_name = "rules_go",
    version = "0.50.0",
    registry = "https://custom.registry",
    patches = ["fix.patch"],
    patch_strip = 1,
)

git_override(
    module_name = "rules_python",
    remote = "https://github.com/bazelbuild/rules_python.git",
    commit = "abc123",
    tag = "v0.35.0",
)

archive_override(
    module_name = "rules_rust",
    urls = ["https://example.com/rules_rust.tar.gz"],
    integrity = "sha256-abc123",
    strip_prefix = "rules_rust-1.0.0",
)

local_path_override(
    module_name = "my_lib",
    path = "/path/to/my_lib",
)
`
	result, err := ParseContent("MODULE.bazel", []byte(content))
	if err != nil {
		t.Fatalf("ParseContent error: %v", err)
	}

	if result.HasErrors() {
		for _, e := range result.Errors {
			t.Errorf("Parse error: %s", e.Error())
		}
		return
	}

	var svo *SingleVersionOverride
	var gitO *GitOverride
	var archiveO *ArchiveOverride
	var localO *LocalPathOverride

	for _, stmt := range result.File.Statements {
		switch s := stmt.(type) {
		case *SingleVersionOverride:
			svo = s
		case *GitOverride:
			gitO = s
		case *ArchiveOverride:
			archiveO = s
		case *LocalPathOverride:
			localO = s
		}
	}

	// Test single_version_override
	if svo == nil {
		t.Fatal("No single_version_override found")
	}
	if svo.Module.String() != "rules_go" {
		t.Errorf("svo.Module = %q, want 'rules_go'", svo.Module.String())
	}
	if svo.Version.String() != "0.50.0" {
		t.Errorf("svo.Version = %q, want '0.50.0'", svo.Version.String())
	}
	if svo.Registry != "https://custom.registry" {
		t.Errorf("svo.Registry = %q, want 'https://custom.registry'", svo.Registry)
	}
	if len(svo.Patches) != 1 || svo.Patches[0] != "fix.patch" {
		t.Errorf("svo.Patches = %v, want ['fix.patch']", svo.Patches)
	}
	if svo.PatchStrip != 1 {
		t.Errorf("svo.PatchStrip = %d, want 1", svo.PatchStrip)
	}

	// Test git_override
	if gitO == nil {
		t.Fatal("No git_override found")
	}
	if gitO.Module.String() != "rules_python" {
		t.Errorf("gitO.Module = %q, want 'rules_python'", gitO.Module.String())
	}
	if gitO.Remote != "https://github.com/bazelbuild/rules_python.git" {
		t.Errorf("gitO.Remote = %q", gitO.Remote)
	}
	if gitO.Commit != "abc123" {
		t.Errorf("gitO.Commit = %q, want 'abc123'", gitO.Commit)
	}
	if gitO.Tag != "v0.35.0" {
		t.Errorf("gitO.Tag = %q, want 'v0.35.0'", gitO.Tag)
	}

	// Test archive_override
	if archiveO == nil {
		t.Fatal("No archive_override found")
	}
	if archiveO.Module.String() != "rules_rust" {
		t.Errorf("archiveO.Module = %q, want 'rules_rust'", archiveO.Module.String())
	}
	if len(archiveO.URLs) != 1 {
		t.Errorf("archiveO.URLs length = %d, want 1", len(archiveO.URLs))
	}
	if archiveO.Integrity != "sha256-abc123" {
		t.Errorf("archiveO.Integrity = %q", archiveO.Integrity)
	}
	if archiveO.StripPrefix != "rules_rust-1.0.0" {
		t.Errorf("archiveO.StripPrefix = %q", archiveO.StripPrefix)
	}

	// Test local_path_override
	if localO == nil {
		t.Fatal("No local_path_override found")
	}
	if localO.Module.String() != "my_lib" {
		t.Errorf("localO.Module = %q, want 'my_lib'", localO.Module.String())
	}
	if localO.Path != "/path/to/my_lib" {
		t.Errorf("localO.Path = %q, want '/path/to/my_lib'", localO.Path)
	}
}

func TestParseContent_UseExtension(t *testing.T) {
	content := `go = use_extension("@rules_go//go:extensions.bzl", "go", dev_dependency = True)
`
	result, err := ParseContent("MODULE.bazel", []byte(content))
	if err != nil {
		t.Fatalf("ParseContent error: %v", err)
	}

	var ext *UseExtension
	for _, stmt := range result.File.Statements {
		if e, ok := stmt.(*UseExtension); ok {
			ext = e
			break
		}
	}

	if ext == nil {
		t.Fatal("No use_extension found")
	}

	if ext.ExtensionFile.String() != "@rules_go//go:extensions.bzl" {
		t.Errorf("ext.ExtensionFile = %q", ext.ExtensionFile.String())
	}
	if ext.ExtensionName.String() != "go" {
		t.Errorf("ext.ExtensionName = %q, want 'go'", ext.ExtensionName.String())
	}
	if !ext.DevDependency {
		t.Error("ext.DevDependency should be true")
	}
}

func TestParseContent_RegisterToolchains(t *testing.T) {
	content := `register_toolchains("@rules_go//go:go_toolchain")
register_toolchains("//toolchains:my_toolchain", dev_dependency = True)
`
	result, err := ParseContent("MODULE.bazel", []byte(content))
	if err != nil {
		t.Fatalf("ParseContent error: %v", err)
	}

	var regs []*RegisterToolchains
	for _, stmt := range result.File.Statements {
		if r, ok := stmt.(*RegisterToolchains); ok {
			regs = append(regs, r)
		}
	}

	if len(regs) != 2 {
		t.Fatalf("Expected 2 register_toolchains, got %d", len(regs))
	}

	if len(regs[0].Patterns) != 1 || regs[0].Patterns[0] != "@rules_go//go:go_toolchain" {
		t.Errorf("regs[0].Patterns = %v", regs[0].Patterns)
	}

	if !regs[1].DevDependency {
		t.Error("regs[1] should be dev dependency")
	}
}

func TestParseContent_Position(t *testing.T) {
	content := `module(name = "test", version = "1.0.0")

bazel_dep(name = "rules_go", version = "0.50.1")
`
	result, err := ParseContent("test.bazel", []byte(content))
	if err != nil {
		t.Fatalf("ParseContent error: %v", err)
	}

	for _, stmt := range result.File.Statements {
		pos := stmt.Position()
		if pos.Filename != "test.bazel" {
			t.Errorf("Position.Filename = %q, want 'test.bazel'", pos.Filename)
		}
		if pos.Line <= 0 {
			t.Errorf("Position.Line = %d, should be > 0", pos.Line)
		}
	}
}

func TestParseContent_Error_MissingName(t *testing.T) {
	content := `bazel_dep(version = "0.50.1")
`
	result, err := ParseContent("MODULE.bazel", []byte(content))
	if err != nil {
		t.Fatalf("ParseContent error: %v", err)
	}

	if !result.HasErrors() {
		t.Error("Expected errors for missing name attribute")
	}
}

func TestParseContent_SyntaxError(t *testing.T) {
	content := `module(name = "test"
` // Missing closing paren

	_, err := ParseContent("MODULE.bazel", []byte(content))
	if err == nil {
		t.Error("Expected syntax error")
	}

	parseErr, ok := err.(*ParseError)
	if !ok {
		t.Errorf("Expected *ParseError, got %T", err)
	}
	if parseErr.Pos.Filename != "MODULE.bazel" {
		t.Errorf("ParseError.Pos.Filename = %q", parseErr.Pos.Filename)
	}
}

func TestParseContent_ComplexModule(t *testing.T) {
	content := `module(
    name = "my_project",
    version = "2.0.0",
    compatibility_level = 2,
    bazel_compatibility = [">=6.0.0", "<8.0.0"],
)

bazel_dep(name = "rules_go", version = "0.50.1")
bazel_dep(name = "gazelle", version = "0.38.0", dev_dependency = True)
bazel_dep(name = "rules_python", version = "0.35.0", repo_name = "python_rules", max_compatibility_level = 1)

single_version_override(
    module_name = "rules_go",
    version = "0.49.0",
    patches = ["//patches:fix1.patch", "//patches:fix2.patch"],
    patch_cmds = ["sed -i '' 's/old/new/g' file.txt"],
    patch_strip = 1,
)

git_override(
    module_name = "custom_rules",
    remote = "https://github.com/example/custom_rules.git",
    commit = "abcdef123456",
    init_submodules = True,
    strip_prefix = "src",
)

go = use_extension("@rules_go//go:extensions.bzl", "go")

register_toolchains("@rules_go//go:go_toolchain")
register_execution_platforms("//platforms:linux_x86_64")
`
	result, err := ParseContent("MODULE.bazel", []byte(content))
	if err != nil {
		t.Fatalf("ParseContent error: %v", err)
	}

	if result.HasErrors() {
		for _, e := range result.Errors {
			t.Errorf("Parse error: %s", e.Error())
		}
		return
	}

	// Count statement types
	counts := make(map[string]int)
	for _, stmt := range result.File.Statements {
		switch stmt.(type) {
		case *ModuleDecl:
			counts["module"]++
		case *BazelDep:
			counts["bazel_dep"]++
		case *SingleVersionOverride:
			counts["single_version_override"]++
		case *GitOverride:
			counts["git_override"]++
		case *UseExtension:
			counts["use_extension"]++
		case *RegisterToolchains:
			counts["register_toolchains"]++
		case *RegisterExecutionPlatforms:
			counts["register_execution_platforms"]++
		}
	}

	expected := map[string]int{
		"module":                       1,
		"bazel_dep":                    3,
		"single_version_override":      1,
		"git_override":                 1,
		"use_extension":                1,
		"register_toolchains":          1,
		"register_execution_platforms": 1,
	}

	for k, v := range expected {
		if counts[k] != v {
			t.Errorf("Count of %s = %d, want %d", k, counts[k], v)
		}
	}
}
