package gobzlmod

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveFromFile_Success(t *testing.T) {
	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/modules/rules_go/0.41.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "rules_go", version = "0.41.0")
			bazel_dep(name = "bazel_skylib", version = "1.4.1")`)
		case "/modules/bazel_skylib/1.4.1/MODULE.bazel":
			fmt.Fprint(w, `module(name = "bazel_skylib", version = "1.4.1")`)
		case "/modules/gazelle/0.32.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "gazelle", version = "0.32.0")`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	// Create temporary MODULE.bazel file
	tempDir, err := os.MkdirTemp("", "api_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	moduleContent := `module(
		name = "test_project",
		version = "1.0.0",
	)
	
	bazel_dep(name = "rules_go", version = "0.41.0")
	bazel_dep(name = "gazelle", version = "0.32.0", dev_dependency = True)`

	moduleFile := filepath.Join(tempDir, "MODULE.bazel")
	err = os.WriteFile(moduleFile, []byte(moduleContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write MODULE.bazel: %v", err)
	}

	tests := []struct {
		name            string
		includeDevDeps  bool
		expectedModules int
	}{
		{
			name:            "with dev dependencies",
			includeDevDeps:  true,
			expectedModules: 3, // rules_go, bazel_skylib, gazelle
		},
		{
			name:            "without dev dependencies",
			includeDevDeps:  false,
			expectedModules: 2, // rules_go, bazel_skylib (gazelle is dev dependency)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			list, err := ResolveDependenciesFromFile(moduleFile, server.URL, tt.includeDevDeps)
			if err != nil {
				t.Fatalf("ResolveDependenciesFromFile() error = %v", err)
			}

			if len(list.Modules) != tt.expectedModules {
				t.Errorf("Expected %d modules, got %d", tt.expectedModules, len(list.Modules))
			}

			// Check that modules are sorted by name
			for i := 1; i < len(list.Modules); i++ {
				if list.Modules[i-1].Name >= list.Modules[i].Name {
					t.Error("Modules are not sorted by name")
					break
				}
			}

			// Verify summary counts
			devCount := 0
			prodCount := 0
			for _, module := range list.Modules {
				if module.DevDependency {
					devCount++
				} else {
					prodCount++
				}
			}

			if list.Summary.DevModules != devCount {
				t.Errorf("Summary.DevModules = %d, want %d", list.Summary.DevModules, devCount)
			}
			if list.Summary.ProductionModules != prodCount {
				t.Errorf("Summary.ProductionModules = %d, want %d", list.Summary.ProductionModules, prodCount)
			}
			if list.Summary.TotalModules != len(list.Modules) {
				t.Errorf("Summary.TotalModules = %d, want %d", list.Summary.TotalModules, len(list.Modules))
			}
		})
	}
}

func TestResolveFromFile_FileNotFound(t *testing.T) {
	nonexistentFile := "/path/that/does/not/exist/MODULE.bazel"

	list, err := ResolveDependenciesFromFile(nonexistentFile, "https://bcr.bazel.build", false)

	if err == nil {
		t.Error("Expected error for nonexistent file")
	}

	if list != nil {
		t.Error("Expected nil list for file error")
	}
}

func TestResolveFromFile_InvalidModuleFile(t *testing.T) {
	// Create temporary invalid MODULE.bazel file
	tempDir, err := os.MkdirTemp("", "api_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	invalidContent := `invalid syntax here (`
	moduleFile := filepath.Join(tempDir, "MODULE.bazel")
	err = os.WriteFile(moduleFile, []byte(invalidContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write invalid MODULE.bazel: %v", err)
	}

	list, err := ResolveDependenciesFromFile(moduleFile, "https://bcr.bazel.build", false)

	if err == nil {
		t.Error("Expected error for invalid MODULE.bazel file")
	}

	if list != nil {
		t.Error("Expected nil list for parse error")
	}
}

func TestResolveFromContent_Success(t *testing.T) {
	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/modules/rules_go/0.41.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "rules_go", version = "0.41.0")`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	content := `module(
		name = "test_project",
		version = "1.0.0",
	)
	
	bazel_dep(name = "rules_go", version = "0.41.0")`

	list, err := ResolveDependenciesFromContent(content, server.URL, false)
	if err != nil {
		t.Fatalf("ResolveDependenciesFromContent() error = %v", err)
	}

	if len(list.Modules) != 1 {
		t.Errorf("Expected 1 module, got %d", len(list.Modules))
	}

	if list.Modules[0].Name != "rules_go" {
		t.Errorf("Expected rules_go, got %s", list.Modules[0].Name)
	}
	if list.Modules[0].Version != "0.41.0" {
		t.Errorf("Expected version 0.41.0, got %s", list.Modules[0].Version)
	}
}

func TestResolveFromContent_ParseError(t *testing.T) {
	content := `invalid syntax here (`

	list, err := ResolveDependenciesFromContent(content, "https://bcr.bazel.build", false)

	if err == nil {
		t.Error("Expected error for invalid content")
	}

	if list != nil {
		t.Error("Expected nil list for parse error")
	}
}

