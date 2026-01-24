package lockfile

import (
	"fmt"
	"testing"
)

func TestLockfileVersionForBazel(t *testing.T) {
	tests := []struct {
		major, minor, patch int
		want                int
	}{
		// Bazel 6.x - lockfile support starts at 6.2.0
		{6, 1, 0, -1}, // No lockfile support
		{6, 2, 0, 1},
		{6, 2, 1, 1},
		{6, 3, 0, 1},
		{6, 3, 1, 1},
		{6, 3, 2, 1},
		{6, 4, 0, 3},
		{6, 5, 0, 3},
		{6, 6, 0, 3},

		// Bazel 7.x
		{7, 0, 0, 3},
		{7, 0, 1, 3},
		{7, 0, 2, 3},
		{7, 1, 0, 6},
		{7, 1, 1, 6},
		{7, 1, 2, 6},
		{7, 2, 0, 11},
		{7, 2, 1, 11},
		{7, 3, 0, 11},
		{7, 3, 1, 11},
		{7, 3, 2, 11},
		{7, 4, 0, 11},
		{7, 4, 1, 11},
		{7, 5, 0, 13},
		{7, 6, 0, 13},
		{7, 6, 1, 13},
		{7, 6, 2, 13},
		{7, 7, 0, 13},
		{7, 7, 1, 13},

		// Bazel 8.x
		{8, 0, 0, 16},
		{8, 0, 1, 16},
		{8, 1, 0, 18},
		{8, 1, 1, 18},
		{8, 2, 0, 18},
		{8, 2, 1, 18},
		{8, 3, 0, 18},
		{8, 3, 1, 18},
		{8, 4, 0, 18},
		{8, 4, 1, 18},
		{8, 4, 2, 18},
		{8, 5, 0, 24},
		{8, 5, 1, 24},

		// Bazel 9.x
		{9, 0, 0, 26},

		// Unknown versions
		{5, 0, 0, -1},
		{10, 0, 0, -1},
	}

	for _, tt := range tests {
		name := fmt.Sprintf("%d.%d.%d", tt.major, tt.minor, tt.patch)
		t.Run(name, func(t *testing.T) {
			got := LockfileVersionForBazel(tt.major, tt.minor, tt.patch)
			if got != tt.want {
				t.Errorf("LockfileVersionForBazel(%d, %d, %d) = %d, want %d",
					tt.major, tt.minor, tt.patch, got, tt.want)
			}
		})
	}
}

func TestBazelVersionsForLockfile(t *testing.T) {
	tests := []struct {
		version   int
		wantCount int
	}{
		{1, 5},  // 6.2.0, 6.2.1, 6.3.0, 6.3.1, 6.3.2
		{3, 6},  // 6.4.0, 6.5.0, 6.6.0, 7.0.0, 7.0.1, 7.0.2
		{6, 3},  // 7.1.0, 7.1.1, 7.1.2
		{11, 7}, // 7.2.0, 7.2.1, 7.3.0, 7.3.1, 7.3.2, 7.4.0, 7.4.1
		{13, 6}, // 7.5.0, 7.6.0, 7.6.1, 7.6.2, 7.7.0, 7.7.1
		{16, 2}, // 8.0.0, 8.0.1
		{18, 9}, // 8.1.0, 8.1.1, 8.2.0, 8.2.1, 8.3.0, 8.3.1, 8.4.0, 8.4.1, 8.4.2
		{24, 2}, // 8.5.0, 8.5.1
		{26, 1}, // 9.0.0
		{99, 0}, // Unknown
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			got := BazelVersionsForLockfile(tt.version)
			if len(got) != tt.wantCount {
				t.Errorf("BazelVersionsForLockfile(%d) returned %d versions, want %d",
					tt.version, len(got), tt.wantCount)
			}
		})
	}
}

