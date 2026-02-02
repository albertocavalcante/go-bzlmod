package gobzlmod

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/albertocavalcante/go-bzlmod/graph"
)

// TestResolutionList_Graph tests that resolution produces a valid graph.
func TestResolutionList_Graph(t *testing.T) {
	// Setup mock registry with a simple dependency tree:
	//   root -> a@1.0.0 -> c@1.0.0
	//        -> b@1.0.0 -> c@1.0.0 (shared)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/modules/a/1.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "a", version = "1.0.0")
bazel_dep(name = "c", version = "1.0.0")`)
		case "/modules/b/1.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "b", version = "1.0.0")
bazel_dep(name = "c", version = "1.0.0")`)
		case "/modules/c/1.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "c", version = "1.0.0")`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	moduleContent := `module(name = "root", version = "1.0.0")
bazel_dep(name = "a", version = "1.0.0")
bazel_dep(name = "b", version = "1.0.0")`

	result, err := ResolveContent(context.Background(), moduleContent, ResolutionOptions{
		Registries: []string{server.URL},
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	// Graph should be populated
	if result.Graph == nil {
		t.Fatal("expected Graph to be non-nil")
	}

	// Check graph has correct number of modules (root + a + b + c = 4)
	if len(result.Graph.Modules) != 4 {
		t.Errorf("expected 4 modules in graph, got %d", len(result.Graph.Modules))
	}

	// Check we can query the graph
	aNode := result.Graph.GetByName("a")
	if aNode == nil {
		t.Fatal("expected to find module 'a' in graph")
	}

	// Check a's dependencies include c
	if len(aNode.Dependencies) != 1 {
		t.Errorf("expected 'a' to have 1 dependency, got %d", len(aNode.Dependencies))
	}

	// Check c has two dependents (a and b)
	cNode := result.Graph.GetByName("c")
	if cNode == nil {
		t.Fatal("expected to find module 'c' in graph")
	}
	if len(cNode.Dependents) != 2 {
		t.Errorf("expected 'c' to have 2 dependents, got %d", len(cNode.Dependents))
	}
}

// TestResolutionList_Graph_Explain tests the explain functionality.
func TestResolutionList_Graph_Explain(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/modules/a/1.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "a", version = "1.0.0")
bazel_dep(name = "b", version = "1.0.0")`)
		case "/modules/b/1.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "b", version = "1.0.0")`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	moduleContent := `module(name = "root", version = "1.0.0")
bazel_dep(name = "a", version = "1.0.0")`

	result, err := ResolveContent(context.Background(), moduleContent, ResolutionOptions{
		Registries: []string{server.URL},
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	// Should be able to explain why b is included
	explanation, err := result.Graph.Explain("b")
	if err != nil {
		t.Fatalf("Explain() error = %v", err)
	}

	if explanation.Module.Name != "b" {
		t.Errorf("expected explanation for 'b', got %s", explanation.Module.Name)
	}

	// Should have at least one dependency chain
	if len(explanation.DependencyChains) == 0 {
		t.Error("expected at least one dependency chain")
	}
}

// TestResolutionList_Graph_ToJSON tests Bazel-compatible JSON output.
func TestResolutionList_Graph_ToJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/modules/a/1.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "a", version = "1.0.0")`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	moduleContent := `module(name = "root", version = "1.0.0")
bazel_dep(name = "a", version = "1.0.0")`

	result, err := ResolveContent(context.Background(), moduleContent, ResolutionOptions{
		Registries: []string{server.URL},
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	jsonBytes, err := result.Graph.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON() error = %v", err)
	}

	// Should produce valid JSON
	if len(jsonBytes) == 0 {
		t.Error("expected non-empty JSON output")
	}

	// Should contain module names
	jsonStr := string(jsonBytes)
	if !contains(jsonStr, "root@1.0.0") {
		t.Error("expected JSON to contain root@1.0.0")
	}
}

