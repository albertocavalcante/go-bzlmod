package label

import (
	"slices"
	"testing"
)

func TestNewVersion(t *testing.T) {
	tests := []struct {
		input      string
		wantErr    bool
		wantMajor  int
		wantMinor  int
		wantPatch  int
		wantPrerel string
		wantBuild  string
	}{
		{"1.0.0", false, 1, 0, 0, "", ""},
		{"0.50.1", false, 0, 50, 1, "", ""},
		{"2.3.4-rc1", false, 2, 3, 4, "rc1", ""},
		{"1.0.0-alpha.1", false, 1, 0, 0, "alpha.1", ""},
		{"1.0.0+build", false, 1, 0, 0, "", "build"},
		{"1.0.0+build.123", false, 1, 0, 0, "", "build.123"},
		{"1.0.0-beta+build", false, 1, 0, 0, "beta", "build"},
		{"", false, 0, 0, 0, "", ""}, // Empty is valid
		// Bazel-specific version formats
		{"1.0", false, 1, 0, 0, "", ""},                                      // Two-part version
		{"1", false, 1, 0, 0, "", ""},                                        // Single-part version
		{"29.0", false, 29, 0, 0, "", ""},                                    // protobuf style
		{"0.0.0-20241220-5e258e33", false, 0, 0, 0, "20241220-5e258e33", ""}, // Date-based prerelease
		{"8.2.1.1", false, 8, 2, 1, "", ""},                                  // Four-part version (buildifier style)
		{"1.2.3.4", false, 1, 2, 3, "", ""},                                  // Four-part version
		// BCR-style versions
		{"1.3.1.bcr.7", false, 1, 3, 1, "", ""},   // BCR suffix
		{"8.2.bcr.3", false, 8, 2, 0, "", ""},     // BCR suffix without patch
		{"1.2.13.bcr.1", false, 1, 2, 13, "", ""}, // BCR suffix
		// v-prefixed versions (non-standard but found in BCR)
		{"v1.0.0", false, 1, 0, 0, "", ""},
		{"v0.7.0-alpha2", false, 0, 7, 0, "alpha2", ""},
		// Commit SHA versions
		{"5d9f3bfb032e9d71b2292b7add7d90cbf9d037a9", false, 0, 0, 0, "", ""},
		// Invalid formats
		{"1.0.0-", true, 0, 0, 0, "", ""},
		{"1.0.0+", true, 0, 0, 0, "", ""},
		{"abc", true, 0, 0, 0, "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			v, err := NewVersion(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("NewVersion(%q) expected error", tt.input)
				}
				return
			}
			if err != nil {
				t.Errorf("NewVersion(%q) unexpected error: %v", tt.input, err)
				return
			}
			if v.Major() != tt.wantMajor {
				t.Errorf("NewVersion(%q).Major() = %d, want %d", tt.input, v.Major(), tt.wantMajor)
			}
			if v.Minor() != tt.wantMinor {
				t.Errorf("NewVersion(%q).Minor() = %d, want %d", tt.input, v.Minor(), tt.wantMinor)
			}
			if v.Patch() != tt.wantPatch {
				t.Errorf("NewVersion(%q).Patch() = %d, want %d", tt.input, v.Patch(), tt.wantPatch)
			}
			if v.Prerelease() != tt.wantPrerel {
				t.Errorf("NewVersion(%q).Prerelease() = %q, want %q", tt.input, v.Prerelease(), tt.wantPrerel)
			}
			if v.Build() != tt.wantBuild {
				t.Errorf("NewVersion(%q).Build() = %q, want %q", tt.input, v.Build(), tt.wantBuild)
			}
		})
	}
}

