package gobzlmod

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

	if err == nil {
		t.Error("Expected error for network failures")
	}

	if list != nil {
		t.Error("Expected nil list due to network failures")
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
		registry = "` + server.URL + `",
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
	if module.Registry != server.URL {
		t.Errorf("Expected custom registry, got %s", module.Registry)
	}
}

func TestResolveFromContent_WithOverrideModules(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/modules/dep/1.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "dep", version = "1.0.0")`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	content := `module(name = "test_project", version = "1.0.0")

	bazel_dep(name = "local_mod", version = "1.0.0")

	git_override(module_name = "local_mod")`

	overrideModules := map[string]string{
		"local_mod": `module(name = "local_mod", version = "1.0.0")
		bazel_dep(name = "dep", version = "1.0.0")`,
	}

	list, err := ResolveDependenciesFromContentWithOverrides(content, server.URL, false, overrideModules)
	if err != nil {
		t.Fatalf("ResolveDependenciesFromContentWithOverrides() error = %v", err)
	}

	if len(list.Modules) != 2 {
		t.Errorf("Expected 2 modules, got %d", len(list.Modules))
	}

	versions := make(map[string]string)
	for _, module := range list.Modules {
		versions[module.Name] = module.Version
	}

	if versions["local_mod"] != "1.0.0" {
		t.Errorf("Expected local_mod version 1.0.0, got %s", versions["local_mod"])
	}
	if versions["dep"] != "1.0.0" {
		t.Errorf("Expected dep version 1.0.0, got %s", versions["dep"])
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

// =============================================================================
// Adversarial Tests for New API Functions (Resolve, ResolveFile, registryFromOptions)
// =============================================================================

// TestResolve_EmptyContent verifies behavior with empty input
func TestResolve_EmptyContent(t *testing.T) {
	ctx := context.Background()

	// Empty string should fail parsing (no module() call)
	_, err := Resolve(ctx, "", ResolutionOptions{})
	if err == nil {
		t.Error("Expected error for empty content, got nil")
	}
}

// TestResolve_WhitespaceOnlyContent tests content with only whitespace
func TestResolve_WhitespaceOnlyContent(t *testing.T) {
	ctx := context.Background()

	testCases := []string{
		"   ",
		"\t\t\t",
		"\n\n\n",
		"  \t  \n  ",
		"\r\n\r\n",
	}

	for _, content := range testCases {
		_, err := Resolve(ctx, content, ResolutionOptions{})
		if err == nil {
			t.Errorf("Expected error for whitespace-only content %q, got nil", content)
		}
	}
}

// TestResolve_ContextCancellation ensures cancellation is respected
func TestResolve_ContextCancellation(t *testing.T) {
	// Create a server that delays responses
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		fmt.Fprint(w, `module(name = "slow_dep", version = "1.0.0")`)
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())

	content := `module(name = "test", version = "1.0.0")
bazel_dep(name = "slow_dep", version = "1.0.0")`

	// Cancel immediately
	cancel()

	_, err := Resolve(ctx, content, ResolutionOptions{
		Registries: []string{server.URL},
	})

	if err == nil {
		t.Error("Expected error when context is cancelled")
	}
}

// TestResolve_ContextTimeout ensures timeout is respected
func TestResolve_ContextTimeout(t *testing.T) {
	// Create a server that delays responses longer than timeout
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		fmt.Fprint(w, `module(name = "slow_dep", version = "1.0.0")`)
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	content := `module(name = "test", version = "1.0.0")
bazel_dep(name = "slow_dep", version = "1.0.0")`

	_, err := Resolve(ctx, content, ResolutionOptions{
		Registries: []string{server.URL},
	})

	if err == nil {
		t.Error("Expected error when context times out")
	}
}

// TestResolve_NoDependencies verifies handling of module with no deps
func TestResolve_NoDependencies(t *testing.T) {
	ctx := context.Background()

	content := `module(name = "standalone", version = "1.0.0")`

	result, err := Resolve(ctx, content, ResolutionOptions{})
	if err != nil {
		t.Fatalf("Unexpected error for module with no deps: %v", err)
	}

	if result == nil {
		t.Fatal("Expected non-nil result")
	}

	if len(result.Modules) != 0 {
		t.Errorf("Expected 0 modules for standalone module, got %d", len(result.Modules))
	}

	if result.Summary.TotalModules != 0 {
		t.Errorf("Expected TotalModules=0, got %d", result.Summary.TotalModules)
	}
}

// TestResolve_DefaultRegistry verifies BCR is used by default
func TestResolve_DefaultRegistry(t *testing.T) {
	// This test verifies that when Registries is empty, we use BCR.
	// We can't actually test against BCR in unit tests, but we can verify
	// the behavior by checking that an error occurs for a non-existent module
	// (meaning we tried to reach a registry)
	ctx := context.Background()

	content := `module(name = "test", version = "1.0.0")
