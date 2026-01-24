package registry

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// =============================================================================
// Adversarial Tests for registry/client.go
// =============================================================================

// TestNewClient_BaseURL tests URL normalization
func TestNewClient_BaseURL(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"https://bcr.bazel.build", "https://bcr.bazel.build"},
		{"https://bcr.bazel.build/", "https://bcr.bazel.build"},
		{"https://bcr.bazel.build//", "https://bcr.bazel.build/"},
		{"http://localhost:8080", "http://localhost:8080"},
		{"http://localhost:8080/", "http://localhost:8080"},
	}

	for _, tt := range tests {
		c := NewClient(tt.input)
		if c.BaseURL() != tt.expected {
			t.Errorf("NewClient(%q).BaseURL() = %q, want %q", tt.input, c.BaseURL(), tt.expected)
		}
	}
}

// TestNewClient_EmptyURL tests empty URL handling
func TestNewClient_EmptyURL(t *testing.T) {
	c := NewClient("")
	if c.BaseURL() != "" {
		t.Errorf("NewClient(\"\").BaseURL() = %q, want empty", c.BaseURL())
	}
}

// TestNewClient_WithValidation tests validation option
func TestNewClient_WithValidation(t *testing.T) {
	c1 := NewClient("https://example.com")
	if !c1.validateResponses {
		t.Error("Default client should have validation enabled")
	}

	c2 := NewClient("https://example.com", WithValidation(false))
	if c2.validateResponses {
		t.Error("Client with WithValidation(false) should have validation disabled")
	}

	c3 := NewClient("https://example.com", WithValidation(true))
	if !c3.validateResponses {
		t.Error("Client with WithValidation(true) should have validation enabled")
	}
}

// TestNewClient_WithHTTPClient tests custom HTTP client option
func TestNewClient_WithHTTPClient(t *testing.T) {
	customClient := &http.Client{Timeout: 5 * time.Second}
	c := NewClient("https://example.com", WithHTTPClient(customClient))

	if c.client != customClient {
		t.Error("Client should use custom HTTP client")
	}
}

// TestGetMetadata_Success tests successful metadata fetch
func TestGetMetadata_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/modules/test_module/metadata.json" {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{
				"versions": ["1.0.0", "1.1.0"],
				"yanked_versions": {"0.9.0": "deprecated"}
			}`)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	c := NewClient(server.URL, WithValidation(false))
	ctx := context.Background()

	metadata, err := c.GetMetadata(ctx, "test_module")
	if err != nil {
		t.Fatalf("GetMetadata failed: %v", err)
	}

	if len(metadata.Versions) != 2 {
		t.Errorf("Expected 2 versions, got %d", len(metadata.Versions))
	}

	if !metadata.IsYanked("0.9.0") {
		t.Error("0.9.0 should be yanked")
	}
}

// TestGetMetadata_Caching tests that metadata is cached
func TestGetMetadata_Caching(t *testing.T) {
	callCount := int32(0)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"versions": ["1.0.0"]}`)
	}))
	defer server.Close()

	c := NewClient(server.URL, WithValidation(false))
	ctx := context.Background()

	// First call
	_, err := c.GetMetadata(ctx, "cached_module")
	if err != nil {
		t.Fatalf("First GetMetadata failed: %v", err)
	}

	// Second call (should use cache)
	_, err = c.GetMetadata(ctx, "cached_module")
	if err != nil {
		t.Fatalf("Second GetMetadata failed: %v", err)
	}

	if atomic.LoadInt32(&callCount) != 1 {
		t.Errorf("Expected 1 HTTP call (cached), got %d", callCount)
	}
}