func TestResolveFromContent_NetworkError(t *testing.T) {
	content := `module(name = "test", version = "1.0.0")
	bazel_dep(name = "nonexistent", version = "1.0.0")`

	// Use invalid registry URL
	list, err := ResolveDependenciesFromContent(content, "http://invalid-registry.com", false)

	// The resolver should complete successfully but with warnings for failed dependencies
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	if list == nil {
		t.Error("Expected non-nil list even with network errors")
		return
	}

	// The failed dependency should not appear in the results
	if len(list.Modules) != 0 {
		t.Errorf("Expected 0 modules due to network failures, got %d", len(list.Modules))
	}
}

func TestResolveFromContent_WithOverrides(t *testing.T) {
	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/modules/rules_go/0.40.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "rules_go", version = "0.40.0")`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	content := `module(name = "test_project", version = "1.0.0")
	
	bazel_dep(name = "rules_go", version = "0.41.0")
	
	single_version_override(
		module_name = "rules_go",
		version = "0.40.0",
		registry = "https://custom.registry.com",
	)`

	list, err := ResolveDependenciesFromContent(content, server.URL, false)
	if err != nil {
		t.Fatalf("ResolveDependenciesFromContent() error = %v", err)
	}

	if len(list.Modules) != 1 {
		t.Errorf("Expected 1 module, got %d", len(list.Modules))
	}

	module := list.Modules[0]
	if module.Name != "rules_go" {
		t.Errorf("Expected rules_go, got %s", module.Name)
	}
	if module.Version != "0.40.0" {
		t.Errorf("Expected version 0.40.0 (overridden), got %s", module.Version)
	}
	if module.Registry != "https://custom.registry.com" {
		t.Errorf("Expected custom registry, got %s", module.Registry)
	}
}

func TestResolveFromContent_MVSSelection(t *testing.T) {
	// Create mock server that simulates transitive dependencies with version conflicts
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/modules/dep_a/1.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "dep_a", version = "1.0.0")
			bazel_dep(name = "shared_dep", version = "2.0.0")`)
		case "/modules/dep_b/1.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "dep_b", version = "1.0.0")
			bazel_dep(name = "shared_dep", version = "2.1.0")`)
		case "/modules/shared_dep/2.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "shared_dep", version = "2.0.0")`)
		case "/modules/shared_dep/2.1.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "shared_dep", version = "2.1.0")`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	content := `module(name = "test_project", version = "1.0.0")
	
	bazel_dep(name = "dep_a", version = "1.0.0")
	bazel_dep(name = "dep_b", version = "1.0.0")`

	list, err := ResolveDependenciesFromContent(content, server.URL, false)
	if err != nil {
		t.Fatalf("ResolveDependenciesFromContent() error = %v", err)
	}

	// Should have 3 modules: dep_a, dep_b, shared_dep
	if len(list.Modules) != 3 {
		t.Errorf("Expected 3 modules, got %d", len(list.Modules))
	}

	// Find shared_dep and verify MVS selected the higher version
	var sharedDep *ModuleToResolve
	for i := range list.Modules {
		if list.Modules[i].Name == "shared_dep" {
			sharedDep = &list.Modules[i]
			break
		}
	}

	if sharedDep == nil {
		t.Fatal("shared_dep not found in resolution list")
	}

	if sharedDep.Version != "2.1.0" {
		t.Errorf("Expected MVS to select version 2.1.0, got %s", sharedDep.Version)
	}
}

func TestResolveFromContent_EmptyModule(t *testing.T) {
	content := `module(name = "empty_project", version = "1.0.0")`

	list, err := ResolveDependenciesFromContent(content, "https://bcr.bazel.build", false)

	if err != nil {
		t.Fatalf("ResolveDependenciesFromContent() error = %v", err)
	}

	if len(list.Modules) != 0 {
		t.Errorf("Expected 0 modules for empty project, got %d", len(list.Modules))
	}

	if list.Summary.TotalModules != 0 {
		t.Errorf("Expected TotalModules = 0, got %d", list.Summary.TotalModules)
	}
}

// Benchmark tests
func BenchmarkResolveFromContent_Simple(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `module(name = "simple_dep", version = "1.0.0")`)
	}))
	defer server.Close()

	content := `module(name = "bench_project", version = "1.0.0")
	bazel_dep(name = "simple_dep", version = "1.0.0")`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := ResolveDependenciesFromContent(content, server.URL, false)
		if err != nil {
			b.Fatalf("ResolveDependenciesFromContent() error = %v", err)
		}
	}
}

func BenchmarkResolveFromContent_Complex(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate a complex dependency tree
		switch r.URL.Path {
		case "/modules/dep1/1.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "dep1", version = "1.0.0")
			bazel_dep(name = "shared1", version = "1.0.0")
			bazel_dep(name = "shared2", version = "1.0.0")`)
		case "/modules/dep2/1.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "dep2", version = "1.0.0")
			bazel_dep(name = "shared1", version = "1.1.0")
			bazel_dep(name = "shared3", version = "1.0.0")`)
		default:
			fmt.Fprint(w, `module(name = "default", version = "1.0.0")`)
		}
	}))
	defer server.Close()

	content := `module(name = "bench_project", version = "1.0.0")
	bazel_dep(name = "dep1", version = "1.0.0")
	bazel_dep(name = "dep2", version = "1.0.0")`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := ResolveDependenciesFromContent(content, server.URL, false)
		if err != nil {
			b.Fatalf("ResolveDependenciesFromContent() error = %v", err)
		}
	}
}
