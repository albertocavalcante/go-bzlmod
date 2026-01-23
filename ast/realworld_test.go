package ast

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseRealWorld_RulesGo(t *testing.T) {
	content, err := os.ReadFile("testdata/rules_go.MODULE.bazel")
	if err != nil {
		t.Skipf("Skipping real-world test: %v", err)
	}

	result, err := ParseContent("rules_go/MODULE.bazel", content)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	// Log any parse warnings but don't fail
	if result.HasErrors() {
		for _, e := range result.Errors {
			t.Logf("Parse warning: %s", e.Error())
		}
	}

	// Count statement types
	counts := make(map[string]int)
	var module *ModuleDecl
	var deps []*BazelDep
	var extensions []*UseExtension
	var tagCalls []*ExtensionTagCall
	var overrides []Statement

	for _, stmt := range result.File.Statements {
		switch s := stmt.(type) {
		case *ModuleDecl:
			counts["module"]++
			module = s
		case *BazelDep:
			counts["bazel_dep"]++
			deps = append(deps, s)
		case *UseExtension:
			counts["use_extension"]++
			extensions = append(extensions, s)
		case *UseRepo:
			counts["use_repo"]++
		case *ExtensionTagCall:
			counts["extension_tag"]++
			tagCalls = append(tagCalls, s)
		case *LocalPathOverride:
			counts["local_path_override"]++
			overrides = append(overrides, s)
		case *RegisterToolchains:
			counts["register_toolchains"]++
		}
	}

	// Verify module declaration
	if module == nil {
		t.Fatal("No module declaration found")
	}
	if module.Name.String() != "rules_go" {
		t.Errorf("module.Name = %q, want 'rules_go'", module.Name.String())
	}
	if module.RepoName.String() != "io_bazel_rules_go" {
		t.Errorf("module.RepoName = %q, want 'io_bazel_rules_go'", module.RepoName.String())
	}

	// Verify dependencies
	if len(deps) < 10 {
		t.Errorf("Expected at least 10 bazel_dep, got %d", len(deps))
	}

	// Check for specific deps
	foundBazelFeatures := false
	foundGazelle := false
	for _, dep := range deps {
		if dep.Name.String() == "bazel_features" {
			foundBazelFeatures = true
			if dep.RepoName.String() != "io_bazel_rules_go_bazel_features" {
				t.Errorf("bazel_features repo_name = %q", dep.RepoName.String())
			}
		}
		if dep.Name.String() == "gazelle" {
			foundGazelle = true
		}
	}
	if !foundBazelFeatures {
		t.Error("bazel_features dependency not found")
	}
	if !foundGazelle {
		t.Error("gazelle dependency not found")
	}

	// Verify use_extension calls
	if len(extensions) < 2 {
		t.Errorf("Expected at least 2 use_extension, got %d", len(extensions))
	}

	// Verify extension tag calls (go_sdk.from_file, etc.)
	if len(tagCalls) < 3 {
		t.Errorf("Expected at least 3 extension tag calls, got %d", len(tagCalls))
	}

	// Check specific tag calls
	foundFromFile := false
	foundDownload := false
	for _, tag := range tagCalls {
		t.Logf("Found extension tag: %s.%s", tag.Extension, tag.TagName)
		if tag.Extension == "go_sdk" && tag.TagName == "from_file" {
			foundFromFile = true
			if name, ok := tag.Attributes["name"].(string); ok {
				if name != "go_default_sdk" {
					t.Errorf("go_sdk.from_file name = %q, want 'go_default_sdk'", name)
				}
			}
		}
		if tag.Extension == "dev_go_sdk" && tag.TagName == "download" {
			foundDownload = true
		}
	}
	if !foundFromFile {
		t.Error("go_sdk.from_file tag call not found")
	}
	if !foundDownload {
		t.Error("dev_go_sdk.download tag call not found")
	}

	// Verify local_path_override
	if len(overrides) != 1 {
		t.Errorf("Expected 1 local_path_override, got %d", len(overrides))
	}

	t.Logf("Parsed %d statements total", len(result.File.Statements))
	t.Logf("Statement counts: %+v", counts)
}

