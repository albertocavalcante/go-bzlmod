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

func TestParseContent_MultipleVersionOverride(t *testing.T) {
	content := `multiple_version_override(
    module_name = "protobuf",
    versions = ["3.19.0", "3.21.0", "4.0.0"],
    registry = "https://bcr.bazel.build",
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

	var mvo *MultipleVersionOverride
	for _, stmt := range result.File.Statements {
		if o, ok := stmt.(*MultipleVersionOverride); ok {
			mvo = o
			break
		}
	}

	if mvo == nil {
		t.Fatal("No multiple_version_override found")
	}
	if mvo.Module.String() != "protobuf" {
		t.Errorf("mvo.Module = %q, want 'protobuf'", mvo.Module.String())
	}
	if len(mvo.Versions) != 3 {
		t.Fatalf("mvo.Versions length = %d, want 3", len(mvo.Versions))
	}
	want := []string{"3.19.0", "3.21.0", "4.0.0"}
	for i, v := range mvo.Versions {
		if v.String() != want[i] {
			t.Errorf("mvo.Versions[%d] = %q, want %q", i, v.String(), want[i])
		}
	}
	if mvo.Registry != "https://bcr.bazel.build" {
		t.Errorf("mvo.Registry = %q", mvo.Registry)
	}
}

func TestParseContent_Include(t *testing.T) {
	content := `include("//third_party:extensions.MODULE.bazel")
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

	var inc *Include
	for _, stmt := range result.File.Statements {
		if i, ok := stmt.(*Include); ok {
			inc = i
			break
		}
	}

	if inc == nil {
		t.Fatal("No include found")
	}
	if inc.Label != "//third_party:extensions.MODULE.bazel" {
		t.Errorf("inc.Label = %q", inc.Label)
	}
}

func TestParseContent_Include_MissingLabel(t *testing.T) {
	content := `include()
`
	result, err := ParseContent("MODULE.bazel", []byte(content))
	if err != nil {
		t.Fatalf("ParseContent error: %v", err)
	}

	if !result.HasErrors() {
		t.Error("Expected error for include() without label")
	}
}

func TestParseContent_UseRepo(t *testing.T) {
	content := `go = use_extension("@rules_go//go:extensions.bzl", "go")
use_repo(go, "go_sdk", "go_toolchains")
`
	result, err := ParseContent("MODULE.bazel", []byte(content))
	if err != nil {
		t.Fatalf("ParseContent error: %v", err)
	}

	var useRepo *UseRepo
	for _, stmt := range result.File.Statements {
		if r, ok := stmt.(*UseRepo); ok {
			useRepo = r
			break
		}
	}

	if useRepo == nil {
		t.Fatal("No use_repo found")
	}
	if len(useRepo.Repos) != 2 {
		t.Fatalf("useRepo.Repos length = %d, want 2", len(useRepo.Repos))
	}
	if useRepo.Repos[0] != "go_sdk" {
		t.Errorf("useRepo.Repos[0] = %q, want 'go_sdk'", useRepo.Repos[0])
	}
	if useRepo.Repos[1] != "go_toolchains" {
		t.Errorf("useRepo.Repos[1] = %q, want 'go_toolchains'", useRepo.Repos[1])
	}
}

func TestParseContent_UseRepoRule(t *testing.T) {
	content := `http_archive = use_repo_rule("@bazel_tools//tools/build_defs/repo:http.bzl", "http_archive")
`
	result, err := ParseContent("MODULE.bazel", []byte(content))
	if err != nil {
		t.Fatalf("ParseContent error: %v", err)
	}

	var rule *UseRepoRule
	for _, stmt := range result.File.Statements {
		if r, ok := stmt.(*UseRepoRule); ok {
			rule = r
			break
		}
	}

	if rule == nil {
		t.Fatal("No use_repo_rule found")
	}
	if rule.RuleFile != "@bazel_tools//tools/build_defs/repo:http.bzl" {
		t.Errorf("rule.RuleFile = %q", rule.RuleFile)
	}
	if rule.RuleName != "http_archive" {
		t.Errorf("rule.RuleName = %q, want 'http_archive'", rule.RuleName)
	}
}

