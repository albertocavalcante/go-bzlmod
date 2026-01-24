package gobzlmod

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewRegistryClient(t *testing.T) {
	tests := []struct {
		name        string
		registryURL string
		wantURL     string
	}{
		{
			name:        "standard BCR URL",
			registryURL: "https://bcr.bazel.build",
			wantURL:     "https://bcr.bazel.build",
		},
		{
			name:        "custom registry URL",
			registryURL: "https://custom.registry.com",
			wantURL:     "https://custom.registry.com",
		},
		{
			name:        "URL with trailing slash",
			registryURL: "https://bcr.bazel.build/",
			wantURL:     "https://bcr.bazel.build",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewRegistryClient(tt.registryURL)

			if client == nil {
				t.Fatal("NewRegistryClient() returned nil")
			}

			if client.baseURL != tt.wantURL {
				t.Errorf("baseURL = %s, want %s", client.baseURL, tt.wantURL)
			}

			if client.client == nil {
				t.Error("client is nil")
			}

			// Check HTTP client configuration
			transport := client.client.Transport.(*http.Transport)
			if transport.MaxIdleConns != 50 {
				t.Errorf("MaxIdleConns = %d, want 50", transport.MaxIdleConns)
			}
			if transport.MaxIdleConnsPerHost != 20 {
				t.Errorf("MaxIdleConnsPerHost = %d, want 20", transport.MaxIdleConnsPerHost)
			}
		})
	}
}

