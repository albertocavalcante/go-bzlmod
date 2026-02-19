package gobzlmod

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/albertocavalcante/go-bzlmod/selection"
)

// resolveWithSelection is a test helper that mimics the old resolveWithSelection API.
func resolveWithSelection(ctx context.Context, moduleContent string, opts ResolutionOptions) (*selectionResult, error) {
	moduleInfo, err := ParseModuleContent(moduleContent)
	if err != nil {
		return nil, fmt.Errorf("parse module content: %w", err)
	}
	reg := registryFromOptions(opts)
	resolver := newSelectionResolver(reg, opts)
	return resolver.Resolve(ctx, moduleInfo)
}

func TestResolveWithSelection_Basic(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/modules/rules_go/0.41.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "rules_go", version = "0.41.0")
bazel_dep(name = "bazel_skylib", version = "1.4.1")`)
		case "/modules/bazel_skylib/1.4.1/MODULE.bazel":
			fmt.Fprint(w, `module(name = "bazel_skylib", version = "1.4.1")`)
		case "/modules/gazelle/0.32.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "gazelle", version = "0.32.0")
bazel_dep(name = "rules_go", version = "0.40.0")`)
		case "/modules/rules_go/0.40.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "rules_go", version = "0.40.0")
bazel_dep(name = "bazel_skylib", version = "1.4.0")`)
		case "/modules/bazel_skylib/1.4.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "bazel_skylib", version = "1.4.0")`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	moduleContent := `module(name = "test", version = "1.0.0")
bazel_dep(name = "rules_go", version = "0.41.0")
bazel_dep(name = "gazelle", version = "0.32.0")`

	opts := ResolutionOptions{
		Registries:     []string{server.URL},
		IncludeDevDeps: false,
	}

	result, err := resolveWithSelection(context.Background(), moduleContent, opts)
	if err != nil {
		t.Fatalf("resolveWithSelection() error = %v", err)
	}

	// Should have resolved modules
	if result.Resolved == nil {
		t.Fatal("Resolved should not be nil")
	}

	// MVS should select highest version of rules_go (0.41.0 > 0.40.0)
	foundRulesGo := false
	for _, m := range result.Resolved.Modules {
		if m.Name == "rules_go" {
			if m.Version != "0.41.0" {
				t.Errorf("expected rules_go@0.41.0, got rules_go@%s", m.Version)
			}
			foundRulesGo = true
		}
	}
	if !foundRulesGo {
		t.Error("rules_go should be in resolved modules")
	}

	// MVS should select highest version of bazel_skylib (1.4.1 > 1.4.0)
	for _, m := range result.Resolved.Modules {
		if m.Name == "bazel_skylib" {
			if m.Version != "1.4.1" {
				t.Errorf("expected bazel_skylib@1.4.1, got bazel_skylib@%s", m.Version)
			}
		}
	}

	// Should have BFS order
	if len(result.BFSOrder) == 0 {
		t.Error("BFSOrder should not be empty")
	}
}

func TestResolveWithSelection_DevDeps(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/modules/rules_go/0.41.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "rules_go", version = "0.41.0")`)
		case "/modules/rules_testing/0.1.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "rules_testing", version = "0.1.0")`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	moduleContent := `module(name = "test", version = "1.0.0")
bazel_dep(name = "rules_go", version = "0.41.0")
bazel_dep(name = "rules_testing", version = "0.1.0", dev_dependency = True)`

	t.Run("without dev deps", func(t *testing.T) {
		opts := ResolutionOptions{
			Registries:     []string{server.URL},
			IncludeDevDeps: false,
		}
		result, err := resolveWithSelection(context.Background(), moduleContent, opts)
		if err != nil {
			t.Fatalf("error: %v", err)
		}

		for _, m := range result.Resolved.Modules {
			if m.Name == "rules_testing" {
				t.Error("rules_testing should not be included without dev deps")
			}
		}
	})

	t.Run("with dev deps", func(t *testing.T) {
		opts := ResolutionOptions{
			Registries:     []string{server.URL},
			IncludeDevDeps: true,
		}
		result, err := resolveWithSelection(context.Background(), moduleContent, opts)
		if err != nil {
			t.Fatalf("error: %v", err)
		}

		foundDevDep := false
		for _, m := range result.Resolved.Modules {
			if m.Name == "rules_testing" {
				foundDevDep = true
			}
		}
		if !foundDevDep {
			t.Error("rules_testing should be included with dev deps")
		}
	})
}

