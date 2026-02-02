package gobzlmod

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/albertocavalcante/go-bzlmod/selection/version"
)

// Mock registry server for testing
func createMockRegistryServer() *httptest.Server {
	mux := http.NewServeMux()

	// Mock responses for different modules
	mux.HandleFunc("/modules/test_module/", func(w http.ResponseWriter, r *http.Request) {
		version := r.URL.Path[len("/modules/test_module/"):]
		switch version {
		case "1.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "test_module", version = "1.0.0")
			bazel_dep(name = "dependency_a", version = "1.0.0")`)
		case "1.1.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "test_module", version = "1.1.0")
			bazel_dep(name = "dependency_a", version = "1.1.0")`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	mux.HandleFunc("/modules/dependency_a/", func(w http.ResponseWriter, r *http.Request) {
		version := r.URL.Path[len("/modules/dependency_a/"):]
		switch version {
		case "1.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "dependency_a", version = "1.0.0")
			bazel_dep(name = "dependency_b", version = "2.0.0")`)
		case "1.1.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "dependency_a", version = "1.1.0")
			bazel_dep(name = "dependency_b", version = "2.1.0")`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	mux.HandleFunc("/modules/dependency_b/", func(w http.ResponseWriter, r *http.Request) {
		version := r.URL.Path[len("/modules/dependency_b/"):]
		switch version {
		case "2.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "dependency_b", version = "2.0.0")`)
		case "2.1.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "dependency_b", version = "2.1.0")`)
		case "2.2.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "dependency_b", version = "2.2.0")`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	return httptest.NewServer(mux)
}

func Test_newDependencyResolver(t *testing.T) {
	registry := newRegistryClient("https://bcr.bazel.build")

	tests := []struct {
		name           string
		includeDevDeps bool
	}{
		{
			name:           "with dev dependencies",
			includeDevDeps: true,
		},
		{
			name:           "without dev dependencies",
			includeDevDeps: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver := newDependencyResolver(registry, tt.includeDevDeps)

			if resolver == nil {
				t.Fatal("newDependencyResolver() returned nil")
			}

			if resolver.registry != registry {
				t.Error("Registry not set correctly")
			}

			if resolver.options.IncludeDevDeps != tt.includeDevDeps {
				t.Errorf("options.IncludeDevDeps = %v, want %v", resolver.options.IncludeDevDeps, tt.includeDevDeps)
			}
		})
	}
}

func TestApplyMVS(t *testing.T) {
	registry := newRegistryClient("https://bcr.bazel.build")
	resolver := newDependencyResolver(registry, false)

	tests := []struct {
		name     string
		depGraph map[string]map[string]*depRequest
		want     map[string]*depRequest
	}{
		{
			name: "single module single version",
			depGraph: map[string]map[string]*depRequest{
				"module_a": {
					"1.0.0": &depRequest{
						Version:    "1.0.0",
						RequiredBy: []string{"<root>"},
					},
				},
			},
			want: map[string]*depRequest{
				"module_a": {
					Version:    "1.0.0",
					RequiredBy: []string{"<root>"},
				},
			},
		},
		{
			name: "single module multiple versions - MVS selects highest",
			depGraph: map[string]map[string]*depRequest{
				"module_a": {
					"1.0.0": &depRequest{
						Version:    "1.0.0",
						RequiredBy: []string{"dependency_b"},
					},
					"1.2.0": &depRequest{
						Version:    "1.2.0",
						RequiredBy: []string{"dependency_c"},
					},
					"1.1.0": &depRequest{
						Version:    "1.1.0",
						RequiredBy: []string{"dependency_d"},
					},
				},
			},
			want: map[string]*depRequest{
				"module_a": {
					Version:    "1.2.0",
					RequiredBy: []string{"dependency_c"},
				},
			},
		},
		{
			name: "bcr versions select highest",
			depGraph: map[string]map[string]*depRequest{
				"module_a": {
					"1.2.3.bcr.2": &depRequest{
						Version:    "1.2.3.bcr.2",
						RequiredBy: []string{"dependency_b"},
					},
					"1.2.3.bcr.10": &depRequest{
						Version:    "1.2.3.bcr.10",
						RequiredBy: []string{"dependency_c"},
					},
				},
			},
			want: map[string]*depRequest{
				"module_a": {
					Version:    "1.2.3.bcr.10",
					RequiredBy: []string{"dependency_c"},
				},
			},
		},
		{
			name: "multiple modules",
			depGraph: map[string]map[string]*depRequest{
				"module_a": {
					"1.0.0": &depRequest{
						Version:    "1.0.0",
						RequiredBy: []string{"<root>"},
					},
				},
				"module_b": {
					"2.1.0": &depRequest{
						Version:    "2.1.0",
						RequiredBy: []string{"module_a"},
					},
					"2.0.0": &depRequest{
						Version:    "2.0.0",
						RequiredBy: []string{"<root>"},
					},
				},
			},
			want: map[string]*depRequest{
				"module_a": {
					Version:    "1.0.0",
					RequiredBy: []string{"<root>"},
				},
				"module_b": {
					Version:    "2.1.0",
					RequiredBy: []string{"module_a"},
				},
			},
		},
		{
			name:     "empty dependency graph",
			depGraph: map[string]map[string]*depRequest{},
			want:     map[string]*depRequest{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolver.applyMVS(tt.depGraph)

			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("applyMVS() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestApplyOverrides(t *testing.T) {
	registry := newRegistryClient("https://bcr.bazel.build")
	resolver := newDependencyResolver(registry, false)

	tests := []struct {
		name      string
		depGraph  map[string]map[string]*depRequest
		overrides []Override
		want      map[string]map[string]*depRequest
	}{
		{
			name: "single_version override",
			depGraph: map[string]map[string]*depRequest{
				"module_a": {
					"1.0.0": &depRequest{Version: "1.0.0", RequiredBy: []string{"<root>"}},
					"1.1.0": &depRequest{Version: "1.1.0", RequiredBy: []string{"dependency_b"}},
				},
			},
			overrides: []Override{
				{
					Type:       "single_version",
					ModuleName: "module_a",
					Version:    "1.2.0",
				},
			},
			want: map[string]map[string]*depRequest{
				"module_a": {
					"1.2.0": &depRequest{
						Version:       "1.2.0",
						DevDependency: false,
						RequiredBy:    []string{"<override>"},
					},
				},
			},
		},
		{
			name: "git override keeps module",
			depGraph: map[string]map[string]*depRequest{
				"module_a": {
					"1.0.0": &depRequest{Version: "1.0.0", RequiredBy: []string{"<root>"}},
				},
				"module_b": {
					"2.0.0": &depRequest{Version: "2.0.0", RequiredBy: []string{"module_a"}},
				},
			},
			overrides: []Override{
				{
					Type:       "git",
					ModuleName: "module_a",
				},
			},
			want: map[string]map[string]*depRequest{
				"module_a": {
					"1.0.0": &depRequest{Version: "1.0.0", RequiredBy: []string{"<root>"}},
				},
				"module_b": {
					"2.0.0": &depRequest{Version: "2.0.0", RequiredBy: []string{"module_a"}},
				},
			},
		},
		{
			name: "local_path override keeps module",
			depGraph: map[string]map[string]*depRequest{
				"module_a": {
					"1.0.0": &depRequest{Version: "1.0.0", RequiredBy: []string{"<root>"}},
				},
			},
			overrides: []Override{
				{
					Type:       "local_path",
					ModuleName: "module_a",
				},
			},
			want: map[string]map[string]*depRequest{
				"module_a": {
					"1.0.0": &depRequest{Version: "1.0.0", RequiredBy: []string{"<root>"}},
				},
			},
		},
		{
			name: "archive override keeps module",
			depGraph: map[string]map[string]*depRequest{
				"module_a": {
					"1.0.0": &depRequest{Version: "1.0.0", RequiredBy: []string{"<root>"}},
				},
			},
			overrides: []Override{
				{
					Type:       "archive",
					ModuleName: "module_a",
				},
			},
			want: map[string]map[string]*depRequest{
				"module_a": {
					"1.0.0": &depRequest{Version: "1.0.0", RequiredBy: []string{"<root>"}},
				},
			},
		},
		{
			name: "override nonexistent module",
			depGraph: map[string]map[string]*depRequest{
				"module_a": {
					"1.0.0": &depRequest{Version: "1.0.0", RequiredBy: []string{"<root>"}},
				},
			},
			overrides: []Override{
				{
					Type:       "single_version",
					ModuleName: "nonexistent",
					Version:    "1.0.0",
				},
			},
			want: map[string]map[string]*depRequest{
				"module_a": {
					"1.0.0": &depRequest{Version: "1.0.0", RequiredBy: []string{"<root>"}},
				},
				"nonexistent": {
					"1.0.0": &depRequest{
						Version:       "1.0.0",
						DevDependency: false,
						RequiredBy:    []string{"<override>"},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver.applyOverrides(tt.depGraph, tt.overrides)

			if !reflect.DeepEqual(tt.depGraph, tt.want) {
				t.Errorf("applyOverrides() resulted in %v, want %v", tt.depGraph, tt.want)
			}
		})
	}
}

func TestBuildResolutionList(t *testing.T) {
	registry := newRegistryClient("https://bcr.bazel.build")
	resolver := newDependencyResolver(registry, false)

	rootModule := &ModuleInfo{
		Name:    "test_project",
		Version: "1.0.0",
		Overrides: []Override{
			{
				Type:       "single_version",
				ModuleName: "custom_module",
				Registry:   "https://custom.registry.com",
			},
		},
	}

	selectedVersions := map[string]*depRequest{
		"module_a": {
			Version:       "1.0.0",
			DevDependency: false,
			RequiredBy:    []string{"<root>"},
		},
		"module_b": {
			Version:       "2.1.0",
			DevDependency: true,
			RequiredBy:    []string{"module_a"},
		},
		"custom_module": {
			Version:       "1.5.0",
			DevDependency: false,
			RequiredBy:    []string{"<override>"},
		},
	}

	moduleDeps := make(map[string][]string) // Empty for this test
	list, err := resolver.buildResolutionList(context.Background(), selectedVersions, moduleDeps, rootModule)
	if err != nil {
		t.Fatalf("buildResolutionList() error = %v", err)
	}

	// Check total number of modules
	if len(list.Modules) != 3 {
		t.Errorf("Expected 3 modules, got %d", len(list.Modules))
	}

	// Check modules are sorted by name
	expectedOrder := []string{"custom_module", "module_a", "module_b"}
	for i, module := range list.Modules {
		if module.Name != expectedOrder[i] {
			t.Errorf("Module %d: expected %s, got %s", i, expectedOrder[i], module.Name)
		}
	}

	// Check custom registry override
	var customModule *ModuleToResolve
	for i := range list.Modules {
		if list.Modules[i].Name == "custom_module" {
			customModule = &list.Modules[i]
			break
		}
	}
	if customModule == nil {
		t.Fatal("custom_module not found in resolution list")
	}
	if customModule.Registry != "https://custom.registry.com" {
		t.Errorf("Expected custom registry, got %s", customModule.Registry)
	}

	// Check summary
	if list.Summary.TotalModules != 3 {
		t.Errorf("Summary.TotalModules = %d, want 3", list.Summary.TotalModules)
	}
	if list.Summary.ProductionModules != 2 {
		t.Errorf("Summary.ProductionModules = %d, want 2", list.Summary.ProductionModules)
	}
	if list.Summary.DevModules != 1 {
		t.Errorf("Summary.DevModules = %d, want 1", list.Summary.DevModules)
	}
}

func TestResolveDependencies_Integration(t *testing.T) {
	// Skip integration test in short mode
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Create mock server
	server := createMockRegistryServer()
	defer server.Close()

	// Create registry and resolver
	registry := newRegistryClient(server.URL)
	resolver := newDependencyResolver(registry, false)

	// Create root module
	rootModule := &ModuleInfo{
		Name:    "root_module",
		Version: "1.0.0",
		Dependencies: []Dependency{
			{Name: "test_module", Version: "1.0.0"},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	list, err := resolver.ResolveDependencies(ctx, rootModule)
	if err != nil {
		t.Fatalf("ResolveDependencies() error = %v", err)
	}

	// Verify the resolved dependencies
	if len(list.Modules) < 3 {
		t.Errorf("Expected at least 3 modules (test_module, dependency_a, dependency_b), got %d", len(list.Modules))
	}

	// Check that MVS worked correctly
	moduleVersions := make(map[string]string)
	for _, module := range list.Modules {
		moduleVersions[module.Name] = module.Version
	}

	// dependency_b should be at version 2.0.0 (required by dependency_a 1.0.0)
	if version, exists := moduleVersions["dependency_b"]; exists {
		if version != "2.0.0" {
			t.Errorf("Expected dependency_b version 2.0.0, got %s", version)
		}
	}
}

func TestVersionComparison(t *testing.T) {
	tests := []struct {
		name     string
		version1 string
		version2 string
		want     int // -1 if v1 < v2, 0 if v1 == v2, 1 if v1 > v2
	}{
		{"equal versions", "1.0.0", "1.0.0", 0},
		{"v1 greater major", "2.0.0", "1.0.0", 1},
		{"v1 greater minor", "1.1.0", "1.0.0", 1},
		{"v1 greater patch", "1.0.1", "1.0.0", 1},
		{"v2 greater", "1.0.0", "1.1.0", -1},
		{"complex versions", "1.2.3", "1.2.4", -1},
		{"bcr suffix versions", "1.2.3.bcr.2", "1.2.3.bcr.10", -1},
		{"prerelease vs release", "1.2.3-rc1", "1.2.3", -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := version.Compare(tt.version1, tt.version2)
			if got != tt.want {
				t.Errorf("version.Compare(%s, %s) = %d, want %d", tt.version1, tt.version2, got, tt.want)
			}
		})
	}
}

func TestResolveDependencies_DevDependencies(t *testing.T) {
	// Create mock server
	server := createMockRegistryServer()
	defer server.Close()

	tests := []struct {
		name           string
		includeDevDeps bool
		rootModule     *ModuleInfo
		wantModules    int
	}{
		{
			name:           "exclude dev dependencies",
			includeDevDeps: false,
			rootModule: &ModuleInfo{
				Name:    "root",
				Version: "1.0.0",
				Dependencies: []Dependency{
					{Name: "test_module", Version: "1.0.0", DevDependency: false},
					{Name: "dev_module", Version: "1.0.0", DevDependency: true},
				},
			},
			wantModules: 3, // test_module + transitive dependencies (dependency_a, dependency_b)
		},
		{
			name:           "include dev dependencies",
			includeDevDeps: true,
			rootModule: &ModuleInfo{
				Name:    "root",
				Version: "1.0.0",
				Dependencies: []Dependency{
					{Name: "test_module", Version: "1.0.0", DevDependency: false},
					{Name: "dev_module", Version: "1.0.0", DevDependency: true},
				},
			},
			wantModules: 3, // test_module + transitive dependencies (dev_module fails 404, so still 3)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := newRegistryClient(server.URL)
			resolver := newDependencyResolverWithOptions(registry, ResolutionOptions{
				IncludeDevDeps: tt.includeDevDeps,
			})

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()

			result, err := resolver.ResolveDependencies(ctx, tt.rootModule)
			if err != nil {
				t.Fatalf("ResolveDependencies() error = %v", err)
			}

			// Count the modules in the result (excluding root)
			if result.Summary.TotalModules != tt.wantModules {
				t.Errorf("Expected %d modules, got %d", tt.wantModules, result.Summary.TotalModules)
			}
		})
	}
}

func TestResolveDependencies_SingleVersionOverrideHydratesTransitiveDeps(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/modules/foo/1.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "foo", version = "1.0.0")
			bazel_dep(name = "bar", version = "1.0.0")`)
		case "/modules/foo/2.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "foo", version = "2.0.0")
			bazel_dep(name = "bar", version = "2.0.0")`)
		case "/modules/bar/1.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "bar", version = "1.0.0")`)
		case "/modules/bar/2.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "bar", version = "2.0.0")`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	registry := newRegistryClient(server.URL)
	resolver := newDependencyResolver(registry, false)

	rootModule := &ModuleInfo{
		Name:    "root",
		Version: "1.0.0",
		Dependencies: []Dependency{
			{Name: "foo", Version: "1.0.0"},
		},
		Overrides: []Override{
			{
				Type:       "single_version",
				ModuleName: "foo",
				Version:    "2.0.0",
			},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	list, err := resolver.ResolveDependencies(ctx, rootModule)
	if err != nil {
		t.Fatalf("ResolveDependencies() error = %v", err)
	}

	versions := make(map[string]string)
	for _, mod := range list.Modules {
		versions[mod.Name] = mod.Version
	}

	if got := versions["foo"]; got != "2.0.0" {
		t.Fatalf("Expected foo version 2.0.0, got %q", got)
	}
	if got := versions["bar"]; got != "2.0.0" {
		t.Fatalf("Expected bar version 2.0.0 from override module, got %q", got)
	}
}

func TestResolveDependencies_GitOverrideKeepsModuleWithoutRegistryFetch(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()

	registry := newRegistryClient(server.URL)
	resolver := newDependencyResolver(registry, false)

	rootModule := &ModuleInfo{
		Name:    "root",
		Version: "1.0.0",
		Dependencies: []Dependency{
			{Name: "local_mod", Version: "1.0.0"},
		},
		Overrides: []Override{
			{
				Type:       "git",
				ModuleName: "local_mod",
			},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	list, err := resolver.ResolveDependencies(ctx, rootModule)
	if err != nil {
		t.Fatalf("ResolveDependencies() error = %v", err)
	}

	found := false
	for _, mod := range list.Modules {
		if mod.Name == "local_mod" {
			found = true
			if mod.Version != "1.0.0" {
				t.Fatalf("Expected local_mod version 1.0.0, got %q", mod.Version)
			}
			break
		}
	}
	if !found {
		t.Fatal("Expected local_mod to remain in the resolution list")
	}
}

func TestResolveDependencies_GitOverrideHydratesProvidedModule(t *testing.T) {
	var fetchedLocal atomic.Bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/modules/dep/1.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "dep", version = "1.0.0")`)
		default:
			if strings.Contains(r.URL.Path, "/modules/local_mod/") {
				fetchedLocal.Store(true)
			}
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	registry := newRegistryClient(server.URL)
	resolver := newDependencyResolver(registry, false)

	overrideContent := `module(name = "local_mod", version = "1.0.0")
	bazel_dep(name = "dep", version = "1.0.0")`
	if err := resolver.AddOverrideModuleContent("local_mod", overrideContent); err != nil {
		t.Fatalf("AddOverrideModuleContent() error = %v", err)
	}

	rootModule := &ModuleInfo{
		Name:    "root",
		Version: "1.0.0",
		Dependencies: []Dependency{
			{Name: "local_mod", Version: "1.0.0"},
		},
		Overrides: []Override{
			{
				Type:       "git",
				ModuleName: "local_mod",
			},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	list, err := resolver.ResolveDependencies(ctx, rootModule)
	if err != nil {
		t.Fatalf("ResolveDependencies() error = %v", err)
	}

	versions := make(map[string]string)
	for _, mod := range list.Modules {
		versions[mod.Name] = mod.Version
	}

	if got := versions["local_mod"]; got != "1.0.0" {
		t.Fatalf("Expected local_mod version 1.0.0, got %q", got)
	}
	if got := versions["dep"]; got != "1.0.0" {
		t.Fatalf("Expected dep version 1.0.0, got %q", got)
	}
	if fetchedLocal.Load() {
		t.Fatal("Expected local_mod to be hydrated from override content without registry fetch")
	}
}

// TestDirectDepsMode_Warn tests that DirectDepsWarn adds warnings for mismatches.
func TestDirectDepsMode_Warn(t *testing.T) {
	registry := newRegistryClient("https://bcr.bazel.build")
	opts := ResolutionOptions{
		IncludeDevDeps: false,
		DirectDepsMode: DirectDepsWarn,
	}
	resolver := newDependencyResolverWithOptions(registry, opts)

	// Root declares dep_a@1.0.0, but transitive deps will bump it higher
	rootModule := &ModuleInfo{
		Name:    "test",
		Version: "1.0.0",
		Dependencies: []Dependency{
			{Name: "dep_a", Version: "1.0.0"},
		},
	}

	// Simulate: dep_a@1.0.0 requested, but dep_a@2.0.0 is selected (via MVS)
	selectedVersions := map[string]*depRequest{
		"dep_a": {Version: "2.0.0", RequiredBy: []string{"other_module"}},
	}

	mismatches := resolver.checkDirectDeps(rootModule, selectedVersions)
	if len(mismatches) != 1 {
		t.Fatalf("Expected 1 mismatch, got %d", len(mismatches))
	}

	m := mismatches[0]
	if m.Name != "dep_a" {
		t.Errorf("Expected mismatch for dep_a, got %s", m.Name)
	}
	if m.DeclaredVersion != "1.0.0" {
		t.Errorf("Expected declared version 1.0.0, got %s", m.DeclaredVersion)
	}
	if m.ResolvedVersion != "2.0.0" {
		t.Errorf("Expected resolved version 2.0.0, got %s", m.ResolvedVersion)
	}
}

// TestDirectDepsMode_NoMismatch tests that matching versions produce no warnings.
func TestDirectDepsMode_NoMismatch(t *testing.T) {
	registry := newRegistryClient("https://bcr.bazel.build")
	opts := ResolutionOptions{
		IncludeDevDeps: false,
		DirectDepsMode: DirectDepsWarn,
	}
	resolver := newDependencyResolverWithOptions(registry, opts)

	rootModule := &ModuleInfo{
		Name:    "test",
		Version: "1.0.0",
		Dependencies: []Dependency{
			{Name: "dep_a", Version: "1.0.0"},
		},
	}

	// Selected version matches declared
	selectedVersions := map[string]*depRequest{
		"dep_a": {Version: "1.0.0", RequiredBy: []string{"<root>"}},
	}

	mismatches := resolver.checkDirectDeps(rootModule, selectedVersions)
	if len(mismatches) != 0 {
		t.Errorf("Expected no mismatches, got %d", len(mismatches))
	}
}

// TestBuildDependencyGraph_MutualDependency tests that mutual dependencies work correctly.
// Mutual dependency: A -> B -> A (common in Bazel ecosystem, e.g., rules_go <-> gazelle).
// Following Bazel's behavior, this should succeed - when B tries to add A, A is already
// in the visited set, so it's skipped silently. No error, no infinite loop.
//
// Bazel source reference:
// https://github.com/bazelbuild/bazel/blob/master/src/main/java/com/google/devtools/build/lib/bazel/bzlmod/Selection.java
// See DepGraphWalker.walk() which uses Set<ModuleKey> known to track visited modules.
func TestBuildDependencyGraph_MutualDependency(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/modules/module_a/1.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "module_a", version = "1.0.0")
			bazel_dep(name = "module_b", version = "1.0.0")`)
		case "/modules/module_b/1.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "module_b", version = "1.0.0")
			bazel_dep(name = "module_a", version = "1.0.0")`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	registry := newRegistryClient(server.URL)
	resolver := newDependencyResolver(registry, false)

	rootModule := &ModuleInfo{
		Name:    "root",
		Version: "1.0.0",
		Dependencies: []Dependency{
			{Name: "module_a", Version: "1.0.0"},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	list, err := resolver.ResolveDependencies(ctx, rootModule)
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

	for _, expected := range []string{"module_a", "module_b"} {
		if !moduleNames[expected] {
			t.Errorf("Expected module %s in resolution list", expected)
		}
	}
}

// TestBuildDependencyGraph_DiamondDependency tests that diamond dependencies work correctly.
// Diamond: root -> A, root -> B, A -> C, B -> C (not a cycle)
func TestBuildDependencyGraph_DiamondDependency(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/modules/module_a/1.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "module_a", version = "1.0.0")
			bazel_dep(name = "module_c", version = "1.0.0")`)
		case "/modules/module_b/1.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "module_b", version = "1.0.0")
			bazel_dep(name = "module_c", version = "1.0.0")`)
		case "/modules/module_c/1.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "module_c", version = "1.0.0")`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	registry := newRegistryClient(server.URL)
	resolver := newDependencyResolver(registry, false)

	rootModule := &ModuleInfo{
		Name:    "root",
		Version: "1.0.0",
		Dependencies: []Dependency{
			{Name: "module_a", Version: "1.0.0"},
			{Name: "module_b", Version: "1.0.0"},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	list, err := resolver.ResolveDependencies(ctx, rootModule)
	if err != nil {
		t.Fatalf("Diamond dependency should not be an error, got: %v", err)
	}

	// Should have all 3 modules resolved
	if len(list.Modules) != 3 {
		t.Errorf("Expected 3 modules in diamond pattern, got %d", len(list.Modules))
	}

	moduleNames := make(map[string]bool)
	for _, m := range list.Modules {
		moduleNames[m.Name] = true
	}

	for _, expected := range []string{"module_a", "module_b", "module_c"} {
		if !moduleNames[expected] {
			t.Errorf("Expected module %s in resolution list", expected)
		}
	}
}

// TestBuildDependencyGraph_DeepChain tests that deep but valid chains work.
func TestBuildDependencyGraph_DeepChain(t *testing.T) {
	const chainDepth = 50

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Create a chain: module_0 -> module_1 -> ... -> module_49
		for i := 0; i < chainDepth; i++ {
			moduleName := fmt.Sprintf("module_%d", i)
			path := fmt.Sprintf("/modules/%s/1.0.0/MODULE.bazel", moduleName)
			if r.URL.Path == path {
				if i < chainDepth-1 {
					nextModule := fmt.Sprintf("module_%d", i+1)
					fmt.Fprintf(w, `module(name = "%s", version = "1.0.0")
					bazel_dep(name = "%s", version = "1.0.0")`, moduleName, nextModule)
				} else {
					// Last module has no dependencies
					fmt.Fprintf(w, `module(name = "%s", version = "1.0.0")`, moduleName)
				}
				return
			}
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	registry := newRegistryClient(server.URL)
	resolver := newDependencyResolver(registry, false)

	rootModule := &ModuleInfo{
		Name:    "root",
		Version: "1.0.0",
		Dependencies: []Dependency{
			{Name: "module_0", Version: "1.0.0"},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	list, err := resolver.ResolveDependencies(ctx, rootModule)
	if err != nil {
		t.Fatalf("Deep chain should not be an error, got: %v", err)
	}

	// Should have all modules in the chain
	if len(list.Modules) != chainDepth {
		t.Errorf("Expected %d modules in chain, got %d", chainDepth, len(list.Modules))
	}
}

// TestBuildDependencyGraph_MaxDepthExceeded tests that very deep chains are rejected.
func TestBuildDependencyGraph_MaxDepthExceeded(t *testing.T) {
	const chainDepth = 1100 // Exceeds maxDependencyDepth (1000)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Create a very deep chain
		for i := 0; i < chainDepth; i++ {
			moduleName := fmt.Sprintf("module_%d", i)
			path := fmt.Sprintf("/modules/%s/1.0.0/MODULE.bazel", moduleName)
			if r.URL.Path == path {
				if i < chainDepth-1 {
					nextModule := fmt.Sprintf("module_%d", i+1)
					fmt.Fprintf(w, `module(name = "%s", version = "1.0.0")
					bazel_dep(name = "%s", version = "1.0.0")`, moduleName, nextModule)
				} else {
					fmt.Fprintf(w, `module(name = "%s", version = "1.0.0")`, moduleName)
				}
				return
			}
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	registry := newRegistryClient(server.URL)
	resolver := newDependencyResolver(registry, false)

	rootModule := &ModuleInfo{
		Name:    "root",
		Version: "1.0.0",
		Dependencies: []Dependency{
			{Name: "module_0", Version: "1.0.0"},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_, err := resolver.ResolveDependencies(ctx, rootModule)
	if err == nil {
		t.Fatal("Expected max depth error, got nil")
	}

	var depthErr *MaxDepthExceededError
	if !errors.As(err, &depthErr) {
		t.Fatalf("Expected MaxDepthExceededError, got %T: %v", err, err)
	}

	errMsg := err.Error()
	if !strings.Contains(errMsg, "maximum dependency depth") {
		t.Errorf("Error message should contain 'maximum dependency depth', got: %s", errMsg)
	}
}

// TestBuildDependencyGraph_SelfReference tests module depending on itself.
// Following Bazel's behavior, this should succeed - when module_a tries to add
// module_a@1.0.0 as a dependency, it's already in the visited set, so it's skipped.
//
// Bazel source reference:
// https://github.com/bazelbuild/bazel/blob/master/src/main/java/com/google/devtools/build/lib/bazel/bzlmod/Selection.java
// See DepGraphWalker.walk() which uses Set<ModuleKey> known to track visited modules.
func TestBuildDependencyGraph_SelfReference(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/modules/module_a/1.0.0/MODULE.bazel":
			// Module depends on itself
			fmt.Fprint(w, `module(name = "module_a", version = "1.0.0")
			bazel_dep(name = "module_a", version = "1.0.0")`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	registry := newRegistryClient(server.URL)
	resolver := newDependencyResolver(registry, false)

	rootModule := &ModuleInfo{
		Name:    "root",
		Version: "1.0.0",
		Dependencies: []Dependency{
			{Name: "module_a", Version: "1.0.0"},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	list, err := resolver.ResolveDependencies(ctx, rootModule)
	if err != nil {
		t.Fatalf("Self-reference should succeed (matching Bazel behavior), got error: %v", err)
	}

	// Should have module_a resolved exactly once
	if len(list.Modules) != 1 {
		t.Errorf("Expected 1 module, got %d", len(list.Modules))
	}

	if list.Modules[0].Name != "module_a" {
		t.Errorf("Expected module_a, got %s", list.Modules[0].Name)
	}
}

// TestBuildDependencyGraph_LongerMutualDependency tests a longer mutual dependency chain: A -> B -> C -> A
// Following Bazel's behavior, this should succeed. Bazel uses a BFS with a global "visited" set
// (called "known" in Selection.java). When C tries to add A as a dependency, A is already in
// the visited set from the initial traversal, so it's skipped silently.
//
// Bazel source reference:
// https://github.com/bazelbuild/bazel/blob/master/src/main/java/com/google/devtools/build/lib/bazel/bzlmod/Selection.java
// See DepGraphWalker.walk() which uses Set<ModuleKey> known to track visited modules.
func TestBuildDependencyGraph_LongerMutualDependency(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/modules/module_a/1.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "module_a", version = "1.0.0")
			bazel_dep(name = "module_b", version = "1.0.0")`)
		case "/modules/module_b/1.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "module_b", version = "1.0.0")
			bazel_dep(name = "module_c", version = "1.0.0")`)
		case "/modules/module_c/1.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "module_c", version = "1.0.0")
			bazel_dep(name = "module_a", version = "1.0.0")`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	registry := newRegistryClient(server.URL)
	resolver := newDependencyResolver(registry, false)

	rootModule := &ModuleInfo{
		Name:    "root",
		Version: "1.0.0",
		Dependencies: []Dependency{
			{Name: "module_a", Version: "1.0.0"},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	list, err := resolver.ResolveDependencies(ctx, rootModule)
	if err != nil {
		t.Fatalf("Mutual dependency chain should succeed (matching Bazel behavior), got error: %v", err)
	}

	// Should have all 3 modules resolved
	if len(list.Modules) != 3 {
		t.Errorf("Expected 3 modules, got %d", len(list.Modules))
	}

	moduleNames := make(map[string]bool)
	for _, m := range list.Modules {
		moduleNames[m.Name] = true
	}

	for _, expected := range []string{"module_a", "module_b", "module_c"} {
		if !moduleNames[expected] {
			t.Errorf("Expected module %s in resolution list", expected)
		}
	}
}

// TestOnProgress_CallbacksInvoked tests that progress callbacks are invoked.
func TestOnProgress_CallbacksInvoked(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/modules/module_a/1.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "module_a", version = "1.0.0")`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	var events []ProgressEvent
	var mu sync.Mutex

	registry := newRegistryClient(server.URL)
	resolver := newDependencyResolverWithOptions(registry, ResolutionOptions{
		OnProgress: func(event ProgressEvent) {
			mu.Lock()
			events = append(events, event)
			mu.Unlock()
		},
	})

	rootModule := &ModuleInfo{
		Name:    "root",
		Version: "1.0.0",
		Dependencies: []Dependency{
			{Name: "module_a", Version: "1.0.0"},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := resolver.ResolveDependencies(ctx, rootModule)
	if err != nil {
		t.Fatalf("ResolveDependencies() error = %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	// Check that we got resolve_start and resolve_end events
	var hasStart, hasEnd bool
	var fetchStarts, fetchEnds int
	for _, e := range events {
		switch e.Type {
		case ProgressResolveStart:
			hasStart = true
		case ProgressResolveEnd:
			hasEnd = true
		case ProgressModuleFetchStart:
			fetchStarts++
		case ProgressModuleFetchEnd:
			fetchEnds++
		}
	}

	if !hasStart {
		t.Error("Expected resolve_start event")
	}
	if !hasEnd {
		t.Error("Expected resolve_end event")
	}
	if fetchStarts == 0 {
		t.Error("Expected at least one module_fetch_start event")
	}
	if fetchEnds == 0 {
		t.Error("Expected at least one module_fetch_end event")
	}
	if fetchStarts != fetchEnds {
		t.Errorf("Mismatch: %d fetch_start events but %d fetch_end events", fetchStarts, fetchEnds)
	}
}

// TestOnProgress_NilCallbackDoesNotPanic tests that nil OnProgress doesn't cause panics.
func TestOnProgress_NilCallbackDoesNotPanic(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/modules/module_a/1.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "module_a", version = "1.0.0")`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	registry := newRegistryClient(server.URL)
	resolver := newDependencyResolverWithOptions(registry, ResolutionOptions{
		OnProgress: nil, // Explicitly nil
	})

	rootModule := &ModuleInfo{
		Name:    "root",
		Version: "1.0.0",
		Dependencies: []Dependency{
			{Name: "module_a", Version: "1.0.0"},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// This should not panic
	_, err := resolver.ResolveDependencies(ctx, rootModule)
	if err != nil {
		t.Fatalf("ResolveDependencies() error = %v", err)
	}
}

// TestOnProgress_ModuleInfoInEvents tests that module fetch events have correct module info.
func TestOnProgress_ModuleInfoInEvents(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/modules/module_a/1.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "module_a", version = "1.0.0")
			bazel_dep(name = "module_b", version = "2.0.0")`)
		case "/modules/module_b/2.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "module_b", version = "2.0.0")`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	var events []ProgressEvent
	var mu sync.Mutex

	registry := newRegistryClient(server.URL)
	resolver := newDependencyResolverWithOptions(registry, ResolutionOptions{
		OnProgress: func(event ProgressEvent) {
			mu.Lock()
			events = append(events, event)
			mu.Unlock()
		},
	})

	rootModule := &ModuleInfo{
		Name:    "root",
		Version: "1.0.0",
		Dependencies: []Dependency{
			{Name: "module_a", Version: "1.0.0"},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := resolver.ResolveDependencies(ctx, rootModule)
	if err != nil {
		t.Fatalf("ResolveDependencies() error = %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	// Collect module names from fetch events
	fetchedModules := make(map[string]bool)
	for _, e := range events {
		if e.Type == ProgressModuleFetchStart {
			if e.Module == "" {
				t.Error("module_fetch_start event missing Module field")
			}
			if e.Version == "" {
				t.Error("module_fetch_start event missing Version field")
			}
			fetchedModules[e.Module+"@"+e.Version] = true
		}
	}

	// Verify expected modules were fetched
	expectedFetches := []string{"module_a@1.0.0", "module_b@2.0.0"}
	for _, expected := range expectedFetches {
		if !fetchedModules[expected] {
			t.Errorf("Expected fetch event for %s", expected)
		}
	}
}

// TestCalculateModuleDepths tests the BFS-based depth calculation.
func TestCalculateModuleDepths(t *testing.T) {
	tests := []struct {
		name       string
		rootDeps   []string
		moduleDeps map[string][]string
		selected   map[string]bool
		wantDepths map[string]int
	}{
		{
			name:     "direct dependencies have depth 1",
			rootDeps: []string{"a", "b"},
			moduleDeps: map[string][]string{
				"a": {},
				"b": {},
			},
			selected: map[string]bool{"a": true, "b": true},
			wantDepths: map[string]int{
				"a": 1,
				"b": 1,
			},
		},
		{
			name:     "transitive dependencies have depth 2+",
			rootDeps: []string{"a"},
			moduleDeps: map[string][]string{
				"a": {"b"},
				"b": {"c"},
				"c": {},
			},
			selected: map[string]bool{"a": true, "b": true, "c": true},
			wantDepths: map[string]int{
				"a": 1,
				"b": 2,
				"c": 3,
			},
		},
		{
			name:     "diamond dependency gets minimum depth",
			rootDeps: []string{"a", "b"},
			moduleDeps: map[string][]string{
				"a": {"c"},
				"b": {"c"},
				"c": {},
			},
			selected: map[string]bool{"a": true, "b": true, "c": true},
			wantDepths: map[string]int{
				"a": 1,
				"b": 1,
				"c": 2, // reachable via both a and b, depth is min(1+1, 1+1) = 2
			},
		},
		{
			name:     "module reachable via short and long paths gets short path depth",
			rootDeps: []string{"a", "d"},
			moduleDeps: map[string][]string{
				"a": {"b"},
				"b": {"c"},
				"c": {},
				"d": {"c"}, // d is direct dep, so c is at depth 2 via d
			},
			selected: map[string]bool{"a": true, "b": true, "c": true, "d": true},
			wantDepths: map[string]int{
				"a": 1,
				"b": 2,
				"c": 2, // via d (depth 1+1=2), not via a->b (1+1+1=3)
				"d": 1,
			},
		},
		{
			name:     "unselected modules are not included",
			rootDeps: []string{"a"},
			moduleDeps: map[string][]string{
				"a": {"b", "c"},
				"b": {},
				"c": {},
			},
			selected: map[string]bool{"a": true, "b": true}, // c not selected
			wantDepths: map[string]int{
				"a": 1,
				"b": 2,
				// c not in result
			},
		},
		{
			name:       "empty deps",
			rootDeps:   []string{},
			moduleDeps: map[string][]string{},
			selected:   map[string]bool{},
			wantDepths: map[string]int{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calculateModuleDepths(tt.rootDeps, tt.moduleDeps, tt.selected)

			if len(got) != len(tt.wantDepths) {
				t.Errorf("got %d depths, want %d", len(got), len(tt.wantDepths))
			}

			for name, wantDepth := range tt.wantDepths {
				if gotDepth, ok := got[name]; !ok {
					t.Errorf("missing depth for %s", name)
				} else if gotDepth != wantDepth {
					t.Errorf("depth[%s] = %d, want %d", name, gotDepth, wantDepth)
				}
			}
		})
	}
}

// TestResolveDependencies_Depth tests that Depth is correctly populated in resolution results.
func TestResolveDependencies_Depth(t *testing.T) {
	// Create a mock server with a chain: root -> a -> b -> c
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/modules/module_a/1.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "module_a", version = "1.0.0")
			bazel_dep(name = "module_b", version = "1.0.0")`)
		case "/modules/module_b/1.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "module_b", version = "1.0.0")
			bazel_dep(name = "module_c", version = "1.0.0")`)
		case "/modules/module_c/1.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "module_c", version = "1.0.0")`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	registry := newRegistryClient(server.URL)
	resolver := newDependencyResolver(registry, false)

	rootModule := &ModuleInfo{
		Name:    "root",
		Version: "1.0.0",
		Dependencies: []Dependency{
			{Name: "module_a", Version: "1.0.0"},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	list, err := resolver.ResolveDependencies(ctx, rootModule)
	if err != nil {
		t.Fatalf("ResolveDependencies() error = %v", err)
	}

	// Build depth map from results
	depths := make(map[string]int)
	for _, m := range list.Modules {
		depths[m.Name] = m.Depth
	}

	// Verify depths
	expectedDepths := map[string]int{
		"module_a": 1, // direct dep
		"module_b": 2, // transitive via a
		"module_c": 3, // transitive via a -> b
	}

	for name, wantDepth := range expectedDepths {
		if gotDepth, ok := depths[name]; !ok {
			t.Errorf("module %s not found in results", name)
		} else if gotDepth != wantDepth {
			t.Errorf("Depth[%s] = %d, want %d", name, gotDepth, wantDepth)
		}
	}
}

// TestResolveDependencies_DepthDiamond tests depth with diamond dependencies.
func TestResolveDependencies_DepthDiamond(t *testing.T) {
	// Diamond: root -> a, root -> b, a -> c, b -> c
	// c should have depth 2 (via both a and b)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/modules/module_a/1.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "module_a", version = "1.0.0")
			bazel_dep(name = "module_c", version = "1.0.0")`)
		case "/modules/module_b/1.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "module_b", version = "1.0.0")
			bazel_dep(name = "module_c", version = "1.0.0")`)
		case "/modules/module_c/1.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "module_c", version = "1.0.0")`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	registry := newRegistryClient(server.URL)
	resolver := newDependencyResolver(registry, false)

	rootModule := &ModuleInfo{
		Name:    "root",
		Version: "1.0.0",
		Dependencies: []Dependency{
			{Name: "module_a", Version: "1.0.0"},
			{Name: "module_b", Version: "1.0.0"},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	list, err := resolver.ResolveDependencies(ctx, rootModule)
	if err != nil {
		t.Fatalf("ResolveDependencies() error = %v", err)
	}

	// Build depth map from results
	depths := make(map[string]int)
	for _, m := range list.Modules {
		depths[m.Name] = m.Depth
	}

	// Verify depths
	expectedDepths := map[string]int{
		"module_a": 1, // direct dep
		"module_b": 1, // direct dep
		"module_c": 2, // transitive via both a and b (minimum depth)
	}

	for name, wantDepth := range expectedDepths {
		if gotDepth, ok := depths[name]; !ok {
			t.Errorf("module %s not found in results", name)
		} else if gotDepth != wantDepth {
			t.Errorf("Depth[%s] = %d, want %d", name, gotDepth, wantDepth)
		}
	}
}

// Benchmark tests
func BenchmarkApplyMVS(b *testing.B) {
	registry := newRegistryClient("https://bcr.bazel.build")
	resolver := newDependencyResolver(registry, false)

	// Create a large dependency graph for benchmarking
	depGraph := make(map[string]map[string]*depRequest)
	for i := 0; i < 100; i++ {
		moduleName := fmt.Sprintf("module_%d", i)
		depGraph[moduleName] = make(map[string]*depRequest)
		for j := 0; j < 10; j++ {
			version := fmt.Sprintf("1.%d.0", j)
			depGraph[moduleName][version] = &depRequest{
				Version:    version,
				RequiredBy: []string{fmt.Sprintf("requirer_%d", j)},
			}
		}
	}

	b.ResetTimer()
	for b.Loop() {
		_ = resolver.applyMVS(depGraph)
	}
}