func TestVersionCompare(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"1.0.0", "1.0.0", 0},
		{"1.0.0", "2.0.0", -1},
		{"2.0.0", "1.0.0", 1},
		{"1.0.0", "1.1.0", -1},
		{"1.1.0", "1.0.0", 1},
		{"1.0.0", "1.0.1", -1},
		{"1.0.1", "1.0.0", 1},
		// Pre-release versions
		{"1.0.0-alpha", "1.0.0", -1},
		{"1.0.0", "1.0.0-alpha", 1},
		{"1.0.0-alpha", "1.0.0-beta", -1},
		{"1.0.0-alpha.1", "1.0.0-alpha.2", -1},
		{"1.0.0-alpha.2", "1.0.0-alpha.10", -1}, // Numeric comparison
		{"1.0.0-1", "1.0.0-2", -1},
		{"1.0.0-rc1", "1.0.0-rc2", -1},
		// Build metadata doesn't affect comparison
		{"1.0.0+build1", "1.0.0+build2", 0},
		{"1.0.0+build", "1.0.0", 0},
		// Four-part versions
		{"8.2.1.1", "8.2.1.2", -1},
		{"8.2.1.2", "8.2.1.1", 1},
		{"8.2.1.1", "8.2.1.1", 0},
		// BCR-style suffix versions
		{"1.3.1.bcr.1", "1.3.1.bcr.2", -1},
		{"1.3.1.bcr.7", "1.3.1.bcr.7", 0},
		{"1.3.1", "1.3.1.bcr.1", -1}, // No suffix < with suffix
		{"1.3.1.bcr.1", "1.3.1", 1},  // With suffix > no suffix
	}

	for _, tt := range tests {
		t.Run(tt.a+"_vs_"+tt.b, func(t *testing.T) {
			a := MustVersion(tt.a)
			b := MustVersion(tt.b)
			got := a.Compare(b)
			if got != tt.want {
				t.Errorf("Version(%q).Compare(%q) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

// TestVersionCompareEmpty tests that empty versions compare as HIGHEST.
// This matches Bazel's behavior where empty version signals NonRegistryOverride.
// Reference: https://github.com/bazelbuild/bazel/blob/master/src/main/java/com/google/devtools/build/lib/bazel/bzlmod/Version.java#L183
func TestVersionCompareEmpty(t *testing.T) {
	tests := []struct {
		name string
		a, b string
		want int
	}{
		{
			name: "empty vs non-empty: empty is HIGHEST",
			a:    "",
			b:    "1.0.0",
			want: 1, // empty > non-empty
		},
		{
			name: "non-empty vs empty: non-empty is lower",
			a:    "1.0.0",
			b:    "",
			want: -1, // non-empty < empty
		},
		{
			name: "empty vs empty: equal",
			a:    "",
			b:    "",
			want: 0,
		},
		{
			name: "empty vs high version: empty still highest",
			a:    "",
			b:    "999.999.999",
			want: 1, // empty > any version
		},
		{
			name: "empty vs prerelease: empty still highest",
			a:    "",
			b:    "1.0.0-alpha",
			want: 1, // empty > prerelease
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := MustVersion(tt.a)
			b := MustVersion(tt.b)
			got := a.Compare(b)
			if got != tt.want {
				t.Errorf("Version(%q).Compare(%q) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

// TestVersionsSortWithEmpty tests that empty versions sort to the end (highest).
func TestVersionsSortWithEmpty(t *testing.T) {
	input := []string{"2.0.0", "", "1.0.0", "1.0.0-alpha"}
	want := []string{"1.0.0-alpha", "1.0.0", "2.0.0", ""} // Empty sorts last (highest)

	versions := make(Versions, len(input))
	for i, s := range input {
		versions[i] = MustVersion(s)
	}

	slices.SortFunc(versions, func(a, b Version) int {
		return a.Compare(b)
	})

	for i, v := range versions {
		if v.String() != want[i] {
			t.Errorf("sorted[%d] = %q, want %q", i, v.String(), want[i])
		}
	}
}

func TestVersionLess(t *testing.T) {
	tests := []struct {
		a, b string
		want bool
	}{
		{"1.0.0", "2.0.0", true},
		{"2.0.0", "1.0.0", false},
		{"1.0.0", "1.0.0", false},
		{"1.0.0-alpha", "1.0.0", true},
	}

	for _, tt := range tests {
		t.Run(tt.a+"<"+tt.b, func(t *testing.T) {
			a := MustVersion(tt.a)
			b := MustVersion(tt.b)
			got := a.Less(b)
			if got != tt.want {
				t.Errorf("Version(%q).Less(%q) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestVersionsSort(t *testing.T) {
	input := []string{"2.0.0", "1.0.0", "1.0.0-alpha", "1.5.0", "1.0.0-beta", "10.0.0"}
	want := []string{"1.0.0-alpha", "1.0.0-beta", "1.0.0", "1.5.0", "2.0.0", "10.0.0"}

	versions := make(Versions, len(input))
	for i, s := range input {
		versions[i] = MustVersion(s)
	}

	slices.SortFunc(versions, func(a, b Version) int {
		return a.Compare(b)
	})

	for i, v := range versions {
		if v.String() != want[i] {
			t.Errorf("sorted[%d] = %q, want %q", i, v.String(), want[i])
		}
	}
}

func TestVersionIsPrerelease(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"1.0.0", false},
		{"1.0.0-alpha", true},
		{"1.0.0-rc1", true},
		{"1.0.0+build", false}, // Build metadata is not prerelease
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			v := MustVersion(tt.input)
			if v.IsPrerelease() != tt.want {
				t.Errorf("Version(%q).IsPrerelease() = %v, want %v", tt.input, v.IsPrerelease(), tt.want)
			}
		})
	}
}

func TestMustVersion(t *testing.T) {
	// Should not panic for valid version
	v := MustVersion("1.0.0")
	if v.String() != "1.0.0" {
		t.Errorf("MustVersion('1.0.0').String() = %q, want '1.0.0'", v.String())
	}

	// Should panic for invalid version
	defer func() {
		if r := recover(); r == nil {
			t.Error("MustVersion('invalid') should have panicked")
		}
	}()
	MustVersion("invalid")
}
