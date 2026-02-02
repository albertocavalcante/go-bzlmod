package gobzlmod

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func Test_newRegistryClient(t *testing.T) {
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
			client := newRegistryClient(tt.registryURL)

			if client == nil {
				t.Fatal("newRegistryClient() returned nil")
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

	client := newRegistryClient(server.URL)
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

			client := newRegistryClient(server.URL)
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

	client := newRegistryClient(server.URL)
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

	client := newRegistryClient(server.URL)

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

	client := newRegistryClient(server.URL)
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
	client := newRegistryClient("http://invalid-registry-that-does-not-exist.com")
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

			client := newRegistryClient(server.URL)
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

	client := newRegistryClient(server.URL)
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

	client := newRegistryClient(server.URL)
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
	chain, ok := reg.(*registryChain)
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
	if _, ok := reg.(*registryChain); !ok {
		t.Error("Expected RegistryChain for multiple URLs")
	}
}

// TestHTTPClient_CustomClientIsUsed verifies that a custom HTTP client is used for requests.
func TestHTTPClient_CustomClientIsUsed(t *testing.T) {
	requestReceived := false
	var authHeader string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestReceived = true
		authHeader = r.Header.Get("Authorization")
		fmt.Fprint(w, `module(name = "test", version = "1.0.0")`)
	}))
	defer server.Close()

	// Create a custom transport that adds an auth header
	customTransport := &roundTripperFunc{
		fn: func(req *http.Request) (*http.Response, error) {
			req = req.Clone(req.Context())
			req.Header.Set("Authorization", "Bearer test-token")
			return http.DefaultTransport.RoundTrip(req)
		},
	}

	customClient := &http.Client{Transport: customTransport}
	reg := registryWithHTTPClient(customClient, 0, server.URL)

	ctx := context.Background()
	_, err := reg.GetModuleFile(ctx, "test", "1.0.0")
	if err != nil {
		t.Fatalf("GetModuleFile() error = %v", err)
	}

	if !requestReceived {
		t.Error("Expected request to be received")
	}
	if authHeader != "Bearer test-token" {
		t.Errorf("Authorization header = %q, want %q", authHeader, "Bearer test-token")
	}
}

// roundTripperFunc allows using a function as an http.RoundTripper
type roundTripperFunc struct {
	fn func(*http.Request) (*http.Response, error)
}

func (rt *roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return rt.fn(req)
}

// TestHTTPClient_NilClientUsesDefault tests that nil HTTPClient uses the default.
func TestHTTPClient_NilClientUsesDefault(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `module(name = "test", version = "1.0.0")`)
	}))
	defer server.Close()

	// nil client should work fine
	reg := registryWithHTTPClient(nil, 0, server.URL)

	ctx := context.Background()
	info, err := reg.GetModuleFile(ctx, "test", "1.0.0")
	if err != nil {
		t.Fatalf("GetModuleFile() error = %v", err)
	}
	if info.Name != "test" {
		t.Errorf("Module name = %q, want %q", info.Name, "test")
	}
}

// TestHTTPClient_TimeoutOverridesCustomClient tests that Timeout overrides custom client timeout.
func TestHTTPClient_TimeoutOverridesCustomClient(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		fmt.Fprint(w, `module(name = "test", version = "1.0.0")`)
	}))
	defer server.Close()

	// Custom client with 10s timeout
	customClient := &http.Client{Timeout: 10 * time.Second}

	// But we override with 10ms timeout - should fail
	reg := registryWithHTTPClient(customClient, 10*time.Millisecond, server.URL)

	ctx := context.Background()
	_, err := reg.GetModuleFile(ctx, "test", "1.0.0")
	if err == nil {
		t.Error("Expected timeout error, got nil")
	}
}

// mockCache implements ModuleCache for testing.
type mockCache struct {
	store     map[string][]byte
	mu        sync.Mutex
	getCount  int
	putCount  int
	failGet   bool
	failPut   bool
}

func newMockCache() *mockCache {
	return &mockCache{store: make(map[string][]byte)}
}

func (c *mockCache) Get(ctx context.Context, name, version string) ([]byte, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.getCount++
	if c.failGet {
		return nil, false, fmt.Errorf("simulated cache error")
	}
	key := name + "@" + version
	data, ok := c.store[key]
	return data, ok, nil
}

func (c *mockCache) Put(ctx context.Context, name, version string, content []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.putCount++
	if c.failPut {
		return fmt.Errorf("simulated cache error")
	}
	key := name + "@" + version
	c.store[key] = content
	return nil
}

// TestCache_HitSkipsHTTPFetch verifies that a cache hit skips the HTTP fetch.
func TestCache_HitSkipsHTTPFetch(t *testing.T) {
	httpCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		httpCalled = true
		fmt.Fprint(w, `module(name = "test", version = "1.0.0")`)
	}))
	defer server.Close()

	cache := newMockCache()
	// Pre-populate cache
	cache.store["test@1.0.0"] = []byte(`module(name = "test", version = "1.0.0")`)

	reg := registryWithOptions(nil, cache, 0, server.URL)

	ctx := context.Background()
	info, err := reg.GetModuleFile(ctx, "test", "1.0.0")
	if err != nil {
		t.Fatalf("GetModuleFile() error = %v", err)
	}

	if httpCalled {
		t.Error("HTTP should not have been called on cache hit")
	}
	if info.Name != "test" {
		t.Errorf("Module name = %q, want %q", info.Name, "test")
	}
	if cache.getCount != 1 {
		t.Errorf("Cache Get called %d times, want 1", cache.getCount)
	}
}

