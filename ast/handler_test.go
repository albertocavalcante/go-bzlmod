package ast

import (
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/albertocavalcante/go-bzlmod/label"
)

// Helper functions for tests that panic on invalid input
func mustApparentRepo(name string) label.ApparentRepo {
	r, err := label.NewApparentRepo(name)
	if err != nil {
		panic(fmt.Sprintf("invalid ApparentRepo %q: %v", name, err))
	}
	return r
}

func mustApparentLabel(s string) label.ApparentLabel {
	l, err := label.ParseApparentLabel(s)
	if err != nil {
		panic(fmt.Sprintf("invalid ApparentLabel %q: %v", s, err))
	}
	return l
}

func mustStarlarkIdentifier(name string) label.StarlarkIdentifier {
	id, err := label.NewStarlarkIdentifier(name)
	if err != nil {
		panic(fmt.Sprintf("invalid StarlarkIdentifier %q: %v", name, err))
	}
	return id
}

// =============================================================================
// Adversarial Tests for ast/handler.go
// =============================================================================

// mockExtensionProxy is a test implementation of ExtensionProxy
type mockExtensionProxy struct {
	tags      []string
	returnErr error
}

func (m *mockExtensionProxy) Tag(name string, attrs map[string]any) error {
	m.tags = append(m.tags, name)
	return m.returnErr
}

// recordingHandler records all handler calls for testing
type recordingHandler struct {
	BaseHandler
	calls []string
	err   error // error to return from all methods
}

func (h *recordingHandler) Module(name label.Module, version label.Version, compatLevel int, repoName label.ApparentRepo) error {
	h.calls = append(h.calls, "Module:"+name.String())
	return h.err
}

func (h *recordingHandler) BazelDep(name label.Module, version label.Version, maxCompat int, repoName label.ApparentRepo, devDep bool) error {
	h.calls = append(h.calls, "BazelDep:"+name.String())
	return h.err
}

func (h *recordingHandler) SingleVersionOverride(moduleName label.Module, version label.Version, registry string, patches, patchCmds []string, patchStrip int) error {
	h.calls = append(h.calls, "SingleVersionOverride:"+moduleName.String())
	return h.err
}

func (h *recordingHandler) GitOverride(moduleName label.Module, remote, commit, tag, branch string, patches, patchCmds []string, patchStrip int, initSubmodules bool, stripPrefix string) error {
	h.calls = append(h.calls, "GitOverride:"+moduleName.String())
	return h.err
}

func (h *recordingHandler) LocalPathOverride(moduleName label.Module, path string) error {
	h.calls = append(h.calls, "LocalPathOverride:"+moduleName.String())
	return h.err
}

func (h *recordingHandler) ArchiveOverride(moduleName label.Module, urls []string, integrity, stripPrefix string, patches, patchCmds []string, patchStrip int) error {
	h.calls = append(h.calls, "ArchiveOverride:"+moduleName.String())
	return h.err
}

func (h *recordingHandler) MultipleVersionOverride(moduleName label.Module, versions []label.Version, registry string) error {
	h.calls = append(h.calls, "MultipleVersionOverride:"+moduleName.String())
	return h.err
}

func (h *recordingHandler) RegisterToolchains(patterns []string, devDep bool) error {
	h.calls = append(h.calls, "RegisterToolchains")
	return h.err
}

func (h *recordingHandler) RegisterExecutionPlatforms(patterns []string, devDep bool) error {
	h.calls = append(h.calls, "RegisterExecutionPlatforms")
	return h.err
}

func (h *recordingHandler) UnknownStatement(name string, pos Position) error {
	h.calls = append(h.calls, "UnknownStatement:"+name)
	return h.err
}

// TestWalk_EmptyFile tests walking an empty file
func TestWalk_EmptyFile(t *testing.T) {
	file := &ModuleFile{Statements: []Statement{}}
	handler := &recordingHandler{}

	err := Walk(file, handler)
	if err != nil {
		t.Errorf("Walk on empty file returned error: %v", err)
	}

	if len(handler.calls) != 0 {
		t.Errorf("Expected 0 calls for empty file, got %d", len(handler.calls))
	}
}