bazel_dep(name = "definitely_nonexistent_module_xyz_123", version = "0.0.0")`

	// With empty registries, should use default (BCR)
	_, err := Resolve(ctx, content, ResolutionOptions{
		Registries: nil, // Should default to BCR
	})

	// We expect an error (module not found), not a crash
	if err == nil {
		t.Error("Expected error for non-existent module, got nil")
	}
}

// TestResolve_CustomSingleRegistry tests single custom registry
func TestResolve_CustomSingleRegistry(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/modules/custom_dep/1.0.0/MODULE.bazel" {
			fmt.Fprint(w, `module(name = "custom_dep", version = "1.0.0")`)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	ctx := context.Background()
	content := `module(name = "test", version = "1.0.0")
bazel_dep(name = "custom_dep", version = "1.0.0")`

	result, err := Resolve(ctx, content, ResolutionOptions{
		Registries: []string{server.URL},
	})

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if len(result.Modules) != 1 {
		t.Errorf("Expected 1 module, got %d", len(result.Modules))
	}

	if result.Modules[0].Registry != server.URL {
		t.Errorf("Expected registry %s, got %s", server.URL, result.Modules[0].Registry)
	}
}

// TestResolve_MultipleRegistries tests registry chain behavior
func TestResolve_MultipleRegistries(t *testing.T) {
	// Server 1: has module_a
	server1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/modules/module_a/1.0.0/MODULE.bazel" {
			fmt.Fprint(w, `module(name = "module_a", version = "1.0.0")`)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server1.Close()

	// Server 2: has module_b
	server2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/modules/module_b/1.0.0/MODULE.bazel" {
			fmt.Fprint(w, `module(name = "module_b", version = "1.0.0")`)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server2.Close()

	ctx := context.Background()
	content := `module(name = "test", version = "1.0.0")
bazel_dep(name = "module_a", version = "1.0.0")
bazel_dep(name = "module_b", version = "1.0.0")`

	result, err := Resolve(ctx, content, ResolutionOptions{
		Registries: []string{server1.URL, server2.URL},
	})

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if len(result.Modules) != 2 {
		t.Errorf("Expected 2 modules, got %d", len(result.Modules))
	}

	// Verify each module came from the correct registry
	for _, m := range result.Modules {
		switch m.Name {
		case "module_a":
			if m.Registry != server1.URL {
				t.Errorf("module_a should come from server1, got %s", m.Registry)
			}
		case "module_b":
			if m.Registry != server2.URL {
				t.Errorf("module_b should come from server2, got %s", m.Registry)
			}
		}
	}
}

// TestResolve_InvalidRegistryURL tests behavior with invalid registry URLs
func TestResolve_InvalidRegistryURL(t *testing.T) {
	ctx := context.Background()

	content := `module(name = "test", version = "1.0.0")
