package gobzlmod

import (
	"encoding/json"
	"testing"
)

// TestModuleToResolve_Key tests the Key() method returns "name@version" format.
func TestModuleToResolve_Key(t *testing.T) {
	tests := []struct {
		name    string
		module  ModuleToResolve
		wantKey string
	}{
		{
			name:    "basic module",
			module:  ModuleToResolve{Name: "rules_go", Version: "0.41.0"},
			wantKey: "rules_go@0.41.0",
		},
		{
			name:    "with bcr suffix",
			module:  ModuleToResolve{Name: "protobuf", Version: "21.7.bcr.1"},
			wantKey: "protobuf@21.7.bcr.1",
		},
		{
			name:    "prerelease version",
			module:  ModuleToResolve{Name: "experimental", Version: "1.0.0-rc1"},
			wantKey: "experimental@1.0.0-rc1",
		},
		{
			name:    "empty version",
			module:  ModuleToResolve{Name: "local_mod", Version: ""},
			wantKey: "local_mod@",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.module.Key()
			if got != tt.wantKey {
				t.Errorf("Key() = %q, want %q", got, tt.wantKey)
			}
		})
	}
}

// TestModuleToResolve_KeyMatchesYankedErrorFormat verifies Key() matches
// the format used in YankedVersionsError.Error() for consistency.
func TestModuleToResolve_KeyMatchesYankedErrorFormat(t *testing.T) {
	module := ModuleToResolve{
		Name:       "example_module",
		Version:    "1.2.3",
		YankReason: "test reason",
	}

	key := module.Key()
	expectedKey := module.Name + "@" + module.Version

	if key != expectedKey {
		t.Errorf("Key() = %q, want %q (should match name@version format)", key, expectedKey)
	}
}

func TestModuleInfo_JSONSerialization(t *testing.T) {
	original := &ModuleInfo{
		Name:               "test_module",
		Version:            "1.0.0",
		CompatibilityLevel: 1,
		Dependencies: []Dependency{
			{
				Name:          "dep1",
				Version:       "2.0.0",
				RepoName:      "custom_repo",
				DevDependency: true,
			},
		},
		Overrides: []Override{
			{
				Type:       "single_version",
				ModuleName: "override_module",
				Version:    "1.5.0",
				Registry:   "https://registry.example.com",
			},
		},
	}

	// Test serialization
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Failed to marshal ModuleInfo: %v", err)
	}

	// Test deserialization
	var restored ModuleInfo
	err = json.Unmarshal(data, &restored)
	if err != nil {
		t.Fatalf("Failed to unmarshal ModuleInfo: %v", err)
	}

	// Verify all fields
	if restored.Name != original.Name {
		t.Errorf("Name mismatch: got %s, want %s", restored.Name, original.Name)
	}
	if restored.Version != original.Version {
		t.Errorf("Version mismatch: got %s, want %s", restored.Version, original.Version)
	}
	if restored.CompatibilityLevel != original.CompatibilityLevel {
		t.Errorf("CompatibilityLevel mismatch: got %d, want %d", restored.CompatibilityLevel, original.CompatibilityLevel)
	}
	if len(restored.Dependencies) != len(original.Dependencies) {
		t.Errorf("Dependencies length mismatch: got %d, want %d", len(restored.Dependencies), len(original.Dependencies))
	}
	if len(restored.Overrides) != len(original.Overrides) {
		t.Errorf("Overrides length mismatch: got %d, want %d", len(restored.Overrides), len(original.Overrides))
	}
}

func TestDependency_JSONSerialization(t *testing.T) {
	tests := []struct {
		name string
		dep  Dependency
	}{
		{
			name: "basic dependency",
			dep: Dependency{
				Name:    "basic_dep",
				Version: "1.0.0",
			},
		},
		{
			name: "dev dependency with repo name",
			dep: Dependency{
				Name:          "dev_dep",
				Version:       "2.1.0",
				RepoName:      "custom_name",
				DevDependency: true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.dep)
			if err != nil {
				t.Fatalf("Failed to marshal Dependency: %v", err)
			}

			var restored Dependency
			err = json.Unmarshal(data, &restored)
			if err != nil {
				t.Fatalf("Failed to unmarshal Dependency: %v", err)
			}

			if restored != tt.dep {
				t.Errorf("Dependency mismatch: got %+v, want %+v", restored, tt.dep)
			}
		})
	}
}