func TestResolveWithSelection_DevDeps_TransitiveDevMarkedCorrectly(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/modules/prod_lib/1.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "prod_lib", version = "1.0.0")`)
		case "/modules/dev_tool/1.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "dev_tool", version = "1.0.0")
bazel_dep(name = "dev_helper", version = "1.0.0")`)
		case "/modules/dev_helper/1.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "dev_helper", version = "1.0.0")`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	moduleContent := `module(name = "test", version = "1.0.0")
bazel_dep(name = "prod_lib", version = "1.0.0")
bazel_dep(name = "dev_tool", version = "1.0.0", dev_dependency = True)`

	opts := ResolutionOptions{
		Registries:     []string{server.URL},
		IncludeDevDeps: true,
	}

	result, err := resolveWithSelection(context.Background(), moduleContent, opts)
	if err != nil {
		t.Fatalf("resolveWithSelection() error = %v", err)
	}

	modules := map[string]ModuleToResolve{}
	for _, m := range result.Resolved.Modules {
		modules[m.Name] = m
	}

	if !modules["dev_tool"].DevDependency {
		t.Fatalf("dev_tool should be DevDependency=true, got false")
	}
	if !modules["dev_helper"].DevDependency {
		t.Fatalf("dev_helper should be DevDependency=true (transitive dev-only), got false")
	}
	if modules["prod_lib"].DevDependency {
		t.Fatalf("prod_lib should be DevDependency=false, got true")
	}

	if result.Resolved.Summary.DevModules != 2 {
		t.Fatalf("Summary.DevModules = %d, want 2", result.Resolved.Summary.DevModules)
	}
	if result.Resolved.Summary.ProductionModules != 1 {
		t.Fatalf("Summary.ProductionModules = %d, want 1", result.Resolved.Summary.ProductionModules)
	}
}

func TestResolveWithSelection_Overrides(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/modules/rules_go/0.41.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "rules_go", version = "0.41.0")
bazel_dep(name = "bazel_skylib", version = "1.4.0")`)
		case "/modules/bazel_skylib/1.5.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "bazel_skylib", version = "1.5.0")`)
		case "/modules/bazel_skylib/1.4.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "bazel_skylib", version = "1.4.0")`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	t.Run("single_version_override", func(t *testing.T) {
		moduleContent := `module(name = "test", version = "1.0.0")
bazel_dep(name = "rules_go", version = "0.41.0")
single_version_override(module_name = "bazel_skylib", version = "1.5.0")`

		opts := ResolutionOptions{
			Registries:     []string{server.URL},
			IncludeDevDeps: false,
		}
		result, err := resolveWithSelection(context.Background(), moduleContent, opts)
		if err != nil {
			t.Fatalf("error: %v", err)
		}

		for _, m := range result.Resolved.Modules {
			if m.Name == "bazel_skylib" {
				if m.Version != "1.5.0" {
					t.Errorf("expected bazel_skylib@1.5.0 (overridden), got @%s", m.Version)
				}
				return
			}
		}
		t.Error("bazel_skylib should be in resolved modules")
	})

	t.Run("git_override skips registry fetch", func(t *testing.T) {
		moduleContent := `module(name = "test", version = "1.0.0")
bazel_dep(name = "rules_go", version = "0.41.0")
git_override(module_name = "bazel_skylib", remote = "https://github.com/bazelbuild/bazel-skylib.git", commit = "abc123")`

		opts := ResolutionOptions{
			Registries:     []string{server.URL},
			IncludeDevDeps: false,
		}
		result, err := resolveWithSelection(context.Background(), moduleContent, opts)
		if err != nil {
			t.Fatalf("error: %v", err)
		}

		// Should resolve successfully even though we didn't mock bazel_skylib in registry
		if result.Resolved == nil {
			t.Error("should resolve with git_override")
		}
	})
}

func TestResolveWithSelection_YankedVersions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/modules/rules_go/0.41.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "rules_go", version = "0.41.0")`)
		case "/modules/rules_go/metadata.json":
			metadata := map[string]any{
				"versions": []string{"0.40.0", "0.41.0"},
				"yanked_versions": map[string]string{
					"0.41.0": "Security vulnerability in 0.41.0",
				},
			}
			json.NewEncoder(w).Encode(metadata)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	moduleContent := `module(name = "test", version = "1.0.0")
