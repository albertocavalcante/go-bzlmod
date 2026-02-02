package registry

import (
	"testing"
)

func TestValidator_ValidateMetadata(t *testing.T) {
	v := NewValidator()

	tests := []struct {
		name    string
		json    string
		wantErr bool
	}{
		{
			name: "valid minimal",
			json: `{
				"homepage": "https://example.com",
				"maintainers": [{"github": "user1"}],
				"repository": ["github:example/repo"],
				"versions": ["1.0.0"]
			}`,
			wantErr: false,
		},
		{
			name: "valid full",
			json: `{
				"homepage": "https://example.com",
				"maintainers": [
					{
						"github": "user1",
						"github_user_id": 12345,
						"name": "Test User",
						"email": "test@example.com"
					}
				],
				"repository": ["github:example/repo"],
				"versions": ["0.9.0", "1.0.0", "1.1.0"],
				"yanked_versions": {"0.9.0": "security issue"},
				"deprecated": "use new_module instead"
			}`,
			wantErr: false,
		},
		{
			name: "missing required homepage",
			json: `{
				"maintainers": [{"github": "user1"}],
				"repository": ["github:example/repo"],
				"versions": ["1.0.0"]
			}`,
			wantErr: true,
		},
		{
			name: "missing required maintainers",
			json: `{
				"homepage": "https://example.com",
				"repository": ["github:example/repo"],
				"versions": ["1.0.0"]
			}`,
			wantErr: true,
		},
		{
			name: "empty maintainers array",
			json: `{
				"homepage": "https://example.com",
				"maintainers": [],
				"repository": ["github:example/repo"],
				"versions": ["1.0.0"]
			}`,
			wantErr: true,
		},
		{
			name:    "invalid json",
			json:    `{invalid`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.ValidateMetadata([]byte(tt.json))
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateMetadata() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidator_ValidateSource(t *testing.T) {
	v := NewValidator()

	tests := []struct {
		name    string
		json    string
		wantErr bool
	}{
		{
			name: "valid archive minimal",
			json: `{
				"url": "https://example.com/archive.zip",
				"integrity": "sha256-abc123def456"
			}`,
			wantErr: false,
		},
		{
			name: "valid archive with patches",
			json: `{
				"url": "https://example.com/archive.zip",
				"integrity": "sha256-abc123def456",
				"strip_prefix": "module-1.0.0",
				"patches": {
					"fix.patch": "sha256-patch123"
				},
				"patch_strip": 1
			}`,
			wantErr: false,
		},
		{
			name: "valid archive explicit type",
			json: `{
				"type": "archive",
				"url": "https://example.com/archive.zip",
				"integrity": "sha256-abc123def456"
			}`,
			wantErr: false,
		},
		{
			name: "valid git_repository",
			json: `{
				"type": "git_repository",
				"remote": "https://github.com/example/repo.git",
				"commit": "abcdef1234567890abcdef1234567890abcdef12"
			}`,
			wantErr: false,
		},
		{
			name: "valid git_repository with tag",
			json: `{
				"type": "git_repository",
				"remote": "https://github.com/example/repo.git",
				"tag": "v1.0.0"
			}`,
			wantErr: false,
		},
		{
			name: "missing url for archive",
			json: `{
				"integrity": "sha256-abc123def456"
			}`,
			wantErr: true,
		},
		{
			name: "missing integrity for archive",
			json: `{
				"url": "https://example.com/archive.zip"
			}`,
			wantErr: true,
		},
		{
			name:    "invalid json",
			json:    `{invalid`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.ValidateSource([]byte(tt.json))
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateSource() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidator_ValidateMetadataStruct(t *testing.T) {
	v := NewValidator()

	valid := &Metadata{
		Homepage: "https://example.com",
		Maintainers: []Maintainer{
			{GitHub: "user1"},
		},
		Repository: []string{"github:example/repo"},
		Versions:   []string{"1.0.0"},
	}

	if err := v.ValidateMetadataStruct(valid); err != nil {
		t.Errorf("ValidateMetadataStruct() unexpected error: %v", err)
	}
}

func TestValidator_ValidateSourceStruct(t *testing.T) {
	v := NewValidator()

	valid := &Source{
		URL:       "https://example.com/archive.zip",
		Integrity: "sha256-abc123",
	}

	if err := v.ValidateSourceStruct(valid); err != nil {
		t.Errorf("ValidateSourceStruct() unexpected error: %v", err)
	}
}

func TestValidator_LazyInit(t *testing.T) {
	// Test that multiple calls to validate methods don't cause issues
	v := NewValidator()

	json1 := `{
		"homepage": "https://example.com",
		"maintainers": [{"github": "user1"}],
		"repository": ["github:example/repo"],
		"versions": ["1.0.0"]
	}`

	json2 := `{
		"url": "https://example.com/archive.zip",
		"integrity": "sha256-abc123"
	}`

	// Call multiple times in sequence
	for range 3 {
		if err := v.ValidateMetadata([]byte(json1)); err != nil {
			t.Errorf("ValidateMetadata() error: %v", err)
		}
		if err := v.ValidateSource([]byte(json2)); err != nil {
			t.Errorf("ValidateSource() error: %v", err)
		}
	}
}