func TestParseContent_InjectRepo(t *testing.T) {
	content := `go = use_extension("@rules_go//go:extensions.bzl", "go")
inject_repo(go, my_custom_go_sdk = "@my_go_sdk")
`
	result, err := ParseContent("MODULE.bazel", []byte(content))
	if err != nil {
		t.Fatalf("ParseContent error: %v", err)
	}

	var inject *InjectRepo
	for _, stmt := range result.File.Statements {
		if i, ok := stmt.(*InjectRepo); ok {
			inject = i
			break
		}
	}

	if inject == nil {
		t.Fatal("No inject_repo found")
	}
	if inject.Extension != "go" {
		t.Errorf("inject.Extension = %q, want 'go'", inject.Extension)
	}
	if val, ok := inject.Repos["my_custom_go_sdk"]; !ok || val != "@my_go_sdk" {
		t.Errorf("inject.Repos = %v", inject.Repos)
	}
}

func TestParseContent_OverrideRepo(t *testing.T) {
	content := `go = use_extension("@rules_go//go:extensions.bzl", "go")
override_repo(go, go_sdk = "@my_patched_go_sdk")
`
	result, err := ParseContent("MODULE.bazel", []byte(content))
	if err != nil {
		t.Fatalf("ParseContent error: %v", err)
	}

	var override *OverrideRepo
	for _, stmt := range result.File.Statements {
		if o, ok := stmt.(*OverrideRepo); ok {
			override = o
			break
		}
	}

	if override == nil {
		t.Fatal("No override_repo found")
	}
	if override.Extension != "go" {
		t.Errorf("override.Extension = %q, want 'go'", override.Extension)
	}
	if val, ok := override.Repos["go_sdk"]; !ok || val != "@my_patched_go_sdk" {
		t.Errorf("override.Repos = %v", override.Repos)
	}
}