bazel_dep(name = "rules_go", version = "0.41.0")`

	t.Run("error on yanked version", func(t *testing.T) {
		opts := ResolutionOptions{
			Registries:     []string{server.URL},
			IncludeDevDeps: false,
			CheckYanked:    true,
			YankedBehavior: YankedVersionError,
		}

		_, err := resolveWithSelection(context.Background(), moduleContent, opts)
		if err == nil {
			t.Fatal("expected error for yanked version")
		}

		var yankedErr *YankedVersionsError
		if !isYankedError(err, &yankedErr) {
			t.Fatalf("expected YankedVersionsError, got %T", err)
		}
	})

	t.Run("warn on yanked version", func(t *testing.T) {
		opts := ResolutionOptions{
			Registries:     []string{server.URL},
			IncludeDevDeps: false,
			CheckYanked:    true,
			YankedBehavior: YankedVersionWarn,
		}

		result, err := resolveWithSelection(context.Background(), moduleContent, opts)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(result.Resolved.Warnings) == 0 {
			t.Error("expected warning for yanked version")
		}
	})
}

func TestResolveWithSelection_UnprunedGraph(t *testing.T) {
	// Create a graph where some modules become unreachable after selection
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/modules/a/1.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "a", version = "1.0.0")
bazel_dep(name = "b", version = "1.0.0")`)
		case "/modules/a/2.0.0/MODULE.bazel":
			// Version 2.0.0 doesn't depend on b anymore
			fmt.Fprint(w, `module(name = "a", version = "2.0.0")`)
		case "/modules/b/1.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "b", version = "1.0.0")`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	// Root depends on a@1.0.0 (which depends on b)
	// Also directly requires a@2.0.0 (which doesn't depend on b)
	// MVS selects a@2.0.0, so b becomes unreachable
	moduleContent := `module(name = "test", version = "1.0.0")
bazel_dep(name = "a", version = "2.0.0")`

	opts := ResolutionOptions{
		Registries:     []string{server.URL},
		IncludeDevDeps: false,
	}
	result, err := resolveWithSelection(context.Background(), moduleContent, opts)
	if err != nil {
		t.Fatalf("error: %v", err)
	}

	// Resolved should have a@2.0.0 only
	if result.Resolved.Summary.TotalModules != 1 {
		t.Errorf("expected 1 resolved module, got %d", result.Resolved.Summary.TotalModules)
	}

	hasA := false
	for _, m := range result.Resolved.Modules {
		if m.Name == "a" && m.Version == "2.0.0" {
			hasA = true
		}
		if m.Name == "b" {
			t.Error("b should not be in resolved modules (unreachable)")
		}
	}
	if !hasA {
		t.Error("a@2.0.0 should be in resolved modules")
	}
}

func TestSelectionResolver_NilModule(t *testing.T) {
	client := newRegistryClient("https://example.com")
	resolver := newSelectionResolver(client, ResolutionOptions{})

	_, err := resolver.Resolve(context.Background(), nil)
	if err == nil {
		t.Error("expected error for nil module")
	}
}

func TestConvertOverrides(t *testing.T) {
	overrides := []Override{
		{Type: "single_version", ModuleName: "foo", Version: "1.0.0"},
		{Type: "git", ModuleName: "bar"},
		{Type: "local_path", ModuleName: "baz", Path: "/tmp/baz"},
		{Type: "archive", ModuleName: "qux"},
	}

	converted := convertOverrides(overrides)

	if len(converted) != 4 {
		t.Errorf("expected 4 overrides, got %d", len(converted))
	}

	// Check single_version
	if _, ok := converted["foo"]; !ok {
		t.Error("foo should be in converted overrides")
	}

	// Check non-registry overrides
	for _, name := range []string{"bar", "baz", "qux"} {
		if _, ok := converted[name]; !ok {
			t.Errorf("%s should be in converted overrides", name)
		}
	}

	bazOverride, ok := converted["baz"].(*selection.NonRegistryOverride)
	if !ok {
		t.Fatalf("baz override type = %T, want *selection.NonRegistryOverride", converted["baz"])
	}
	if bazOverride.Path != "/tmp/baz" {
		t.Fatalf("baz override path = %q, want %q", bazOverride.Path, "/tmp/baz")
	}
}

// TestResolveWithSelection_ConcurrentQueueAccess tests that the buildDepGraph
// function is race-free when goroutines concurrently add items to the queue.
// This test should be run with -race to detect data races.
func TestResolveWithSelection_ConcurrentQueueAccess(t *testing.T) {
	// Create a dependency graph with many modules that each have multiple deps.
	// This maximizes concurrent goroutines adding to the queue simultaneously.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		// Root deps: lib_a through lib_j (10 direct deps)
		case "/modules/lib_a/1.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "lib_a", version = "1.0.0")
bazel_dep(name = "shared_x", version = "1.0.0")
bazel_dep(name = "shared_y", version = "1.0.0")`)
		case "/modules/lib_b/1.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "lib_b", version = "1.0.0")
bazel_dep(name = "shared_x", version = "1.0.0")
bazel_dep(name = "shared_z", version = "1.0.0")`)
		case "/modules/lib_c/1.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "lib_c", version = "1.0.0")
