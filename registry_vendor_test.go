package gobzlmod

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/albertocavalcante/go-bzlmod/registry"
)

func TestVendorRegistry_GetModuleFile(t *testing.T) {
	// Create a temporary vendor directory
	vendorDir := t.TempDir()

	// Create module structure
	modulePath := filepath.Join(vendorDir, "modules", "test_module", "1.0.0")
	if err := os.MkdirAll(modulePath, 0755); err != nil {
		t.Fatalf("Failed to create module directory: %v", err)
	}

	moduleContent := `module(
    name = "test_module",
    version = "1.0.0",
)

bazel_dep(name = "dep_a", version = "2.0.0")
`
	if err := os.WriteFile(filepath.Join(modulePath, "MODULE.bazel"), []byte(moduleContent), 0644); err != nil {
		t.Fatalf("Failed to write MODULE.bazel: %v", err)
	}

	// Create vendor registry
	vendor, err := newVendorRegistry(vendorDir)
	if err != nil {
		t.Fatalf("newVendorRegistry() error = %v", err)
	}

	// Test GetModuleFile
	ctx := context.Background()
	info, err := vendor.GetModuleFile(ctx, "test_module", "1.0.0")
	if err != nil {
		t.Fatalf("GetModuleFile() error = %v", err)
	}

	if info.Name != "test_module" {
		t.Errorf("Name = %q, want test_module", info.Name)
	}
	if info.Version != "1.0.0" {
		t.Errorf("Version = %q, want 1.0.0", info.Version)
	}
	if len(info.Dependencies) != 1 {
		t.Errorf("len(Dependencies) = %d, want 1", len(info.Dependencies))
	}
}

func TestVendorRegistry_GetModuleMetadata_Synthesized(t *testing.T) {
	// Create a temporary vendor directory with multiple versions
	vendorDir := t.TempDir()

	versions := []string{"1.0.0", "1.1.0", "2.0.0"}
	for _, v := range versions {
		modulePath := filepath.Join(vendorDir, "modules", "test_module", v)
		if err := os.MkdirAll(modulePath, 0755); err != nil {
			t.Fatalf("Failed to create module directory: %v", err)
		}
		moduleContent := `module(name = "test_module", version = "` + v + `")`
		if err := os.WriteFile(filepath.Join(modulePath, "MODULE.bazel"), []byte(moduleContent), 0644); err != nil {
			t.Fatalf("Failed to write MODULE.bazel: %v", err)
		}
	}

	// Create vendor registry
	vendor, err := newVendorRegistry(vendorDir)
	if err != nil {
		t.Fatalf("newVendorRegistry() error = %v", err)
	}

	// Test synthesized metadata
	ctx := context.Background()
	metadata, err := vendor.GetModuleMetadata(ctx, "test_module")
	if err != nil {
		t.Fatalf("GetModuleMetadata() error = %v", err)
	}

	if len(metadata.Versions) != 3 {
		t.Errorf("len(Versions) = %d, want 3", len(metadata.Versions))
	}
}

func TestVendorRegistry_GetModuleMetadata_FromFile(t *testing.T) {
	// Create a temporary vendor directory with metadata.json
	vendorDir := t.TempDir()

	moduleDir := filepath.Join(vendorDir, "modules", "test_module")
	if err := os.MkdirAll(moduleDir, 0755); err != nil {
		t.Fatalf("Failed to create module directory: %v", err)
	}

	metadataContent := `{
		"versions": ["1.0.0", "2.0.0"],
		"yanked_versions": {"1.0.0": "security issue"}
	}`
	if err := os.WriteFile(filepath.Join(moduleDir, "metadata.json"), []byte(metadataContent), 0644); err != nil {
		t.Fatalf("Failed to write metadata.json: %v", err)
	}

	// Create vendor registry
	vendor, err := newVendorRegistry(vendorDir)
	if err != nil {
		t.Fatalf("newVendorRegistry() error = %v", err)
	}

	// Test metadata from file
	ctx := context.Background()
	metadata, err := vendor.GetModuleMetadata(ctx, "test_module")
	if err != nil {
		t.Fatalf("GetModuleMetadata() error = %v", err)
	}

	if len(metadata.Versions) != 2 {
		t.Errorf("len(Versions) = %d, want 2", len(metadata.Versions))
	}
	if !metadata.IsYanked("1.0.0") {
		t.Error("expected 1.0.0 to be yanked")
	}
}

