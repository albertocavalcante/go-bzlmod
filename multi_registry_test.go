package gobzlmod

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// setupMultiRegistryTest creates test servers and modules for multi-registry testing
func setupMultiRegistryTest() (*httptest.Server, *httptest.Server, func()) {
	// Registry 1: Primary registry with core modules
	registry1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		// core_module and its dependencies
		case strings.Contains(r.URL.Path, "/modules/core_module/1.0.0/MODULE.bazel"):
			fmt.Fprint(w, `module(name = "core_module", version = "1.0.0")
bazel_dep(name = "dep_a", version = "2.0.0")
bazel_dep(name = "dep_b", version = "1.5.0")`)

		case strings.Contains(r.URL.Path, "/modules/dep_a/2.0.0/MODULE.bazel"):
			fmt.Fprint(w, `module(name = "dep_a", version = "2.0.0")`)

		// Metadata
		case strings.Contains(r.URL.Path, "/modules/core_module/metadata.json"):
			fmt.Fprint(w, `{"versions": ["1.0.0"]}`)
		case strings.Contains(r.URL.Path, "/modules/dep_a/metadata.json"):
			fmt.Fprint(w, `{"versions": ["2.0.0"]}`)

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))

	// Registry 2: Fallback registry with additional modules
	registry2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		// dep_b only exists in registry 2
		case strings.Contains(r.URL.Path, "/modules/dep_b/1.5.0/MODULE.bazel"):
			fmt.Fprint(w, `module(name = "dep_b", version = "1.5.0")
bazel_dep(name = "dep_c", version = "3.0.0")`)

		case strings.Contains(r.URL.Path, "/modules/dep_c/3.0.0/MODULE.bazel"):
			fmt.Fprint(w, `module(name = "dep_c", version = "3.0.0")`)

		// Metadata
		case strings.Contains(r.URL.Path, "/modules/dep_b/metadata.json"):
			fmt.Fprint(w, `{"versions": ["1.5.0"]}`)
		case strings.Contains(r.URL.Path, "/modules/dep_c/metadata.json"):
			fmt.Fprint(w, `{"versions": ["3.0.0"]}`)

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))

	cleanup := func() {
		registry1.Close()
		registry2.Close()
	}

	return registry1, registry2, cleanup
}

func TestMultiRegistry_BasicResolution(t *testing.T) {
	reg1, reg2, cleanup := setupMultiRegistryTest()
	defer cleanup()

	// Create resolver with multiple registries
	opts := ResolutionOptions{
		IncludeDevDeps: false,
		Registries:     []string{reg1.URL, reg2.URL},
	}
	resolver := NewDependencyResolverWithOptions(nil, opts)

	rootModule := &ModuleInfo{
		Name:    "my_project",
		Version: "0.0.0",
		Dependencies: []Dependency{
			{Name: "core_module", Version: "1.0.0"},
		},
	}

	ctx := context.Background()
	result, err := resolver.ResolveDependencies(ctx, rootModule)
	if err != nil {
		t.Fatalf("ResolveDependencies() error = %v", err)
	}

	// Verify we got all modules
	expectedModules := map[string]bool{
		"core_module": false,
		"dep_a":       false,
		"dep_b":       false,
		"dep_c":       false,
	}

	for _, mod := range result.Modules {
		if _, ok := expectedModules[mod.Name]; ok {
			expectedModules[mod.Name] = true
		}
	}

	for name, found := range expectedModules {
		if !found {
			t.Errorf("Expected module %s not found in resolution", name)
		}
	}

	// Verify registry assignments
	for _, mod := range result.Modules {
		switch mod.Name {
		case "core_module", "dep_a":
			if mod.Registry != reg1.URL {
				t.Errorf("Module %s should be from registry1, got %s", mod.Name, mod.Registry)
			}
		case "dep_b", "dep_c":
			if mod.Registry != reg2.URL {
				t.Errorf("Module %s should be from registry2, got %s", mod.Name, mod.Registry)
			}
		}
	}
}