bazel_dep(name = "shared_y", version = "1.0.0")
bazel_dep(name = "shared_z", version = "1.0.0")`)
		case "/modules/lib_d/1.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "lib_d", version = "1.0.0")
bazel_dep(name = "shared_x", version = "1.0.0")`)
		case "/modules/lib_e/1.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "lib_e", version = "1.0.0")
bazel_dep(name = "shared_y", version = "1.0.0")`)
		case "/modules/lib_f/1.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "lib_f", version = "1.0.0")
bazel_dep(name = "shared_z", version = "1.0.0")`)
		case "/modules/lib_g/1.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "lib_g", version = "1.0.0")
bazel_dep(name = "leaf_1", version = "1.0.0")
bazel_dep(name = "leaf_2", version = "1.0.0")`)
		case "/modules/lib_h/1.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "lib_h", version = "1.0.0")
bazel_dep(name = "leaf_3", version = "1.0.0")
bazel_dep(name = "leaf_4", version = "1.0.0")`)
		case "/modules/lib_i/1.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "lib_i", version = "1.0.0")
bazel_dep(name = "leaf_5", version = "1.0.0")`)
		case "/modules/lib_j/1.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "lib_j", version = "1.0.0")
bazel_dep(name = "leaf_6", version = "1.0.0")`)
		// Shared modules (accessed by multiple libs)
		case "/modules/shared_x/1.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "shared_x", version = "1.0.0")
bazel_dep(name = "deep_1", version = "1.0.0")`)
		case "/modules/shared_y/1.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "shared_y", version = "1.0.0")
bazel_dep(name = "deep_2", version = "1.0.0")`)
		case "/modules/shared_z/1.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "shared_z", version = "1.0.0")
bazel_dep(name = "deep_3", version = "1.0.0")`)
		// Leaf modules (no further deps)
		case "/modules/leaf_1/1.0.0/MODULE.bazel",
			"/modules/leaf_2/1.0.0/MODULE.bazel",
			"/modules/leaf_3/1.0.0/MODULE.bazel",
			"/modules/leaf_4/1.0.0/MODULE.bazel",
			"/modules/leaf_5/1.0.0/MODULE.bazel",
			"/modules/leaf_6/1.0.0/MODULE.bazel",
			"/modules/deep_1/1.0.0/MODULE.bazel",
			"/modules/deep_2/1.0.0/MODULE.bazel",
			"/modules/deep_3/1.0.0/MODULE.bazel":
			// Extract module name from path for proper response
			name := r.URL.Path[len("/modules/"):]
			name = name[:len(name)-len("/1.0.0/MODULE.bazel")]
			fmt.Fprintf(w, `module(name = "%s", version = "1.0.0")`, name)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	moduleContent := `module(name = "test", version = "1.0.0")
bazel_dep(name = "lib_a", version = "1.0.0")
bazel_dep(name = "lib_b", version = "1.0.0")
bazel_dep(name = "lib_c", version = "1.0.0")
bazel_dep(name = "lib_d", version = "1.0.0")
bazel_dep(name = "lib_e", version = "1.0.0")
bazel_dep(name = "lib_f", version = "1.0.0")
bazel_dep(name = "lib_g", version = "1.0.0")
bazel_dep(name = "lib_h", version = "1.0.0")
bazel_dep(name = "lib_i", version = "1.0.0")
bazel_dep(name = "lib_j", version = "1.0.0")`

	opts := ResolutionOptions{
		Registries:     []string{server.URL},
		IncludeDevDeps: false,
	}

	// Run multiple times to increase chance of catching race
	for i := 0; i < 10; i++ {
		result, err := resolveWithSelection(context.Background(), moduleContent, opts)
		if err != nil {
			t.Fatalf("iteration %d: resolveWithSelection() error = %v", i, err)
		}

		// Verify all expected modules are present
		// 10 libs + 3 shared + 6 leaves + 3 deep = 22 modules
		expectedModules := map[string]bool{
			"lib_a": true, "lib_b": true, "lib_c": true, "lib_d": true, "lib_e": true,
			"lib_f": true, "lib_g": true, "lib_h": true, "lib_i": true, "lib_j": true,
			"shared_x": true, "shared_y": true, "shared_z": true,
			"leaf_1": true, "leaf_2": true, "leaf_3": true,
			"leaf_4": true, "leaf_5": true, "leaf_6": true,
			"deep_1": true, "deep_2": true, "deep_3": true,
		}

		for _, m := range result.Resolved.Modules {
			delete(expectedModules, m.Name)
		}

		if len(expectedModules) > 0 {
			missing := make([]string, 0, len(expectedModules))
			for name := range expectedModules {
				missing = append(missing, name)
			}
			t.Errorf("iteration %d: missing modules: %v", i, missing)
		}
	}
}
