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

	"github.com/albertocavalcante/go-bzlmod/registry"
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

	moduleDeps := make(map[string][]string)        // Empty for this test
	moduleInfoCache := make(map[string]*ModuleInfo) // Empty for this test
	list, err := resolver.buildResolutionList(context.Background(), selectedVersions, moduleDeps, moduleInfoCache, rootModule)
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
		for i := range chainDepth {
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
		for i := range chainDepth {
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
	for i := range 100 {
		moduleName := fmt.Sprintf("module_%d", i)
		depGraph[moduleName] = make(map[string]*depRequest)
		for j := range 10 {
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

// TestMultiRoundNodepDiscovery_SingleRound tests that modules without nodep deps
// complete in a single discovery round.
func TestMultiRoundNodepDiscovery_SingleRound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/modules/module_a/1.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "module_a", version = "1.0.0")
			bazel_dep(name = "module_b", version = "1.0.0")`)
		case "/modules/module_b/1.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "module_b", version = "1.0.0")`)
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

// TestMultiRoundNodepDiscovery_NodepFulfilledFirstRound tests that nodep deps
// are included when they reference modules already in the graph.
func TestMultiRoundNodepDiscovery_NodepFulfilledFirstRound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/modules/module_a/1.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "module_a", version = "1.0.0")`)
		case "/modules/module_b/1.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "module_b", version = "1.0.0")`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	registry := newRegistryClient(server.URL)
	resolver := newDependencyResolver(registry, false)

	// Root has both regular dep and nodep dep on modules that will exist
	rootModule := &ModuleInfo{
		Name:    "root",
		Version: "1.0.0",
		Dependencies: []Dependency{
			{Name: "module_a", Version: "1.0.0"},
			{Name: "module_b", Version: "1.0.0"},
		},
		NodepDependencies: []Dependency{
			{Name: "module_a", Version: "1.0.0"}, // Will be fulfilled since module_a is a regular dep
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	list, err := resolver.ResolveDependencies(ctx, rootModule)
	if err != nil {
		t.Fatalf("ResolveDependencies() error = %v", err)
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

// TestMultiRoundNodepDiscovery_UnfulfilledNodepIgnored tests that nodep deps
// referencing non-existent modules are ignored (don't cause errors).
func TestMultiRoundNodepDiscovery_UnfulfilledNodepIgnored(t *testing.T) {
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
	resolver := newDependencyResolver(registry, false)

	// Root has nodep dep on a module that doesn't exist in the graph
	rootModule := &ModuleInfo{
		Name:    "root",
		Version: "1.0.0",
		Dependencies: []Dependency{
			{Name: "module_a", Version: "1.0.0"},
		},
		NodepDependencies: []Dependency{
			{Name: "nonexistent_module", Version: "1.0.0"}, // Won't be fulfilled
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	list, err := resolver.ResolveDependencies(ctx, rootModule)
	if err != nil {
		t.Fatalf("ResolveDependencies() error = %v", err)
	}

	// Should only have module_a (nonexistent_module is ignored)
	if len(list.Modules) != 1 {
		t.Errorf("Expected 1 module, got %d", len(list.Modules))
	}

	if list.Modules[0].Name != "module_a" {
		t.Errorf("Expected module_a, got %s", list.Modules[0].Name)
	}
}

// TestMultiRoundNodepDiscovery_MultipleRounds tests that nodep deps that become
// fulfillable in subsequent rounds are correctly handled.
func TestMultiRoundNodepDiscovery_MultipleRounds(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/modules/module_a/1.0.0/MODULE.bazel":
			// module_a brings in module_b as a regular dep
			fmt.Fprint(w, `module(name = "module_a", version = "1.0.0")
			bazel_dep(name = "module_b", version = "1.0.0")`)
		case "/modules/module_b/1.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "module_b", version = "1.0.0")`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	registry := newRegistryClient(server.URL)
	resolver := newDependencyResolver(registry, false)

	// Root has a nodep dep on module_b, which doesn't exist yet but will be
	// brought in transitively by module_a
	rootModule := &ModuleInfo{
		Name:    "root",
		Version: "1.0.0",
		Dependencies: []Dependency{
			{Name: "module_a", Version: "1.0.0"},
		},
		NodepDependencies: []Dependency{
			{Name: "module_b", Version: "1.0.0"}, // Will be fulfilled in round 2
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	list, err := resolver.ResolveDependencies(ctx, rootModule)
	if err != nil {
		t.Fatalf("ResolveDependencies() error = %v", err)
	}

	// Both modules should be resolved (module_b via both regular and nodep edge)
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

// TestMultiRoundNodepDiscovery_TransitiveNodep tests nodep deps in transitive modules.
func TestMultiRoundNodepDiscovery_TransitiveNodep(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/modules/module_a/1.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "module_a", version = "1.0.0")`)
		case "/modules/module_b/1.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "module_b", version = "1.0.0")`)
		case "/modules/module_c/1.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "module_c", version = "1.0.0")`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	registry := newRegistryClient(server.URL)
	resolver := newDependencyResolver(registry, false)

	// Create a scenario where:
	// - Root has regular deps on a, b, c
	// - Module A has a nodep dep on module_b (which exists)
	// This simulates use_extension in module_a that uses module_b

	// First we need to set up module_a with a nodep dependency
	// Since the server returns static content, we'll use AddOverrideModuleContent
	moduleAContent := `module(name = "module_a", version = "1.0.0")`
	moduleAInfo, _ := ParseModuleContent(moduleAContent)
	moduleAInfo.NodepDependencies = []Dependency{
		{Name: "module_b", Version: "1.0.0"},
	}

	// Use override to inject our modified module info
	if err := resolver.AddOverrideModuleInfo("module_a", moduleAInfo); err != nil {
		t.Fatalf("AddOverrideModuleInfo() error = %v", err)
	}

	rootModule := &ModuleInfo{
		Name:    "root",
		Version: "1.0.0",
		Dependencies: []Dependency{
			{Name: "module_a", Version: "1.0.0"},
			{Name: "module_b", Version: "1.0.0"},
			{Name: "module_c", Version: "1.0.0"},
		},
		Overrides: []Override{
			{Type: "git", ModuleName: "module_a"}, // Use our override
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	list, err := resolver.ResolveDependencies(ctx, rootModule)
	if err != nil {
		t.Fatalf("ResolveDependencies() error = %v", err)
	}

	// All three modules should be resolved
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

// TestIsNodepDep_FieldExists tests that the IsNodepDep field exists and works correctly.
func TestIsNodepDep_FieldExists(t *testing.T) {
	dep := Dependency{
		Name:       "test",
		Version:    "1.0.0",
		IsNodepDep: true,
	}

	if !dep.IsNodepDep {
		t.Error("IsNodepDep should be true")
	}

	dep2 := Dependency{
		Name:    "test2",
		Version: "2.0.0",
	}

	if dep2.IsNodepDep {
		t.Error("IsNodepDep should be false by default")
	}
}

// TestNodepDependencies_FieldExists tests that ModuleInfo has NodepDependencies field.
func TestNodepDependencies_FieldExists(t *testing.T) {
	module := &ModuleInfo{
		Name:    "test",
		Version: "1.0.0",
		Dependencies: []Dependency{
			{Name: "regular", Version: "1.0.0"},
		},
		NodepDependencies: []Dependency{
			{Name: "nodep", Version: "1.0.0", IsNodepDep: true},
		},
	}

	if len(module.NodepDependencies) != 1 {
		t.Errorf("Expected 1 nodep dependency, got %d", len(module.NodepDependencies))
	}

	if module.NodepDependencies[0].Name != "nodep" {
		t.Errorf("Expected nodep dependency name 'nodep', got %s", module.NodepDependencies[0].Name)
	}
}

// TestMultiRoundNodepDiscovery_ComplexScenario tests a complex multi-round scenario
// where nodep edges become fulfillable across multiple rounds.
//
// Scenario:
// - Root has regular dep on A, nodep dep on D
// - A has regular dep on B
// - B has regular dep on C
// - C has regular dep on D
//
// In this case:
// - Round 1: Discover A, B, C, D (via regular deps)
//   - Root's nodep on D is unfulfilled initially
//   - But D gets discovered via C's regular dep
// - Round 2: D is now in the graph, so root's nodep on D can be fulfilled
//
// The nodep should participate in version selection.
func TestMultiRoundNodepDiscovery_ComplexScenario(t *testing.T) {
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
			bazel_dep(name = "module_d", version = "1.0.0")`)
		case "/modules/module_d/1.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "module_d", version = "1.0.0")`)
		case "/modules/module_d/2.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "module_d", version = "2.0.0")`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	registry := newRegistryClient(server.URL)
	resolver := newDependencyResolver(registry, false)

	// Root has regular dep on A and nodep dep on D@2.0.0
	// D is also brought in via A->B->C at version 1.0.0
	// With nodep fulfilled, both versions should participate in MVS
	// and 2.0.0 should be selected as the highest
	rootModule := &ModuleInfo{
		Name:    "root",
		Version: "1.0.0",
		Dependencies: []Dependency{
			{Name: "module_a", Version: "1.0.0"},
		},
		NodepDependencies: []Dependency{
			{Name: "module_d", Version: "2.0.0"}, // Will be fulfilled after D is discovered
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	list, err := resolver.ResolveDependencies(ctx, rootModule)
	if err != nil {
		t.Fatalf("ResolveDependencies() error = %v", err)
	}

	// All four modules should be resolved
	if len(list.Modules) != 4 {
		t.Errorf("Expected 4 modules, got %d", len(list.Modules))
		for _, m := range list.Modules {
			t.Logf("  - %s@%s", m.Name, m.Version)
		}
	}

	// Check versions - module_d should be 2.0.0 due to nodep requiring higher version
	for _, m := range list.Modules {
		if m.Name == "module_d" {
			if m.Version != "2.0.0" {
				t.Errorf("Expected module_d@2.0.0 (from nodep), got module_d@%s", m.Version)
			}
			break
		}
	}
}

// TestMultiRoundNodepDiscovery_DevDependencyExcluded tests that nodep deps
// respect the dev dependency filtering.
func TestMultiRoundNodepDiscovery_DevDependencyExcluded(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/modules/module_a/1.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "module_a", version = "1.0.0")`)
		case "/modules/dev_module/1.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "dev_module", version = "1.0.0")`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	registry := newRegistryClient(server.URL)
	// Create resolver WITHOUT dev deps
	resolver := newDependencyResolverWithOptions(registry, ResolutionOptions{
		IncludeDevDeps: false,
	})

	rootModule := &ModuleInfo{
		Name:    "root",
		Version: "1.0.0",
		Dependencies: []Dependency{
			{Name: "module_a", Version: "1.0.0"},
		},
		NodepDependencies: []Dependency{
			{Name: "dev_module", Version: "1.0.0", DevDependency: true}, // Should be excluded
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	list, err := resolver.ResolveDependencies(ctx, rootModule)
	if err != nil {
		t.Fatalf("ResolveDependencies() error = %v", err)
	}

	// Only module_a should be resolved (dev_module excluded)
	if len(list.Modules) != 1 {
		t.Errorf("Expected 1 module, got %d", len(list.Modules))
		for _, m := range list.Modules {
			t.Logf("  - %s@%s", m.Name, m.Version)
		}
	}

	if list.Modules[0].Name != "module_a" {
		t.Errorf("Expected module_a, got %s", list.Modules[0].Name)
	}
}

// TestCheckFieldCompatibility tests that field compatibility checking works correctly.
func TestCheckFieldCompatibility(t *testing.T) {
	tests := []struct {
		name         string
		rootModule   *ModuleInfo
		bazelVersion string
		wantWarnings int
	}{
		{
			name: "no max_compatibility_level",
			rootModule: &ModuleInfo{
				Name:    "root",
				Version: "1.0.0",
				Dependencies: []Dependency{
					{Name: "dep_a", Version: "1.0.0"},
				},
			},
			bazelVersion: "6.6.0",
			wantWarnings: 0,
		},
		{
			name: "max_compatibility_level with Bazel 7.0.0",
			rootModule: &ModuleInfo{
				Name:    "root",
				Version: "1.0.0",
				Dependencies: []Dependency{
					{Name: "dep_a", Version: "1.0.0", MaxCompatibilityLevel: 2},
				},
			},
			bazelVersion: "7.0.0",
			wantWarnings: 0,
		},
		{
			name: "max_compatibility_level with Bazel 6.6.0",
			rootModule: &ModuleInfo{
				Name:    "root",
				Version: "1.0.0",
				Dependencies: []Dependency{
					{Name: "dep_a", Version: "1.0.0", MaxCompatibilityLevel: 2},
				},
			},
			bazelVersion: "6.6.0",
			wantWarnings: 1,
		},
		{
			name: "multiple deps with max_compatibility_level only warns once",
			rootModule: &ModuleInfo{
				Name:    "root",
				Version: "1.0.0",
				Dependencies: []Dependency{
					{Name: "dep_a", Version: "1.0.0", MaxCompatibilityLevel: 2},
					{Name: "dep_b", Version: "1.0.0", MaxCompatibilityLevel: 3},
				},
			},
			bazelVersion: "6.6.0",
			wantWarnings: 1, // Only one warning for the field, not per dependency
		},
		{
			name: "empty bazel version returns no warnings",
			rootModule: &ModuleInfo{
				Name:    "root",
				Version: "1.0.0",
				Dependencies: []Dependency{
					{Name: "dep_a", Version: "1.0.0", MaxCompatibilityLevel: 2},
				},
			},
			bazelVersion: "",
			wantWarnings: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			warnings := checkFieldCompatibility(tt.rootModule, tt.bazelVersion)
			if len(warnings) != tt.wantWarnings {
				t.Errorf("checkFieldCompatibility() returned %d warnings, want %d: %v",
					len(warnings), tt.wantWarnings, warnings)
			}
		})
	}
}

// TestResolutionSummary_FieldWarnings tests that FieldWarnings are populated in the summary.
func TestResolutionSummary_FieldWarnings(t *testing.T) {
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
		BazelVersion: "6.6.0",
	})

	rootModule := &ModuleInfo{
		Name:    "root",
		Version: "1.0.0",
		Dependencies: []Dependency{
			{Name: "module_a", Version: "1.0.0", MaxCompatibilityLevel: 2},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	list, err := resolver.ResolveDependencies(ctx, rootModule)
	if err != nil {
		t.Fatalf("ResolveDependencies() error = %v", err)
	}

	// Should have a field warning for max_compatibility_level
	if len(list.Summary.FieldWarnings) == 0 {
		t.Error("Expected field warnings for max_compatibility_level with Bazel 6.6.0")
	}

	// Verify the warning message contains expected info
	found := false
	for _, w := range list.Summary.FieldWarnings {
		if strings.Contains(w, "max_compatibility_level") && strings.Contains(w, "7.0.0") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected warning about max_compatibility_level requiring Bazel 7.0.0, got: %v",
			list.Summary.FieldWarnings)
	}
}

// TestModuleDeps_SelectedVersionDependencies tests that the Dependencies field of a
// resolved module reflects the SELECTED version's dependencies, not some other version's.
//
// Regression test for: moduleDeps was keyed by module name only, so when multiple
// versions of the same module were fetched, the last goroutine to write would win.
// After MVS selected the highest version, Dependencies could reflect a different version.
func TestModuleDeps_SelectedVersionDependencies(t *testing.T) {
	// Setup:
	//   root -> lib_a@1.0.0 (production)
	//   root -> bumper@1.0.0 (production)
	//   bumper@1.0.0 -> lib_a@2.0.0 (MVS will select 2.0.0)
	//   lib_a@1.0.0 depends on [old_dep@1.0.0]   (v1-only dep)
	//   lib_a@2.0.0 depends on [new_dep@1.0.0]    (v2-only dep)
	//
	// Expected: After MVS selects lib_a@2.0.0, its Dependencies should be ["new_dep"],
	// not ["old_dep"] (which was lib_a@1.0.0's dependency list).
	//
	// The server delays lib_a@1.0.0 so it is fetched AFTER lib_a@2.0.0, ensuring
	// the stale v1 deps overwrite the correct v2 deps in the buggy code path.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/modules/bumper/1.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "bumper", version = "1.0.0")
bazel_dep(name = "lib_a", version = "2.0.0")`)
		case "/modules/lib_a/1.0.0/MODULE.bazel":
			// Delay v1.0.0 to ensure it is processed AFTER v2.0.0
			time.Sleep(200 * time.Millisecond)
			fmt.Fprint(w, `module(name = "lib_a", version = "1.0.0")
bazel_dep(name = "old_dep", version = "1.0.0")`)
		case "/modules/lib_a/2.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "lib_a", version = "2.0.0")
bazel_dep(name = "new_dep", version = "1.0.0")`)
		case "/modules/old_dep/1.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "old_dep", version = "1.0.0")`)
		case "/modules/new_dep/1.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "new_dep", version = "1.0.0")`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	reg := newRegistryClient(server.URL)
	resolver := newDependencyResolver(reg, false)

	rootModule := &ModuleInfo{
		Name:    "root",
		Version: "1.0.0",
		Dependencies: []Dependency{
			{Name: "lib_a", Version: "1.0.0"},
			{Name: "bumper", Version: "1.0.0"},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := resolver.ResolveDependencies(ctx, rootModule)
	if err != nil {
		t.Fatalf("ResolveDependencies failed: %v", err)
	}

	// MVS should select lib_a@2.0.0
	for _, m := range result.Modules {
		if m.Name == "lib_a" {
			if m.Version != "2.0.0" {
				t.Fatalf("lib_a version = %s, want 2.0.0 (MVS should select highest)", m.Version)
			}
			// The critical check: Dependencies should reflect v2.0.0's deps
			hasNewDep := false
			hasOldDep := false
			for _, dep := range m.Dependencies {
				if dep == "new_dep" {
					hasNewDep = true
				}
				if dep == "old_dep" {
					hasOldDep = true
				}
			}
			if !hasNewDep {
				t.Errorf("lib_a@2.0.0 Dependencies = %v, missing 'new_dep': "+
					"Dependencies should reflect selected version's deps",
					m.Dependencies)
			}
			if hasOldDep {
				t.Errorf("lib_a@2.0.0 Dependencies = %v, has 'old_dep': "+
					"Dependencies contains stale dep from v1.0.0",
					m.Dependencies)
			}
			return
		}
	}
	t.Fatal("module 'lib_a' not found in resolution result")
}

// TestDevDependencyFlag_ProductionPathOverridesDevPath tests that when a module is
// required as both a dev dependency (by root) and a production dependency (by another
// module), it is correctly marked as DevDependency=false.
//
// Regression test for: DevDependency flag logic was inverted, promoting to true when
// any dev requester existed, even if a production path also required the module.
func TestDevDependencyFlag_ProductionPathOverridesDevPath(t *testing.T) {
	// Setup:
	//   root -> shared@1.0.0 (dev_dependency = true)
	//   root -> lib_a@1.0.0  (production)
	//   lib_a@1.0.0 -> shared@1.0.0 (production)
	//
	// Expected: shared should be DevDependency=false because lib_a requires it
	// as a production dependency.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/modules/lib_a/1.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "lib_a", version = "1.0.0")
bazel_dep(name = "shared", version = "1.0.0")`)
		case "/modules/shared/1.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "shared", version = "1.0.0")`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	reg := newRegistryClient(server.URL)
	resolver := newDependencyResolver(reg, true) // includeDevDeps=true

	rootModule := &ModuleInfo{
		Name:    "root",
		Version: "1.0.0",
		Dependencies: []Dependency{
			{Name: "lib_a", Version: "1.0.0", DevDependency: false},
			{Name: "shared", Version: "1.0.0", DevDependency: true},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := resolver.ResolveDependencies(ctx, rootModule)
	if err != nil {
		t.Fatalf("ResolveDependencies failed: %v", err)
	}

	for _, m := range result.Modules {
		if m.Name == "shared" {
			if m.DevDependency {
				t.Errorf("module 'shared' has DevDependency=true, want false: "+
					"it is required as a production dep by lib_a")
			}
			return
		}
	}
	t.Fatal("module 'shared' not found in resolution result")
}

// TestDevDependencyFlag_AllDevRequesters tests that a module required only via
// dev dependency paths is correctly marked as DevDependency=true.
func TestDevDependencyFlag_AllDevRequesters(t *testing.T) {
	// Setup:
	//   root -> dev_only@1.0.0 (dev_dependency = true)
	//   root -> lib_a@1.0.0    (production)
	//   lib_a has no dep on dev_only
	//
	// Expected: dev_only should be DevDependency=true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/modules/lib_a/1.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "lib_a", version = "1.0.0")`)
		case "/modules/dev_only/1.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "dev_only", version = "1.0.0")`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	reg := newRegistryClient(server.URL)
	resolver := newDependencyResolver(reg, true)

	rootModule := &ModuleInfo{
		Name:    "root",
		Version: "1.0.0",
		Dependencies: []Dependency{
			{Name: "lib_a", Version: "1.0.0", DevDependency: false},
			{Name: "dev_only", Version: "1.0.0", DevDependency: true},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := resolver.ResolveDependencies(ctx, rootModule)
	if err != nil {
		t.Fatalf("ResolveDependencies failed: %v", err)
	}

	for _, m := range result.Modules {
		if m.Name == "dev_only" {
			if !m.DevDependency {
				t.Errorf("module 'dev_only' has DevDependency=false, want true: "+
					"it is only required via dev dependency paths")
			}
			return
		}
	}
	t.Fatal("module 'dev_only' not found in resolution result")
}

// TestDevDependencyFlag_SummaryCounts tests that ProductionModules and DevModules
// summary counts are accurate when dev and production dependencies coexist.
func TestDevDependencyFlag_SummaryCounts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/modules/prod_lib/1.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "prod_lib", version = "1.0.0")
bazel_dep(name = "shared", version = "1.0.0")`)
		case "/modules/shared/1.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "shared", version = "1.0.0")`)
		case "/modules/dev_tool/1.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "dev_tool", version = "1.0.0")`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	reg := newRegistryClient(server.URL)
	resolver := newDependencyResolver(reg, true)

	rootModule := &ModuleInfo{
		Name:    "root",
		Version: "1.0.0",
		Dependencies: []Dependency{
			{Name: "prod_lib", Version: "1.0.0", DevDependency: false},
			{Name: "shared", Version: "1.0.0", DevDependency: true},   // also required by prod_lib
			{Name: "dev_tool", Version: "1.0.0", DevDependency: true}, // only dev
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := resolver.ResolveDependencies(ctx, rootModule)
	if err != nil {
		t.Fatalf("ResolveDependencies failed: %v", err)
	}

	// Expected: prod_lib=production, shared=production (required by prod_lib), dev_tool=dev
	wantProd := 2 // prod_lib + shared
	wantDev := 1  // dev_tool

	if result.Summary.ProductionModules != wantProd {
		t.Errorf("Summary.ProductionModules = %d, want %d", result.Summary.ProductionModules, wantProd)
		for _, m := range result.Modules {
			t.Logf("  %s: DevDependency=%v", m.Name, m.DevDependency)
		}
	}
	if result.Summary.DevModules != wantDev {
		t.Errorf("Summary.DevModules = %d, want %d", result.Summary.DevModules, wantDev)
	}
}

// TestFindNonYankedVersion_PicksClosestVersion tests that yanked version substitution
// selects the closest (lowest) non-yanked replacement, not just the first one encountered
// in an arbitrarily-ordered version list.
//
// Regression test for: findNonYankedVersion iterated NonYankedVersions() without sorting,
// so an unsorted metadata.json Versions list could cause it to pick a much higher version
// than necessary (e.g., 5.0.0 instead of 2.0.0).
func TestFindNonYankedVersion_PicksClosestVersion(t *testing.T) {
	// Mock registry with unsorted metadata.json versions.
	// Requested version 1.0.0 is yanked. Non-yanked versions available:
	//   5.0.0 (compat=0) - far from requested
	//   2.0.0 (compat=0) - closest replacement
	//   3.0.0 (compat=0)
	// Metadata returns versions in arbitrary (non-ascending) order: [5.0.0, 2.0.0, 3.0.0]
	// The correct replacement is 2.0.0 (closest >= 1.0.0 with same compat level).
	mock := &mockRegistry{
		getModuleMetadata: func(_ context.Context, name string) (*registry.Metadata, error) {
			if name == "lib" {
				return &registry.Metadata{
					Versions:       []string{"1.0.0", "5.0.0", "2.0.0", "3.0.0"}, // unsorted
					YankedVersions: map[string]string{"1.0.0": "security issue"},
				}, nil
			}
			return nil, &RegistryError{StatusCode: 404}
		},
		getModuleFile: func(_ context.Context, name, ver string) (*ModuleInfo, error) {
			if name == "lib" {
				return &ModuleInfo{
					Name:               name,
					Version:            ver,
					CompatibilityLevel: 0,
				}, nil
			}
			return nil, &RegistryError{StatusCode: 404}
		},
	}

	resolver := &dependencyResolver{
		registry: mock,
		options:  ResolutionOptions{SubstituteYanked: true},
	}

	ctx := context.Background()
	replacement := resolver.findNonYankedVersion(ctx, "lib", "1.0.0")

	if replacement != "2.0.0" {
		t.Errorf("findNonYankedVersion() = %q, want \"2.0.0\" (closest non-yanked version)", replacement)
	}
}

// TestFindNonYankedVersion_NotYanked tests that non-yanked versions are returned unchanged.
func TestFindNonYankedVersion_NotYanked(t *testing.T) {
	mock := &mockRegistry{
		getModuleMetadata: func(_ context.Context, name string) (*registry.Metadata, error) {
			return &registry.Metadata{
				Versions: []string{"1.0.0", "2.0.0"},
			}, nil
		},
	}

	resolver := &dependencyResolver{
		registry: mock,
		options:  ResolutionOptions{SubstituteYanked: true},
	}

	ctx := context.Background()
	result := resolver.findNonYankedVersion(ctx, "lib", "1.0.0")
	if result != "1.0.0" {
		t.Errorf("findNonYankedVersion() = %q, want \"1.0.0\" (not yanked)", result)
	}
}
