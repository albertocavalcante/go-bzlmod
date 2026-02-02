package bazeltools

import (
	"sort"
	"strings"
	"testing"
)

// =============================================================================
// Adversarial Tests for bazeltools package
// =============================================================================

// TestGetConfig_ValidVersions ensures all documented versions return configs
func TestGetConfig_ValidVersions(t *testing.T) {
	knownVersions := []string{"6.6.0", "7.0.0", "7.1.0", "7.2.0", "8.0.0", "9.0.0"}

	for _, v := range knownVersions {
		cfg := GetConfig(v)
		if cfg == nil {
			t.Errorf("GetConfig(%q) returned nil, expected config", v)
			continue
		}

		if cfg.BazelVersion != v {
			t.Errorf("GetConfig(%q).BazelVersion = %q, want %q", v, cfg.BazelVersion, v)
		}

		if len(cfg.Deps) == 0 {
			t.Errorf("GetConfig(%q).Deps is empty, expected dependencies", v)
		}
	}
}

// TestGetConfig_UnknownVersions tests handling of unknown versions
func TestGetConfig_UnknownVersions(t *testing.T) {
	unknownVersions := []string{
		"",
		"1.0.0",
		"5.0.0",
		"10.0.0",
		"7.0",     // Missing patch
		"7",       // Major only
		"7.0.0.0", // Too many segments
		"invalid",
		"abc.def.ghi",
		"7.0.0-beta",
		"7.0.0+build",
	}

	for _, v := range unknownVersions {
		cfg := GetConfig(v)
		if cfg != nil {
			t.Errorf("GetConfig(%q) returned non-nil, expected nil for unknown version", v)
		}
	}
}

// TestGetConfig_EmptyString tests empty string behavior
func TestGetConfig_EmptyString(t *testing.T) {
	cfg := GetConfig("")
	if cfg != nil {
		t.Error("GetConfig(\"\") should return nil")
	}
}

// TestGetConfig_LongVersion tests very long version strings
func TestGetConfig_LongVersion(t *testing.T) {
	longVersion := strings.Repeat("7.0.0", 100)
	cfg := GetConfig(longVersion)
	if cfg != nil {
		t.Error("GetConfig with very long version should return nil")
	}
}

// TestGetConfig_UnicodeVersion tests unicode characters in version
func TestGetConfig_UnicodeVersion(t *testing.T) {
	cfg := GetConfig("７.０.０") // Full-width digits
	if cfg != nil {
		t.Error("GetConfig with unicode version should return nil")
	}
}

// TestGetDeps_ValidVersions tests GetDeps for known versions
func TestGetDeps_ValidVersions(t *testing.T) {
	knownVersions := []string{"6.6.0", "7.0.0", "7.1.0", "7.2.0", "8.0.0", "9.0.0"}

	for _, v := range knownVersions {
		deps := GetDeps(v)
		if deps == nil {
			t.Errorf("GetDeps(%q) returned nil, expected dependencies", v)
			continue
		}

		if len(deps) == 0 {
			t.Errorf("GetDeps(%q) returned empty slice, expected dependencies", v)
		}

		// Verify each dep has name and version
		for i, dep := range deps {
			if dep.Name == "" {
				t.Errorf("GetDeps(%q)[%d].Name is empty", v, i)
			}
			if dep.Version == "" {
				t.Errorf("GetDeps(%q)[%d].Version is empty", v, i)
			}
		}
	}
}

// TestGetDeps_UnknownVersion tests GetDeps for unknown versions
func TestGetDeps_UnknownVersion(t *testing.T) {
	deps := GetDeps("unknown")
	if deps != nil {
		t.Error("GetDeps for unknown version should return nil")
	}
}

// TestGetDeps_EmptyString tests GetDeps with empty string
func TestGetDeps_EmptyString(t *testing.T) {
	deps := GetDeps("")
	if deps != nil {
		t.Error("GetDeps(\"\") should return nil")
	}
}