// TestWalk_NilStatements tests walking a file with nil statements slice
func TestWalk_NilStatements(t *testing.T) {
	file := &ModuleFile{Statements: nil}
	handler := &recordingHandler{}

	err := Walk(file, handler)
	if err != nil {
		t.Errorf("Walk on nil statements returned error: %v", err)
	}
}

// TestWalk_ModuleDecl tests walking a module declaration
func TestWalk_ModuleDecl(t *testing.T) {
	file := &ModuleFile{
		Statements: []Statement{
			&ModuleDecl{
				Name:    label.MustModule("test_module"),
				Version: label.MustVersion("1.0.0"),
			},
		},
	}
	handler := &recordingHandler{}

	err := Walk(file, handler)
	if err != nil {
		t.Errorf("Walk returned error: %v", err)
	}

	if len(handler.calls) != 1 {
		t.Fatalf("Expected 1 call, got %d", len(handler.calls))
	}

	if handler.calls[0] != "Module:test_module" {
		t.Errorf("Expected 'Module:test_module', got %q", handler.calls[0])
	}
}

// TestWalk_BazelDep tests walking bazel_dep declarations
func TestWalk_BazelDep(t *testing.T) {
	file := &ModuleFile{
		Statements: []Statement{
			&BazelDep{Name: label.MustModule("dep1"), Version: label.MustVersion("1.0.0")},
			&BazelDep{Name: label.MustModule("dep2"), Version: label.MustVersion("2.0.0"), DevDependency: true},
		},
	}
	handler := &recordingHandler{}

	err := Walk(file, handler)
	if err != nil {
		t.Errorf("Walk returned error: %v", err)
	}

	if len(handler.calls) != 2 {
		t.Fatalf("Expected 2 calls, got %d", len(handler.calls))
	}

	if handler.calls[0] != "BazelDep:dep1" {
		t.Errorf("Expected 'BazelDep:dep1', got %q", handler.calls[0])
	}
	if handler.calls[1] != "BazelDep:dep2" {
		t.Errorf("Expected 'BazelDep:dep2', got %q", handler.calls[1])
	}
}

// TestWalk_ErrorPropagation tests that errors stop processing
func TestWalk_ErrorPropagation(t *testing.T) {
	file := &ModuleFile{
		Statements: []Statement{
			&BazelDep{Name: label.MustModule("dep1"), Version: label.MustVersion("1.0.0")},
			&BazelDep{Name: label.MustModule("dep2"), Version: label.MustVersion("2.0.0")},
			&BazelDep{Name: label.MustModule("dep3"), Version: label.MustVersion("3.0.0")},
		},
	}

	expectedErr := errors.New("test error")
	handler := &recordingHandler{err: expectedErr}

	err := Walk(file, handler)
	if err != expectedErr {
		t.Errorf("Expected error %v, got %v", expectedErr, err)
	}

	// Should have stopped after first call
	if len(handler.calls) != 1 {
		t.Errorf("Expected 1 call (stopped on error), got %d", len(handler.calls))
	}
}

// TestWalk_AllOverrideTypes tests all override types
func TestWalk_AllOverrideTypes(t *testing.T) {
	file := &ModuleFile{
		Statements: []Statement{
			&SingleVersionOverride{Module: label.MustModule("single")},
			&MultipleVersionOverride{Module: label.MustModule("multiple")},
			&GitOverride{Module: label.MustModule("git")},
			&ArchiveOverride{Module: label.MustModule("archive")},
			&LocalPathOverride{Module: label.MustModule("local")},
		},
	}
	handler := &recordingHandler{}

	err := Walk(file, handler)
	if err != nil {
		t.Errorf("Walk returned error: %v", err)
	}

	expected := []string{
		"SingleVersionOverride:single",
		"MultipleVersionOverride:multiple",
		"GitOverride:git",
		"ArchiveOverride:archive",
		"LocalPathOverride:local",
	}

	if len(handler.calls) != len(expected) {
		t.Fatalf("Expected %d calls, got %d", len(expected), len(handler.calls))
	}

	for i, exp := range expected {
		if handler.calls[i] != exp {
			t.Errorf("Call %d: expected %q, got %q", i, exp, handler.calls[i])
		}
	}
}