// TestGetMetadata_ClearCache tests cache clearing
func TestGetMetadata_ClearCache(t *testing.T) {
	callCount := int32(0)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"versions": ["1.0.0"]}`)
	}))
	defer server.Close()

	c := NewClient(server.URL, WithValidation(false))
	ctx := context.Background()

	// First call
	_, _ = c.GetMetadata(ctx, "clear_cache_module")

	// Clear cache
	c.ClearCache()

	// Second call (should hit server again)
	_, _ = c.GetMetadata(ctx, "clear_cache_module")

	if atomic.LoadInt32(&callCount) != 2 {
		t.Errorf("Expected 2 HTTP calls after cache clear, got %d", callCount)
	}
}

// TestGetMetadata_NotFound tests 404 handling
func TestGetMetadata_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	c := NewClient(server.URL, WithValidation(false))
	ctx := context.Background()

	_, err := c.GetMetadata(ctx, "nonexistent")
	if err == nil {
		t.Error("Expected error for 404, got nil")
	}

	if !strings.Contains(err.Error(), "404") {
		t.Errorf("Error should mention 404: %v", err)
	}
}

// TestGetMetadata_ServerError tests 5xx handling
func TestGetMetadata_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	c := NewClient(server.URL, WithValidation(false))
	ctx := context.Background()

	_, err := c.GetMetadata(ctx, "error_module")
	if err == nil {
		t.Error("Expected error for 500, got nil")
	}
}

// TestGetMetadata_InvalidJSON tests malformed JSON handling
func TestGetMetadata_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{invalid json`)
	}))
	defer server.Close()

	c := NewClient(server.URL, WithValidation(false))
	ctx := context.Background()

	_, err := c.GetMetadata(ctx, "invalid_json")
	if err == nil {
		t.Error("Expected error for invalid JSON, got nil")
	}
}

// TestGetMetadata_EmptyResponse tests empty response handling
func TestGetMetadata_EmptyResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, ``)
	}))
	defer server.Close()

	c := NewClient(server.URL, WithValidation(false))
	ctx := context.Background()

	_, err := c.GetMetadata(ctx, "empty_response")
	if err == nil {
		t.Error("Expected error for empty response, got nil")
	}
}

// TestGetMetadata_ContextCancellation tests context cancellation
func TestGetMetadata_ContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"versions": ["1.0.0"]}`)
	}))
	defer server.Close()

	c := NewClient(server.URL, WithValidation(false))
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := c.GetMetadata(ctx, "cancelled")
	if err == nil {
		t.Error("Expected error for cancelled context")
	}
}

// TestGetMetadata_ContextTimeout tests context timeout
func TestGetMetadata_ContextTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"versions": ["1.0.0"]}`)
	}))
	defer server.Close()

	c := NewClient(server.URL, WithValidation(false))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	_, err := c.GetMetadata(ctx, "timeout")
	if err == nil {
		t.Error("Expected error for timeout")
	}
}

// TestGetSource_Success tests successful source fetch
func TestGetSource_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/modules/test_module/1.0.0/source.json" {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{
				"url": "https://example.com/archive.zip",
				"integrity": "sha256-abc123",
				"strip_prefix": "test_module-1.0.0"
			}`)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	c := NewClient(server.URL, WithValidation(false))
	ctx := context.Background()

	source, err := c.GetSource(ctx, "test_module", "1.0.0")
	if err != nil {
		t.Fatalf("GetSource failed: %v", err)
	}

	if source.URL != "https://example.com/archive.zip" {
		t.Errorf("URL = %q, want https://example.com/archive.zip", source.URL)
	}

	if !source.IsArchive() {
		t.Error("Expected archive type")
	}
}

// TestGetSource_Caching tests that source is cached
func TestGetSource_Caching(t *testing.T) {
	callCount := int32(0)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"url": "https://example.com/archive.zip", "integrity": "sha256-abc"}`)
	}))
	defer server.Close()

	c := NewClient(server.URL, WithValidation(false))
	ctx := context.Background()

	// First call
	_, _ = c.GetSource(ctx, "cached_source", "1.0.0")

	// Second call (should use cache)
	_, _ = c.GetSource(ctx, "cached_source", "1.0.0")

	if atomic.LoadInt32(&callCount) != 1 {
		t.Errorf("Expected 1 HTTP call (cached), got %d", callCount)
	}
}