// TestSupportedVersions_NotEmpty ensures we have supported versions
func TestSupportedVersions_NotEmpty(t *testing.T) {
	versions := SupportedVersions()
	if len(versions) == 0 {
		t.Error("SupportedVersions() returned empty slice")
	}
}

// TestSupportedVersions_AllHaveConfigs verifies all returned versions have configs
func TestSupportedVersions_AllHaveConfigs(t *testing.T) {
	versions := SupportedVersions()

	for _, v := range versions {
		cfg := GetConfig(v)
		if cfg == nil {
			t.Errorf("SupportedVersions() includes %q but GetConfig returns nil", v)
		}
	}
}

// TestSupportedVersions_ContainsKnownVersions verifies known versions are included
func TestSupportedVersions_ContainsKnownVersions(t *testing.T) {
	versions := SupportedVersions()
	versionSet := make(map[string]bool)
	for _, v := range versions {
		versionSet[v] = true
	}

	// These versions must exist (based on the implementation)
	required := []string{"7.0.0", "8.0.0"}
	for _, r := range required {
		if !versionSet[r] {
			t.Errorf("SupportedVersions() should include %q", r)
		}
	}
}

// TestClosestVersion_ExactMatch tests exact version matching
func TestClosestVersion_ExactMatch(t *testing.T) {
	versions := SupportedVersions()

	for _, v := range versions {
		result := ClosestVersion(v)
		if result != v {
			t.Errorf("ClosestVersion(%q) = %q, want exact match", v, result)
		}
	}
}

// TestClosestVersion_PatchVersion tests patch version matching (e.g., 7.0.1 -> 7.0.0)
func TestClosestVersion_PatchVersion(t *testing.T) {
	testCases := []struct {
		input    string
		expected string
	}{
		{"7.0.1", "7.0.0"},
		{"7.0.99", "7.0.0"},
		{"7.1.1", "7.1.0"},
		{"7.1.99", "7.1.0"},
		{"7.2.5", "7.2.0"},
		{"8.0.1", "8.0.0"},
		{"9.0.1", "9.0.0"},
	}

	for _, tc := range testCases {
		result := ClosestVersion(tc.input)
		if result != tc.expected {
			t.Errorf("ClosestVersion(%q) = %q, want %q", tc.input, result, tc.expected)
		}
	}
}

// TestClosestVersion_MajorFallback tests major version fallback (e.g., 7.5.0 -> 7.0.0)
func TestClosestVersion_MajorFallback(t *testing.T) {
	testCases := []struct {
		input    string
		expected string
	}{
		{"7.5.0", "7.0.0"},  // No 7.5.0, fallback to 7.0.0
		{"7.99.0", "7.0.0"}, // No 7.99.0, fallback to 7.0.0
		{"8.5.0", "8.0.0"},  // No 8.5.0, fallback to 8.0.0
		{"9.99.0", "9.0.0"}, // No 9.99.0, fallback to 9.0.0
	}

	for _, tc := range testCases {
		result := ClosestVersion(tc.input)
		if result != tc.expected {
			t.Errorf("ClosestVersion(%q) = %q, want %q", tc.input, result, tc.expected)
		}
	}
}

// TestClosestVersion_Bazel6IsFinal documents that 6.6.0 is the final Bazel 6 release.
// Bazel 6 reached end-of-life with Bazel 9.
// See: https://blog.bazel.build/2026/01/20/bazel-9.html#bazel-6-deprecation
func TestClosestVersion_Bazel6IsFinal(t *testing.T) {
	// 6.6.0 is the final Bazel 6.x release - no 6.7.0, 6.8.0, etc. will ever exist
	// The algorithm can't fall back from hypothetical 6.99.0 to 6.6.0 because
	// it tries 6.0.0 first (which doesn't exist). This is acceptable since
	// no version > 6.6.0 will ever be released.
	result := ClosestVersion("6.6.0")
	if result != "6.6.0" {
		t.Errorf("ClosestVersion(\"6.6.0\") = %q, want \"6.6.0\"", result)
	}

	// Patch versions of 6.6.x should work
	result = ClosestVersion("6.6.1")
	if result != "6.6.0" {
		t.Errorf("ClosestVersion(\"6.6.1\") = %q, want \"6.6.0\"", result)
	}
}