// TestWalk_RegisterStatements tests register_toolchains and register_execution_platforms
func TestWalk_RegisterStatements(t *testing.T) {
	file := &ModuleFile{
		Statements: []Statement{
			&RegisterToolchains{Patterns: []string{"//toolchains:all"}},
			&RegisterExecutionPlatforms{Patterns: []string{"//platforms:all"}},
		},
	}
	handler := &recordingHandler{}

	err := Walk(file, handler)
	if err != nil {
		t.Errorf("Walk returned error: %v", err)
	}

	if len(handler.calls) != 2 {
		t.Fatalf("Expected 2 calls, got %d", len(handler.calls))
	}

	if handler.calls[0] != "RegisterToolchains" {
		t.Errorf("Expected 'RegisterToolchains', got %q", handler.calls[0])
	}
	if handler.calls[1] != "RegisterExecutionPlatforms" {
		t.Errorf("Expected 'RegisterExecutionPlatforms', got %q", handler.calls[1])
	}
}

// TestWalk_UnknownStatement tests handling of unknown statements
func TestWalk_UnknownStatement(t *testing.T) {
	file := &ModuleFile{
		Statements: []Statement{
			&UnknownStatement{FuncName: "custom_func", Pos: Position{Line: 10}},
		},
	}
	handler := &recordingHandler{}

	err := Walk(file, handler)
	if err != nil {
		t.Errorf("Walk returned error: %v", err)
	}

	if len(handler.calls) != 1 {
		t.Fatalf("Expected 1 call, got %d", len(handler.calls))
	}

	if handler.calls[0] != "UnknownStatement:custom_func" {
		t.Errorf("Expected 'UnknownStatement:custom_func', got %q", handler.calls[0])
	}
}

// TestWalk_UseExtensionWithTags tests use_extension with tag calls
func TestWalk_UseExtensionWithTags(t *testing.T) {
	proxy := &mockExtensionProxy{}

	// Override UseExtension to return our mock proxy
	useExtHandler := &useExtensionHandler{
		BaseHandler: BaseHandler{},
		proxy:       proxy,
	}

	file := &ModuleFile{
		Statements: []Statement{
			&UseExtension{
				ExtensionFile: mustApparentLabel("@rules_go//go:extensions.bzl"),
				ExtensionName: mustStarlarkIdentifier("go_sdk"),
				Tags: []ExtensionTag{
					{Name: "download", Attributes: map[string]any{"version": "1.21"}},
					{Name: "host", Attributes: map[string]any{}},
				},
			},
		},
	}

	err := Walk(file, useExtHandler)
	if err != nil {
		t.Errorf("Walk returned error: %v", err)
	}

	if len(proxy.tags) != 2 {
		t.Fatalf("Expected 2 tags, got %d", len(proxy.tags))
	}

	if proxy.tags[0] != "download" {
		t.Errorf("Expected first tag 'download', got %q", proxy.tags[0])
	}
	if proxy.tags[1] != "host" {
		t.Errorf("Expected second tag 'host', got %q", proxy.tags[1])
	}
}

// useExtensionHandler is a test handler that returns a mock proxy
type useExtensionHandler struct {
	BaseHandler
	proxy *mockExtensionProxy
}

func (h *useExtensionHandler) UseExtension(extFile label.ApparentLabel, extName label.StarlarkIdentifier, devDep, isolate bool) (ExtensionProxy, error) {
	return h.proxy, nil
}

