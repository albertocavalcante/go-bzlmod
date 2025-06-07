package gobzlmod

import (
	"encoding/json"
	"testing"
)

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
	req := &DepRequest{
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