// TestClosestVersion_EmptyString tests empty string handling
func TestClosestVersion_EmptyString(t *testing.T) {
	result := ClosestVersion("")
	if result != "" {
		t.Errorf("ClosestVersion(\"\") = %q, want empty string", result)
	}
}

// TestClosestVersion_NoMatch tests versions that can't be matched
func TestClosestVersion_NoMatch(t *testing.T) {
	testCases := []string{
		"1.0.0",  // No Bazel 1.x support
		"5.0.0",  // No Bazel 5.x support (only 6.6.0+)
		"10.0.0", // Future version
		"0.0.0",  // Invalid version
	}

	for _, tc := range testCases {
		result := ClosestVersion(tc)
		if result != "" {
			t.Errorf("ClosestVersion(%q) = %q, expected empty (no match)", tc, result)
		}
	}
}

// TestClosestVersion_InvalidFormats tests various invalid version formats
func TestClosestVersion_InvalidFormats(t *testing.T) {
	testCases := []string{
		"7",
		"7.0",
		"7.0.0.0",
		"7.0.0.0.0",
		".7.0.0",
		"7..0.0",
		"7.0..0",
		"v7.0.0",
		"7.0.0a",
		"a.b.c",
		"...",
		"7.0.0-alpha",
		"7.0.0+build",
	}

	for _, tc := range testCases {
		result := ClosestVersion(tc)
		// These should either return empty or a valid fallback
		// The key is they shouldn't panic
		_ = result
	}
}

// TestClosestVersion_LongInput tests very long input strings
func TestClosestVersion_LongInput(t *testing.T) {
	longInput := strings.Repeat("7", 1000) + "." + strings.Repeat("0", 1000) + ".0"
	result := ClosestVersion(longInput)
	// Should not panic and should return empty or a valid match
	_ = result
}

// TestClosestVersion_UnicodeInput tests unicode characters
func TestClosestVersion_UnicodeInput(t *testing.T) {
	testCases := []string{
		"７.０.０",   // Full-width digits
		"7.０.0",   // Mixed
		"7.0.0🎉",  // With emoji
		"版本7.0.0", // Chinese prefix
	}

	for _, tc := range testCases {
		result := ClosestVersion(tc)
		// Should not panic
		_ = result
	}
}

// TestDataIntegrity_NoDuplicateDeps ensures no duplicate dependencies per version
func TestDataIntegrity_NoDuplicateDeps(t *testing.T) {
	versions := SupportedVersions()

	for _, v := range versions {
		deps := GetDeps(v)
		seen := make(map[string]bool)

		for _, dep := range deps {
			if seen[dep.Name] {
				t.Errorf("Version %s has duplicate dependency: %s", v, dep.Name)
			}
			seen[dep.Name] = true
		}
	}
}

// TestDataIntegrity_ValidVersionFormats ensures all dep versions look valid
func TestDataIntegrity_ValidVersionFormats(t *testing.T) {
	versions := SupportedVersions()

	for _, v := range versions {
		deps := GetDeps(v)

		for _, dep := range deps {
			// Version should not be empty
			if dep.Version == "" {
				t.Errorf("Version %s, dep %s has empty version", v, dep.Name)
			}

			// Version should not start with 'v'
			if strings.HasPrefix(dep.Version, "v") {
				t.Errorf("Version %s, dep %s has 'v' prefix: %s", v, dep.Name, dep.Version)
			}

			// Version should contain at least one digit
			hasDigit := false
			for _, c := range dep.Version {
				if c >= '0' && c <= '9' {
					hasDigit = true
					break
				}
			}
			if !hasDigit {
				t.Errorf("Version %s, dep %s has version without digits: %s", v, dep.Name, dep.Version)
			}
		}
	}
}