// TestWalk_UseExtensionNilProxy tests use_extension that returns nil proxy
func TestWalk_UseExtensionNilProxy(t *testing.T) {
	handler := &BaseHandler{}

	file := &ModuleFile{
		Statements: []Statement{
			&UseExtension{
				ExtensionFile: mustApparentLabel("@ext//:ext.bzl"),
				ExtensionName: mustStarlarkIdentifier("ext"),
				Tags: []ExtensionTag{
					{Name: "should_be_skipped"},
				},
			},
		},
	}

	// BaseHandler returns nil proxy, tags should be skipped
	err := Walk(file, handler)
	if err != nil {
		t.Errorf("Walk returned error: %v", err)
	}
}

// TestWalk_UseExtensionProxyError tests error from proxy.Tag
func TestWalk_UseExtensionProxyError(t *testing.T) {
	expectedErr := errors.New("tag error")
	proxy := &mockExtensionProxy{returnErr: expectedErr}

	handler := &useExtensionHandler{proxy: proxy}

	file := &ModuleFile{
		Statements: []Statement{
			&UseExtension{
				ExtensionFile: mustApparentLabel("@ext//:ext.bzl"),
				ExtensionName: mustStarlarkIdentifier("ext"),
				Tags: []ExtensionTag{
					{Name: "will_fail"},
				},
			},
		},
	}

	err := Walk(file, handler)
	if err != expectedErr {
		t.Errorf("Expected error %v, got %v", expectedErr, err)
	}
}

// TestBaseHandler_AllMethodsReturnNil verifies BaseHandler default behavior
func TestBaseHandler_AllMethodsReturnNil(t *testing.T) {
	h := &BaseHandler{}

	if err := h.Module(label.MustModule("m"), label.MustVersion("1.0"), 0, mustApparentRepo("")); err != nil {
		t.Errorf("Module returned error: %v", err)
	}

	if err := h.BazelDep(label.MustModule("d"), label.MustVersion("1.0"), 0, mustApparentRepo(""), false); err != nil {
		t.Errorf("BazelDep returned error: %v", err)
	}

	if proxy, err := h.UseExtension(mustApparentLabel("@x//:x.bzl"), mustStarlarkIdentifier("x"), false, false); err != nil || proxy != nil {
		t.Errorf("UseExtension returned (%v, %v), want (nil, nil)", proxy, err)
	}

	if err := h.UseRepo([]string{"repo"}, false); err != nil {
		t.Errorf("UseRepo returned error: %v", err)
	}

	if err := h.SingleVersionOverride(label.MustModule("m"), label.MustVersion("1.0"), "", nil, nil, 0); err != nil {
		t.Errorf("SingleVersionOverride returned error: %v", err)
	}

	if err := h.MultipleVersionOverride(label.MustModule("m"), nil, ""); err != nil {
		t.Errorf("MultipleVersionOverride returned error: %v", err)
	}

	if err := h.GitOverride(label.MustModule("m"), "", "", "", "", nil, nil, 0, false, ""); err != nil {
		t.Errorf("GitOverride returned error: %v", err)
	}

	if err := h.ArchiveOverride(label.MustModule("m"), nil, "", "", nil, nil, 0); err != nil {
		t.Errorf("ArchiveOverride returned error: %v", err)
	}

	if err := h.LocalPathOverride(label.MustModule("m"), ""); err != nil {
		t.Errorf("LocalPathOverride returned error: %v", err)
	}

	if err := h.RegisterToolchains(nil, false); err != nil {
		t.Errorf("RegisterToolchains returned error: %v", err)
	}

	if err := h.RegisterExecutionPlatforms(nil, false); err != nil {
		t.Errorf("RegisterExecutionPlatforms returned error: %v", err)
	}

	if err := h.UnknownStatement("func", Position{}); err != nil {
		t.Errorf("UnknownStatement returned error: %v", err)
	}
}

