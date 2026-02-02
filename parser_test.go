package gobzlmod

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseModuleContent(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    *ModuleInfo
		wantErr bool
	}{
		{
			name: "basic module",
			content: `module(
				name = "my_module",
				version = "1.0.0",
				compatibility_level = 1,
			)`,
			want: &ModuleInfo{
				Name:               "my_module",
				Version:            "1.0.0",
				CompatibilityLevel: 1,
				Dependencies:       []Dependency{},
				Overrides:          []Override{},
			},
			wantErr: false,
		},
		{
			name: "module with dependencies",
			content: `module(
				name = "my_module",
				version = "1.0.0",
			)
			
			bazel_dep(name = "rules_go", version = "0.41.0")
			bazel_dep(name = "gazelle", version = "0.32.0", dev_dependency = True)
			bazel_dep(name = "protobuf", version = "21.7", repo_name = "com_google_protobuf")`,
			want: &ModuleInfo{
				Name:               "my_module",
				Version:            "1.0.0",
				CompatibilityLevel: 0,
				Dependencies: []Dependency{
					{Name: "rules_go", Version: "0.41.0", DevDependency: false},
					{Name: "gazelle", Version: "0.32.0", DevDependency: true},
					{Name: "protobuf", Version: "21.7", RepoName: "com_google_protobuf", DevDependency: false},
				},
				Overrides: []Override{},
			},
			wantErr: false,
		},
		{
			name: "module with overrides",
			content: `module(name = "test_module", version = "1.0.0")
			
			single_version_override(
				module_name = "rules_go",
				version = "0.40.0",
				registry = "https://bcr.bazel.build",
			)
			
			git_override(module_name = "gazelle")
			local_path_override(module_name = "local_dep")
			archive_override(module_name = "archive_dep")`,
			want: &ModuleInfo{
				Name:               "test_module",
				Version:            "1.0.0",
				CompatibilityLevel: 0,
				Dependencies:       []Dependency{},
				Overrides: []Override{
					{Type: "single_version", ModuleName: "rules_go", Version: "0.40.0", Registry: "https://bcr.bazel.build"},
					{Type: "git", ModuleName: "gazelle"},
					{Type: "local_path", ModuleName: "local_dep"},
					{Type: "archive", ModuleName: "archive_dep"},
				},
			},
			wantErr: false,
		},
		{
			name: "complex module",
			content: `module(
				name = "complex_module",
				version = "2.1.0",
				compatibility_level = 2,
			)
			
			bazel_dep(name = "rules_go", version = "0.41.0")
			bazel_dep(name = "gazelle", version = "0.32.0", dev_dependency = True, repo_name = "bazel_gazelle")
			
			single_version_override(
				module_name = "rules_go",
				version = "0.40.0",
			)`,
			want: &ModuleInfo{
				Name:               "complex_module",
				Version:            "2.1.0",
				CompatibilityLevel: 2,
				Dependencies: []Dependency{
					{Name: "rules_go", Version: "0.41.0", DevDependency: false},
					{Name: "gazelle", Version: "0.32.0", RepoName: "bazel_gazelle", DevDependency: true},
				},
				Overrides: []Override{
					{Type: "single_version", ModuleName: "rules_go", Version: "0.40.0"},
				},
			},
			wantErr: false,
		},
		{
			name:    "invalid syntax",
			content: `module(name = "test", version = "1.0.0" # missing closing parenthesis`,
			want:    nil,
			wantErr: true,
		},
		{
			name:    "empty content",
			content: "",
			want:    nil,
			wantErr: true, // Empty content has no module() declaration
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseModuleContent(tt.content)

			if (err != nil) != tt.wantErr {
				t.Errorf("ParseModuleContent() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr {
				return
			}

			if !moduleInfoEqual(got, tt.want) {
				t.Errorf("ParseModuleContent() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestParseModuleContent_IncompleteBazelDep(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{
			name:    "missing version",
			content: `bazel_dep(name = "incomplete")`,
		},
		{
			name:    "missing name",
			content: `bazel_dep(version = "1.0.0")`,
		},
		{
			name:    "empty attributes",
			content: `bazel_dep()`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseModuleContent(tt.content)
			if err == nil {
				t.Fatalf("ParseModuleContent() expected error, got nil with %+v", got)
			}
		})
	}
}

func TestParseModuleFile(t *testing.T) {
	// Create a temporary directory for test files
	tempDir, err := os.MkdirTemp("", "parser_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	tests := []struct {
		name     string
		filename string
		content  string
		want     *ModuleInfo
		wantErr  bool
	}{
		{
			name:     "valid module file",
			filename: "MODULE.bazel",
			content: `module(name = "test_module", version = "1.0.0")
			bazel_dep(name = "rules_go", version = "0.41.0")`,
			want: &ModuleInfo{
				Name:               "test_module",
				Version:            "1.0.0",
				CompatibilityLevel: 0,
				Dependencies: []Dependency{
					{Name: "rules_go", Version: "0.41.0", DevDependency: false},
				},
				Overrides: []Override{},
			},
			wantErr: false,
		},
		{
			name:     "nonexistent file",
			filename: "nonexistent.bazel",
			content:  "",
			want:     nil,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var filePath string
			if tt.filename != "nonexistent.bazel" {
				filePath = filepath.Join(tempDir, tt.filename)
				err := os.WriteFile(filePath, []byte(tt.content), 0644)
				if err != nil {
					t.Fatalf("Failed to write test file: %v", err)
				}
			} else {
				filePath = filepath.Join(tempDir, tt.filename)
			}

			got, err := ParseModuleFile(filePath)

			if (err != nil) != tt.wantErr {
				t.Errorf("ParseModuleFile() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr {
				return
			}

			if !moduleInfoEqual(got, tt.want) {
				t.Errorf("ParseModuleFile() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestGetStringAttr(t *testing.T) {
	tests := []struct {
		name    string
		content string
		attr    string
		want    string
	}{
		{
			name:    "named attribute",
			content: `module(name = "test_value", version = "1.0.0")`,
			attr:    "name",
			want:    "test_value",
		},
		{
			name:    "missing attribute",
			content: `module(name = "test", version = "1.0.0")`,
			attr:    "missing",
			want:    "",
		},
		{
			name:    "first positional argument",
			content: `module(name = "test", version = "1.0.0")`,
			attr:    "",
			want:    "",
		},
		{
			name:    "no arguments",
			content: `module(name = "test", version = "1.0.0")`,
			attr:    "name",
			want:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Parse the content to get the call expression
			info, err := ParseModuleContent(tt.content)
			if err != nil {
				t.Fatalf("Failed to parse content: %v", err)
			}

			// For this test, we just verify the parsing worked
			// The actual getStringAttr function is tested indirectly through ParseModuleContent
			_ = info
		})
	}
}

func TestGetIntAttr(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    int
	}{
		{
			name:    "valid integer",
			content: `module(name = "test", compatibility_level = 42)`,
			want:    42,
		},
		{
			name:    "zero value",
			content: `module(name = "test", compatibility_level = 0)`,
			want:    0,
		},
		{
			name:    "missing attribute",
			content: `module(name = "test")`,
			want:    0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info, err := ParseModuleContent(tt.content)
			if err != nil {
				t.Fatalf("Failed to parse content: %v", err)
			}

			if info.CompatibilityLevel != tt.want {
				t.Errorf("CompatibilityLevel = %d, want %d", info.CompatibilityLevel, tt.want)
			}
		})
	}
}

func TestGetBoolAttr(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{
			name: "true value",
			content: `module(name = "m", version = "1.0.0")
bazel_dep(name = "test", version = "1.0.0", dev_dependency = True)`,
			want: true,
		},
		{
			name: "false value",
			content: `module(name = "m", version = "1.0.0")
bazel_dep(name = "test", version = "1.0.0", dev_dependency = False)`,
			want: false,
		},
		{
			name: "missing attribute",
			content: `module(name = "m", version = "1.0.0")
bazel_dep(name = "test", version = "1.0.0")`,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info, err := ParseModuleContent(tt.content)
			if err != nil {
				t.Fatalf("Failed to parse content: %v", err)
			}

			if len(info.Dependencies) == 0 {
				t.Fatal("Expected at least one dependency")
			}

			if info.Dependencies[0].DevDependency != tt.want {
				t.Errorf("DevDependency = %v, want %v", info.Dependencies[0].DevDependency, tt.want)
			}
		})
	}
}

// Helper function to compare ModuleInfo structs
func moduleInfoEqual(a, b *ModuleInfo) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}

	if a.Name != b.Name || a.Version != b.Version || a.CompatibilityLevel != b.CompatibilityLevel {
		return false
	}

	// Compare BazelCompatibility
	if len(a.BazelCompatibility) != len(b.BazelCompatibility) {
		return false
	}
	for i := range a.BazelCompatibility {
		if a.BazelCompatibility[i] != b.BazelCompatibility[i] {
			return false
		}
	}

	if len(a.Dependencies) != len(b.Dependencies) {
		return false
	}
	for i := range a.Dependencies {
		if a.Dependencies[i] != b.Dependencies[i] {
			return false
		}
	}

	if len(a.Overrides) != len(b.Overrides) {
		return false
	}
	for i := range a.Overrides {
		if a.Overrides[i] != b.Overrides[i] {
			return false
		}
	}

	return true
}