// TestGetSource_DifferentVersionsNotCached tests different versions are fetched separately
func TestGetSource_DifferentVersionsNotCached(t *testing.T) {
	callCount := int32(0)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"url": "https://example.com/archive.zip", "integrity": "sha256-abc"}`)
	}))
	defer server.Close()

	c := NewClient(server.URL, WithValidation(false))
	ctx := context.Background()

	_, _ = c.GetSource(ctx, "multi_version", "1.0.0")
	_, _ = c.GetSource(ctx, "multi_version", "2.0.0")

	if atomic.LoadInt32(&callCount) != 2 {
		t.Errorf("Expected 2 HTTP calls for different versions, got %d", callCount)
	}
}

// TestGetModuleFile_Success tests fetching MODULE.bazel content
func TestGetModuleFile_Success(t *testing.T) {
	expectedContent := `module(name = "test", version = "1.0.0")`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/modules/test/1.0.0/MODULE.bazel" {
			fmt.Fprint(w, expectedContent)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	c := NewClient(server.URL)
	ctx := context.Background()

	content, err := c.GetModuleFile(ctx, "test", "1.0.0")
	if err != nil {
		t.Fatalf("GetModuleFile failed: %v", err)
	}

	if string(content) != expectedContent {
		t.Errorf("Content = %q, want %q", string(content), expectedContent)
	}
}

// TestGetModuleFile_NotFound tests 404 for MODULE.bazel
func TestGetModuleFile_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	c := NewClient(server.URL)
	ctx := context.Background()

	_, err := c.GetModuleFile(ctx, "nonexistent", "1.0.0")
	if err == nil {
		t.Error("Expected error for 404")
	}
}

// TestGetRegistryConfig_Success tests fetching bazel_registry.json
func TestGetRegistryConfig_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/bazel_registry.json" {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"mirrors": ["https://mirror.example.com"]}`)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	c := NewClient(server.URL)
	ctx := context.Background()

	config, err := c.GetRegistryConfig(ctx)
	if err != nil {
		t.Fatalf("GetRegistryConfig failed: %v", err)
	}

	if len(config.Mirrors) != 1 {
		t.Errorf("Expected 1 mirror, got %d", len(config.Mirrors))
	}
}

// TestGetModuleVersion_Success tests combined metadata and source fetch
func TestGetModuleVersion_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/modules/combined/metadata.json":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"versions": ["1.0.0", "2.0.0"]}`)
		case "/modules/combined/1.0.0/source.json":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"url": "https://example.com/1.0.0.zip", "integrity": "sha256-abc"}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	c := NewClient(server.URL, WithValidation(false))
	ctx := context.Background()

	info, err := c.GetModuleVersion(ctx, "combined", "1.0.0")
	if err != nil {
		t.Fatalf("GetModuleVersion failed: %v", err)
	}

	if info.Name != "combined" {
		t.Errorf("Name = %q, want combined", info.Name)
	}
	if info.Version != "1.0.0" {
		t.Errorf("Version = %q, want 1.0.0", info.Version)
	}
	if info.Metadata == nil {
		t.Error("Metadata should not be nil")
	}
	if info.Source == nil {
		t.Error("Source should not be nil")
	}
}

// TestGetModuleVersion_VersionNotFound tests non-existent version
func TestGetModuleVersion_VersionNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/modules/version_check/metadata.json" {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"versions": ["1.0.0"]}`)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	c := NewClient(server.URL, WithValidation(false))
	ctx := context.Background()

	_, err := c.GetModuleVersion(ctx, "version_check", "2.0.0")
	if err == nil {
		t.Error("Expected error for non-existent version")
	}

	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("Error should mention 'not found': %v", err)
	}
}

// TestConcurrentAccess tests thread safety
func TestConcurrentAccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "metadata.json") {
			fmt.Fprint(w, `{"versions": ["1.0.0"]}`)
		} else {
			fmt.Fprint(w, `{"url": "https://example.com/a.zip", "integrity": "sha256-abc"}`)
		}
	}))
	defer server.Close()

	c := NewClient(server.URL, WithValidation(false))
	ctx := context.Background()

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			moduleName := fmt.Sprintf("module_%d", i%10)
			_, _ = c.GetMetadata(ctx, moduleName)
			_, _ = c.GetSource(ctx, moduleName, "1.0.0")
		}(i)
	}
	wg.Wait()
}

