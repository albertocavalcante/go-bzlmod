package gobzlmod

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// createMultiRegistryServers creates test HTTP servers simulating multiple registries
func createMultiRegistryServers() (*httptest.Server, *httptest.Server, *httptest.Server) {
	// Registry 1: Has module_a and module_b
	registry1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/modules/module_a/1.0.0/MODULE.bazel"):
			fmt.Fprint(w, `module(name = "module_a", version = "1.0.0")`)
		case strings.Contains(r.URL.Path, "/modules/module_a/2.0.0/MODULE.bazel"):
			fmt.Fprint(w, `module(name = "module_a", version = "2.0.0")
bazel_dep(name = "shared_dep", version = "1.0.0")`)
		case strings.Contains(r.URL.Path, "/modules/module_b/1.0.0/MODULE.bazel"):
			fmt.Fprint(w, `module(name = "module_b", version = "1.0.0")`)
		case strings.Contains(r.URL.Path, "/modules/module_a/metadata.json"):
			fmt.Fprint(w, `{"versions": ["1.0.0", "2.0.0"]}`)
		case strings.Contains(r.URL.Path, "/modules/module_b/metadata.json"):
			fmt.Fprint(w, `{"versions": ["1.0.0"]}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))

	// Registry 2: Has module_c and shared_dep
	registry2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/modules/module_c/1.5.0/MODULE.bazel"):
			fmt.Fprint(w, `module(name = "module_c", version = "1.5.0")`)
		case strings.Contains(r.URL.Path, "/modules/shared_dep/1.0.0/MODULE.bazel"):
			fmt.Fprint(w, `module(name = "shared_dep", version = "1.0.0")`)
		case strings.Contains(r.URL.Path, "/modules/shared_dep/2.0.0/MODULE.bazel"):
			fmt.Fprint(w, `module(name = "shared_dep", version = "2.0.0")`)
		case strings.Contains(r.URL.Path, "/modules/module_c/metadata.json"):
			fmt.Fprint(w, `{"versions": ["1.5.0"]}`)
		case strings.Contains(r.URL.Path, "/modules/shared_dep/metadata.json"):
			fmt.Fprint(w, `{"versions": ["1.0.0", "2.0.0"]}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))

	// Registry 3: Fallback registry with module_d
	registry3 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/modules/module_d/3.0.0/MODULE.bazel"):
			fmt.Fprint(w, `module(name = "module_d", version = "3.0.0")`)
		case strings.Contains(r.URL.Path, "/modules/module_d/metadata.json"):
			fmt.Fprint(w, `{"versions": ["3.0.0"]}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))

	return registry1, registry2, registry3
}

func TestNewRegistryChain(t *testing.T) {
	tests := []struct {
		name         string
		registryURLs []string
		wantErr      bool
		wantClients  int
	}{
		{
			name:         "empty URLs",
			registryURLs: []string{},
			wantErr:      true,
		},
		{
			name:         "nil URLs",
			registryURLs: nil,
			wantErr:      true,
		},
		{
			name:         "all invalid file URLs",
			registryURLs: []string{"file:///nonexistent/path1", "file:///nonexistent/path2"},
			wantErr:      true,
		},
		{
			name:         "single registry",
			registryURLs: []string{"https://bcr.bazel.build"},
			wantErr:      false,
			wantClients:  1,
		},
		{
			name:         "multiple registries",
			registryURLs: []string{"https://registry1.example.com", "https://bcr.bazel.build"},
			wantErr:      false,
			wantClients:  2,
		},
		{
			name:         "mixed valid and invalid file URLs",
			registryURLs: []string{"file:///nonexistent/path", "https://bcr.bazel.build"},
			wantErr:      false,
			wantClients:  1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chain, err := newRegistryChain(tt.registryURLs)

			if tt.wantErr {
				if err == nil {
					t.Errorf("newRegistryChain() error = nil, want error")
				}
				if chain != nil {
					t.Errorf("newRegistryChain() returned non-nil chain with error")
				}
				return
			}

			if err != nil {
				t.Fatalf("newRegistryChain() unexpected error: %v", err)
			}

			if chain == nil {
				t.Fatal("newRegistryChain() = nil, want non-nil")
			}

			if len(chain.clients) != tt.wantClients {
				t.Errorf("chain has %d clients, want %d", len(chain.clients), tt.wantClients)
			}
		})
	}
}

func TestRegistryChain_GetModuleFile_FirstRegistryMatch(t *testing.T) {
	reg1, reg2, reg3 := createMultiRegistryServers()
	defer reg1.Close()
	defer reg2.Close()
	defer reg3.Close()

	chain, err := newRegistryChain([]string{reg1.URL, reg2.URL, reg3.URL})
	if err != nil {
		t.Fatalf("newRegistryChain() error = %v", err)
	}
	ctx := context.Background()

	// Test: module_a is in registry1 (first registry)
	t.Run("module in first registry", func(t *testing.T) {
		info, err := chain.GetModuleFile(ctx, "module_a", "1.0.0")
		if err != nil {
			t.Fatalf("GetModuleFile() error = %v", err)
		}
		if info.Name != "module_a" {
			t.Errorf("got name %s, want module_a", info.Name)
		}
		if info.Version != "1.0.0" {
			t.Errorf("got version %s, want 1.0.0", info.Version)
		}

		// Verify registry mapping was stored
		registry := chain.GetRegistryForModule("module_a")
		if registry != reg1.URL {
			t.Errorf("module_a mapped to %s, want %s", registry, reg1.URL)
		}
	})

	// Test: module_c is in registry2 (second registry)
	t.Run("module in second registry", func(t *testing.T) {
		info, err := chain.GetModuleFile(ctx, "module_c", "1.5.0")
		if err != nil {
			t.Fatalf("GetModuleFile() error = %v", err)
		}
		if info.Name != "module_c" {
			t.Errorf("got name %s, want module_c", info.Name)
		}

		// Verify registry mapping
		registry := chain.GetRegistryForModule("module_c")
		if registry != reg2.URL {
			t.Errorf("module_c mapped to %s, want %s", registry, reg2.URL)
		}
	})

	// Test: module_d is in registry3 (third registry)
	t.Run("module in third registry", func(t *testing.T) {
		info, err := chain.GetModuleFile(ctx, "module_d", "3.0.0")
		if err != nil {
			t.Fatalf("GetModuleFile() error = %v", err)
		}
		if info.Name != "module_d" {
			t.Errorf("got name %s, want module_d", info.Name)
		}

		// Verify registry mapping
		registry := chain.GetRegistryForModule("module_d")
		if registry != reg3.URL {
			t.Errorf("module_d mapped to %s, want %s", registry, reg3.URL)
		}
	})
}

func TestRegistryChain_ModuleStickiness(t *testing.T) {
	// Test that once a module is found in a registry, all versions come from that registry
	reg1, reg2, _ := createMultiRegistryServers()
	defer reg1.Close()
	defer reg2.Close()

	chain, err := newRegistryChain([]string{reg1.URL, reg2.URL})
	if err != nil {
		t.Fatalf("newRegistryChain() error = %v", err)
	}
	ctx := context.Background()

	// First lookup of module_a@1.0.0 -> finds in reg1
	_, err = chain.GetModuleFile(ctx, "module_a", "1.0.0")
	if err != nil {
		t.Fatalf("First lookup failed: %v", err)
	}

	// Verify module_a is mapped to reg1
	if chain.GetRegistryForModule("module_a") != reg1.URL {
		t.Errorf("module_a should be mapped to registry1")
	}

	// Second lookup of module_a@2.0.0 should use reg1 (not search again)
	_, err = chain.GetModuleFile(ctx, "module_a", "2.0.0")
	if err != nil {
		t.Fatalf("Second lookup failed: %v", err)
	}

	// Verify it's still mapped to reg1
	if chain.GetRegistryForModule("module_a") != reg1.URL {
		t.Errorf("module_a should still be mapped to registry1 after second lookup")
	}
}

func TestRegistryChain_NotFound(t *testing.T) {
	reg1, reg2, reg3 := createMultiRegistryServers()
	defer reg1.Close()
	defer reg2.Close()
	defer reg3.Close()

	chain, err := newRegistryChain([]string{reg1.URL, reg2.URL, reg3.URL})
	if err != nil {
		t.Fatalf("newRegistryChain() error = %v", err)
	}
	ctx := context.Background()

	// Test: module that doesn't exist in any registry
	_, err = chain.GetModuleFile(ctx, "nonexistent_module", "1.0.0")
	if err == nil {
		t.Error("GetModuleFile() should return error for nonexistent module")
	}

	// Error message should mention multiple registries were checked
	errMsg := err.Error()
	if !strings.Contains(errMsg, "not found") {
		t.Errorf("error message should mention 'not found', got: %s", errMsg)
	}
}

func TestRegistryChain_GetModuleMetadata(t *testing.T) {
	reg1, reg2, _ := createMultiRegistryServers()
	defer reg1.Close()
	defer reg2.Close()

	chain, err := newRegistryChain([]string{reg1.URL, reg2.URL})
	if err != nil {
		t.Fatalf("newRegistryChain() error = %v", err)
	}
	ctx := context.Background()

	// First, fetch a module to establish mapping
	_, err = chain.GetModuleFile(ctx, "module_a", "1.0.0")
	if err != nil {
		t.Fatalf("GetModuleFile() error = %v", err)
	}

	// Now fetch metadata - should use the same registry
	metadata, err := chain.GetModuleMetadata(ctx, "module_a")
	if err != nil {
		t.Fatalf("GetModuleMetadata() error = %v", err)
	}

	if len(metadata.Versions) == 0 {
		t.Error("metadata should have versions")
	}
}

func TestRegistryChain_BaseURL(t *testing.T) {
	chain, err := newRegistryChain([]string{"https://reg1.example.com", "https://reg2.example.com"})
	if err != nil {
		t.Fatalf("newRegistryChain() error = %v", err)
	}

	// BaseURL should return the first registry
	if chain.BaseURL() != "https://reg1.example.com" {
		t.Errorf("BaseURL() = %s, want https://reg1.example.com", chain.BaseURL())
	}
}

func TestRegistryChain_GetRegistryForModule_NotFound(t *testing.T) {
	chain, err := newRegistryChain([]string{"https://reg1.example.com", "https://reg2.example.com"})
	if err != nil {
		t.Fatalf("newRegistryChain() error = %v", err)
	}

	// Should return empty string for modules not yet looked up
	if registry := chain.GetRegistryForModule("unknown_module"); registry != "" {
		t.Errorf("GetRegistryForModule(unknown_module) = %s, want empty string", registry)
	}
}

func TestRegistryInterface_Implementation(t *testing.T) {
	// Verify that both registryClient and registryChain implement Registry
	var _ Registry = (*registryClient)(nil)
	var _ Registry = (*registryChain)(nil)
}

func TestRegistryChain_ConcurrentAccess(t *testing.T) {
	// Test that concurrent access to the registry chain is safe
	reg1, reg2, _ := createMultiRegistryServers()
	defer reg1.Close()
	defer reg2.Close()

	chain, err := newRegistryChain([]string{reg1.URL, reg2.URL})
	if err != nil {
		t.Fatalf("newRegistryChain() error = %v", err)
	}
	ctx := context.Background()

	// Launch multiple concurrent requests
	done := make(chan error, 10)
	for range 10 {
		go func() {
			_, err := chain.GetModuleFile(ctx, "module_a", "1.0.0")
			done <- err
		}()
	}

	// Check that all requests succeeded
	for range 10 {
		if err := <-done; err != nil {
			t.Errorf("Concurrent request failed: %v", err)
		}
	}

	// Verify module_a is still correctly mapped
	if chain.GetRegistryForModule("module_a") != reg1.URL {
		t.Error("module_a should be mapped to registry1 after concurrent access")
	}
}

func TestRegistryChain_FallbackOnError(t *testing.T) {
	// Test that the chain falls back to the next registry on errors
	// This tests the fix for https://github.com/bazelbuild/bazel/issues/26442

	// Registry 1: Returns error for module_error
	errorRegistry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "module_error") {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer errorRegistry.Close()

	// Registry 2: Has module_error
	successRegistry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/modules/module_error/1.0.0/MODULE.bazel") {
			fmt.Fprint(w, `module(name = "module_error", version = "1.0.0")`)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer successRegistry.Close()

	chain, err := newRegistryChain([]string{errorRegistry.URL, successRegistry.URL})
	if err != nil {
		t.Fatalf("newRegistryChain() error = %v", err)
	}
	ctx := context.Background()

	// Should successfully get module from second registry despite first registry error
	info, err := chain.GetModuleFile(ctx, "module_error", "1.0.0")
	if err != nil {
		t.Fatalf("GetModuleFile() should succeed with fallback, got error: %v", err)
	}

	if info.Name != "module_error" {
		t.Errorf("got name %s, want module_error", info.Name)
	}

	// Verify it used the second registry
	if chain.GetRegistryForModule("module_error") != successRegistry.URL {
		t.Errorf("module_error should be mapped to second registry")
	}
}

func TestRegistryChain_CachedRegistryVersionMissFallsBack(t *testing.T) {
	// Registry 1 serves only module_x@2.0.0
	reg1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/modules/module_x/2.0.0/MODULE.bazel") {
			fmt.Fprint(w, `module(name = "module_x", version = "2.0.0")`)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer reg1.Close()

	// Registry 2 serves only module_x@1.0.0
	reg2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/modules/module_x/1.0.0/MODULE.bazel") {
			fmt.Fprint(w, `module(name = "module_x", version = "1.0.0")`)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer reg2.Close()

	chain, err := newRegistryChain([]string{reg1.URL, reg2.URL})
	if err != nil {
		t.Fatalf("newRegistryChain() error = %v", err)
	}
	ctx := context.Background()

	// First lookup caches module_x to registry 1.
	_, err = chain.GetModuleFile(ctx, "module_x", "2.0.0")
	if err != nil {
		t.Fatalf("first lookup failed: %v", err)
	}

	// Second lookup requests a version missing from cached registry.
	// Expected behavior: fallback to registry 2 rather than failing immediately.
	info, err := chain.GetModuleFile(ctx, "module_x", "1.0.0")
	if err != nil {
		t.Fatalf("GetModuleFile() should fallback on cached-registry miss, got error: %v", err)
	}
	if info.Name != "module_x" || info.Version != "1.0.0" {
		t.Fatalf("GetModuleFile() = %s@%s, want module_x@1.0.0", info.Name, info.Version)
	}
}