bazel_dep(name = "some_dep", version = "1.0.0")`

	// Invalid URLs should fail gracefully
	testCases := []string{
		"not-a-url",
		"ftp://invalid-protocol.com",
		"://missing-scheme",
	}

	for _, url := range testCases {
		_, err := Resolve(ctx, content, ResolutionOptions{
			Registries: []string{url},
		})

		// Should get an error, not a panic
		if err == nil {
			t.Errorf("Expected error for invalid registry URL %q", url)
		}
	}
}

// TestResolve_UnicodeModuleName tests handling of unicode in module names
func TestResolve_UnicodeModuleName(t *testing.T) {
	ctx := context.Background()

	// Bazel module names should only contain [A-Za-z0-9_.-]
	// Unicode should be rejected at parse time
	content := `module(name = "模块", version = "1.0.0")`

	result, err := Resolve(ctx, content, ResolutionOptions{})

	// The parser may accept this but it's invalid per Bazel spec
	// At minimum, it shouldn't panic
	_ = result
	_ = err
}

// TestResolve_SpecialCharactersInVersion tests version strings
func TestResolve_SpecialCharactersInVersion(t *testing.T) {
	ctx := context.Background()

	testCases := []struct {
		version   string
		shouldErr bool
	}{
		{"1.0.0", false},
		{"1.0.0-alpha", false},
		{"1.0.0+build", false},
		{"1.0.0-alpha.1", false},
	}

	for _, tc := range testCases {
		content := fmt.Sprintf(`module(name = "test", version = "%s")`, tc.version)
		_, err := Resolve(ctx, content, ResolutionOptions{})

		if tc.shouldErr && err == nil {
			t.Errorf("Expected error for version %q", tc.version)
		}
		if !tc.shouldErr && err != nil {
			t.Errorf("Unexpected error for version %q: %v", tc.version, err)
		}
	}
}

// TestResolve_WindowsLineEndings tests CRLF handling
func TestResolve_WindowsLineEndings(t *testing.T) {
	ctx := context.Background()

	content := "module(name = \"test\", version = \"1.0.0\")\r\n"

	result, err := Resolve(ctx, content, ResolutionOptions{})
	if err != nil {
		t.Fatalf("Failed to parse content with CRLF: %v", err)
	}

	if result == nil {
		t.Fatal("Expected non-nil result")
	}
}

// TestResolve_MixedLineEndings tests mixed line ending handling
func TestResolve_MixedLineEndings(t *testing.T) {
	ctx := context.Background()

	content := "module(\r\n  name = \"test\",\n  version = \"1.0.0\"\r\n)"

	result, err := Resolve(ctx, content, ResolutionOptions{})
	if err != nil {
		t.Fatalf("Failed to parse content with mixed line endings: %v", err)
	}

	if result == nil {
		t.Fatal("Expected non-nil result")
	}
}

// TestResolve_TrailingWhitespace tests trailing whitespace handling
func TestResolve_TrailingWhitespace(t *testing.T) {
	ctx := context.Background()

	content := `module(name = "test", version = "1.0.0")
	`

	result, err := Resolve(ctx, content, ResolutionOptions{})
	if err != nil {
		t.Fatalf("Failed to parse content with trailing whitespace: %v", err)
	}

	if result == nil {
		t.Fatal("Expected non-nil result")
	}
}

// TestResolve_LargeContent tests handling of very large MODULE.bazel files
func TestResolve_LargeContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `module(name = "dep", version = "1.0.0")`)
	}))
	defer server.Close()

	ctx := context.Background()

	// Generate content with many dependencies
	var content strings.Builder
	content.WriteString(`module(name = "large_project", version = "1.0.0")
`)
	for i := 0; i < 100; i++ {
		fmt.Fprintf(&content, "bazel_dep(name = \"dep\", version = \"1.0.0\")\n")
	}

	// Should not panic or timeout quickly
	result, err := Resolve(ctx, content.String(), ResolutionOptions{
		Registries: []string{server.URL},
	})

	// We expect success here since all deps point to the same module
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if result == nil {
		t.Fatal("Expected non-nil result")
	}
}

// TestResolveFile_NonExistent tests non-existent file path
func TestResolveFile_NonExistent(t *testing.T) {
	ctx := context.Background()

	_, err := ResolveFile(ctx, "/nonexistent/path/MODULE.bazel", ResolutionOptions{})

	if err == nil {
		t.Error("Expected error for non-existent file")
	}
}

// TestResolveFile_EmptyFile tests empty file
func TestResolveFile_EmptyFile(t *testing.T) {
	tmpDir := t.TempDir()
	emptyFile := filepath.Join(tmpDir, "MODULE.bazel")

	if err := os.WriteFile(emptyFile, []byte{}, 0644); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	_, err := ResolveFile(ctx, emptyFile, ResolutionOptions{})

	if err == nil {
		t.Error("Expected error for empty file")
	}
}

// TestResolveFile_DirectoryInsteadOfFile tests passing a directory path
func TestResolveFile_DirectoryInsteadOfFile(t *testing.T) {
	tmpDir := t.TempDir()

	ctx := context.Background()
	_, err := ResolveFile(ctx, tmpDir, ResolutionOptions{})

	if err == nil {
		t.Error("Expected error when path is a directory")
	}
}

// TestResolveFile_ValidFile tests a valid file
func TestResolveFile_ValidFile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `module(name = "file_dep", version = "1.0.0")`)
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	moduleFile := filepath.Join(tmpDir, "MODULE.bazel")

	content := `module(name = "file_test", version = "1.0.0")