func TestOverride_JSONSerialization(t *testing.T) {
	tests := []struct {
		name     string
		override Override
	}{
		{
			name: "single_version override",
			override: Override{
				Type:       "single_version",
				ModuleName: "test_module",
				Version:    "1.0.0",
				Registry:   "https://registry.example.com",
			},
		},
		{
			name: "git override",
			override: Override{
				Type:       "git",
				ModuleName: "git_module",
			},
		},
		{
			name: "local_path override",
			override: Override{
				Type:       "local_path",
				ModuleName: "local_module",
			},
		},
		{
			name: "archive override",
			override: Override{
				Type:       "archive",
				ModuleName: "archive_module",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.override)
			if err != nil {
				t.Fatalf("Failed to marshal Override: %v", err)
			}

			var restored Override
			err = json.Unmarshal(data, &restored)
			if err != nil {
				t.Fatalf("Failed to unmarshal Override: %v", err)
			}

			if restored != tt.override {
				t.Errorf("Override mismatch: got %+v, want %+v", restored, tt.override)
			}
		})
	}
}

func TestResolutionList_JSONSerialization(t *testing.T) {
	original := &ResolutionList{
		Modules: []ModuleToResolve{
			{
				Name:          "module1",
				Version:       "1.0.0",
				Registry:      "https://bcr.bazel.build",
				DevDependency: false,
				RequiredBy:    []string{"<root>"},
			},
			{
				Name:          "module2",
				Version:       "2.0.0",
				Registry:      "https://custom.registry.com",
				DevDependency: true,
				RequiredBy:    []string{"module1"},
			},
		},
		Summary: ResolutionSummary{
			TotalModules:      2,
			ProductionModules: 1,
			DevModules:        1,
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Failed to marshal ResolutionList: %v", err)
	}

	var restored ResolutionList
	err = json.Unmarshal(data, &restored)
	if err != nil {
		t.Fatalf("Failed to unmarshal ResolutionList: %v", err)
	}

	if len(restored.Modules) != len(original.Modules) {
		t.Errorf("Modules length mismatch: got %d, want %d", len(restored.Modules), len(original.Modules))
	}

	if restored.Summary.TotalModules != original.Summary.TotalModules {
		t.Errorf("TotalModules mismatch: got %d, want %d", restored.Summary.TotalModules, original.Summary.TotalModules)
	}
	if restored.Summary.ProductionModules != original.Summary.ProductionModules {
		t.Errorf("ProductionModules mismatch: got %d, want %d", restored.Summary.ProductionModules, original.Summary.ProductionModules)
	}
	if restored.Summary.DevModules != original.Summary.DevModules {
		t.Errorf("DevModules mismatch: got %d, want %d", restored.Summary.DevModules, original.Summary.DevModules)
	}
}

func TestDepRequest_Creation(t *testing.T) {
	req := &depRequest{
		Version:       "1.0.0",
		DevDependency: true,
		RequiredBy:    []string{"module1", "module2"},
	}

	if req.Version != "1.0.0" {
		t.Errorf("Version mismatch: got %s, want %s", req.Version, "1.0.0")
	}
	if !req.DevDependency {
		t.Error("Expected DevDependency to be true")
	}
	if len(req.RequiredBy) != 2 {
		t.Errorf("RequiredBy length mismatch: got %d, want %d", len(req.RequiredBy), 2)
	}
}

// TestYankedVersionsError_SingleModule tests the error message format for a single yanked module.
func TestYankedVersionsError_SingleModule(t *testing.T) {
	err := &YankedVersionsError{
		Modules: []ModuleToResolve{
			{
				Name:       "example_module",
				Version:    "1.2.3",
				YankReason: "security vulnerability CVE-2024-1234",
			},
		},
	}

	expected := "selected yanked version example_module@1.2.3: security vulnerability CVE-2024-1234"
	got := err.Error()

	if got != expected {
		t.Errorf("Error message mismatch:\ngot:  %s\nwant: %s", got, expected)
	}
}

// TestYankedVersionsError_MultipleModules tests the error message format for multiple yanked modules.
func TestYankedVersionsError_MultipleModules(t *testing.T) {
	err := &YankedVersionsError{
		Modules: []ModuleToResolve{
			{
				Name:       "module_a",
				Version:    "1.0.0",
				YankReason: "deprecated API",
			},
			{
				Name:       "module_b",
				Version:    "2.1.0",
				YankReason: "critical bug in v2.1.0",
			},
			{
				Name:       "module_c",
				Version:    "3.0.0",
				YankReason: "use 3.0.1 instead",
			},
		},
	}

	expected := `selected 3 yanked versions:
  - module_a@1.0.0: deprecated API
  - module_b@2.1.0: critical bug in v2.1.0
  - module_c@3.0.0: use 3.0.1 instead`
	got := err.Error()

	if got != expected {
		t.Errorf("Error message mismatch:\ngot:\n%s\nwant:\n%s", got, expected)
	}
}

// TestDirectDepsMismatchError_SingleMismatch tests the error message format for a single mismatch.
func TestDirectDepsMismatchError_SingleMismatch(t *testing.T) {
	err := &DirectDepsMismatchError{
		Mismatches: []DirectDepMismatch{
			{
				Name:            "example_dep",
				DeclaredVersion: "1.0.0",
				ResolvedVersion: "1.2.0",
			},
		},
	}

	expected := "direct dependency example_dep declared as 1.0.0 but resolved to 1.2.0"
	got := err.Error()

	if got != expected {
		t.Errorf("Error message mismatch:\ngot:  %s\nwant: %s", got, expected)
	}
}

// TestDirectDepsMismatchError_MultipleMismatches tests the error message format for multiple mismatches.
func TestDirectDepsMismatchError_MultipleMismatches(t *testing.T) {
	err := &DirectDepsMismatchError{
		Mismatches: []DirectDepMismatch{
			{
				Name:            "dep_a",
				DeclaredVersion: "1.0.0",
				ResolvedVersion: "1.5.0",
			},
			{
				Name:            "dep_b",
				DeclaredVersion: "2.0.0",
				ResolvedVersion: "2.1.0",
			},
			{
				Name:            "dep_c",
				DeclaredVersion: "3.1.0",
				ResolvedVersion: "3.2.5",
			},
		},
	}

	expected := `3 direct dependencies don't match resolved versions:
  - dep_a: declared 1.0.0, resolved 1.5.0
  - dep_b: declared 2.0.0, resolved 2.1.0
  - dep_c: declared 3.1.0, resolved 3.2.5`
	got := err.Error()

	if got != expected {
		t.Errorf("Error message mismatch:\ngot:\n%s\nwant:\n%s", got, expected)
	}
}

// BenchmarkYankedVersionsError_Small benchmarks error generation for a small number of yanked modules.
func BenchmarkYankedVersionsError_Small(b *testing.B) {
	err := &YankedVersionsError{
		Modules: []ModuleToResolve{
			{
				Name:       "module_a",
				Version:    "1.0.0",
				YankReason: "deprecated API",
			},
			{
				Name:       "module_b",
				Version:    "2.1.0",
				YankReason: "critical bug in v2.1.0",
			},
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = err.Error()
	}
}

// BenchmarkYankedVersionsError_Large benchmarks error generation for a large number of yanked modules.
func BenchmarkYankedVersionsError_Large(b *testing.B) {
	modules := make([]ModuleToResolve, 100)
	for i := 0; i < 100; i++ {
		modules[i] = ModuleToResolve{
			Name:       "module_" + string(rune('a'+i%26)),
			Version:    "1.0.0",
			YankReason: "yanked for testing purposes with a reasonably long reason string",
		}
	}

	err := &YankedVersionsError{
		Modules: modules,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = err.Error()
	}
}

// BenchmarkDirectDepsMismatchError_Small benchmarks error generation for a small number of mismatches.
func BenchmarkDirectDepsMismatchError_Small(b *testing.B) {
	err := &DirectDepsMismatchError{
		Mismatches: []DirectDepMismatch{
			{
				Name:            "dep_a",
				DeclaredVersion: "1.0.0",
				ResolvedVersion: "1.5.0",
			},
			{
				Name:            "dep_b",
				DeclaredVersion: "2.0.0",
				ResolvedVersion: "2.1.0",
			},
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = err.Error()
	}
}

// BenchmarkDirectDepsMismatchError_Large benchmarks error generation for a large number of mismatches.
func BenchmarkDirectDepsMismatchError_Large(b *testing.B) {
	mismatches := make([]DirectDepMismatch, 100)
	for i := 0; i < 100; i++ {
		mismatches[i] = DirectDepMismatch{
			Name:            "dependency_module_" + string(rune('a'+i%26)),
			DeclaredVersion: "1.0.0",
			ResolvedVersion: "2.0.0",
		}
	}

	err := &DirectDepsMismatchError{
		Mismatches: mismatches,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = err.Error()
	}
}