// TestResolutionList_Graph_Path tests finding paths between modules.
func TestResolutionList_Graph_Path(t *testing.T) {
	// Setup: root -> a -> b -> c
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/modules/a/1.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "a", version = "1.0.0")
bazel_dep(name = "b", version = "1.0.0")`)
		case "/modules/b/1.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "b", version = "1.0.0")
bazel_dep(name = "c", version = "1.0.0")`)
		case "/modules/c/1.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "c", version = "1.0.0")`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	moduleContent := `module(name = "root", version = "1.0.0")
bazel_dep(name = "a", version = "1.0.0")`

	result, err := ResolveContent(context.Background(), moduleContent, ResolutionOptions{
		Registries: []string{server.URL},
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	// Find path from root to c
	rootKey := graph.ModuleKey{Name: "root", Version: "1.0.0"}
	cKey := graph.ModuleKey{Name: "c", Version: "1.0.0"}

	path := result.Graph.Path(rootKey, cKey)
	if path == nil {
		t.Fatal("expected to find path from root to c")
	}

	// Path should be: root -> a -> b -> c (length 4)
	if len(path) != 4 {
		t.Errorf("expected path length 4, got %d", len(path))
	}
}

// TestModuleToResolve_Dependencies tests that Dependencies field is populated.
func TestModuleToResolve_Dependencies(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/modules/a/1.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "a", version = "1.0.0")
bazel_dep(name = "b", version = "1.0.0")
bazel_dep(name = "c", version = "1.0.0")`)
		case "/modules/b/1.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "b", version = "1.0.0")`)
		case "/modules/c/1.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "c", version = "1.0.0")`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	moduleContent := `module(name = "root", version = "1.0.0")
bazel_dep(name = "a", version = "1.0.0")`

	result, err := ResolveContent(context.Background(), moduleContent, ResolutionOptions{
		Registries: []string{server.URL},
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	// Find module 'a' and check its dependencies
	var moduleA *ModuleToResolve
	for i := range result.Modules {
		if result.Modules[i].Name == "a" {
			moduleA = &result.Modules[i]
			break
		}
	}

	if moduleA == nil {
		t.Fatal("expected to find module 'a'")
	}

	// 'a' should have dependencies on 'b' and 'c'
	if len(moduleA.Dependencies) != 2 {
		t.Errorf("expected 'a' to have 2 dependencies, got %d", len(moduleA.Dependencies))
	}

	// Check dependencies contain b and c
	hasB, hasC := false, false
	for _, dep := range moduleA.Dependencies {
		if dep == "b" {
			hasB = true
		}
		if dep == "c" {
			hasC = true
		}
	}
	if !hasB || !hasC {
		t.Errorf("expected dependencies to contain 'b' and 'c', got %v", moduleA.Dependencies)
	}
}

// TestResolutionList_Graph_Stats tests graph statistics.
func TestResolutionList_Graph_Stats(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/modules/a/1.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "a", version = "1.0.0")
bazel_dep(name = "b", version = "1.0.0")`)
		case "/modules/b/1.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "b", version = "1.0.0")`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	moduleContent := `module(name = "root", version = "1.0.0")
bazel_dep(name = "a", version = "1.0.0")`

	result, err := ResolveContent(context.Background(), moduleContent, ResolutionOptions{
		Registries: []string{server.URL},
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	stats := result.Graph.Stats()

	// Total modules: root + a + b = 3
	if stats.TotalModules != 3 {
		t.Errorf("expected 3 total modules, got %d", stats.TotalModules)
	}

	// Direct deps of root: just 'a'
	if stats.DirectDependencies != 1 {
		t.Errorf("expected 1 direct dependency, got %d", stats.DirectDependencies)
	}

	// Max depth: root -> a -> b = 2
	if stats.MaxDepth != 2 {
		t.Errorf("expected max depth 2, got %d", stats.MaxDepth)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
