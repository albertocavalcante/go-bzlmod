package compat

import (
	"testing"
)

func TestIsSupported(t *testing.T) {
	tests := []struct {
		name         string
		bazelVersion string
		fieldName    string
		want         bool
	}{
		// Empty version - no constraint
		{
			name:         "empty version allows all fields",
			bazelVersion: "",
			fieldName:    "mirror_urls",
			want:         true,
		},
		// Unknown field - assume supported
		{
			name:         "unknown field is assumed supported",
			bazelVersion: "7.0.0",
			fieldName:    "unknown_field",
			want:         true,
		},
		// mirror_urls (requires 7.7.0+)
		{
			name:         "mirror_urls supported in 7.7.0",
			bazelVersion: "7.7.0",
			fieldName:    "mirror_urls",
			want:         true,
		},
		{
			name:         "mirror_urls supported in 8.0.0",
			bazelVersion: "8.0.0",
			fieldName:    "mirror_urls",
			want:         true,
		},
		{
			name:         "mirror_urls not supported in 7.6.0",
			bazelVersion: "7.6.0",
			fieldName:    "mirror_urls",
			want:         false,
		},
		{
			name:         "mirror_urls not supported in 7.0.0",
			bazelVersion: "7.0.0",
			fieldName:    "mirror_urls",
			want:         false,
		},
		// max_compatibility_level (requires 7.0.0+)
		{
			name:         "max_compatibility_level supported in 7.0.0",
			bazelVersion: "7.0.0",
			fieldName:    "max_compatibility_level",
			want:         true,
		},
		{
			name:         "max_compatibility_level supported in 8.0.0",
			bazelVersion: "8.0.0",
			fieldName:    "max_compatibility_level",
			want:         true,
		},
		{
			name:         "max_compatibility_level not supported in 6.6.0",
			bazelVersion: "6.6.0",
			fieldName:    "max_compatibility_level",
			want:         false,
		},
		// include (requires 7.2.0+)
		{
			name:         "include supported in 7.2.0",
			bazelVersion: "7.2.0",
			fieldName:    "include",
			want:         true,
		},
		{
			name:         "include not supported in 7.1.0",
			bazelVersion: "7.1.0",
			fieldName:    "include",
			want:         false,
		},
		// use_repo_rule (requires 7.0.0+)
		{
			name:         "use_repo_rule supported in 7.0.0",
			bazelVersion: "7.0.0",
			fieldName:    "use_repo_rule",
			want:         true,
		},
		{
			name:         "use_repo_rule not supported in 6.6.0",
			bazelVersion: "6.6.0",
			fieldName:    "use_repo_rule",
			want:         false,
		},
		// override_repo (requires 8.0.0+)
		{
			name:         "override_repo supported in 8.0.0",
			bazelVersion: "8.0.0",
			fieldName:    "override_repo",
			want:         true,
		},
		{
			name:         "override_repo not supported in 7.7.0",
			bazelVersion: "7.7.0",
			fieldName:    "override_repo",
			want:         false,
		},
		// inject_repo (requires 8.0.0+)
		{
			name:         "inject_repo supported in 8.0.0",
			bazelVersion: "8.0.0",
			fieldName:    "inject_repo",
			want:         true,
		},
		{
			name:         "inject_repo not supported in 7.7.0",
			bazelVersion: "7.7.0",
			fieldName:    "inject_repo",
			want:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsSupported(tt.bazelVersion, tt.fieldName)
			if got != tt.want {
				t.Errorf("IsSupported(%q, %q) = %v, want %v",
					tt.bazelVersion, tt.fieldName, got, tt.want)
			}
		})
	}
}

func TestCheckField(t *testing.T) {
	tests := []struct {
		name         string
		bazelVersion string
		fieldName    string
		wantWarning  bool
	}{
		{
			name:         "empty version returns nil",
			bazelVersion: "",
			fieldName:    "mirror_urls",
			wantWarning:  false,
		},
		{
			name:         "unknown field returns nil",
			bazelVersion: "7.0.0",
			fieldName:    "unknown_field",
			wantWarning:  false,
		},
		{
			name:         "supported field returns nil",
			bazelVersion: "7.7.0",
			fieldName:    "mirror_urls",
			wantWarning:  false,
		},
		{
			name:         "unsupported field returns warning",
			bazelVersion: "7.6.0",
			fieldName:    "mirror_urls",
			wantWarning:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CheckField(tt.bazelVersion, tt.fieldName)
			if (got != nil) != tt.wantWarning {
				t.Errorf("CheckField(%q, %q) returned warning=%v, want warning=%v",
					tt.bazelVersion, tt.fieldName, got != nil, tt.wantWarning)
			}
		})
	}
}

func TestFieldWarningString(t *testing.T) {
	w := &FieldWarning{
		Field:       "mirror_urls",
		MinVersion:  "7.7.0",
		UsedVersion: "7.6.0",
		Location:    LocationSource,
		Description: "Backup URLs for source archive",
	}

	expected := "mirror_urls requires Bazel 7.7.0+, but target is 7.6.0"
	if got := w.String(); got != expected {
		t.Errorf("FieldWarning.String() = %q, want %q", got, expected)
	}
}

func TestGetRequirement(t *testing.T) {
	tests := []struct {
		name       string
		fieldName  string
		wantNil    bool
		wantMinVer string
	}{
		{
			name:       "mirror_urls exists",
			fieldName:  "mirror_urls",
			wantNil:    false,
			wantMinVer: "7.7.0",
		},
		{
			name:       "max_compatibility_level exists",
			fieldName:  "max_compatibility_level",
			wantNil:    false,
			wantMinVer: "7.0.0",
		},
		{
			name:      "unknown field returns nil",
			fieldName: "nonexistent_field",
			wantNil:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetRequirement(tt.fieldName)
			if tt.wantNil {
				if got != nil {
					t.Errorf("GetRequirement(%q) = %+v, want nil", tt.fieldName, got)
				}
				return
			}
			if got == nil {
				t.Fatalf("GetRequirement(%q) = nil, want non-nil", tt.fieldName)
			}
			if got.MinVersion != tt.wantMinVer {
				t.Errorf("GetRequirement(%q).MinVersion = %q, want %q",
					tt.fieldName, got.MinVersion, tt.wantMinVer)
			}
		})
	}
}

func TestGetAllRequirements(t *testing.T) {
	reqs := GetAllRequirements()
	if len(reqs) == 0 {
		t.Error("GetAllRequirements() returned empty slice")
	}

	// Verify it's a copy (modifying shouldn't affect original)
	originalLen := len(reqs)
	reqs[0].Name = "modified"
	freshReqs := GetAllRequirements()
	if freshReqs[0].Name == "modified" {
		t.Error("GetAllRequirements() did not return a copy")
	}
	if len(freshReqs) != originalLen {
		t.Errorf("GetAllRequirements() length changed: got %d, want %d",
			len(freshReqs), originalLen)
	}
}

func TestGetRequirementsForLocation(t *testing.T) {
	tests := []struct {
		location      FieldLocation
		expectedField string
	}{
		{LocationSource, "mirror_urls"},
		{LocationModule, "max_compatibility_level"},
	}

	for _, tt := range tests {
		t.Run(string(tt.location), func(t *testing.T) {
			reqs := GetRequirementsForLocation(tt.location)
			if len(reqs) == 0 {
				t.Errorf("GetRequirementsForLocation(%q) returned empty slice", tt.location)
				return
			}
			found := false
			for _, req := range reqs {
				if req.Name == tt.expectedField {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("GetRequirementsForLocation(%q) did not include %q",
					tt.location, tt.expectedField)
			}
		})
	}
}