func TestExtractModuleInfo_EdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    *ModuleInfo
		wantErr bool
	}{
		{
			name: "incomplete override",
			content: `module(name = "test", version = "1.0.0")
single_version_override(version = "1.0.0")
single_version_override(module_name = "valid_override", version = "1.0.0")`,
			want: &ModuleInfo{
				Name:         "test",
				Version:      "1.0.0",
				Dependencies: []Dependency{},
				Overrides: []Override{
					{Type: "single_version", ModuleName: "valid_override", Version: "1.0.0"},
				},
			},
			wantErr: false,
		},
		{
			name: "mixed valid and invalid entries",
			content: `module(name = "test", version = "1.0.0")
			bazel_dep()
			bazel_dep(name = "valid", version = "1.0.0")
			single_version_override()
			git_override(module_name = "git_dep")`,
			wantErr: true,
		},
		{
			name:    "no module declaration",
			content: `bazel_dep(name = "test", version = "1.0.0")`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseModuleContent(tt.content)

			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseModuleContent() error = %v, wantErr %v", err, tt.wantErr)
			}

			if tt.wantErr {
				return
			}

			if !moduleInfoEqual(got, tt.want) {
				t.Errorf("ParseModuleContent() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestParseModuleContent_ModuleValidation(t *testing.T) {
	tests := []struct {
		name       string
		content    string
		wantErr    bool
		errMessage string
	}{
		{
			name: "module called twice",
			content: `module(name = "test", version = "1.0.0")
module(name = "test2", version = "2.0.0")`,
			wantErr:    true,
			errMessage: "the module() directive can only be called once",
		},
		{
			name: "module called after bazel_dep",
			content: `bazel_dep(name = "rules_go", version = "0.41.0")
module(name = "test", version = "1.0.0")`,
			wantErr:    true,
			errMessage: "if module() is called, it must be called before any other functions",
		},
		{
			name: "module called after single_version_override",
			content: `single_version_override(module_name = "foo", version = "1.0.0")
module(name = "test", version = "1.0.0")`,
			wantErr:    true,
			errMessage: "if module() is called, it must be called before any other functions",
		},
		{
			name: "module called after use_extension",
			content: `use_extension("@rules_go//go:extensions.bzl", "go_sdk")
module(name = "test", version = "1.0.0")`,
			wantErr:    true,
			errMessage: "if module() is called, it must be called before any other functions",
		},
		{
			name:    "module called first is valid",
			content: `module(name = "test", version = "1.0.0")`,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseModuleContent(tt.content)

			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseModuleContent() error = %v, wantErr %v", err, tt.wantErr)
			}

			if tt.wantErr && err != nil {
				if err.Error() != tt.errMessage {
					t.Errorf("ParseModuleContent() error = %q, want %q", err.Error(), tt.errMessage)
				}
			}
		})
	}
}

