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
	registry1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/modules/core_module/1.0.0/MODULE.bazel"):
			fmt.Fprint(w, `module(name = "core_module", version = "1.0.0")
bazel_dep(name = "dep_a", version = "2.0.0")
bazel_dep(name = "dep_b", version = "1.5.0")`)
		case strings.Contains(r.URL.Path, "/modules/dep_a/2.0.0/MODULE.bazel"):
			fmt.Fprint(w, `module(name = "dep_a", version = "2.0.0")`)
		case strings.Contains(r.URL.Path, "/modules/core_module/metadata.json"):
			fmt.Fprint(w, `{"versions": ["1.0.0"]}`)
		case strings.Contains(r.URL.Path, "/modules/dep_a/metadata.json"):
			fmt.Fprint(w, `{"versions": ["2.0.0"]}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))

	registry2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/modules/dep_b/1.5.0/MODULE.bazel"):
			fmt.Fprint(w, `module(name = "dep_b", version = "1.5.0")
bazel_dep(name = "dep_c", version = "3.0.0")`)
		case strings.Contains(r.URL.Path, "/modules/dep_c/3.0.0/MODULE.bazel"):
			fmt.Fprint(w, `module(name = "dep_c", version = "3.0.0")`)
		case strings.Contains(r.URL.Path, "/modules/dep_b/metadata.json"):
			fmt.Fprint(w, `{"versions": ["1.5.0"]}`)
		case strings.Contains(r.URL.Path, "/modules/dep_c/metadata.json"):
			fmt.Fprint(w, `{"versions": ["3.0.0"]}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))

	return registry1, registry2, func() { registry1.Close(); registry2.Close() }
}

func TestMultiRegistry_BasicResolution(t *testing.T) {
	reg1, reg2, cleanup := setupMultiRegistryTest()
	defer cleanup()

	content := `module(name = "my_project", version = "0.0.0")
bazel_dep(name = "core_module", version = "1.0.0")`

	result, err := ResolveContent(context.Background(), content, ResolutionOptions{
		Registries: []string{reg1.URL, reg2.URL},
	})
	if err != nil {
		t.Fatalf("ResolveContent() error = %v", err)
	}

	expected := map[string]bool{"core_module": false, "dep_a": false, "dep_b": false, "dep_c": false}
	for _, mod := range result.Modules {
		expected[mod.Name] = true
	}
	for name, found := range expected {
		if !found {
			t.Errorf("expected module %s not found", name)
		}
	}

	for _, mod := range result.Modules {
		switch mod.Name {
		case "core_module", "dep_a":
			if mod.Registry != reg1.URL {
				t.Errorf("%s should be from registry1, got %s", mod.Name, mod.Registry)
			}
		case "dep_b", "dep_c":
			if mod.Registry != reg2.URL {
				t.Errorf("%s should be from registry2, got %s", mod.Name, mod.Registry)
			}
		}
	}
}

func TestMultiRegistry_ModuleStickiness(t *testing.T) {
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

	registry2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/modules/module_x/") {
			t.Error("registry2 should not be called for module_x")
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer registry2.Close()

	content := `module(name = "test", version = "0.0.0")
bazel_dep(name = "module_x", version = "1.0.0")`

	result, err := ResolveContent(context.Background(), content, ResolutionOptions{
		Registries: []string{registry1.URL, registry2.URL},
	})
	if err != nil {
		t.Fatalf("ResolveContent() error = %v", err)
	}

	for _, mod := range result.Modules {
		if mod.Name == "module_x" && mod.Registry != registry1.URL {
			t.Errorf("module_x should be from registry1, got %s", mod.Registry)
		}
	}
}

func TestMultiRegistry_SingleRegistryBackwardsCompatibility(t *testing.T) {
	reg1, reg2, cleanup := setupMultiRegistryTest()
	defer cleanup()

	content := `module(name = "test", version = "0.0.0")
bazel_dep(name = "core_module", version = "1.0.0")`

	// Single registry: dep_b should not be found (only in reg2)
	result, err := ResolveContent(context.Background(), content, ResolutionOptions{
		Registries: []string{reg1.URL},
	})
	if err != nil {
		t.Fatalf("ResolveContent() error = %v", err)
	}

	for _, mod := range result.Modules {
		if mod.Name == "dep_b" {
			t.Error("dep_b should not be resolved (only in registry2)")
		}
	}

	// Both registries: dep_b should be found
	result2, err := ResolveContent(context.Background(), content, ResolutionOptions{
		Registries: []string{reg1.URL, reg2.URL},
	})
	if err != nil {
		t.Fatalf("ResolveContent() error = %v", err)
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

func TestMultiRegistry_RegistryOverride(t *testing.T) {
	reg1, reg2, cleanup := setupMultiRegistryTest()
	defer cleanup()

	registry3 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/modules/override_mod/5.0.0/MODULE.bazel"):
			fmt.Fprint(w, `module(name = "override_mod", version = "5.0.0")`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer registry3.Close()

	content := fmt.Sprintf(`module(name = "root", version = "0.0.0")
bazel_dep(name = "override_mod", version = "5.0.0")
single_version_override(module_name = "override_mod", version = "5.0.0", registry = "%s")`, registry3.URL)

	result, err := ResolveContent(context.Background(), content, ResolutionOptions{
		Registries: []string{reg1.URL, reg2.URL},
	})
	if err != nil {
		t.Fatalf("ResolveContent() error = %v", err)
	}

	found := false
	for _, mod := range result.Modules {
		if mod.Name == "override_mod" {
			found = true
			if mod.Registry != registry3.URL {
				t.Errorf("override_mod should use registry3, got %s", mod.Registry)
			}
		}
	}
	if !found {
		t.Error("override_mod not found in resolution")
	}
}

func TestMultiRegistry_MVSAcrossRegistries(t *testing.T) {
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
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer registry.Close()

	content := `module(name = "root", version = "0.0.0")
bazel_dep(name = "dep_x", version = "1.0.0")
bazel_dep(name = "dep_y", version = "1.0.0")`

	result, err := ResolveContent(context.Background(), content, ResolutionOptions{
		Registries: []string{registry.URL},
	})
	if err != nil {
		t.Fatalf("ResolveContent() error = %v", err)
	}

	for _, mod := range result.Modules {
		if mod.Name == "mvs_test" && mod.Version != "2.0.0" {
			t.Errorf("MVS should select 2.0.0, got %s", mod.Version)
		}
	}
}