func TestParseRealWorld_Include(t *testing.T) {
	content := []byte(`module(name = "my_project", version = "1.0.0")

include("//bazel:deps.MODULE.bazel")
include("//third_party:MODULE.bazel")

bazel_dep(name = "rules_go", version = "0.50.1")
`)

	result, err := ParseContent("MODULE.bazel", content)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	var includes []*Include
	for _, stmt := range result.File.Statements {
		if inc, ok := stmt.(*Include); ok {
			includes = append(includes, inc)
		}
	}

	if len(includes) != 2 {
		t.Fatalf("Expected 2 include statements, got %d", len(includes))
	}

	if includes[0].Label != "//bazel:deps.MODULE.bazel" {
		t.Errorf("includes[0].Label = %q, want '//bazel:deps.MODULE.bazel'", includes[0].Label)
	}
	if includes[1].Label != "//third_party:MODULE.bazel" {
		t.Errorf("includes[1].Label = %q, want '//third_party:MODULE.bazel'", includes[1].Label)
	}
}

func TestParseRealWorld_ExtensionTags(t *testing.T) {
	content := []byte(`module(name = "test", version = "1.0.0")

go_sdk = use_extension("//go:extensions.bzl", "go_sdk")
go_sdk.download(
    name = "go_sdk",
    version = "1.22.0",
)
go_sdk.from_file(
    name = "custom_sdk",
    go_mod = "//:go.mod",
)

npm = use_extension("@aspect_rules_js//npm:extensions.bzl", "npm")
npm.npm_translate_lock(
    name = "npm",
    pnpm_lock = "//:pnpm-lock.yaml",
    verify_node_modules_ignored = "//:.bazelignore",
)
`)

	result, err := ParseContent("MODULE.bazel", content)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	var tagCalls []*ExtensionTagCall
	for _, stmt := range result.File.Statements {
		if tag, ok := stmt.(*ExtensionTagCall); ok {
			tagCalls = append(tagCalls, tag)
		}
	}

	if len(tagCalls) != 3 {
		t.Fatalf("Expected 3 extension tag calls, got %d", len(tagCalls))
	}

	// Verify go_sdk.download
	if tagCalls[0].Extension != "go_sdk" || tagCalls[0].TagName != "download" {
		t.Errorf("tagCalls[0] = %s.%s, want go_sdk.download", tagCalls[0].Extension, tagCalls[0].TagName)
	}
	if version, ok := tagCalls[0].Attributes["version"].(string); !ok || version != "1.22.0" {
		t.Errorf("go_sdk.download version = %v, want '1.22.0'", tagCalls[0].Attributes["version"])
	}

	// Verify go_sdk.from_file
	if tagCalls[1].Extension != "go_sdk" || tagCalls[1].TagName != "from_file" {
		t.Errorf("tagCalls[1] = %s.%s, want go_sdk.from_file", tagCalls[1].Extension, tagCalls[1].TagName)
	}

	// Verify npm.npm_translate_lock
	if tagCalls[2].Extension != "npm" || tagCalls[2].TagName != "npm_translate_lock" {
		t.Errorf("tagCalls[2] = %s.%s, want npm.npm_translate_lock", tagCalls[2].Extension, tagCalls[2].TagName)
	}
}

