package label

import (
	"testing"
)

func TestNewModule(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"valid simple", "rules_go", false},
		{"valid with numbers", "rules_go2", false},
		{"valid with dots", "rules.go", false},
		{"valid with dashes", "rules-go", false},
		{"valid with underscores", "rules_go", false},
		{"valid complex", "my-module.name_v2", false},
		{"valid single char", "a", false},
		{"empty", "", true},
		{"starts with number", "2rules", true},
		{"starts with uppercase", "Rules", true},
		{"contains uppercase", "rulesGo", true},
		{"ends with dash", "rules-", true},
		{"ends with dot", "rules.", true},
		{"contains spaces", "rules go", true},
		{"contains special chars", "rules@go", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := NewModule(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("NewModule(%q) expected error, got nil", tt.input)
				}
			} else {
				if err != nil {
					t.Errorf("NewModule(%q) unexpected error: %v", tt.input, err)
				}
				if m.String() != tt.input {
					t.Errorf("NewModule(%q).String() = %q, want %q", tt.input, m.String(), tt.input)
				}
			}
		})
	}
}

func TestMustModule(t *testing.T) {
	// Should not panic for valid module
	m := MustModule("rules_go")
	if m.String() != "rules_go" {
		t.Errorf("MustModule('rules_go').String() = %q, want 'rules_go'", m.String())
	}

	// Should panic for invalid module
	defer func() {
		if r := recover(); r == nil {
			t.Error("MustModule('INVALID') should have panicked")
		}
	}()
	MustModule("INVALID")
}

func TestModuleIsEmpty(t *testing.T) {
	var empty Module
	if !empty.IsEmpty() {
		t.Error("zero-value Module should be empty")
	}

	m := MustModule("rules_go")
	if m.IsEmpty() {
		t.Error("valid Module should not be empty")
	}
}

func TestNewApparentRepo(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"valid", "my_repo", false},
		{"valid with numbers", "repo123", false},
		{"empty (valid)", "", false},
		{"starts with number", "123repo", true},
		{"contains spaces", "my repo", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := NewApparentRepo(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("NewApparentRepo(%q) expected error, got nil", tt.input)
				}
			} else {
				if err != nil {
					t.Errorf("NewApparentRepo(%q) unexpected error: %v", tt.input, err)
				}
				if r.String() != tt.input {
					t.Errorf("NewApparentRepo(%q).String() = %q, want %q", tt.input, r.String(), tt.input)
				}
			}
		})
	}
}

func TestCanonicalRepo(t *testing.T) {
	module := MustModule("rules_go")
	version := MustVersion("0.50.1")

	repo := NewCanonicalRepo(module, version)
	if repo.String() != "rules_go+0.50.1" {
		t.Errorf("CanonicalRepo.String() = %q, want 'rules_go+0.50.1'", repo.String())
	}

	// Empty version uses ~ suffix
	emptyVersion := Version{}
	rootRepo := NewCanonicalRepo(module, emptyVersion)
	if rootRepo.String() != "rules_go~" {
		t.Errorf("CanonicalRepo with empty version = %q, want 'rules_go~'", rootRepo.String())
	}
}

func TestParseApparentLabel(t *testing.T) {
	tests := []struct {
		input      string
		wantRepo   string
		wantPkg    string
		wantTarget string
		wantErr    bool
	}{
		{"@repo//pkg:target", "repo", "pkg", "target", false},
		{"@repo//pkg/sub:target", "repo", "pkg/sub", "target", false},
		{"//pkg:target", "", "pkg", "target", false},
		{"//pkg/sub:target", "", "pkg/sub", "target", false},
		{"//pkg", "", "pkg", "pkg", false},
		{":target", "", "", "target", false},
		{"invalid", "", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			l, err := ParseApparentLabel(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ParseApparentLabel(%q) expected error", tt.input)
				}
				return
			}
			if err != nil {
				t.Errorf("ParseApparentLabel(%q) error: %v", tt.input, err)
				return
			}
			if l.Repo().String() != tt.wantRepo {
				t.Errorf("ParseApparentLabel(%q).Repo() = %q, want %q", tt.input, l.Repo().String(), tt.wantRepo)
			}
			if l.Package() != tt.wantPkg {
				t.Errorf("ParseApparentLabel(%q).Package() = %q, want %q", tt.input, l.Package(), tt.wantPkg)
			}
			if l.Target() != tt.wantTarget {
				t.Errorf("ParseApparentLabel(%q).Target() = %q, want %q", tt.input, l.Target(), tt.wantTarget)
			}
		})
	}
}

func TestStarlarkIdentifier(t *testing.T) {
	tests := []struct {
		input   string
		wantErr bool
	}{
		{"valid_ident", false},
		{"_private", false},
		{"CamelCase", false},
		{"ident123", false},
		{"", true},
		{"123invalid", true},
		{"invalid-ident", true},
		{"invalid.ident", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			_, err := NewStarlarkIdentifier(tt.input)
			if tt.wantErr && err == nil {
				t.Errorf("NewStarlarkIdentifier(%q) expected error", tt.input)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("NewStarlarkIdentifier(%q) unexpected error: %v", tt.input, err)
			}
		})
	}
}