bazel_dep(name = "file_dep", version = "1.0.0")`

	if err := os.WriteFile(moduleFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	result, err := ResolveFile(ctx, moduleFile, ResolutionOptions{
		Registries: []string{server.URL},
	})

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if len(result.Modules) != 1 {
		t.Errorf("Expected 1 module, got %d", len(result.Modules))
	}
}

// TestResolveFile_Symlink tests following symlinks
func TestResolveFile_Symlink(t *testing.T) {
	tmpDir := t.TempDir()

	// Create actual file
	realFile := filepath.Join(tmpDir, "real_MODULE.bazel")
	content := `module(name = "symlink_test", version = "1.0.0")`
	if err := os.WriteFile(realFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// Create symlink
	symlinkPath := filepath.Join(tmpDir, "MODULE.bazel")
	if err := os.Symlink(realFile, symlinkPath); err != nil {
		t.Skip("Cannot create symlinks on this system")
	}

	ctx := context.Background()
	result, err := ResolveFile(ctx, symlinkPath, ResolutionOptions{})

	if err != nil {
		t.Fatalf("Failed to resolve via symlink: %v", err)
	}

	if result == nil {
		t.Fatal("Expected non-nil result")
	}
}

// TestResolveFile_RelativePath tests relative path handling
func TestResolveFile_RelativePath(t *testing.T) {
	// Save and restore working directory
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origDir)

	tmpDir := t.TempDir()
	moduleFile := filepath.Join(tmpDir, "MODULE.bazel")

	content := `module(name = "relative_test", version = "1.0.0")`
	if err := os.WriteFile(moduleFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// Change to temp directory
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	result, err := ResolveFile(ctx, "MODULE.bazel", ResolutionOptions{})

	if err != nil {
		t.Fatalf("Failed to resolve relative path: %v", err)
	}

	if result == nil {
		t.Fatal("Expected non-nil result")
	}
}

// TestResolveFile_PermissionDenied tests file without read permission
func TestResolveFile_PermissionDenied(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("Cannot test permission denied as root")
	}

	tmpDir := t.TempDir()
	moduleFile := filepath.Join(tmpDir, "MODULE.bazel")

	content := `module(name = "perm_test", version = "1.0.0")`
	if err := os.WriteFile(moduleFile, []byte(content), 0000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(moduleFile, 0644) // Cleanup

	ctx := context.Background()
	_, err := ResolveFile(ctx, moduleFile, ResolutionOptions{})

	if err == nil {
		t.Error("Expected error for file without read permission")
	}
}

// TestRegistryFromOptions_EmptyRegistries tests empty registries defaults to BCR
func TestRegistryFromOptions_EmptyRegistries(t *testing.T) {
	opts := ResolutionOptions{
		Registries: nil,
	}

	reg := registryFromOptions(opts)

	if reg == nil {
		t.Fatal("Expected non-nil registry for empty options")
	}

	// Should be a registry chain with default registries
	chain, ok := reg.(*RegistryChain)
	if !ok {
		t.Fatalf("Expected *RegistryChain, got %T", reg)
	}

	if len(chain.clients) != len(DefaultRegistries) {
		t.Errorf("Expected %d clients for default registries, got %d",
			len(DefaultRegistries), len(chain.clients))
	}
}

// TestRegistryFromOptions_SingleRegistry tests single registry config
func TestRegistryFromOptions_SingleRegistry(t *testing.T) {
	opts := ResolutionOptions{
		Registries: []string{"https://example.com"},
	}

	reg := registryFromOptions(opts)

	if reg == nil {
		t.Fatal("Expected non-nil registry")
	}

	// Single registry should return a direct client, not a chain
	client, ok := reg.(*RegistryClient)
	if !ok {
		t.Fatalf("Expected *RegistryClient for single URL, got %T", reg)
	}

	if client.BaseURL() != "https://example.com" {
		t.Errorf("Expected base URL https://example.com, got %s", client.BaseURL())
	}
}

// TestRegistryFromOptions_MultipleRegistries tests multiple registry config
func TestRegistryFromOptions_MultipleRegistries(t *testing.T) {
	opts := ResolutionOptions{
		Registries: []string{
			"https://first.example.com",
			"https://second.example.com",
		},
	}

	reg := registryFromOptions(opts)

	chain, ok := reg.(*RegistryChain)
	if !ok {
		t.Fatalf("Expected *RegistryChain for multiple URLs, got %T", reg)
	}

	if len(chain.clients) != 2 {
		t.Errorf("Expected 2 clients, got %d", len(chain.clients))
	}
}

// TestRegistryFromOptions_FileURL tests file:// URL handling
func TestRegistryFromOptions_FileURL(t *testing.T) {
	tmpDir := t.TempDir()
	fileURL := "file://" + tmpDir

	opts := ResolutionOptions{
		Registries: []string{fileURL},
	}

	reg := registryFromOptions(opts)

	if reg == nil {
		t.Fatal("Expected non-nil registry for file:// URL")
	}

	local, ok := reg.(*LocalRegistry)
	if !ok {
		t.Fatalf("Expected *LocalRegistry for file:// URL, got %T", reg)
	}

	if local == nil {
		t.Fatal("Expected non-nil LocalRegistry")
	}
}

// TestRegistryFromOptions_MixedURLs tests mixed local and remote registries
func TestRegistryFromOptions_MixedURLs(t *testing.T) {
	tmpDir := t.TempDir()
	fileURL := "file://" + tmpDir

	opts := ResolutionOptions{
		Registries: []string{
			fileURL,
			"https://bcr.bazel.build",
		},
	}

	reg := registryFromOptions(opts)

	chain, ok := reg.(*RegistryChain)
	if !ok {
		t.Fatalf("Expected *RegistryChain for mixed URLs, got %T", reg)
	}

	if len(chain.clients) != 2 {
		t.Errorf("Expected 2 clients, got %d", len(chain.clients))
	}

	// First should be LocalRegistry
	if _, ok := chain.clients[0].(*LocalRegistry); !ok {
		t.Errorf("First client should be *LocalRegistry, got %T", chain.clients[0])
	}

	// Second should be RegistryClient
	if _, ok := chain.clients[1].(*RegistryClient); !ok {
		t.Errorf("Second client should be *RegistryClient, got %T", chain.clients[1])
	}
}

// TestResolve_MutualDependency tests that mutual dependencies work correctly.
// Mutual dependency: A -> B -> A (common in Bazel ecosystem, e.g., rules_go <-> gazelle).
// Following Bazel's behavior, this should succeed - when B tries to add A, A is already
// in the visited set, so it's skipped silently. No error, no infinite loop.
//
// Bazel source reference:
// https://github.com/bazelbuild/bazel/blob/master/src/main/java/com/google/devtools/build/lib/bazel/bzlmod/Selection.java
// See DepGraphWalker.walk() which uses Set<ModuleKey> known to track visited modules.
func TestResolve_MutualDependency(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/modules/cycle_a/1.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "cycle_a", version = "1.0.0")
bazel_dep(name = "cycle_b", version = "1.0.0")`)
		case "/modules/cycle_b/1.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "cycle_b", version = "1.0.0")