func TestVendorRegistry_GetModuleSource_Synthesized(t *testing.T) {
	// Create a temporary vendor directory without source.json
	vendorDir := t.TempDir()

	modulePath := filepath.Join(vendorDir, "modules", "test_module", "1.0.0")
	if err := os.MkdirAll(modulePath, 0755); err != nil {
		t.Fatalf("Failed to create module directory: %v", err)
	}
	moduleContent := `module(name = "test_module", version = "1.0.0")`
	if err := os.WriteFile(filepath.Join(modulePath, "MODULE.bazel"), []byte(moduleContent), 0644); err != nil {
		t.Fatalf("Failed to write MODULE.bazel: %v", err)
	}

	// Create vendor registry
	vendor, err := newVendorRegistry(vendorDir)
	if err != nil {
		t.Fatalf("newVendorRegistry() error = %v", err)
	}

	// Test synthesized source
	ctx := context.Background()
	source, err := vendor.GetModuleSource(ctx, "test_module", "1.0.0")
	if err != nil {
		t.Fatalf("GetModuleSource() error = %v", err)
	}

	if !source.IsLocalPath() {
		t.Errorf("expected local_path source, got type %q", source.Type)
	}
	if source.Path != modulePath {
		t.Errorf("Path = %q, want %q", source.Path, modulePath)
	}
}

func TestVendorRegistry_GetModuleSource_FromFile(t *testing.T) {
	// Create a temporary vendor directory with source.json
	vendorDir := t.TempDir()

	modulePath := filepath.Join(vendorDir, "modules", "test_module", "1.0.0")
	if err := os.MkdirAll(modulePath, 0755); err != nil {
		t.Fatalf("Failed to create module directory: %v", err)
	}

	moduleContent := `module(name = "test_module", version = "1.0.0")`
	if err := os.WriteFile(filepath.Join(modulePath, "MODULE.bazel"), []byte(moduleContent), 0644); err != nil {
		t.Fatalf("Failed to write MODULE.bazel: %v", err)
	}

	sourceContent := `{
		"type": "git_repository",
		"remote": "https://github.com/example/test.git",
		"commit": "abc123"
	}`
	if err := os.WriteFile(filepath.Join(modulePath, "source.json"), []byte(sourceContent), 0644); err != nil {
		t.Fatalf("Failed to write source.json: %v", err)
	}

	// Create vendor registry
	vendor, err := newVendorRegistry(vendorDir)
	if err != nil {
		t.Fatalf("newVendorRegistry() error = %v", err)
	}

	// Test source from file
	ctx := context.Background()
	source, err := vendor.GetModuleSource(ctx, "test_module", "1.0.0")
	if err != nil {
		t.Fatalf("GetModuleSource() error = %v", err)
	}

	if !source.IsGitRepository() {
		t.Errorf("expected git_repository source, got type %q", source.Type)
	}
	if source.Remote != "https://github.com/example/test.git" {
		t.Errorf("Remote = %q, want https://github.com/example/test.git", source.Remote)
	}
}

func TestVendorRegistry_HasModule(t *testing.T) {
	vendorDir := t.TempDir()

	// Create only module_a
	modulePath := filepath.Join(vendorDir, "modules", "module_a", "1.0.0")
	if err := os.MkdirAll(modulePath, 0755); err != nil {
		t.Fatalf("Failed to create module directory: %v", err)
	}

	vendor, err := newVendorRegistry(vendorDir)
	if err != nil {
		t.Fatalf("newVendorRegistry() error = %v", err)
	}

	if !vendor.HasModule("module_a") {
		t.Error("HasModule(module_a) = false, want true")
	}
	if vendor.HasModule("module_b") {
		t.Error("HasModule(module_b) = true, want false")
	}
}