// TestDependencyCollector tests the DependencyCollector helper
func TestDependencyCollector(t *testing.T) {
	file := &ModuleFile{
		Statements: []Statement{
			&ModuleDecl{Name: label.MustModule("root"), Version: label.MustVersion("1.0.0")},
			&BazelDep{Name: label.MustModule("dep1"), Version: label.MustVersion("1.0.0")},
			&BazelDep{Name: label.MustModule("dep2"), Version: label.MustVersion("2.0.0"), DevDependency: true},
			&BazelDep{Name: label.MustModule("dep3"), Version: label.MustVersion("3.0.0"), MaxCompatibilityLevel: 2},
		},
	}

	collector := &DependencyCollector{}
	err := Walk(file, collector)
	if err != nil {
		t.Fatalf("Walk returned error: %v", err)
	}

	if len(collector.Dependencies) != 3 {
		t.Fatalf("Expected 3 dependencies, got %d", len(collector.Dependencies))
	}

	// Check first dep
	if collector.Dependencies[0].Name.String() != "dep1" {
		t.Errorf("deps[0].Name = %q, want 'dep1'", collector.Dependencies[0].Name.String())
	}

	// Check dev dependency
	if !collector.Dependencies[1].DevDependency {
		t.Error("deps[1] should be a dev dependency")
	}

	// Check max compatibility level
	if collector.Dependencies[2].MaxCompatibilityLevel != 2 {
		t.Errorf("deps[2].MaxCompatibilityLevel = %d, want 2", collector.Dependencies[2].MaxCompatibilityLevel)
	}
}

// TestOverrideCollector tests the OverrideCollector helper
func TestOverrideCollector(t *testing.T) {
	file := &ModuleFile{
		Statements: []Statement{
			&SingleVersionOverride{
				Module:   label.MustModule("single_mod"),
				Version:  label.MustVersion("1.0.0"),
				Registry: "https://custom.registry",
				Patches:  []string{"fix.patch"},
			},
			&MultipleVersionOverride{
				Module:   label.MustModule("multi_mod"),
				Versions: []label.Version{label.MustVersion("1.0.0"), label.MustVersion("2.0.0")},
			},
			&GitOverride{
				Module: label.MustModule("git_mod"),
				Remote: "https://github.com/example/repo.git",
				Commit: "abc123",
			},
			&ArchiveOverride{
				Module:    label.MustModule("archive_mod"),
				URLs:      []string{"https://example.com/archive.zip"},
				Integrity: "sha256-abc123",
			},
			&LocalPathOverride{
				Module: label.MustModule("local_mod"),
				Path:   "/path/to/module",
			},
		},
	}

	collector := &OverrideCollector{}
	err := Walk(file, collector)
	if err != nil {
		t.Fatalf("Walk returned error: %v", err)
	}

	// Check counts
	if len(collector.SingleVersionOverrides) != 1 {
		t.Errorf("Expected 1 single_version_override, got %d", len(collector.SingleVersionOverrides))
	}
	if len(collector.MultipleVersionOverrides) != 1 {
		t.Errorf("Expected 1 multiple_version_override, got %d", len(collector.MultipleVersionOverrides))
	}
	if len(collector.GitOverrides) != 1 {
		t.Errorf("Expected 1 git_override, got %d", len(collector.GitOverrides))
	}
	if len(collector.ArchiveOverrides) != 1 {
		t.Errorf("Expected 1 archive_override, got %d", len(collector.ArchiveOverrides))
	}
	if len(collector.LocalPathOverrides) != 1 {
		t.Errorf("Expected 1 local_path_override, got %d", len(collector.LocalPathOverrides))
	}

	// Verify details
	if collector.SingleVersionOverrides[0].Registry != "https://custom.registry" {
		t.Errorf("single_version_override.Registry = %q", collector.SingleVersionOverrides[0].Registry)
	}

	if collector.GitOverrides[0].Commit != "abc123" {
		t.Errorf("git_override.Commit = %q", collector.GitOverrides[0].Commit)
	}

	if collector.LocalPathOverrides[0].Path != "/path/to/module" {
		t.Errorf("local_path_override.Path = %q", collector.LocalPathOverrides[0].Path)
	}
}