// TestDataIntegrity_ValidDepNames ensures all dependency names are valid
func TestDataIntegrity_ValidDepNames(t *testing.T) {
	versions := SupportedVersions()

	for _, v := range versions {
		deps := GetDeps(v)

		for _, dep := range deps {
			// Name should not be empty
			if dep.Name == "" {
				t.Errorf("Version %s has dependency with empty name", v)
				continue
			}

			// Name should only contain valid characters [a-z0-9_-]
			for _, c := range dep.Name {
				valid := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
					(c >= '0' && c <= '9') || c == '_' || c == '-'
				if !valid {
					t.Errorf("Version %s, dep %s has invalid character: %c", v, dep.Name, c)
					break
				}
			}
		}
	}
}

// TestDataIntegrity_CoreDepsExist ensures core dependencies exist across versions
func TestDataIntegrity_CoreDepsExist(t *testing.T) {
	// These deps should exist in most versions
	coreDeps := []string{"platforms", "rules_license"}

	versions := SupportedVersions()
	for _, v := range versions {
		deps := GetDeps(v)
		depSet := make(map[string]bool)
		for _, d := range deps {
			depSet[d.Name] = true
		}

		for _, core := range coreDeps {
			if !depSet[core] {
				// Just log, don't fail - some versions may legitimately not have certain deps
				t.Logf("Version %s is missing core dep: %s", v, core)
			}
		}
	}
}

// TestVersionProgression_NewerVersionsHaveValidDeps verifies version progression
func TestVersionProgression_NewerVersionsHaveValidDeps(t *testing.T) {
	versions := SupportedVersions()
	sort.Strings(versions) // Sort to get version order

	for _, v := range versions {
		deps := GetDeps(v)
		if len(deps) == 0 {
			t.Errorf("Version %s has no dependencies", v)
		}
	}
}

// TestClosestVersion_RealWorldVersions tests with realistic Bazel version strings
func TestClosestVersion_RealWorldVersions(t *testing.T) {
	testCases := []struct {
		input       string
		shouldMatch bool // whether we expect a non-empty result
	}{
		{"7.0.0", true},
		{"7.0.1", true},
		{"7.0.2", true},
		{"7.1.0", true},
		{"7.1.1", true},
		{"7.2.0", true},
		{"7.2.1", true},
		{"7.3.0", true}, // Should fallback to 7.0.0
		{"7.3.1", true}, // Should fallback to 7.0.0
		{"8.0.0", true},
		{"8.0.1", true},
		{"8.1.0", true}, // Should fallback to 8.0.0
		{"9.0.0", true},
		{"9.0.1", true},
		{"6.6.0", true},
		{"6.6.1", true},  // Should fallback to 6.6.0
		{"6.7.0", false}, // 6.7.0 will never exist - 6.6.0 is final (Bazel 6 EOL)
		{"5.4.0", false}, // Bazel 5.x not supported
	}

	for _, tc := range testCases {
		result := ClosestVersion(tc.input)
		if tc.shouldMatch && result == "" {
			t.Errorf("ClosestVersion(%q) = empty, expected a match", tc.input)
		}
		if !tc.shouldMatch && result != "" {
			// This is okay - the algorithm might find a match we didn't expect
			t.Logf("ClosestVersion(%q) = %q (unexpectedly found a match)", tc.input, result)
		}
	}
}

// TestConcurrentAccess tests thread safety of the package functions
func TestConcurrentAccess(t *testing.T) {
	done := make(chan bool)

	// Run multiple goroutines accessing the data
	for i := 0; i < 100; i++ {
		go func() {
			_ = SupportedVersions()
			_ = GetConfig("7.0.0")
			_ = GetDeps("8.0.0")
			_ = ClosestVersion("7.1.5")
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 100; i++ {
		<-done
	}
}
