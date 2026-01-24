package gobzlmod

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalRegistry_GetModuleFile(t *testing.T) {
	// Create a temporary local registry
	tmpDir := t.TempDir()
	modulePath := filepath.Join(tmpDir, "modules", "example", "1.0.0")
	if err := os.MkdirAll(modulePath, 0755); err != nil {
		t.Fatal(err)
	}

	moduleContent := `module(name = "example", version = "1.0.0")
bazel_dep(name = "other", version = "2.0.0")
`
	if err := os.WriteFile(filepath.Join(modulePath, "MODULE.bazel"), []byte(moduleContent), 0644); err != nil {
		t.Fatal(err)
	}

	reg := NewLocalRegistry(tmpDir)
	ctx := context.Background()

	// Test successful fetch
	info, err := reg.GetModuleFile(ctx, "example", "1.0.0")
	if err != nil {
		t.Fatalf("GetModuleFile failed: %v", err)
	}
	if info.Name != "example" {
		t.Errorf("Name = %q, want %q", info.Name, "example")
	}
	if info.Version != "1.0.0" {
		t.Errorf("Version = %q, want %q", info.Version, "1.0.0")
	}
	if len(info.Dependencies) != 1 {
		t.Errorf("Dependencies count = %d, want 1", len(info.Dependencies))
	}

	// Test caching (second call should use cache)
	info2, err := reg.GetModuleFile(ctx, "example", "1.0.0")
	if err != nil {
		t.Fatalf("Second GetModuleFile failed: %v", err)
	}
	if info2 != info {
		t.Error("Expected cached result to be same instance")
	}

	// Test non-existent module
	_, err = reg.GetModuleFile(ctx, "nonexistent", "1.0.0")
	if err == nil {
		t.Error("Expected error for non-existent module")
	}
	regErr, ok := err.(*RegistryError)
	if !ok {
		t.Errorf("Expected RegistryError, got %T", err)
	} else if regErr.StatusCode != 404 {
		t.Errorf("StatusCode = %d, want 404", regErr.StatusCode)
	}
}

func TestLocalRegistry_BaseURL(t *testing.T) {
	// Use temp dir for cross-platform compatibility
	tmpDir := t.TempDir()
	reg := NewLocalRegistry(tmpDir)

	baseURL := reg.BaseURL()

	// Should start with file://
	if !strings.HasPrefix(baseURL, "file://") {
		t.Errorf("BaseURL should start with file://, got %q", baseURL)
	}

	// Should use forward slashes (URL standard)
	if strings.Contains(baseURL, "\\") {
		t.Errorf("BaseURL should use forward slashes, got %q", baseURL)
	}
}

func TestParseFileURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr bool
		// We don't check exact paths since they're OS-dependent after filepath.Clean
	}{
		{
			name: "Unix path",
			url:  "file:///tmp/registry",
		},
		{
			name: "Unix nested path",
			url:  "file:///home/user/bazel/registry",
		},
		{
			name: "Windows path uppercase",
			url:  "file:///C:/Users/registry",
		},
		{
			name: "Windows path lowercase",
			url:  "file:///c:/Users/registry",
		},
		{
			name:    "Not a file URL",
			url:     "https://example.com",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseFileURL(tt.url)
			if tt.wantErr {
				if err == nil {
					t.Error("Expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			// Just verify we got a non-empty path
			if got == "" {
				t.Error("parseFileURL returned empty path")
			}
		})
	}
}

func TestIsWindowsDriveLetter(t *testing.T) {
	tests := []struct {
		c    byte
		want bool
	}{
		{'A', true},
		{'Z', true},
		{'a', true},
		{'z', true},
		{'C', true},
		{'c', true},
		{'0', false},
		{'/', false},
		{'\\', false},
	}

	for _, tt := range tests {
		if got := isWindowsDriveLetter(tt.c); got != tt.want {
			t.Errorf("isWindowsDriveLetter(%q) = %v, want %v", tt.c, got, tt.want)
		}
	}
}

func TestPathToFileURL(t *testing.T) {
	// Test that URLs always use forward slashes
	tmpDir := t.TempDir()
	url := pathToFileURL(tmpDir)

	if !strings.HasPrefix(url, "file://") {
		t.Errorf("URL should start with file://, got %q", url)
	}

	if strings.Contains(url, "\\") {
		t.Errorf("URL should not contain backslashes, got %q", url)
	}
}

func TestIsFileURL(t *testing.T) {
	tests := []struct {
		url  string
		want bool
	}{
		{"file:///path", true},
		{"file://localhost/path", true},
		{"https://example.com", false},
		{"http://example.com", false},
		{"/local/path", false},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			if got := isFileURL(tt.url); got != tt.want {
				t.Errorf("isFileURL(%q) = %v, want %v", tt.url, got, tt.want)
			}
		})
	}
}

func TestRegistry_FileURL(t *testing.T) {
	// Create a temporary local registry
	tmpDir := t.TempDir()
	modulePath := filepath.Join(tmpDir, "modules", "local-mod", "1.0.0")
	if err := os.MkdirAll(modulePath, 0755); err != nil {
		t.Fatal(err)
	}

	moduleContent := `module(name = "local-mod", version = "1.0.0")`
	if err := os.WriteFile(filepath.Join(modulePath, "MODULE.bazel"), []byte(moduleContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Test Registry() with file:// URL
	fileURL := "file://" + tmpDir
	reg := Registry(fileURL)

	if reg == nil {
		t.Fatal("Registry() returned nil")
	}

	// Verify it's a LocalRegistry
	if _, ok := reg.(*LocalRegistry); !ok {
		t.Errorf("Expected *LocalRegistry, got %T", reg)
	}

	// Test fetching a module
	ctx := context.Background()
	info, err := reg.GetModuleFile(ctx, "local-mod", "1.0.0")
	if err != nil {
		t.Fatalf("GetModuleFile failed: %v", err)
	}
	if info.Name != "local-mod" {
		t.Errorf("Name = %q, want %q", info.Name, "local-mod")
	}
}

func TestRegistryChain_MixedLocalAndRemote(t *testing.T) {
	// Create a temporary local registry with a specific module
	tmpDir := t.TempDir()
	modulePath := filepath.Join(tmpDir, "modules", "local-only", "1.0.0")
	if err := os.MkdirAll(modulePath, 0755); err != nil {
		t.Fatal(err)
	}

	moduleContent := `module(name = "local-only", version = "1.0.0")`
	if err := os.WriteFile(filepath.Join(modulePath, "MODULE.bazel"), []byte(moduleContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Create a chain with local first, then BCR
	fileURL := "file://" + tmpDir
	chain, err := NewRegistryChain([]string{fileURL, DefaultRegistry})
	if err != nil {
		t.Fatalf("NewRegistryChain() error = %v", err)
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

	// Test fetching from local registry
	ctx := context.Background()
	info, err := chain.GetModuleFile(ctx, "local-only", "1.0.0")
	if err != nil {
		t.Fatalf("GetModuleFile for local module failed: %v", err)
	}
	if info.Name != "local-only" {
		t.Errorf("Name = %q, want %q", info.Name, "local-only")
	}

	// Verify it was cached to the local registry (index 0)
	chain.moduleRegistryMu.RLock()
	idx, found := chain.moduleRegistry["local-only"]
	chain.moduleRegistryMu.RUnlock()

	if !found {
		t.Error("Module should be cached in moduleRegistry")
	}
	if idx != 0 {
		t.Errorf("Module should be cached at index 0 (local), got %d", idx)
	}
}