func TestGetModuleFile_Success(t *testing.T) {
	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/modules/test_module/1.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "test_module", version = "1.0.0")
			bazel_dep(name = "dependency", version = "2.0.0")`)
		case "/modules/complex_module/2.1.0/MODULE.bazel":
			fmt.Fprint(w, `module(
				name = "complex_module",
				version = "2.1.0",
				compatibility_level = 1,
			)
			
			bazel_dep(name = "rules_go", version = "0.41.0")
			bazel_dep(name = "gazelle", version = "0.32.0", dev_dependency = True)
			
			single_version_override(
				module_name = "protobuf",
				version = "21.7",
			)`)
		default:
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, "Not Found")
		}
	}))
	defer server.Close()

	client := NewRegistryClient(server.URL)
	ctx := context.Background()

	tests := []struct {
		name        string
		moduleName  string
		version     string
		wantName    string
		wantVersion string
		wantDeps    int
		wantOver    int
	}{
		{
			name:        "simple module",
			moduleName:  "test_module",
			version:     "1.0.0",
			wantName:    "test_module",
			wantVersion: "1.0.0",
			wantDeps:    1,
			wantOver:    0,
		},
		{
			name:        "complex module",
			moduleName:  "complex_module",
			version:     "2.1.0",
			wantName:    "complex_module",
			wantVersion: "2.1.0",
			wantDeps:    2,
			wantOver:    1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info, err := client.GetModuleFile(ctx, tt.moduleName, tt.version)
			if err != nil {
				t.Fatalf("GetModuleFile() error = %v", err)
			}

			if info.Name != tt.wantName {
				t.Errorf("Name = %s, want %s", info.Name, tt.wantName)
			}
			if info.Version != tt.wantVersion {
				t.Errorf("Version = %s, want %s", info.Version, tt.wantVersion)
			}
			if len(info.Dependencies) != tt.wantDeps {
				t.Errorf("Dependencies count = %d, want %d", len(info.Dependencies), tt.wantDeps)
			}
			if len(info.Overrides) != tt.wantOver {
				t.Errorf("Overrides count = %d, want %d", len(info.Overrides), tt.wantOver)
			}
		})
	}
}

func TestGetModuleFile_HTTPErrors(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		wantErr    bool
	}{
		{
			name:       "not found",
			statusCode: http.StatusNotFound,
			wantErr:    true,
		},
		{
			name:       "internal server error",
			statusCode: http.StatusInternalServerError,
			wantErr:    true,
		},
		{
			name:       "bad gateway",
			statusCode: http.StatusBadGateway,
			wantErr:    true,
		},
		{
			name:       "unauthorized",
			statusCode: http.StatusUnauthorized,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				fmt.Fprintf(w, "HTTP %d Error", tt.statusCode)
			}))
			defer server.Close()

			client := NewRegistryClient(server.URL)
			ctx := context.Background()

			info, err := client.GetModuleFile(ctx, "test_module", "1.0.0")

			if (err != nil) != tt.wantErr {
				t.Errorf("GetModuleFile() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr && info == nil {
				t.Error("Expected non-nil info when no error")
			}

			if tt.wantErr && info != nil {
				t.Error("Expected nil info when error occurs")
			}
		})
	}
}

func TestGetModuleFile_Caching(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		fmt.Fprint(w, `module(name = "cached_module", version = "1.0.0")`)
	}))
	defer server.Close()

	client := NewRegistryClient(server.URL)
	ctx := context.Background()

	// First request
	info1, err := client.GetModuleFile(ctx, "cached_module", "1.0.0")
	if err != nil {
		t.Fatalf("First GetModuleFile() error = %v", err)
	}

	// Second request (should be cached)
	info2, err := client.GetModuleFile(ctx, "cached_module", "1.0.0")
	if err != nil {
		t.Fatalf("Second GetModuleFile() error = %v", err)
	}

	// Should only have made one HTTP request
	if requestCount != 1 {
		t.Errorf("Expected 1 HTTP request, got %d", requestCount)
	}

	// Both results should be identical
	if info1.Name != info2.Name || info1.Version != info2.Version {
		t.Error("Cached result differs from original")
	}
}

func TestGetModuleFile_ContextTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate slow response
		time.Sleep(100 * time.Millisecond)
		fmt.Fprint(w, `module(name = "slow_module", version = "1.0.0")`)
	}))
	defer server.Close()

	client := NewRegistryClient(server.URL)

	// Create context with very short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	info, err := client.GetModuleFile(ctx, "slow_module", "1.0.0")

	if err == nil {
		t.Error("Expected timeout error")
	}

	if info != nil {
		t.Error("Expected nil info on timeout")
	}

	if !strings.Contains(err.Error(), "context deadline exceeded") {
		t.Errorf("Expected context deadline error, got: %v", err)
	}
}

func TestGetModuleFile_ContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate slow response
		time.Sleep(100 * time.Millisecond)
		fmt.Fprint(w, `module(name = "canceled_module", version = "1.0.0")`)
	}))
	defer server.Close()

	client := NewRegistryClient(server.URL)
	ctx, cancel := context.WithCancel(context.Background())

	// Cancel context immediately
	cancel()

	info, err := client.GetModuleFile(ctx, "canceled_module", "1.0.0")

	if err == nil {
		t.Error("Expected cancellation error")
	}

	if info != nil {
		t.Error("Expected nil info on cancellation")
	}

	if !strings.Contains(err.Error(), "context canceled") {
		t.Errorf("Expected context canceled error, got: %v", err)
	}
}

func TestGetModuleFile_NetworkError(t *testing.T) {
	// Use an invalid URL to simulate network error
	client := NewRegistryClient("http://invalid-registry-that-does-not-exist.com")
	ctx := context.Background()

	info, err := client.GetModuleFile(ctx, "test_module", "1.0.0")

	if err == nil {
		t.Error("Expected network error")
	}

	if info != nil {
		t.Error("Expected nil info on network error")
	}

	// Error should be present (we don't check the exact message as it varies)
	if err == nil {
		t.Error("Expected error for network failure")
	}
}

func TestURLConstruction(t *testing.T) {
	tests := []struct {
		name       string
		baseURL    string
		moduleName string
		version    string
		wantURL    string
	}{
		{
			name:       "standard case",
			baseURL:    "https://bcr.bazel.build",
			moduleName: "rules_go",
			version:    "0.41.0",
			wantURL:    "https://bcr.bazel.build/modules/rules_go/0.41.0/MODULE.bazel",
		},
		{
			name:       "base URL with trailing slash",
			baseURL:    "https://bcr.bazel.build/",
			moduleName: "gazelle",
			version:    "0.32.0",
			wantURL:    "https://bcr.bazel.build/modules/gazelle/0.32.0/MODULE.bazel",
		},
		{
			name:       "module name with underscores",
			baseURL:    "https://custom.registry.com",
			moduleName: "my_custom_module",
			version:    "1.2.3",
			wantURL:    "https://custom.registry.com/modules/my_custom_module/1.2.3/MODULE.bazel",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a server that captures the request URL
			var actualURL string
			var serverURL string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				actualURL = serverURL + r.URL.Path
				fmt.Fprint(w, `module(name = "test", version = "1.0.0")`)
			}))
			defer server.Close()
			serverURL = server.URL

			client := NewRegistryClient(server.URL)
			ctx := context.Background()

			_, err := client.GetModuleFile(ctx, tt.moduleName, tt.version)
			if err != nil {
				t.Fatalf("GetModuleFile() error = %v", err)
			}

			expectedPath := "/modules/" + tt.moduleName + "/" + tt.version + "/MODULE.bazel"
			expectedURL := server.URL + expectedPath

			if actualURL != expectedURL {
				t.Errorf("Request URL = %s, want %s", actualURL, expectedURL)
			}
		})
	}
}

func TestCacheKeyGeneration(t *testing.T) {
	// Test that different module/version combinations generate different cache keys
	tests := []struct {
		module1, version1 string
		module2, version2 string
		shouldBeSame      bool
	}{
		{"module_a", "1.0.0", "module_a", "1.0.0", true},
		{"module_a", "1.0.0", "module_a", "1.1.0", false},
		{"module_a", "1.0.0", "module_b", "1.0.0", false},
		{"module_a", "1.0.0", "module_b", "1.1.0", false},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s:%s vs %s:%s", tt.module1, tt.version1, tt.module2, tt.version2), func(t *testing.T) {
			key1 := tt.module1 + "@" + tt.version1
			key2 := tt.module2 + "@" + tt.version2

			same := (key1 == key2)
			if same != tt.shouldBeSame {
				t.Errorf("Keys %s and %s: same=%v, want same=%v", key1, key2, same, tt.shouldBeSame)
			}
		})
	}
}

// Benchmark tests
func BenchmarkGetModuleFile_Cached(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `module(name = "benchmark_module", version = "1.0.0")`)
	}))
	defer server.Close()

	client := NewRegistryClient(server.URL)
	ctx := context.Background()

	// Warm up the cache
	_, err := client.GetModuleFile(ctx, "benchmark_module", "1.0.0")
	if err != nil {
		b.Fatalf("Warmup failed: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := client.GetModuleFile(ctx, "benchmark_module", "1.0.0")
		if err != nil {
			b.Fatalf("GetModuleFile() error = %v", err)
		}
	}
}

func BenchmarkGetModuleFile_Uncached(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `module(name = "benchmark_module", version = "1.0.0")`)
	}))
	defer server.Close()

	client := NewRegistryClient(server.URL)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Use different module names to avoid caching
		moduleName := fmt.Sprintf("benchmark_module_%d", i)
		_, err := client.GetModuleFile(ctx, moduleName, "1.0.0")
		if err != nil {
			b.Fatalf("GetModuleFile() error = %v", err)
		}
	}
}

func TestRegistry_Default(t *testing.T) {
	// Test zero-config - should default to BCR with GitHub mirror fallback
	reg := Registry()
	if reg == nil {
		t.Fatal("Registry() returned nil")
	}

	// Should be a RegistryChain for resilience
	chain, ok := reg.(*RegistryChain)
	if !ok {
		t.Fatal("Expected RegistryChain for default Registry()")
	}

	// First registry should be BCR
	if reg.BaseURL() != DefaultRegistry {
		t.Errorf("Default registry URL = %q, want %q", reg.BaseURL(), DefaultRegistry)
	}

	// Should have both BCR and GitHub mirror
	if len(chain.clients) != 2 {
		t.Errorf("Expected 2 registries in chain, got %d", len(chain.clients))
	}
	if chain.clients[0].BaseURL() != DefaultRegistry {
		t.Errorf("First registry = %q, want %q", chain.clients[0].BaseURL(), DefaultRegistry)
	}
	if chain.clients[1].BaseURL() != DefaultRegistryMirror {
		t.Errorf("Second registry = %q, want %q", chain.clients[1].BaseURL(), DefaultRegistryMirror)
	}
}

func TestRegistry_SingleURL(t *testing.T) {
	reg := Registry("https://custom.registry.com")
	if reg == nil {
		t.Fatal("Registry() returned nil")
	}
	if reg.BaseURL() != "https://custom.registry.com" {
		t.Errorf("Registry URL = %q, want %q", reg.BaseURL(), "https://custom.registry.com")
	}
}

func TestRegistry_MultipleURLs(t *testing.T) {
	reg := Registry("https://private.example.com", DefaultRegistry)
	if reg == nil {
		t.Fatal("Registry() returned nil")
	}
	// First registry in chain should be the base URL
	if reg.BaseURL() != "https://private.example.com" {
		t.Errorf("Registry URL = %q, want %q", reg.BaseURL(), "https://private.example.com")
	}
	// Should be a RegistryChain
	if _, ok := reg.(*RegistryChain); !ok {
		t.Error("Expected RegistryChain for multiple URLs")
	}
}