// TestOverrideCollector_EmptyFile tests collector with no overrides
func TestOverrideCollector_EmptyFile(t *testing.T) {
	file := &ModuleFile{
		Statements: []Statement{
			&ModuleDecl{Name: label.MustModule("root"), Version: label.MustVersion("1.0.0")},
			&BazelDep{Name: label.MustModule("dep"), Version: label.MustVersion("1.0.0")},
		},
	}

	collector := &OverrideCollector{}
	err := Walk(file, collector)
	if err != nil {
		t.Fatalf("Walk returned error: %v", err)
	}

	// All slices should be nil
	if collector.SingleVersionOverrides != nil {
		t.Error("SingleVersionOverrides should be nil")
	}
	if collector.GitOverrides != nil {
		t.Error("GitOverrides should be nil")
	}
}

// TestWalk_MixedStatements tests walking a file with many statement types
func TestWalk_MixedStatements(t *testing.T) {
	file := &ModuleFile{
		Statements: []Statement{
			&ModuleDecl{Name: label.MustModule("mixed_test"), Version: label.MustVersion("1.0.0")},
			&BazelDep{Name: label.MustModule("dep1"), Version: label.MustVersion("1.0.0")},
			&BazelDep{Name: label.MustModule("dep2"), Version: label.MustVersion("2.0.0")},
			&SingleVersionOverride{Module: label.MustModule("dep1"), Version: label.MustVersion("1.1.0")},
			&RegisterToolchains{Patterns: []string{"//toolchains:java"}},
			&UnknownStatement{FuncName: "custom"},
		},
	}

	handler := &recordingHandler{}
	err := Walk(file, handler)
	if err != nil {
		t.Fatalf("Walk returned error: %v", err)
	}

	if len(handler.calls) != 6 {
		t.Errorf("Expected 6 calls, got %d: %v", len(handler.calls), handler.calls)
	}
}

// TestWalk_LargeFile tests walking a file with many statements
func TestWalk_LargeFile(t *testing.T) {
	statements := make([]Statement, 0, 1000)
	statements = append(statements, &ModuleDecl{Name: label.MustModule("large"), Version: label.MustVersion("1.0.0")})

	for i := 0; i < 999; i++ {
		statements = append(statements, &BazelDep{
			Name:    label.MustModule("dep_" + string(rune('a'+i%26))),
			Version: label.MustVersion("1.0.0"),
		})
	}

	file := &ModuleFile{Statements: statements}
	collector := &DependencyCollector{}

	err := Walk(file, collector)
	if err != nil {
		t.Fatalf("Walk returned error: %v", err)
	}

	if len(collector.Dependencies) != 999 {
		t.Errorf("Expected 999 dependencies, got %d", len(collector.Dependencies))
	}
}

// TestWalk_ConcurrentSafe tests that handlers can be used concurrently
func TestWalk_ConcurrentSafe(t *testing.T) {
	file := &ModuleFile{
		Statements: []Statement{
			&BazelDep{Name: label.MustModule("dep1"), Version: label.MustVersion("1.0.0")},
			&BazelDep{Name: label.MustModule("dep2"), Version: label.MustVersion("2.0.0")},
		},
	}

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			collector := &DependencyCollector{}
			err := Walk(file, collector)
			if err != nil {
				t.Errorf("Walk returned error: %v", err)
			}
			if len(collector.Dependencies) != 2 {
				t.Errorf("Expected 2 dependencies, got %d", len(collector.Dependencies))
			}
		}()
	}
	wg.Wait()
}

// TestWalk_UseRepoStatement tests use_repo handling
func TestWalk_UseRepoStatement(t *testing.T) {
	var calledRepos []string

	// Create a custom handler to capture UseRepo calls
	customHandler := &useRepoHandler{repos: &calledRepos}

	file := &ModuleFile{
		Statements: []Statement{
			&UseRepo{Repos: []string{"repo1", "repo2", "repo3"}, DevDependency: true},
		},
	}

	err := Walk(file, customHandler)
	if err != nil {
		t.Fatalf("Walk returned error: %v", err)
	}

	if len(calledRepos) != 3 {
		t.Errorf("Expected 3 repos, got %d", len(calledRepos))
	}
}

