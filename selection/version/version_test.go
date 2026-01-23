package version

import (
	"testing"
)

// TestVersionParsing tests version parsing against Bazel's Version.java behavior.
func TestVersionParsing(t *testing.T) {
	tests := []struct {
		input      string
		wantNorm   string
		wantRelLen int
		wantPreLen int
		wantErr    bool
	}{
		// Basic semver
		{"1.0.0", "1.0.0", 3, 0, false},
		{"1.2.3", "1.2.3", 3, 0, false},

		// With prerelease
		{"1.0.0-alpha", "1.0.0-alpha", 3, 1, false},
		{"1.0.0-alpha.1", "1.0.0-alpha.1", 3, 2, false},
		{"1.0.0-0.3.7", "1.0.0-0.3.7", 3, 3, false},
		{"1.0.0-x.7.z.92", "1.0.0-x.7.z.92", 3, 4, false},

		// With build metadata (should be stripped)
		{"1.0.0+build", "1.0.0", 3, 0, false},
		{"1.0.0+build.123", "1.0.0", 3, 0, false},
		{"1.0.0-alpha+build", "1.0.0-alpha", 3, 1, false},

		// Bazel-specific: variable segment counts
		{"1", "1", 1, 0, false},
		{"1.0", "1.0", 2, 0, false},
		{"1.0.0.0", "1.0.0.0", 4, 0, false},

		// Empty version (special case)
		{"", "", 0, 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			v, err := Parse(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("Parse(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if err != nil {
				return
			}

			if v.Normalized != tt.wantNorm {
				t.Errorf("Parse(%q).Normalized = %q, want %q", tt.input, v.Normalized, tt.wantNorm)
			}
			if len(v.Release) != tt.wantRelLen {
				t.Errorf("Parse(%q) release len = %d, want %d", tt.input, len(v.Release), tt.wantRelLen)
			}
			if len(v.Prerelease) != tt.wantPreLen {
				t.Errorf("Parse(%q) prerelease len = %d, want %d", tt.input, len(v.Prerelease), tt.wantPreLen)
			}
		})
	}
}

// TestVersionComparison tests version comparison against Bazel's Version.java.
// Reference: Version.java lines 182-191 describes the comparison order.
func TestVersionComparison(t *testing.T) {
	tests := []struct {
		a, b string
		want int // -1, 0, or 1
	}{
		// Basic numeric comparison
		{"1.0.0", "2.0.0", -1},
		{"2.0.0", "1.0.0", 1},
		{"1.0.0", "1.0.0", 0},

		// Minor and patch
		{"1.0.0", "1.1.0", -1},
		{"1.0.0", "1.0.1", -1},
		{"1.0.1", "1.0.0", 1},

		// Prerelease sorts BEFORE release
		// Reference: Version.java line 185 - trueFirst for isPrerelease
		{"1.0.0-alpha", "1.0.0", -1},
		{"1.0.0", "1.0.0-alpha", 1},

		// Prerelease comparison
		{"1.0.0-alpha", "1.0.0-beta", -1},
		{"1.0.0-alpha.1", "1.0.0-alpha.2", -1},
		{"1.0.0-1", "1.0.0-2", -1},

		// Numeric vs alphanumeric in prerelease
		// Reference: Version.java line 112-114 - digits-only sorts first (trueFirst)
		{"1.0.0-1", "1.0.0-alpha", -1},
		{"1.0.0-alpha", "1.0.0-1", 1},

		// Different segment counts
		{"1.0", "1.0.0", -1},
		{"1.0.0.0", "1.0.0", 1},

		// Empty version sorts LAST (higher than everything)
		// Reference: Version.java lines 77-81, 183
		{"1.0.0", "", -1},
		{"", "1.0.0", 1},
		{"", "", 0},
		{"999.999.999", "", -1},

		// BCR-style versions
		{"1.3.1.bcr.7", "1.3.1", 1},  // More segments = higher
		{"1.3.1.bcr.7", "1.3.2", -1}, // 1.3.2 > 1.3.1.bcr.7

		// Large version numbers
		{"10.0.0", "9.0.0", 1},
		{"1.10.0", "1.9.0", 1},
	}

	for _, tt := range tests {
		t.Run(tt.a+"_vs_"+tt.b, func(t *testing.T) {
			got := Compare(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("Compare(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
			}

			// Also test symmetry
			gotReverse := Compare(tt.b, tt.a)
			wantReverse := -tt.want
			if gotReverse != wantReverse {
				t.Errorf("Compare(%q, %q) = %d, want %d (symmetry)", tt.b, tt.a, gotReverse, wantReverse)
			}
		})
	}
}

// TestIdentifierComparison tests identifier comparison per Bazel rules.
// Reference: Version.java lines 111-119
func TestIdentifierComparison(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		// Numeric comparison
		{"1", "2", -1},
		{"10", "9", 1},
		{"10", "10", 0},

		// Digits-only sorts BEFORE alphanumeric
		{"1", "alpha", -1},
		{"alpha", "1", 1},

		// Alphanumeric comparison
		{"alpha", "beta", -1},
		{"beta", "alpha", 1},
		{"alpha", "alpha", 0},
	}

	for _, tt := range tests {
		t.Run(tt.a+"_vs_"+tt.b, func(t *testing.T) {
			idA := ParseIdentifier(tt.a)
			idB := ParseIdentifier(tt.b)
			got := CompareIdentifiers(idA, idB)
			if got != tt.want {
				t.Errorf("CompareIdentifiers(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

// TestSort tests version sorting.
func TestSort(t *testing.T) {
	versions := []string{"2.0.0", "1.0.0", "1.0.0-alpha", "1.1.0", ""}
	Sort(versions)

	expected := []string{"1.0.0-alpha", "1.0.0", "1.1.0", "2.0.0", ""}
	for i, v := range versions {
		if v != expected[i] {
			t.Errorf("Sort result[%d] = %q, want %q", i, v, expected[i])
		}
	}
}

// TestMax tests the Max function.
func TestMax(t *testing.T) {
	tests := []struct {
		a, b, want string
	}{
		{"1.0.0", "2.0.0", "2.0.0"},
		{"2.0.0", "1.0.0", "2.0.0"},
		{"1.0.0", "1.0.0", "1.0.0"},
		{"1.0.0", "", ""},                 // Empty is highest
		{"1.0.0-alpha", "1.0.0", "1.0.0"}, // Release > prerelease
	}

	for _, tt := range tests {
		t.Run(tt.a+"_"+tt.b, func(t *testing.T) {
			got := Max(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("Max(%q, %q) = %q, want %q", tt.a, tt.b, got, tt.want)
			}
		})
	}
}
