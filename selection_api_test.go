package gobzlmod

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

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
		IncludeDevDeps: false,
	}

	result, err := ResolveWithSelection(context.Background(), moduleContent, server.URL, opts)
	if err != nil {
		t.Fatalf("ResolveWithSelection() error = %v", err)
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
		opts := ResolutionOptions{IncludeDevDeps: false}
		result, err := ResolveWithSelection(context.Background(), moduleContent, server.URL, opts)
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
		opts := ResolutionOptions{IncludeDevDeps: true}
		result, err := ResolveWithSelection(context.Background(), moduleContent, server.URL, opts)
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

		opts := ResolutionOptions{IncludeDevDeps: false}
		result, err := ResolveWithSelection(context.Background(), moduleContent, server.URL, opts)
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

		opts := ResolutionOptions{IncludeDevDeps: false}
		result, err := ResolveWithSelection(context.Background(), moduleContent, server.URL, opts)
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
			metadata := map[string]interface{}{
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
			IncludeDevDeps: false,
			CheckYanked:    true,
			YankedBehavior: YankedVersionError,
		}

		_, err := ResolveWithSelection(context.Background(), moduleContent, server.URL, opts)
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
			IncludeDevDeps: false,
			CheckYanked:    true,
			YankedBehavior: YankedVersionWarn,
		}

		result, err := ResolveWithSelection(context.Background(), moduleContent, server.URL, opts)
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

	opts := ResolutionOptions{IncludeDevDeps: false}
	result, err := ResolveWithSelection(context.Background(), moduleContent, server.URL, opts)
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
	client := NewRegistryClient("https://example.com")
	resolver := NewSelectionResolver(client, ResolutionOptions{})

	_, err := resolver.Resolve(context.Background(), nil)
	if err == nil {
		t.Error("expected error for nil module")
	}
}

func TestConvertOverrides(t *testing.T) {
	overrides := []Override{
		{Type: "single_version", ModuleName: "foo", Version: "1.0.0"},
		{Type: "git", ModuleName: "bar"},
		{Type: "local_path", ModuleName: "baz"},
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
}