// TestSpecialCharactersInModuleName tests module names with special chars
func TestSpecialCharactersInModuleName(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Log the path for debugging
		t.Logf("Request path: %s", r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	c := NewClient(server.URL, WithValidation(false))
	ctx := context.Background()

	// These should not panic
	testNames := []string{
		"module-with-dash",
		"module_with_underscore",
		"module.with.dots",
		"UPPERCASE",
		"MixedCase",
	}

	for _, name := range testNames {
		_, err := c.GetMetadata(ctx, name)
		// We expect 404, not panic
		if err == nil {
			t.Errorf("Expected error for module %q", name)
		}
	}
}

// TestSpecialCharactersInVersion tests version strings with special chars
func TestSpecialCharactersInVersion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	c := NewClient(server.URL, WithValidation(false))
	ctx := context.Background()

	testVersions := []string{
		"1.0.0",
		"1.0.0-alpha",
		"1.0.0-beta.1",
		"1.0.0+build",
		"1.0.0-alpha+build",
		"0.0.0",
	}

	for _, version := range testVersions {
		_, err := c.GetSource(ctx, "test", version)
		// We expect 404, not panic
		if err == nil {
			t.Errorf("Expected error for version %q", version)
		}
	}
}

// TestLargeResponse tests handling of large responses
func TestLargeResponse(t *testing.T) {
	// Generate a large list of versions
	var versions []string
	for i := 0; i < 1000; i++ {
		versions = append(versions, fmt.Sprintf("%d.0.0", i))
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"versions": [`)
		for i, v := range versions {
			if i > 0 {
				fmt.Fprint(w, ",")
			}
			fmt.Fprintf(w, `"%s"`, v)
		}
		fmt.Fprint(w, `]}`)
	}))
	defer server.Close()

	c := NewClient(server.URL, WithValidation(false))
	ctx := context.Background()

	metadata, err := c.GetMetadata(ctx, "large_module")
	if err != nil {
		t.Fatalf("GetMetadata failed: %v", err)
	}

	if len(metadata.Versions) != 1000 {
		t.Errorf("Expected 1000 versions, got %d", len(metadata.Versions))
	}
}

// TestHTTPRedirect tests handling of HTTP redirects
func TestHTTPRedirect(t *testing.T) {
	finalServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"versions": ["1.0.0"]}`)
	}))
	defer finalServer.Close()

	redirectServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, finalServer.URL+r.URL.Path, http.StatusMovedPermanently)
	}))
	defer redirectServer.Close()

	c := NewClient(redirectServer.URL, WithValidation(false))
	ctx := context.Background()

	metadata, err := c.GetMetadata(ctx, "redirect_test")
	if err != nil {
		t.Fatalf("GetMetadata failed with redirect: %v", err)
	}

	if len(metadata.Versions) != 1 {
		t.Errorf("Expected 1 version after redirect, got %d", len(metadata.Versions))
	}
}

// TestSlowServer tests handling of slow responses (without timeout)
func TestSlowServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"versions": ["1.0.0"]}`)
	}))
	defer server.Close()

	// Use a custom client with longer timeout
	customClient := &http.Client{Timeout: 5 * time.Second}
	c := NewClient(server.URL, WithValidation(false), WithHTTPClient(customClient))
	ctx := context.Background()

	metadata, err := c.GetMetadata(ctx, "slow_module")
	if err != nil {
		t.Fatalf("GetMetadata failed for slow server: %v", err)
	}

	if len(metadata.Versions) != 1 {
		t.Errorf("Expected 1 version, got %d", len(metadata.Versions))
	}
}

// TestGitRepositorySource tests git_repository source type
func TestGitRepositorySource(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"type": "git_repository",
			"remote": "https://github.com/example/repo.git",
			"commit": "abc123def456789",
			"init_submodules": true
		}`)
	}))
	defer server.Close()

	c := NewClient(server.URL, WithValidation(false))
	ctx := context.Background()

	source, err := c.GetSource(ctx, "git_module", "1.0.0")
	if err != nil {
		t.Fatalf("GetSource failed: %v", err)
	}

	if !source.IsGitRepository() {
		t.Error("Expected git_repository type")
	}

	if source.Remote != "https://github.com/example/repo.git" {
		t.Errorf("Remote = %q, want https://github.com/example/repo.git", source.Remote)
	}

	if source.Commit != "abc123def456789" {
		t.Errorf("Commit = %q, want abc123def456789", source.Commit)
	}
}