func TestVendorRegistry_HasVersion(t *testing.T) {
	vendorDir := t.TempDir()

	// Create module_a with version 1.0.0
	modulePath := filepath.Join(vendorDir, "modules", "module_a", "1.0.0")
	if err := os.MkdirAll(modulePath, 0755); err != nil {
		t.Fatalf("Failed to create module directory: %v", err)
	}
	moduleContent := `module(name = "module_a", version = "1.0.0")`
	if err := os.WriteFile(filepath.Join(modulePath, "MODULE.bazel"), []byte(moduleContent), 0644); err != nil {
		t.Fatalf("Failed to write MODULE.bazel: %v", err)
	}

	vendor, err := newVendorRegistry(vendorDir)
	if err != nil {
		t.Fatalf("newVendorRegistry() error = %v", err)
	}

	if !vendor.HasVersion("module_a", "1.0.0") {
		t.Error("HasVersion(module_a, 1.0.0) = false, want true")
	}
	if vendor.HasVersion("module_a", "2.0.0") {
		t.Error("HasVersion(module_a, 2.0.0) = true, want false")
	}
	if vendor.HasVersion("module_b", "1.0.0") {
		t.Error("HasVersion(module_b, 1.0.0) = true, want false")
	}
}

func TestVendorRegistry_InvalidDir(t *testing.T) {
	// Test with non-existent directory
	_, err := newVendorRegistry("/nonexistent/path")
	if err == nil {
		t.Error("expected error for non-existent directory")
	}
}

func TestVendorChain_FallbackToRemote(t *testing.T) {
	// Create a minimal vendor directory
	vendorDir := t.TempDir()
	modulePath := filepath.Join(vendorDir, "modules", "vendor_only", "1.0.0")
	if err := os.MkdirAll(modulePath, 0755); err != nil {
		t.Fatalf("Failed to create module directory: %v", err)
	}
	moduleContent := `module(name = "vendor_only", version = "1.0.0")`
	if err := os.WriteFile(filepath.Join(modulePath, "MODULE.bazel"), []byte(moduleContent), 0644); err != nil {
		t.Fatalf("Failed to write MODULE.bazel: %v", err)
	}

	vendor, err := newVendorRegistry(vendorDir)
	if err != nil {
		t.Fatalf("newVendorRegistry() error = %v", err)
	}

	// Create a mock remote registry that we can verify was/wasn't called
	mockCalled := false
	mock := &mockRegistry{
		getModuleFile: func(ctx context.Context, name, version string) (*ModuleInfo, error) {
			mockCalled = true
			return &ModuleInfo{Name: name, Version: version}, nil
		},
	}

	chain := newVendorChain(vendor, mock)

	ctx := context.Background()

	// Test 1: Module in vendor should not call remote
	mockCalled = false
	info, err := chain.GetModuleFile(ctx, "vendor_only", "1.0.0")
	if err != nil {
		t.Fatalf("GetModuleFile(vendor_only) error = %v", err)
	}
	if info.Name != "vendor_only" {
		t.Errorf("Name = %q, want vendor_only", info.Name)
	}
	if mockCalled {
		t.Error("expected remote not to be called for vendored module")
	}

	// Test 2: Module not in vendor should call remote
	mockCalled = false
	info, err = chain.GetModuleFile(ctx, "remote_only", "1.0.0")
	if err != nil {
		t.Fatalf("GetModuleFile(remote_only) error = %v", err)
	}
	if info.Name != "remote_only" {
		t.Errorf("Name = %q, want remote_only", info.Name)
	}
	if !mockCalled {
		t.Error("expected remote to be called for non-vendored module")
	}
}

// mockRegistry is a simple mock for testing
type mockRegistry struct {
	getModuleFile     func(ctx context.Context, name, version string) (*ModuleInfo, error)
	getModuleMetadata func(ctx context.Context, name string) (*registry.Metadata, error)
}


func (m *mockRegistry) GetModuleFile(ctx context.Context, name, version string) (*ModuleInfo, error) {
	if m.getModuleFile != nil {
		return m.getModuleFile(ctx, name, version)
	}
	return nil, &RegistryError{StatusCode: 404}
}

func (m *mockRegistry) GetModuleMetadata(ctx context.Context, name string) (*registry.Metadata, error) {
	if m.getModuleMetadata != nil {
		return m.getModuleMetadata(ctx, name)
	}
	return nil, &RegistryError{StatusCode: 404}
}

func (m *mockRegistry) GetModuleSource(ctx context.Context, name, version string) (*registry.Source, error) {
	return nil, &RegistryError{StatusCode: 404}
}

func (m *mockRegistry) BaseURL() string {
	return "mock://registry"
}
