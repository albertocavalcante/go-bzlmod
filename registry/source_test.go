package registry

import (
	"encoding/json"
	"testing"
)

func TestSourceParsing(t *testing.T) {
	tests := []struct {
		name     string
		json     string
		wantType string
		check    func(t *testing.T, s *Source)
	}{
		{
			name: "archive source",
			json: `{
				"url": "https://github.com/example/repo/archive/v1.0.0.tar.gz",
				"integrity": "sha256-abc123",
				"strip_prefix": "repo-1.0.0"
			}`,
			wantType: "",
			check: func(t *testing.T, s *Source) {
				if !s.IsArchive() {
					t.Error("expected IsArchive() to be true")
				}
				if s.IsGitRepository() {
					t.Error("expected IsGitRepository() to be false")
				}
				if s.IsLocalPath() {
					t.Error("expected IsLocalPath() to be false")
				}
				if s.URL != "https://github.com/example/repo/archive/v1.0.0.tar.gz" {
					t.Errorf("URL = %q, want https://github.com/example/repo/archive/v1.0.0.tar.gz", s.URL)
				}
				if s.Integrity != "sha256-abc123" {
					t.Errorf("Integrity = %q, want sha256-abc123", s.Integrity)
				}
				if s.StripPrefix != "repo-1.0.0" {
					t.Errorf("StripPrefix = %q, want repo-1.0.0", s.StripPrefix)
				}
			},
		},
		{
			name: "explicit archive type",
			json: `{
				"type": "archive",
				"url": "https://example.com/archive.tar.gz",
				"integrity": "sha256-def456"
			}`,
			wantType: "archive",
			check: func(t *testing.T, s *Source) {
				if !s.IsArchive() {
					t.Error("expected IsArchive() to be true")
				}
			},
		},
		{
			name: "git repository source",
			json: `{
				"type": "git_repository",
				"remote": "https://github.com/example/repo.git",
				"commit": "abc123def456",
				"shallow_since": "2023-01-01",
				"init_submodules": true
			}`,
			wantType: "git_repository",
			check: func(t *testing.T, s *Source) {
				if s.IsArchive() {
					t.Error("expected IsArchive() to be false")
				}
				if !s.IsGitRepository() {
					t.Error("expected IsGitRepository() to be true")
				}
				if s.IsLocalPath() {
					t.Error("expected IsLocalPath() to be false")
				}
				if s.Remote != "https://github.com/example/repo.git" {
					t.Errorf("Remote = %q, want https://github.com/example/repo.git", s.Remote)
				}
				if s.Commit != "abc123def456" {
					t.Errorf("Commit = %q, want abc123def456", s.Commit)
				}
				if s.ShallowSince != "2023-01-01" {
					t.Errorf("ShallowSince = %q, want 2023-01-01", s.ShallowSince)
				}
				if !s.InitSubmodules {
					t.Error("expected InitSubmodules to be true")
				}
			},
		},
		{
			name: "git repository with tag",
			json: `{
				"type": "git_repository",
				"remote": "https://github.com/example/repo.git",
				"tag": "v1.0.0"
			}`,
			wantType: "git_repository",
			check: func(t *testing.T, s *Source) {
				if !s.IsGitRepository() {
					t.Error("expected IsGitRepository() to be true")
				}
				if s.Tag != "v1.0.0" {
					t.Errorf("Tag = %q, want v1.0.0", s.Tag)
				}
			},
		},
		{
			name: "local path source",
			json: `{
				"type": "local_path",
				"path": "/home/user/modules/my-module"
			}`,
			wantType: "local_path",
			check: func(t *testing.T, s *Source) {
				if s.IsArchive() {
					t.Error("expected IsArchive() to be false")
				}
				if s.IsGitRepository() {
					t.Error("expected IsGitRepository() to be false")
				}
				if !s.IsLocalPath() {
					t.Error("expected IsLocalPath() to be true")
				}
				if s.Path != "/home/user/modules/my-module" {
					t.Errorf("Path = %q, want /home/user/modules/my-module", s.Path)
				}
			},
		},
		{
			name: "archive with patches",
			json: `{
				"url": "https://example.com/archive.tar.gz",
				"integrity": "sha256-xyz789",
				"patches": {
					"fix.patch": "sha256-patch1",
					"update.patch": "sha256-patch2"
				},
				"patch_strip": 1
			}`,
			check: func(t *testing.T, s *Source) {
				if !s.IsArchive() {
					t.Error("expected IsArchive() to be true")
				}
				if len(s.Patches) != 2 {
					t.Errorf("len(Patches) = %d, want 2", len(s.Patches))
				}
				if s.Patches["fix.patch"] != "sha256-patch1" {
					t.Errorf("Patches[fix.patch] = %q, want sha256-patch1", s.Patches["fix.patch"])
				}
				if s.PatchStrip != 1 {
					t.Errorf("PatchStrip = %d, want 1", s.PatchStrip)
				}
			},
		},
		{
			name: "archive with overlay",
			json: `{
				"url": "https://example.com/archive.tar.gz",
				"integrity": "sha256-overlay123",
				"overlay": {
					"BUILD.bazel": "sha256-build",
					"MODULE.bazel": "sha256-module"
				}
			}`,
			check: func(t *testing.T, s *Source) {
				if len(s.Overlay) != 2 {
					t.Errorf("len(Overlay) = %d, want 2", len(s.Overlay))
				}
				if s.Overlay["BUILD.bazel"] != "sha256-build" {
					t.Errorf("Overlay[BUILD.bazel] = %q, want sha256-build", s.Overlay["BUILD.bazel"])
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var source Source
			if err := json.Unmarshal([]byte(tt.json), &source); err != nil {
				t.Fatalf("json.Unmarshal() error = %v", err)
			}
			if source.Type != tt.wantType {
				t.Errorf("Type = %q, want %q", source.Type, tt.wantType)
			}
			if tt.check != nil {
				tt.check(t, &source)
			}
		})
	}
}

func TestSourceHelperMethods(t *testing.T) {
	tests := []struct {
		name            string
		source          Source
		wantIsArchive   bool
		wantIsGit       bool
		wantIsLocalPath bool
	}{
		{
			name:            "empty type is archive",
			source:          Source{},
			wantIsArchive:   true,
			wantIsGit:       false,
			wantIsLocalPath: false,
		},
		{
			name:            "explicit archive",
			source:          Source{Type: "archive"},
			wantIsArchive:   true,
			wantIsGit:       false,
			wantIsLocalPath: false,
		},
		{
			name:            "git_repository",
			source:          Source{Type: "git_repository"},
			wantIsArchive:   false,
			wantIsGit:       true,
			wantIsLocalPath: false,
		},
		{
			name:            "local_path",
			source:          Source{Type: "local_path"},
			wantIsArchive:   false,
			wantIsGit:       false,
			wantIsLocalPath: true,
		},
		{
			name:            "unknown type",
			source:          Source{Type: "unknown"},
			wantIsArchive:   false,
			wantIsGit:       false,
			wantIsLocalPath: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.source.IsArchive(); got != tt.wantIsArchive {
				t.Errorf("IsArchive() = %v, want %v", got, tt.wantIsArchive)
			}
			if got := tt.source.IsGitRepository(); got != tt.wantIsGit {
				t.Errorf("IsGitRepository() = %v, want %v", got, tt.wantIsGit)
			}
			if got := tt.source.IsLocalPath(); got != tt.wantIsLocalPath {
				t.Errorf("IsLocalPath() = %v, want %v", got, tt.wantIsLocalPath)
			}
		})
	}
}