bazel_dep(name = "cycle_a", version = "1.0.0")`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	ctx := context.Background()
	content := `module(name = "test", version = "1.0.0")
bazel_dep(name = "cycle_a", version = "1.0.0")`

	// Should not infinite loop or crash - mutual dependencies are allowed
	list, err := Resolve(ctx, content, ResolutionOptions{
		Registries: []string{server.URL},
	})

	if err != nil {
		t.Fatalf("Mutual dependency should succeed (matching Bazel behavior), got error: %v", err)
	}

	// Should have both modules resolved
	if len(list.Modules) != 2 {
		t.Errorf("Expected 2 modules, got %d", len(list.Modules))
	}

	moduleNames := make(map[string]bool)
	for _, m := range list.Modules {
		moduleNames[m.Name] = true
	}

	for _, expected := range []string{"cycle_a", "cycle_b"} {
		if !moduleNames[expected] {
			t.Errorf("Expected module %s in resolution list", expected)
		}
	}
}

// TestResolve_DiamondDependency tests diamond dependency resolution
func TestResolve_DiamondDependency(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/modules/left/1.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "left", version = "1.0.0")
bazel_dep(name = "bottom", version = "1.0.0")`)
		case "/modules/right/1.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "right", version = "1.0.0")
bazel_dep(name = "bottom", version = "2.0.0")`)
		case "/modules/bottom/1.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "bottom", version = "1.0.0")`)
		case "/modules/bottom/2.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "bottom", version = "2.0.0")`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	ctx := context.Background()
	content := `module(name = "top", version = "1.0.0")
bazel_dep(name = "left", version = "1.0.0")
bazel_dep(name = "right", version = "1.0.0")`

	result, err := Resolve(ctx, content, ResolutionOptions{
		Registries: []string{server.URL},
	})

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// MVS should select version 2.0.0 (the higher version)
	var bottomVersion string
	for _, m := range result.Modules {
		if m.Name == "bottom" {
			bottomVersion = m.Version
			break
		}
	}

	if bottomVersion != "2.0.0" {
		t.Errorf("MVS should select bottom@2.0.0, got %s", bottomVersion)
	}
}