type useRepoHandler struct {
	BaseHandler
	repos *[]string
}

func (h *useRepoHandler) UseRepo(repos []string, devDep bool) error {
	*h.repos = append(*h.repos, repos...)
	return nil
}

// TestDependencyCollector_WithRepoName tests collecting deps with repo_name
func TestDependencyCollector_WithRepoName(t *testing.T) {
	file := &ModuleFile{
		Statements: []Statement{
			&BazelDep{
				Name:     label.MustModule("rules_python"),
				Version:  label.MustVersion("0.35.0"),
				RepoName: mustApparentRepo("py_rules"),
			},
		},
	}

	collector := &DependencyCollector{}
	err := Walk(file, collector)
	if err != nil {
		t.Fatalf("Walk returned error: %v", err)
	}

	if len(collector.Dependencies) != 1 {
		t.Fatalf("Expected 1 dependency, got %d", len(collector.Dependencies))
	}

	if collector.Dependencies[0].RepoName.String() != "py_rules" {
		t.Errorf("RepoName = %q, want 'py_rules'", collector.Dependencies[0].RepoName.String())
	}
}

// TestOverrideCollector_PatchDetails tests that patches are collected correctly
func TestOverrideCollector_PatchDetails(t *testing.T) {
	file := &ModuleFile{
		Statements: []Statement{
			&SingleVersionOverride{
				Module:     label.MustModule("patched_mod"),
				Version:    label.MustVersion("1.0.0"),
				Patches:    []string{"fix1.patch", "fix2.patch"},
				PatchCmds:  []string{"sed -i s/old/new/g file.txt"},
				PatchStrip: 1,
			},
		},
	}

	collector := &OverrideCollector{}
	err := Walk(file, collector)
	if err != nil {
		t.Fatalf("Walk returned error: %v", err)
	}

	override := collector.SingleVersionOverrides[0]

	if len(override.Patches) != 2 {
		t.Errorf("Expected 2 patches, got %d", len(override.Patches))
	}

	if len(override.PatchCmds) != 1 {
		t.Errorf("Expected 1 patch command, got %d", len(override.PatchCmds))
	}

	if override.PatchStrip != 1 {
		t.Errorf("PatchStrip = %d, want 1", override.PatchStrip)
	}
}

// TestOverrideCollector_GitOverrideDetails tests git_override field collection
func TestOverrideCollector_GitOverrideDetails(t *testing.T) {
	file := &ModuleFile{
		Statements: []Statement{
			&GitOverride{
				Module:         label.MustModule("git_mod"),
				Remote:         "https://github.com/example/repo.git",
				Commit:         "abc123def456",
				Tag:            "v1.0.0",
				Branch:         "main",
				InitSubmodules: true,
				StripPrefix:    "src",
			},
		},
	}

	collector := &OverrideCollector{}
	err := Walk(file, collector)
	if err != nil {
		t.Fatalf("Walk returned error: %v", err)
	}

	override := collector.GitOverrides[0]

	if override.Remote != "https://github.com/example/repo.git" {
		t.Errorf("Remote = %q", override.Remote)
	}
	if override.Commit != "abc123def456" {
		t.Errorf("Commit = %q", override.Commit)
	}
	if override.Tag != "v1.0.0" {
		t.Errorf("Tag = %q", override.Tag)
	}
	if override.Branch != "main" {
		t.Errorf("Branch = %q", override.Branch)
	}
	if !override.InitSubmodules {
		t.Error("InitSubmodules should be true")
	}
	if override.StripPrefix != "src" {
		t.Errorf("StripPrefix = %q", override.StripPrefix)
	}
}