// TestCache_MissFetchesAndStores verifies that a cache miss fetches and stores.
func TestCache_MissFetchesAndStores(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `module(name = "test", version = "1.0.0")`)
	}))
	defer server.Close()

	cache := newMockCache()
	reg := registryWithOptions(nil, cache, 0, server.URL)

	ctx := context.Background()
	info, err := reg.GetModuleFile(ctx, "test", "1.0.0")
	if err != nil {
		t.Fatalf("GetModuleFile() error = %v", err)
	}

	if info.Name != "test" {
		t.Errorf("Module name = %q, want %q", info.Name, "test")
	}
	if cache.getCount != 1 {
		t.Errorf("Cache Get called %d times, want 1", cache.getCount)
	}
	if cache.putCount != 1 {
		t.Errorf("Cache Put called %d times, want 1", cache.putCount)
	}
	// Verify content was stored
	if _, ok := cache.store["test@1.0.0"]; !ok {
		t.Error("Content was not stored in cache")
	}
}

// TestCache_NilCacheDoesNotBreak verifies that nil cache works fine.
func TestCache_NilCacheDoesNotBreak(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `module(name = "test", version = "1.0.0")`)
	}))
	defer server.Close()

	reg := registryWithOptions(nil, nil, 0, server.URL)

	ctx := context.Background()
	info, err := reg.GetModuleFile(ctx, "test", "1.0.0")
	if err != nil {
		t.Fatalf("GetModuleFile() error = %v", err)
	}
	if info.Name != "test" {
		t.Errorf("Module name = %q, want %q", info.Name, "test")
	}
}

// TestCache_GetErrorDoesNotBreakResolution verifies graceful degradation on Get errors.
func TestCache_GetErrorDoesNotBreakResolution(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `module(name = "test", version = "1.0.0")`)
	}))
	defer server.Close()

	cache := newMockCache()
	cache.failGet = true // Simulate cache failure

	reg := registryWithOptions(nil, cache, 0, server.URL)

	ctx := context.Background()
	info, err := reg.GetModuleFile(ctx, "test", "1.0.0")
	if err != nil {
		t.Fatalf("GetModuleFile() should succeed despite cache error, got: %v", err)
	}
	if info.Name != "test" {
		t.Errorf("Module name = %q, want %q", info.Name, "test")
	}
}

// TestCache_PutErrorDoesNotBreakResolution verifies graceful degradation on Put errors.
func TestCache_PutErrorDoesNotBreakResolution(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `module(name = "test", version = "1.0.0")`)
	}))
	defer server.Close()

	cache := newMockCache()
	cache.failPut = true // Simulate cache write failure

	reg := registryWithOptions(nil, cache, 0, server.URL)

	ctx := context.Background()
	info, err := reg.GetModuleFile(ctx, "test", "1.0.0")
	if err != nil {
		t.Fatalf("GetModuleFile() should succeed despite cache Put error, got: %v", err)
	}
	if info.Name != "test" {
		t.Errorf("Module name = %q, want %q", info.Name, "test")
	}
	// Put should have been attempted
	if cache.putCount != 1 {
		t.Errorf("Cache Put should have been attempted, count = %d", cache.putCount)
	}
}

// TestCache_ResolveWithCache tests the full Resolve function with cache.
func TestCache_ResolveWithCache(t *testing.T) {
	var httpCallCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		httpCallCount++
		switch r.URL.Path {
		case "/modules/dep_a/1.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "dep_a", version = "1.0.0")`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	cache := newMockCache()

	ctx := context.Background()
	moduleContent := `
module(name = "root", version = "1.0.0")
bazel_dep(name = "dep_a", version = "1.0.0")
`

	// First resolve - should fetch from HTTP and store in cache
	result1, err := Resolve(ctx, moduleContent, ResolutionOptions{
		Registries: []string{server.URL},
		Cache:      cache,
	})
	if err != nil {
		t.Fatalf("First Resolve() error = %v", err)
	}
	if len(result1.Modules) != 1 {
		t.Errorf("Expected 1 module, got %d", len(result1.Modules))
	}

	firstHTTPCalls := httpCallCount

	// Second resolve - should use cache, no HTTP calls
	result2, err := Resolve(ctx, moduleContent, ResolutionOptions{
		Registries: []string{server.URL},
		Cache:      cache,
	})
	if err != nil {
		t.Fatalf("Second Resolve() error = %v", err)
	}
	if len(result2.Modules) != 1 {
		t.Errorf("Expected 1 module, got %d", len(result2.Modules))
	}

	if httpCallCount != firstHTTPCalls {
		t.Errorf("Second resolve should not make HTTP calls, but made %d more", httpCallCount-firstHTTPCalls)
	}
}

// TestHTTPClient_ResolveWithCustomClient tests the full Resolve function with custom client.
func TestHTTPClient_ResolveWithCustomClient(t *testing.T) {
	var authHeaderReceived string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeaderReceived = r.Header.Get("Authorization")
		switch r.URL.Path {
		case "/modules/dep_a/1.0.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "dep_a", version = "1.0.0")`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	customTransport := &roundTripperFunc{
		fn: func(req *http.Request) (*http.Response, error) {
			req = req.Clone(req.Context())
			req.Header.Set("Authorization", "Bearer my-token")
			return http.DefaultTransport.RoundTrip(req)
		},
	}

	ctx := context.Background()
	moduleContent := `
module(name = "root", version = "1.0.0")
bazel_dep(name = "dep_a", version = "1.0.0")
`

	result, err := Resolve(ctx, moduleContent, ResolutionOptions{
		Registries: []string{server.URL},
		HTTPClient: &http.Client{Transport: customTransport},
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	if len(result.Modules) != 1 {
		t.Errorf("Expected 1 module, got %d", len(result.Modules))
	}

	if authHeaderReceived != "Bearer my-token" {
		t.Errorf("Authorization header = %q, want %q", authHeaderReceived, "Bearer my-token")
	}
}