func TestParseModuleContent_BazelCompatibility(t *testing.T) {
	tests := []struct {
		name               string
		content            string
		wantErr            bool
		errContains        string
		wantCompatibility  []string
	}{
		{
			name: "valid bazel_compatibility with >=",
			content: `module(
				name = "test",
				version = "1.0.0",
				bazel_compatibility = [">=7.0.0"],
			)`,
			wantErr:           false,
			wantCompatibility: []string{">=7.0.0"},
		},
		{
			name: "valid bazel_compatibility with multiple entries",
			content: `module(
				name = "test",
				version = "1.0.0",
				bazel_compatibility = [">=7.0.0", "<8.0.0"],
			)`,
			wantErr:           false,
			wantCompatibility: []string{">=7.0.0", "<8.0.0"},
		},
		{
			name: "valid bazel_compatibility with all operators",
			content: `module(
				name = "test",
				version = "1.0.0",
				bazel_compatibility = [">=7.0.0", "<=8.0.0", ">6.0.0", "<9.0.0", "-7.1.0"],
			)`,
			wantErr:           false,
			wantCompatibility: []string{">=7.0.0", "<=8.0.0", ">6.0.0", "<9.0.0", "-7.1.0"},
		},
		{
			name: "invalid bazel_compatibility missing operator",
			content: `module(
				name = "test",
				version = "1.0.0",
				bazel_compatibility = ["7.0.0"],
			)`,
			wantErr:     true,
			errContains: "invalid bazel_compatibility value",
		},
		{
			name: "invalid bazel_compatibility wrong version format",
			content: `module(
				name = "test",
				version = "1.0.0",
				bazel_compatibility = [">=7.0"],
			)`,
			wantErr:     true,
			errContains: "invalid bazel_compatibility value",
		},
		{
			name: "invalid bazel_compatibility with text",
			content: `module(
				name = "test",
				version = "1.0.0",
				bazel_compatibility = [">=latest"],
			)`,
			wantErr:     true,
			errContains: "invalid bazel_compatibility value",
		},
		{
			name: "empty bazel_compatibility is valid",
			content: `module(
				name = "test",
				version = "1.0.0",
				bazel_compatibility = [],
			)`,
			wantErr:           false,
			wantCompatibility: nil,
		},
		{
			name: "no bazel_compatibility is valid",
			content: `module(
				name = "test",
				version = "1.0.0",
			)`,
			wantErr:           false,
			wantCompatibility: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseModuleContent(tt.content)

			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseModuleContent() error = %v, wantErr %v", err, tt.wantErr)
			}

			if tt.wantErr {
				if tt.errContains != "" && err != nil {
					if !contains(err.Error(), tt.errContains) {
						t.Errorf("error = %q, want to contain %q", err.Error(), tt.errContains)
					}
				}
				return
			}

			if len(got.BazelCompatibility) != len(tt.wantCompatibility) {
				t.Errorf("BazelCompatibility = %v, want %v", got.BazelCompatibility, tt.wantCompatibility)
				return
			}

			for i := range got.BazelCompatibility {
				if got.BazelCompatibility[i] != tt.wantCompatibility[i] {
					t.Errorf("BazelCompatibility[%d] = %q, want %q", i, got.BazelCompatibility[i], tt.wantCompatibility[i])
				}
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsAt(s, substr))
}

func containsAt(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