func TestParseContent_FlagAlias(t *testing.T) {
	content := `flag_alias(
    name = "my_flag",
    starlark_flag = "@my_project//flags:my_flag",
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

	var alias *FlagAlias
	for _, stmt := range result.File.Statements {
		if a, ok := stmt.(*FlagAlias); ok {
			alias = a
			break
		}
	}

	if alias == nil {
		t.Fatal("No flag_alias found")
	}
	if alias.Name != "my_flag" {
		t.Errorf("alias.Name = %q, want 'my_flag'", alias.Name)
	}
	if alias.StarlarkFlag != "@my_project//flags:my_flag" {
		t.Errorf("alias.StarlarkFlag = %q", alias.StarlarkFlag)
	}
}

func TestParseContent_FlagAlias_MissingAttrs(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{
			name:    "missing name",
			content: `flag_alias(starlark_flag = "@foo//bar:baz")`,
		},
		{
			name:    "missing starlark_flag",
			content: `flag_alias(name = "my_flag")`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseContent("MODULE.bazel", []byte(tt.content))
			if err != nil {
				t.Fatalf("ParseContent error: %v", err)
			}
			if !result.HasErrors() {
				t.Error("Expected error for missing attribute")
			}
		})
	}
}

func TestParseContent_ExtensionTagCall(t *testing.T) {
	content := `go = use_extension("@rules_go//go:extensions.bzl", "go")
go.sdk(name = "go_sdk", version = "1.21.0")
go.download(name = "another_sdk", version = "1.22.0")
`
	result, err := ParseContent("MODULE.bazel", []byte(content))
	if err != nil {
		t.Fatalf("ParseContent error: %v", err)
	}

	var tags []*ExtensionTagCall
	for _, stmt := range result.File.Statements {
		if t, ok := stmt.(*ExtensionTagCall); ok {
			tags = append(tags, t)
		}
	}

	if len(tags) != 2 {
		t.Fatalf("Expected 2 extension tag calls, got %d", len(tags))
	}

	// First tag: go.sdk(...)
	if tags[0].Extension != "go" {
		t.Errorf("tags[0].Extension = %q, want 'go'", tags[0].Extension)
	}
	if tags[0].TagName != "sdk" {
		t.Errorf("tags[0].TagName = %q, want 'sdk'", tags[0].TagName)
	}
	if name, ok := tags[0].Attributes["name"].(string); !ok || name != "go_sdk" {
		t.Errorf("tags[0].Attributes['name'] = %v", tags[0].Attributes["name"])
	}
	if version, ok := tags[0].Attributes["version"].(string); !ok || version != "1.21.0" {
		t.Errorf("tags[0].Attributes['version'] = %v", tags[0].Attributes["version"])
	}

	// Second tag: go.download(...)
	if tags[1].TagName != "download" {
		t.Errorf("tags[1].TagName = %q, want 'download'", tags[1].TagName)
	}
}

func TestParseContent_RegisterExecutionPlatforms(t *testing.T) {
	content := `register_execution_platforms(
    "//platforms:linux_x86_64",
    "//platforms:macos_arm64",
    dev_dependency = True,
)
`
	result, err := ParseContent("MODULE.bazel", []byte(content))
	if err != nil {
		t.Fatalf("ParseContent error: %v", err)
	}

	var reg *RegisterExecutionPlatforms
	for _, stmt := range result.File.Statements {
		if r, ok := stmt.(*RegisterExecutionPlatforms); ok {
			reg = r
			break
		}
	}

	if reg == nil {
		t.Fatal("No register_execution_platforms found")
	}
	if len(reg.Patterns) != 2 {
		t.Fatalf("reg.Patterns length = %d, want 2", len(reg.Patterns))
	}
	if reg.Patterns[0] != "//platforms:linux_x86_64" {
		t.Errorf("reg.Patterns[0] = %q", reg.Patterns[0])
	}
	if reg.Patterns[1] != "//platforms:macos_arm64" {
		t.Errorf("reg.Patterns[1] = %q", reg.Patterns[1])
	}
	if !reg.DevDependency {
		t.Error("reg.DevDependency should be true")
	}
}

func TestParseContent_UnknownStatement(t *testing.T) {
	content := `unknown_func(some_arg = "value")
`
	result, err := ParseContent("MODULE.bazel", []byte(content))
	if err != nil {
		t.Fatalf("ParseContent error: %v", err)
	}

	var unknown *UnknownStatement
	for _, stmt := range result.File.Statements {
		if u, ok := stmt.(*UnknownStatement); ok {
			unknown = u
			break
		}
	}

	if unknown == nil {
		t.Fatal("No UnknownStatement found")
	}
	if unknown.FuncName != "unknown_func" {
		t.Errorf("unknown.FuncName = %q, want 'unknown_func'", unknown.FuncName)
	}
}

func TestParseContent_GitOverride_AllFields(t *testing.T) {
	content := `git_override(
    module_name = "mylib",
    remote = "https://github.com/example/mylib.git",
    commit = "abc123def",
    tag = "v1.0.0",
    branch = "main",
    patches = ["fix.patch"],
    patch_cmds = ["echo patched"],
    patch_strip = 1,
    init_submodules = True,
    strip_prefix = "src",
)
`
	result, err := ParseContent("MODULE.bazel", []byte(content))
	if err != nil {
		t.Fatalf("ParseContent error: %v", err)
	}

	var gitO *GitOverride
	for _, stmt := range result.File.Statements {
		if g, ok := stmt.(*GitOverride); ok {
			gitO = g
			break
		}
	}

	if gitO == nil {
		t.Fatal("No git_override found")
	}
	if gitO.Module.String() != "mylib" {
		t.Errorf("gitO.Module = %q", gitO.Module.String())
	}
	if gitO.Remote != "https://github.com/example/mylib.git" {
		t.Errorf("gitO.Remote = %q", gitO.Remote)
	}
	if gitO.Commit != "abc123def" {
		t.Errorf("gitO.Commit = %q", gitO.Commit)
	}
	if gitO.Tag != "v1.0.0" {
		t.Errorf("gitO.Tag = %q", gitO.Tag)
	}
	if gitO.Branch != "main" {
		t.Errorf("gitO.Branch = %q", gitO.Branch)
	}
	if len(gitO.Patches) != 1 || gitO.Patches[0] != "fix.patch" {
		t.Errorf("gitO.Patches = %v", gitO.Patches)
	}
	if len(gitO.PatchCmds) != 1 || gitO.PatchCmds[0] != "echo patched" {
		t.Errorf("gitO.PatchCmds = %v", gitO.PatchCmds)
	}
	if gitO.PatchStrip != 1 {
		t.Errorf("gitO.PatchStrip = %d", gitO.PatchStrip)
	}
	if !gitO.InitSubmodules {
		t.Error("gitO.InitSubmodules should be true")
	}
	if gitO.StripPrefix != "src" {
		t.Errorf("gitO.StripPrefix = %q", gitO.StripPrefix)
	}
}

func TestParseContent_LocalPathOverride_MissingPath(t *testing.T) {
	content := `local_path_override(module_name = "mylib")
`
	result, err := ParseContent("MODULE.bazel", []byte(content))
	if err != nil {
		t.Fatalf("ParseContent error: %v", err)
	}

	if !result.HasErrors() {
		t.Error("Expected error for local_path_override without path")
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
