package version

import (
	"errors"
	"strings"
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

// TestMaxPrefersFirstOnTie pins the tie-break in Max.
//
// Every case in TestMax uses two *identical* strings for the equal case, so
// Max's `>=` was indistinguishable from `>`: nothing observed which spelling
// came back. Two versions can compare equal and still be textually different --
// build metadata is stripped by the parser, and a leading zero in a numeric
// identifier is insignificant -- and in MVS the selected string is what ends up
// in the lockfile, so which spelling wins is observable.
func TestMaxPrefersFirstOnTie(t *testing.T) {
	tests := []struct {
		name string
		a, b string
		want string
	}{
		{"build metadata is stripped, so these compare equal", "1.0.0+build", "1.0.0", "1.0.0+build"},
		{"and the other way round", "1.0.0", "1.0.0+build", "1.0.0"},
		{"leading zeros are numerically insignificant", "1.0.07", "1.0.7", "1.0.07"},
		{"and the other way round", "1.0.7", "1.0.07", "1.0.7"},
		{"identical strings", "1.2.3", "1.2.3", "1.2.3"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Precondition: these really do compare equal, otherwise the test
			// would be asserting ordinary Max behavior rather than the tie-break.
			if c := Compare(tt.a, tt.b); c != 0 {
				t.Fatalf("precondition failed: Compare(%q, %q) = %d, want 0", tt.a, tt.b, c)
			}
			if got := Max(tt.a, tt.b); got != tt.want {
				t.Errorf("Max(%q, %q) = %q, want %q", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

// TestParseRejectsMalformed covers Parse's error path, which had no test at all:
// every row of TestVersionParsing has wantErr false, leaving Parse's error
// return and ParseError.Error at 0% coverage.
func TestParseRejectsMalformed(t *testing.T) {
	malformed := []string{
		"1.0-",   // trailing prerelease separator with nothing after it
		"1.0.0-", // same, with a three-segment release
		"-alpha", // prerelease with no release
		"1.0 0",  // space is not in the permitted character class
		"1.0!",   // punctuation other than . - +
		"héllo",  // non-ASCII
		"1.0.0+", // trailing build separator
		"+build", // build metadata with no release
	}
	for _, s := range malformed {
		t.Run(s, func(t *testing.T) {
			_, err := Parse(s)
			if err == nil {
				t.Fatalf("Parse(%q) succeeded, want an error", s)
			}

			var pe *ParseError
			if !errors.As(err, &pe) {
				t.Fatalf("Parse(%q) error is %T, want *ParseError", s, err)
			}
			if pe.Version != s {
				t.Errorf("ParseError.Version = %q, want %q", pe.Version, s)
			}
			// The message must name the offending input, or a resolution
			// failure deep in a dependency graph is untraceable.
			if !strings.Contains(pe.Error(), s) {
				t.Errorf("ParseError.Error() = %q, should mention the input %q", pe.Error(), s)
			}
			if pe.Message == "" {
				t.Error("ParseError.Message is empty")
			}
		})
	}
}

// TestParseAcceptsSurprisingButValid documents inputs that look malformed and
// are nevertheless accepted, so a future tightening of the regex is a
// deliberate decision rather than an accident.
func TestParseAcceptsSurprisingButValid(t *testing.T) {
	tests := []struct {
		in             string
		wantNormalized string
	}{
		// The release character class includes '.', so consecutive dots parse
		// as an empty middle identifier rather than failing.
		{"1..0", "1..0"},
		// Build metadata is matched and then discarded, including from Normalized.
		{"1.0.0+build.5", "1.0.0"},
		// Leading zeros survive verbatim in Normalized even though they do not
		// affect comparison.
		{"1.0.07", "1.0.07"},
		// Wider than uint64: ParseIdentifier falls back to string comparison
		// rather than erroring.
		{"99999999999999999999", "99999999999999999999"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			pv, err := Parse(tt.in)
			if err != nil {
				t.Fatalf("Parse(%q) = %v, want success", tt.in, err)
			}
			if pv.Normalized != tt.wantNormalized {
				t.Errorf("Normalized = %q, want %q", pv.Normalized, tt.wantNormalized)
			}
		})
	}
}

// TestCompareFallsBackToLexicographic covers Compare's `errA != nil || errB !=
// nil` guard. The asymmetric case -- exactly one side malformed -- is what an
// `&&` mutation breaks, and nothing exercised it.
func TestCompareFallsBackToLexicographic(t *testing.T) {
	tests := []struct {
		name string
		a, b string
		want int
	}{
		// '!' (0x21) sorts below '.' (0x2e), so plain string comparison is negative.
		{"only a is malformed", "1.0!", "1.0.0", strings.Compare("1.0!", "1.0.0")},
		{"only b is malformed", "1.0.0", "1.0!", strings.Compare("1.0.0", "1.0!")},

		// These two are the cases that actually distinguish the fallback from
		// the mutation `errA != nil && errB != nil`. Under `&&`, a single
		// malformed side is NOT short-circuited, so Compare proceeds with the
		// zero ParsedVersion that Parse returns alongside its error -- an empty
		// release list, which compareIdentifierLists always ranks as the shorter
		// and therefore lesser operand. That coincides with strings.Compare
		// whenever the malformed string also sorts lower, which is why
		// "1.0!" vs "1.0.0" above cannot detect it. Picking a malformed string
		// that sorts HIGHER separates them.
		{"malformed a sorts above b", "z!", "1.0.0", strings.Compare("z!", "1.0.0")},
		{"malformed b sorts above a", "1.0.0", "z!", strings.Compare("1.0.0", "z!")},
		{"both malformed", "1.0!", "1.0@", strings.Compare("1.0!", "1.0@")},
		{"both malformed and equal", "1.0!", "1.0!", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Compare(tt.a, tt.b); got != tt.want {
				t.Errorf("Compare(%q, %q) = %d, want %d (strings.Compare fallback)", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

// TestParseIdentifierEdgeCases covers the empty identifier and the uint64
// overflow fallback, neither of which had a test.
func TestParseIdentifierEdgeCases(t *testing.T) {
	empty := ParseIdentifier("")
	if empty.IsDigitsOnly {
		t.Error(`ParseIdentifier("").IsDigitsOnly = true, want false`)
	}
	if empty.AsString != "" {
		t.Errorf(`ParseIdentifier("").AsString = %q, want ""`, empty.AsString)
	}

	// Wider than uint64, so the numeric parse fails and it must degrade to a
	// string identifier rather than silently truncating.
	big := ParseIdentifier("99999999999999999999")
	if big.IsDigitsOnly {
		t.Error("a 20-digit identifier must fall back to string comparison, not claim to be numeric")
	}
	if big.AsNumber != 0 {
		t.Errorf("AsNumber = %d, want 0 for the string fallback", big.AsNumber)
	}

	// Leading zeros: numerically 7, but the original text is preserved.
	z := ParseIdentifier("007")
	if !z.IsDigitsOnly || z.AsNumber != 7 || z.AsString != "007" {
		t.Errorf(`ParseIdentifier("007") = %+v, want {IsDigitsOnly:true AsNumber:7 AsString:"007"}`, z)
	}
}