// TestSourceWithPatches tests source with patches
func TestSourceWithPatches(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"url": "https://example.com/archive.zip",
			"integrity": "sha256-abc123",
			"patches": {
				"fix1.patch": "sha256-patch1",
				"fix2.patch": "sha256-patch2"
			},
			"patch_strip": 1
		}`)
	}))
	defer server.Close()

	c := NewClient(server.URL, WithValidation(false))
	ctx := context.Background()

	source, err := c.GetSource(ctx, "patched_module", "1.0.0")
	if err != nil {
		t.Fatalf("GetSource failed: %v", err)
	}

	if len(source.Patches) != 2 {
		t.Errorf("Expected 2 patches, got %d", len(source.Patches))
	}

	if source.PatchStrip != 1 {
		t.Errorf("PatchStrip = %d, want 1", source.PatchStrip)
	}
}

// =============================================================================
// HTTP Timeout Configuration Tests (TDD)
// =============================================================================

// TestNewClient_DefaultTimeout verifies default timeout is 15 seconds when not specified
func TestNewClient_DefaultTimeout(t *testing.T) {
	c := NewClient("https://example.com")

	if c.client.Timeout != DefaultRequestTimeout {
		t.Errorf("Default timeout = %v, want %v", c.client.Timeout, DefaultRequestTimeout)
	}

	if c.client.Timeout != 15*time.Second {
		t.Errorf("Default timeout = %v, want 15s", c.client.Timeout)
	}
}

// TestNewClient_WithTimeout_CustomValue verifies custom timeout can be set via options
func TestNewClient_WithTimeout_CustomValue(t *testing.T) {
	customTimeout := 30 * time.Second
	c := NewClient("https://example.com", WithTimeout(customTimeout))

	if c.client.Timeout != customTimeout {
		t.Errorf("Timeout = %v, want %v", c.client.Timeout, customTimeout)
	}
}

// TestNewClient_WithTimeout_Zero verifies zero timeout uses default
func TestNewClient_WithTimeout_Zero(t *testing.T) {
	c := NewClient("https://example.com", WithTimeout(0))

	// Zero timeout should fall back to default
	if c.client.Timeout != DefaultRequestTimeout {
		t.Errorf("Zero timeout should use default: got %v, want %v", c.client.Timeout, DefaultRequestTimeout)
	}
}

// TestNewClient_WithTimeout_Negative verifies negative timeout uses default
func TestNewClient_WithTimeout_Negative(t *testing.T) {
	c := NewClient("https://example.com", WithTimeout(-5*time.Second))

	// Negative timeout should fall back to default
	if c.client.Timeout != DefaultRequestTimeout {
		t.Errorf("Negative timeout should use default: got %v, want %v", c.client.Timeout, DefaultRequestTimeout)
	}
}

// TestNewClient_WithTimeout_VeryShort verifies very short timeout is respected
func TestNewClient_WithTimeout_VeryShort(t *testing.T) {
	shortTimeout := 10 * time.Millisecond
	c := NewClient("https://example.com", WithTimeout(shortTimeout))

	if c.client.Timeout != shortTimeout {
		t.Errorf("Timeout = %v, want %v", c.client.Timeout, shortTimeout)
	}
}

// TestNewClient_WithTimeout_VeryLong verifies very long timeout is respected
func TestNewClient_WithTimeout_VeryLong(t *testing.T) {
	longTimeout := 5 * time.Minute
	c := NewClient("https://example.com", WithTimeout(longTimeout))

	if c.client.Timeout != longTimeout {
		t.Errorf("Timeout = %v, want %v", c.client.Timeout, longTimeout)
	}
}

// TestGetMetadata_TimeoutActuallyRespected tests timeout is actually enforced with slow server
func TestGetMetadata_TimeoutActuallyRespected(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Server delays for 200ms
		time.Sleep(200 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"versions": ["1.0.0"]}`)
	}))
	defer server.Close()

	// Create client with 50ms timeout (shorter than server delay)
	c := NewClient(server.URL, WithValidation(false), WithTimeout(50*time.Millisecond))
	ctx := context.Background()

	start := time.Now()
	_, err := c.GetMetadata(ctx, "timeout_test")
	elapsed := time.Since(start)

	if err == nil {
		t.Error("Expected timeout error, got nil")
	}

	// Verify timeout happened quickly (within reasonable margin)
	// Should timeout around 50ms, not wait for the full 200ms
	if elapsed > 150*time.Millisecond {
		t.Errorf("Timeout took too long: %v (should be around 50ms, not 200ms)", elapsed)
	}
}