func TestMultiRegistry_ModuleStickiness(t *testing.T) {
	// Test that once a module is found in a registry, all versions come from that registry

	// Registry 1: Has module_x version 1.0.0 and 2.0.0
	registry1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/modules/module_x/1.0.0/MODULE.bazel"):
			fmt.Fprint(w, `module(name = "module_x", version = "1.0.0")`)
		case strings.Contains(r.URL.Path, "/modules/module_x/2.0.0/MODULE.bazel"):
			fmt.Fprint(w, `module(name = "module_x", version = "2.0.0")`)
		case strings.Contains(r.URL.Path, "/modules/module_x/metadata.json"):
			fmt.Fprint(w, `{"versions": ["1.0.0", "2.0.0"]}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer registry1.Close()

	// Registry 2: Also has module_x but shouldn't be used
	registry2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/modules/module_x/"):
			t.Error("Registry 2 should not be called for module_x")
			w.WriteHeader(http.StatusNotFound)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer registry2.Close()

	opts := ResolutionOptions{
		Registries: []string{registry1.URL, registry2.URL},
	}
	resolver := NewDependencyResolverWithOptions(nil, opts)

	rootModule := &ModuleInfo{
		Name:    "test",
		Version: "0.0.0",
		Dependencies: []Dependency{
			// Request version 1.0.0 first
			{Name: "module_x", Version: "1.0.0"},
		},
	}

	ctx := context.Background()
	result, err := resolver.ResolveDependencies(ctx, rootModule)
	if err != nil {
		t.Fatalf("ResolveDependencies() error = %v", err)
	}

	// Verify module_x came from registry1
	for _, mod := range result.Modules {
		if mod.Name == "module_x" && mod.Registry != registry1.URL {
			t.Errorf("module_x should be from registry1, got %s", mod.Registry)
		}
	}
}

func TestMultiRegistry_SingleRegistryBackwardsCompatibility(t *testing.T) {
	// Test that single registry still works
	reg1, reg2, cleanup := setupMultiRegistryTest()
	defer cleanup()

	// Use ResolutionOptions with single registry
	opts := ResolutionOptions{
		Registries: []string{reg1.URL},
	}
	resolver := NewDependencyResolverWithOptions(nil, opts)

	rootModule := &ModuleInfo{
		Name:    "test",
		Version: "0.0.0",
		Dependencies: []Dependency{
			{Name: "core_module", Version: "1.0.0"},
		},
	}

	ctx := context.Background()
	result, err := resolver.ResolveDependencies(ctx, rootModule)
	if err != nil {
		t.Fatalf("ResolveDependencies() error = %v", err)
	}

	// Should resolve core_module and dep_a from registry1
	foundCore := false
	foundDepA := false
	for _, mod := range result.Modules {
		if mod.Name == "core_module" {
			foundCore = true
			if mod.Registry != reg1.URL {
				t.Errorf("core_module should be from registry1, got %s", mod.Registry)
			}
		}
		if mod.Name == "dep_a" {
			foundDepA = true
		}
	}

	if !foundCore {
		t.Error("core_module not found in result")
	}
	if !foundDepA {
		t.Error("dep_a not found in result")
	}

	// dep_b should NOT be found since it's only in registry2
	for _, mod := range result.Modules {
		if mod.Name == "dep_b" {
			t.Error("dep_b should not be resolved (only in registry2)")
		}
	}

	// Using both registries should find dep_b
	opts2 := ResolutionOptions{
		Registries: []string{reg1.URL, reg2.URL},
	}
	resolver2 := NewDependencyResolverWithOptions(nil, opts2)

	result2, err := resolver2.ResolveDependencies(ctx, rootModule)
	if err != nil {
		t.Fatalf("ResolveDependencies() error = %v", err)
	}

	foundDepB := false
	for _, mod := range result2.Modules {
		if mod.Name == "dep_b" {
			foundDepB = true
			if mod.Registry != reg2.URL {
				t.Errorf("dep_b should be from registry2, got %s", mod.Registry)
			}
		}
	}

	if !foundDepB {
		t.Error("dep_b should be found when both registries are used")
	}
}

func TestMultiRegistry_NoRegistriesConfigured(t *testing.T) {
	// Test that providing no registries but using a RegistryClient still works
	registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/modules/test_mod/1.0.0/MODULE.bazel"):
			fmt.Fprint(w, `module(name = "test_mod", version = "1.0.0")`)
		case strings.Contains(r.URL.Path, "/modules/test_mod/metadata.json"):
			fmt.Fprint(w, `{"versions": ["1.0.0"]}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer registry.Close()

	// Use traditional constructor with explicit client
	client := NewRegistryClient(registry.URL)
	resolver := NewDependencyResolver(client, false)

	rootModule := &ModuleInfo{
		Name:    "root",
		Version: "0.0.0",
		Dependencies: []Dependency{
			{Name: "test_mod", Version: "1.0.0"},
		},
	}

	ctx := context.Background()
	result, err := resolver.ResolveDependencies(ctx, rootModule)
	if err != nil {
		t.Fatalf("ResolveDependencies() error = %v", err)
	}

	if len(result.Modules) != 1 {
		t.Errorf("Expected 1 module, got %d", len(result.Modules))
	}

	if result.Modules[0].Name != "test_mod" {
		t.Errorf("Expected test_mod, got %s", result.Modules[0].Name)
	}
}

func TestMultiRegistry_RegistryOverride(t *testing.T) {
	// Test that registry overrides in MODULE.bazel work with multi-registry
	reg1, reg2, cleanup := setupMultiRegistryTest()
	defer cleanup()

	// Registry 3 for overridden module
	registry3 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/modules/override_mod/5.0.0/MODULE.bazel"):
			fmt.Fprint(w, `module(name = "override_mod", version = "5.0.0")`)
		case strings.Contains(r.URL.Path, "/modules/override_mod/metadata.json"):
			fmt.Fprint(w, `{"versions": ["5.0.0"]}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer registry3.Close()

	opts := ResolutionOptions{
		Registries: []string{reg1.URL, reg2.URL},
	}
	resolver := NewDependencyResolverWithOptions(nil, opts)

	rootModule := &ModuleInfo{
		Name:    "root",
		Version: "0.0.0",
		Dependencies: []Dependency{
			{Name: "override_mod", Version: "5.0.0"},
		},
		Overrides: []Override{
			{
				Type:       "single_version",
				ModuleName: "override_mod",
				Version:    "5.0.0",
				Registry:   registry3.URL,
			},
		},
	}

	ctx := context.Background()
	result, err := resolver.ResolveDependencies(ctx, rootModule)
	if err != nil {
		t.Fatalf("ResolveDependencies() error = %v", err)
	}

	// Find override_mod in results
	found := false
	for _, mod := range result.Modules {
		if mod.Name == "override_mod" {
			found = true
			if mod.Registry != registry3.URL {
				t.Errorf("override_mod should use registry3 from override, got %s", mod.Registry)
			}
		}
	}

	if !found {
		t.Error("override_mod not found in resolution")
	}
}

func TestMultiRegistry_MVSAcrossRegistries(t *testing.T) {
	// Test that MVS works correctly when different versions come from different registries
	// This shouldn't happen in practice (module stickiness), but test the edge case

	registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/modules/mvs_test/1.0.0/MODULE.bazel"):
			fmt.Fprint(w, `module(name = "mvs_test", version = "1.0.0")`)
		case strings.Contains(r.URL.Path, "/modules/mvs_test/2.0.0/MODULE.bazel"):
			fmt.Fprint(w, `module(name = "mvs_test", version = "2.0.0")`)
		case strings.Contains(r.URL.Path, "/modules/dep_x/1.0.0/MODULE.bazel"):
			fmt.Fprint(w, `module(name = "dep_x", version = "1.0.0")
bazel_dep(name = "mvs_test", version = "1.0.0")`)
		case strings.Contains(r.URL.Path, "/modules/dep_y/1.0.0/MODULE.bazel"):
			fmt.Fprint(w, `module(name = "dep_y", version = "1.0.0")
bazel_dep(name = "mvs_test", version = "2.0.0")`)
		case strings.Contains(r.URL.Path, "/modules/mvs_test/metadata.json"):
			fmt.Fprint(w, `{"versions": ["1.0.0", "2.0.0"]}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer registry.Close()

	opts := ResolutionOptions{
		Registries: []string{registry.URL},
	}
	resolver := NewDependencyResolverWithOptions(nil, opts)

	rootModule := &ModuleInfo{
		Name:    "root",
		Version: "0.0.0",
		Dependencies: []Dependency{
			{Name: "dep_x", Version: "1.0.0"},
			{Name: "dep_y", Version: "1.0.0"},
		},
	}

	ctx := context.Background()
	result, err := resolver.ResolveDependencies(ctx, rootModule)
	if err != nil {
		t.Fatalf("ResolveDependencies() error = %v", err)
	}

	// MVS should select version 2.0.0 of mvs_test
	for _, mod := range result.Modules {
		if mod.Name == "mvs_test" {
			if mod.Version != "2.0.0" {
				t.Errorf("MVS should select 2.0.0, got %s", mod.Version)
			}
		}
	}
}