func TestGetVersionInfo(t *testing.T) {
	// Test LTS detection
	info := GetVersionInfo(16) // Bazel 8.0.0
	if info == nil {
		t.Fatal("GetVersionInfo(16) returned nil")
	}
	if !info.IsLTS {
		t.Error("Bazel 8.0.0 should be marked as LTS")
	}
	if len(info.Features) == 0 {
		t.Error("Expected features for version 16")
	}

	// Test non-LTS
	info = GetVersionInfo(6) // Bazel 7.1.0
	if info == nil {
		t.Fatal("GetVersionInfo(6) returned nil")
	}
	if info.IsLTS {
		t.Error("Bazel 7.1.0 should not be marked as LTS")
	}

	// Test unknown version
	info = GetVersionInfo(99)
	if info != nil {
		t.Error("GetVersionInfo(99) should return nil for unknown version")
	}
}

func TestKnownLockfileVersions(t *testing.T) {
	versions := KnownLockfileVersions()

	// Should be sorted ascending
	for i := 1; i < len(versions); i++ {
		if versions[i] <= versions[i-1] {
			t.Errorf("KnownLockfileVersions not sorted: %d <= %d", versions[i], versions[i-1])
		}
	}

	// Should include expected versions
	expected := map[int]bool{1: true, 3: true, 6: true, 11: true, 13: true, 16: true, 18: true, 24: true, 26: true}
	for _, v := range versions {
		delete(expected, v)
	}
	if len(expected) > 0 {
		t.Errorf("Missing expected versions: %v", expected)
	}
}

func TestLatestVersions(t *testing.T) {
	if LatestVersion() != 26 {
		t.Errorf("LatestVersion() = %d, want 26", LatestVersion())
	}
	if LatestLTSVersion() != 16 {
		t.Errorf("LatestLTSVersion() = %d, want 16", LatestLTSVersion())
	}
}

func TestIsExactMatchRequired(t *testing.T) {
	if !IsExactMatchRequired() {
		t.Error("IsExactMatchRequired should return true")
	}
}

func TestBazelVersion_String(t *testing.T) {
	v := BazelVersion{Major: 8, Minor: 1, Patch: 0}
	if v.String() != "8.1.0" {
		t.Errorf("BazelVersion.String() = %q, want %q", v.String(), "8.1.0")
	}
}

func TestLockfile_IsExactMatch(t *testing.T) {
	tests := []struct {
		version int
		want    bool
	}{
		{CurrentVersion, true},
		{CurrentVersion - 1, false},
		{CurrentVersion + 1, false},
		{0, false},
	}

	for _, tt := range tests {
		lf := &Lockfile{Version: tt.version}
		if got := lf.IsExactMatch(); got != tt.want {
			t.Errorf("Lockfile{Version: %d}.IsExactMatch() = %v, want %v",
				tt.version, got, tt.want)
		}
	}
}

func TestLockfile_IsCompatible_Updated(t *testing.T) {
	tests := []struct {
		version int
		want    bool
	}{
		{10, false}, // Too old (pre-incremental format)
		{11, true},  // Bazel 7.2.0 - incremental format introduced
		{16, true},  // Bazel 8.0.0
		{26, true},  // Bazel 9.0.0 (current)
		{30, true},  // Future version within range
		{31, false}, // Too new
	}

	for _, tt := range tests {
		lf := &Lockfile{Version: tt.version}
		if got := lf.IsCompatible(); got != tt.want {
			t.Errorf("Lockfile{Version: %d}.IsCompatible() = %v, want %v",
				tt.version, got, tt.want)
		}
	}
}

func TestLockfile_RequiredBazelVersion(t *testing.T) {
	lf := &Lockfile{Version: 16}
	versions := lf.RequiredBazelVersion()
	if len(versions) == 0 {
		t.Fatal("Expected at least one Bazel version")
	}
	// First version should be 8.0.0
	if versions[0].Major != 8 || versions[0].Minor != 0 {
		t.Errorf("Expected Bazel 8.0.x, got %v", versions[0])
	}

	// Unknown version
	lf = &Lockfile{Version: 99}
	if versions := lf.RequiredBazelVersion(); versions != nil {
		t.Errorf("Expected nil for unknown version, got %v", versions)
	}
}
