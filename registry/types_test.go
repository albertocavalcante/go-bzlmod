package registry

import (
	"encoding/json"
	"testing"
)

func TestMetadata_LatestVersion(t *testing.T) {
	tests := []struct {
		name     string
		versions []string
		want     string
	}{
		{"empty", nil, ""},
		{"single", []string{"1.0.0"}, "1.0.0"},
		{"multiple", []string{"1.0.0", "1.1.0", "2.0.0"}, "2.0.0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &Metadata{Versions: tt.versions}
			if got := m.LatestVersion(); got != tt.want {
				t.Errorf("LatestVersion() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMetadata_IsYanked(t *testing.T) {
	m := &Metadata{
		YankedVersions: map[string]string{
			"1.0.0": "security vulnerability",
		},
	}

	if !m.IsYanked("1.0.0") {
		t.Error("IsYanked(1.0.0) = false, want true")
	}
	if m.IsYanked("2.0.0") {
		t.Error("IsYanked(2.0.0) = true, want false")
	}
}

func TestMetadata_YankReason(t *testing.T) {
	m := &Metadata{
		YankedVersions: map[string]string{
			"1.0.0": "security vulnerability",
		},
	}

	if got := m.YankReason("1.0.0"); got != "security vulnerability" {
		t.Errorf("YankReason(1.0.0) = %q, want %q", got, "security vulnerability")
	}
	if got := m.YankReason("2.0.0"); got != "" {
		t.Errorf("YankReason(2.0.0) = %q, want empty", got)
	}
}

func TestMetadata_IsDeprecated(t *testing.T) {
	m1 := &Metadata{}
	if m1.IsDeprecated() {
		t.Error("IsDeprecated() = true for empty, want false")
	}

	m2 := &Metadata{Deprecated: "use new_module instead"}
	if !m2.IsDeprecated() {
		t.Error("IsDeprecated() = false when set, want true")
	}
}

func TestMetadata_HasVersion(t *testing.T) {
	m := &Metadata{Versions: []string{"1.0.0", "1.1.0", "2.0.0"}}

	if !m.HasVersion("1.1.0") {
		t.Error("HasVersion(1.1.0) = false, want true")
	}
	if m.HasVersion("3.0.0") {
		t.Error("HasVersion(3.0.0) = true, want false")
	}
}

func TestSource_IsArchive(t *testing.T) {
	tests := []struct {
		sourceType string
		want       bool
	}{
		{"", true},
		{"archive", true},
		{"git_repository", false},
	}

	for _, tt := range tests {
		t.Run(tt.sourceType, func(t *testing.T) {
			s := &Source{Type: tt.sourceType}
			if got := s.IsArchive(); got != tt.want {
				t.Errorf("IsArchive() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSource_IsGitRepository(t *testing.T) {
	tests := []struct {
		sourceType string
		want       bool
	}{
		{"", false},
		{"archive", false},
		{"git_repository", true},
	}

	for _, tt := range tests {
		t.Run(tt.sourceType, func(t *testing.T) {
			s := &Source{Type: tt.sourceType}
			if got := s.IsGitRepository(); got != tt.want {
				t.Errorf("IsGitRepository() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMetadata_JSONRoundTrip(t *testing.T) {
	original := &Metadata{
		Homepage: "https://github.com/example/module",
		Maintainers: []Maintainer{
			{
				GitHub:       "user1",
				GitHubUserID: 12345,
				Name:         "Test User",
				Email:        "test@example.com",
			},
		},
		Repository: []string{"github:example/module"},
		Versions:   []string{"1.0.0", "1.1.0"},
		YankedVersions: map[string]string{
			"0.9.0": "deprecated",
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var restored Metadata
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if restored.Homepage != original.Homepage {
		t.Errorf("Homepage = %q, want %q", restored.Homepage, original.Homepage)
	}
	if len(restored.Maintainers) != len(original.Maintainers) {
		t.Errorf("Maintainers count = %d, want %d", len(restored.Maintainers), len(original.Maintainers))
	}
	if restored.Maintainers[0].GitHub != original.Maintainers[0].GitHub {
		t.Errorf("Maintainer.GitHub = %q, want %q", restored.Maintainers[0].GitHub, original.Maintainers[0].GitHub)
	}
}

func TestSource_JSONRoundTrip(t *testing.T) {
	t.Run("archive", func(t *testing.T) {
		original := &Source{
			URL:         "https://example.com/archive.zip",
			Integrity:   "sha256-abc123",
			StripPrefix: "module-1.0.0",
			Patches: map[string]string{
				"fix.patch": "sha256-def456",
			},
			PatchStrip: 1,
		}

		data, err := json.Marshal(original)
		if err != nil {
			t.Fatalf("Marshal failed: %v", err)
		}

		var restored Source
		if err := json.Unmarshal(data, &restored); err != nil {
			t.Fatalf("Unmarshal failed: %v", err)
		}

		if restored.URL != original.URL {
			t.Errorf("URL = %q, want %q", restored.URL, original.URL)
		}
		if restored.Integrity != original.Integrity {
			t.Errorf("Integrity = %q, want %q", restored.Integrity, original.Integrity)
		}
		if !restored.IsArchive() {
			t.Error("restored should be archive type")
		}
	})

	t.Run("git_repository", func(t *testing.T) {
		original := &Source{
			Type:           "git_repository",
			Remote:         "https://github.com/example/repo.git",
			Commit:         "abc123def456",
			InitSubmodules: true,
		}

		data, err := json.Marshal(original)
		if err != nil {
			t.Fatalf("Marshal failed: %v", err)
		}

		var restored Source
		if err := json.Unmarshal(data, &restored); err != nil {
			t.Fatalf("Unmarshal failed: %v", err)
		}

		if !restored.IsGitRepository() {
			t.Error("restored should be git_repository type")
		}
		if restored.Remote != original.Remote {
			t.Errorf("Remote = %q, want %q", restored.Remote, original.Remote)
		}
	})
}

func TestSource_ValidateForBazelVersion(t *testing.T) {
	tests := []struct {
		name         string
		source       *Source
		bazelVersion string
		wantWarnings int
	}{
		{
			name:         "empty version returns no warnings",
			source:       &Source{MirrorURLs: []string{"https://mirror.example.com"}},
			bazelVersion: "",
			wantWarnings: 0,
		},
		{
			name:         "no mirror_urls returns no warnings",
			source:       &Source{URL: "https://example.com/archive.tar.gz"},
			bazelVersion: "7.0.0",
			wantWarnings: 0,
		},
		{
			name:         "mirror_urls with Bazel 7.7.0 returns no warnings",
			source:       &Source{MirrorURLs: []string{"https://mirror.example.com"}},
			bazelVersion: "7.7.0",
			wantWarnings: 0,
		},
		{
			name:         "mirror_urls with Bazel 8.0.0 returns no warnings",
			source:       &Source{MirrorURLs: []string{"https://mirror.example.com"}},
			bazelVersion: "8.0.0",
			wantWarnings: 0,
		},
		{
			name:         "mirror_urls with Bazel 7.6.0 returns warning",
			source:       &Source{MirrorURLs: []string{"https://mirror.example.com"}},
			bazelVersion: "7.6.0",
			wantWarnings: 1,
		},
		{
			name:         "mirror_urls with Bazel 7.0.0 returns warning",
			source:       &Source{MirrorURLs: []string{"https://mirror.example.com"}},
			bazelVersion: "7.0.0",
			wantWarnings: 1,
		},
		{
			name:         "mirror_urls with Bazel 6.6.0 returns warning",
			source:       &Source{MirrorURLs: []string{"https://mirror.example.com"}},
			bazelVersion: "6.6.0",
			wantWarnings: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			warnings := tt.source.ValidateForBazelVersion(tt.bazelVersion)
			if len(warnings) != tt.wantWarnings {
				t.Errorf("ValidateForBazelVersion(%q) returned %d warnings, want %d: %v",
					tt.bazelVersion, len(warnings), tt.wantWarnings, warnings)
			}
		})
	}
}

func TestSource_MirrorURLsJSON(t *testing.T) {
	// Test that MirrorURLs is properly serialized/deserialized
	original := &Source{
		URL:        "https://example.com/archive.tar.gz",
		Integrity:  "sha256-abc123",
		MirrorURLs: []string{"https://mirror1.example.com", "https://mirror2.example.com"},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var restored Source
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if len(restored.MirrorURLs) != len(original.MirrorURLs) {
		t.Errorf("MirrorURLs count = %d, want %d", len(restored.MirrorURLs), len(original.MirrorURLs))
	}
	for i, url := range restored.MirrorURLs {
		if url != original.MirrorURLs[i] {
			t.Errorf("MirrorURLs[%d] = %q, want %q", i, url, original.MirrorURLs[i])
		}
	}
}