// TestGetSource_TimeoutActuallyRespected tests timeout for source.json requests
func TestGetSource_TimeoutActuallyRespected(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"url": "https://example.com/archive.zip", "integrity": "sha256-abc"}`)
	}))
	defer server.Close()

	c := NewClient(server.URL, WithValidation(false), WithTimeout(50*time.Millisecond))
	ctx := context.Background()

	start := time.Now()
	_, err := c.GetSource(ctx, "timeout_source", "1.0.0")
	elapsed := time.Since(start)

	if err == nil {
		t.Error("Expected timeout error, got nil")
	}

	if elapsed > 150*time.Millisecond {
		t.Errorf("Timeout took too long: %v (should be around 50ms)", elapsed)
	}
}

// TestGetModuleFile_TimeoutActuallyRespected tests timeout for MODULE.bazel requests
func TestGetModuleFile_TimeoutActuallyRespected(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		fmt.Fprint(w, `module(name = "test", version = "1.0.0")`)
	}))
	defer server.Close()

	c := NewClient(server.URL, WithTimeout(50*time.Millisecond))
	ctx := context.Background()

	start := time.Now()
	_, err := c.GetModuleFile(ctx, "timeout_module", "1.0.0")
	elapsed := time.Since(start)

	if err == nil {
		t.Error("Expected timeout error, got nil")
	}

	if elapsed > 150*time.Millisecond {
		t.Errorf("Timeout took too long: %v (should be around 50ms)", elapsed)
	}
}

// TestTimeout_LongerThanDelay verifies requests succeed when timeout is longer than delay
func TestTimeout_LongerThanDelay(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"versions": ["1.0.0"]}`)
	}))
	defer server.Close()

	// Timeout is longer than server delay, should succeed
	c := NewClient(server.URL, WithValidation(false), WithTimeout(200*time.Millisecond))
	ctx := context.Background()

	metadata, err := c.GetMetadata(ctx, "slow_but_succeeds")
	if err != nil {
		t.Fatalf("Request should succeed with sufficient timeout: %v", err)
	}

	if len(metadata.Versions) != 1 {
		t.Errorf("Expected 1 version, got %d", len(metadata.Versions))
	}
}

// TestTimeout_MultipleOptions verifies WithTimeout can be combined with other options
func TestTimeout_MultipleOptions(t *testing.T) {
	customClient := &http.Client{Timeout: 1 * time.Second}
	c := NewClient("https://example.com",
		WithValidation(false),
		WithTimeout(25*time.Second),
		WithHTTPClient(customClient))

	// WithHTTPClient should override timeout settings
	if c.client.Timeout != 1*time.Second {
		t.Errorf("WithHTTPClient should take precedence: got timeout %v", c.client.Timeout)
	}
}

// TestTimeout_OptionsOrder verifies option application order
func TestTimeout_OptionsOrder(t *testing.T) {
	// When WithTimeout comes before WithHTTPClient, custom client should win
	c1 := NewClient("https://example.com",
		WithTimeout(25*time.Second),
		WithHTTPClient(&http.Client{Timeout: 1 * time.Second}))

	if c1.client.Timeout != 1*time.Second {
		t.Errorf("Last option (WithHTTPClient) should win: got %v", c1.client.Timeout)
	}

	// When WithHTTPClient comes before WithTimeout, timeout should win
	c2 := NewClient("https://example.com",
		WithHTTPClient(&http.Client{Timeout: 1 * time.Second}),
		WithTimeout(25*time.Second))

	if c2.client.Timeout != 25*time.Second {
		t.Errorf("Last option (WithTimeout) should win: got %v", c2.client.Timeout)
	}
}