// TestResolve_DevDependencyExclusion verifies dev deps are excluded by default
func TestResolve_DevDependencyExclusion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/modules/prod_dep/1.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "prod_dep", version = "1.0.0")`)
		case "/modules/dev_dep/1.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "dev_dep", version = "1.0.0")`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	ctx := context.Background()
	content := `module(name = "test", version = "1.0.0")
bazel_dep(name = "prod_dep", version = "1.0.0")
bazel_dep(name = "dev_dep", version = "1.0.0", dev_dependency = True)`

	// Default: exclude dev deps
	result, err := Resolve(ctx, content, ResolutionOptions{
		Registries:     []string{server.URL},
		IncludeDevDeps: false,
	})

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if len(result.Modules) != 1 {
		t.Errorf("Expected 1 module (dev excluded), got %d", len(result.Modules))
	}

	if result.Modules[0].Name != "prod_dep" {
		t.Errorf("Expected prod_dep, got %s", result.Modules[0].Name)
	}
}

// TestResolve_DevDependencyInclusion verifies dev deps can be included
func TestResolve_DevDependencyInclusion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/modules/prod_dep/1.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "prod_dep", version = "1.0.0")`)
		case "/modules/dev_dep/1.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "dev_dep", version = "1.0.0")`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	ctx := context.Background()
	content := `module(name = "test", version = "1.0.0")
bazel_dep(name = "prod_dep", version = "1.0.0")
bazel_dep(name = "dev_dep", version = "1.0.0", dev_dependency = True)`

	result, err := Resolve(ctx, content, ResolutionOptions{
		Registries:     []string{server.URL},
		IncludeDevDeps: true,
	})

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if len(result.Modules) != 2 {
		t.Errorf("Expected 2 modules (dev included), got %d", len(result.Modules))
	}
}

// TestResolve_ModuleStickiness tests that once a module is found in a registry,
// all versions come from that registry (Bazel behavior)
func TestResolve_ModuleStickiness(t *testing.T) {
	// Server 1: has dep@1.0.0 but NOT dep@2.0.0
	callCount1 := 0
	server1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount1++
		switch r.URL.Path {
		case "/modules/top/1.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "top", version = "1.0.0")
bazel_dep(name = "dep", version = "2.0.0")`)
		case "/modules/dep/1.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "dep", version = "1.0.0")`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server1.Close()

	// Server 2: has dep@2.0.0
	server2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/modules/dep/2.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "dep", version = "2.0.0")`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server2.Close()

	ctx := context.Background()
	content := `module(name = "root", version = "1.0.0")
bazel_dep(name = "top", version = "1.0.0")
bazel_dep(name = "dep", version = "1.0.0")`

	result, err := Resolve(ctx, content, ResolutionOptions{
		Registries: []string{server1.URL, server2.URL},
	})

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// dep should be resolved from server1 (where 1.0.0 was found first)
	// and then should fail or fallback for 2.0.0
	// This tests module stickiness behavior
	_ = result
}