func TestParseRealWorld_ComplexAttributes(t *testing.T) {
	content := []byte(`module(name = "test", version = "1.0.0")

npm = use_extension("@aspect_rules_js//npm:extensions.bzl", "npm")
npm.npm_translate_lock(
    name = "npm",
    lifecycle_hooks = {
        "@kubernetes/client-node": ["build"],
        "protoc-gen-grpc@2.0.3": [],
    },
    lifecycle_hooks_execution_requirements = {
        "*": ["no-sandbox"],
        "@kubernetes/client-node": [],
    },
    data = [
        "//:package.json",
        "//:pnpm-workspace.yaml",
        "//examples:package.json",
    ],
    generate_bzl_library_targets = True,
    update_pnpm_lock = True,
)
`)

	result, err := ParseContent("MODULE.bazel", content)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	var tagCalls []*ExtensionTagCall
	for _, stmt := range result.File.Statements {
		if tag, ok := stmt.(*ExtensionTagCall); ok {
			tagCalls = append(tagCalls, tag)
		}
	}

	if len(tagCalls) != 1 {
		t.Fatalf("Expected 1 extension tag call, got %d", len(tagCalls))
	}

	tag := tagCalls[0]

	// Verify dict attribute
	hooks, ok := tag.Attributes["lifecycle_hooks"].(map[string]any)
	if !ok {
		t.Fatalf("lifecycle_hooks is not a map, got %T", tag.Attributes["lifecycle_hooks"])
	}
	if len(hooks) != 2 {
		t.Errorf("lifecycle_hooks has %d entries, want 2", len(hooks))
	}

	// Verify list attribute
	data, ok := tag.Attributes["data"].([]any)
	if !ok {
		t.Fatalf("data is not a list, got %T", tag.Attributes["data"])
	}
	if len(data) != 3 {
		t.Errorf("data has %d entries, want 3", len(data))
	}

	// Verify boolean attributes
	if generate, ok := tag.Attributes["generate_bzl_library_targets"].(bool); !ok || !generate {
		t.Errorf("generate_bzl_library_targets = %v, want true", tag.Attributes["generate_bzl_library_targets"])
	}
}

func TestParseRealWorld_RepoNameNone(t *testing.T) {
	// Some projects use repo_name = None explicitly
	content := []byte(`module(name = "test", version = "1.0.0")

bazel_dep(name = "aspect_bazel_lib", version = "2.22.5", repo_name = None)
`)

	result, err := ParseContent("MODULE.bazel", content)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	if result.HasErrors() {
		for _, e := range result.Errors {
			t.Errorf("Parse error: %s", e.Error())
		}
	}

	var deps []*BazelDep
	for _, stmt := range result.File.Statements {
		if dep, ok := stmt.(*BazelDep); ok {
			deps = append(deps, dep)
		}
	}

	if len(deps) != 1 {
		t.Fatalf("Expected 1 bazel_dep, got %d", len(deps))
	}

	// repo_name = None should result in empty repo name
	if !deps[0].RepoName.IsEmpty() {
		t.Errorf("repo_name should be empty for None, got %q", deps[0].RepoName.String())
	}
}

func TestParseAllTestdata(t *testing.T) {
	// Parse all testdata files
	matches, err := filepath.Glob("testdata/*.MODULE.bazel")
	if err != nil {
		t.Fatal(err)
	}

	for _, path := range matches {
		t.Run(filepath.Base(path), func(t *testing.T) {
			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("Failed to read %s: %v", path, err)
			}

			result, err := ParseContent(path, content)
			if err != nil {
				t.Fatalf("Parse error: %v", err)
			}

			if result.HasErrors() {
				for _, e := range result.Errors {
					t.Errorf("Parse error: %s", e.Error())
				}
			}

			t.Logf("Parsed %d statements from %s", len(result.File.Statements), path)

			// Count and log statement types
			counts := make(map[string]int)
			for _, stmt := range result.File.Statements {
				switch stmt.(type) {
				case *ModuleDecl:
					counts["module"]++
				case *BazelDep:
					counts["bazel_dep"]++
				case *UseExtension:
					counts["use_extension"]++
				case *UseRepo:
					counts["use_repo"]++
				case *ExtensionTagCall:
					counts["extension_tag"]++
				case *SingleVersionOverride:
					counts["single_version_override"]++
				case *GitOverride:
					counts["git_override"]++
				case *ArchiveOverride:
					counts["archive_override"]++
				case *LocalPathOverride:
					counts["local_path_override"]++
				case *RegisterToolchains:
					counts["register_toolchains"]++
				case *RegisterExecutionPlatforms:
					counts["register_execution_platforms"]++
				case *Include:
					counts["include"]++
				case *UnknownStatement:
					counts["unknown"]++
				}
			}
			t.Logf("Statement counts: %+v", counts)
		})
	}
}
