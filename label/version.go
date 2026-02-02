package label

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Version represents a validated Bazel module version.
// Follows semantic versioning with Bazel extensions for pre-release and build metadata.
// Format: MAJOR.MINOR.PATCH[.SUFFIX][-PRERELEASE][+BUILD]
// Where SUFFIX can be additional version parts like ".1" or BCR suffixes like ".bcr.7"
type Version struct {
	raw        string
	major      int
	minor      int
	patch      int
	suffix     string // Optional suffix like ".1" or ".bcr.7"
	prerelease string
	build      string
}

// versionRegex matches Bazel module versions.
// Bazel allows more flexible versions than strict semver:
// - Single-part versions: 1
// - Two-part versions: 29.0
// - Three-part versions: 1.2.3
// - Four-part versions: 8.2.1.1 (common for buildifier, etc.)
// - BCR suffix versions: 1.3.1.bcr.7, 8.2.bcr.3 (BCR-specific patches)
// - Optional v-prefix: v1.2.3 (non-standard but found in BCR)
// - Prerelease with any format: 0.0.0-20241220-5e258e33
// - Build metadata: 1.2.3+build
//
// The regex captures: major, minor (optional), patch (optional), extra suffix (optional), prerelease, build
var versionRegex = regexp.MustCompile(`^v?(\d+)(?:\.(\d+))?(?:\.(\d+))?((?:\.[a-zA-Z0-9]+)*)?(?:-([a-zA-Z0-9._-]+))?(?:\+([a-zA-Z0-9._-]+))?$`)

// commitSHARegex matches git commit SHAs used as versions (40 hex chars)
var commitSHARegex = regexp.MustCompile(`^[0-9a-f]{40}$`)

// NewVersion creates a validated Version from a string.
func NewVersion(s string) (Version, error) {
	if s == "" {
		return Version{}, nil // Empty version is valid for some contexts
	}

	// Handle commit SHA versions (used by some BCR modules)
	if commitSHARegex.MatchString(s) {
		return Version{raw: s}, nil
	}

	matches := versionRegex.FindStringSubmatch(s)
	if matches == nil {
		return Version{}, fmt.Errorf("invalid version %q: must follow version format", s)
	}

	major, _ := strconv.Atoi(matches[1])
	var minor, patch int
	if matches[2] != "" {
		minor, _ = strconv.Atoi(matches[2])
	}
	if matches[3] != "" {
		patch, _ = strconv.Atoi(matches[3])
	}
	// matches[4] is the suffix like ".bcr.7" or ".1"

	return Version{
		raw:        s,
		major:      major,
		minor:      minor,
		patch:      patch,
		suffix:     matches[4],
		prerelease: matches[5],
		build:      matches[6],
	}, nil
}

// MustVersion creates a Version or panics. Use only for constants/tests.
func MustVersion(s string) Version {
	v, err := NewVersion(s)
	if err != nil {
		panic(err)
	}
	return v
}

// String returns the version string.
func (v Version) String() string {
	return v.raw
}

// IsEmpty returns true if this is a zero-value Version.
func (v Version) IsEmpty() bool {
	return v.raw == ""
}

// Major returns the major version number.
func (v Version) Major() int {
	return v.major
}

// Minor returns the minor version number.
func (v Version) Minor() int {
	return v.minor
}

// Patch returns the patch version number.
func (v Version) Patch() int {
	return v.patch
}

// Suffix returns the optional version suffix (e.g., ".1" or ".bcr.7").
func (v Version) Suffix() string {
	return v.suffix
}

// HasSuffix returns true if this version has a suffix.
func (v Version) HasSuffix() bool {
	return v.suffix != ""
}

// Prerelease returns the pre-release identifier (e.g., "rc1", "alpha.1").
func (v Version) Prerelease() string {
	return v.prerelease
}

// Build returns the build metadata (e.g., "build.123").
func (v Version) Build() string {
	return v.build
}

// IsPrerelease returns true if this is a pre-release version.
func (v Version) IsPrerelease() bool {
	return v.prerelease != ""
}

// Compare compares two versions.
// Returns -1 if v < other, 0 if v == other, 1 if v > other.
// Pre-release versions are considered less than release versions.
func (v Version) Compare(other Version) int {
	// Compare major.minor.patch
	if v.major != other.major {
		return intCompare(v.major, other.major)
	}
	if v.minor != other.minor {
		return intCompare(v.minor, other.minor)
	}
	if v.patch != other.patch {
		return intCompare(v.patch, other.patch)
	}
	// Compare suffix (e.g., ".1" vs ".2" or ".bcr.1" vs ".bcr.2")
	if v.suffix != other.suffix {
		return compareSuffix(v.suffix, other.suffix)
	}

	// Pre-release versions have lower precedence
	if v.prerelease == "" && other.prerelease != "" {
		return 1
	}
	if v.prerelease != "" && other.prerelease == "" {
		return -1
	}
	if v.prerelease != other.prerelease {
		return comparePrerelease(v.prerelease, other.prerelease)
	}

	// Build metadata does not affect precedence
	return 0
}

// compareSuffix compares version suffixes like ".1", ".bcr.7"
func compareSuffix(a, b string) int {
	// Empty suffix is less than non-empty suffix
	if a == "" && b != "" {
		return -1
	}
	if a != "" && b == "" {
		return 1
	}
	// Strip leading dot and split by dots
	aParts := strings.Split(strings.TrimPrefix(a, "."), ".")
	bParts := strings.Split(strings.TrimPrefix(b, "."), ".")

	for i := range min(len(aParts), len(bParts)) {
		aNum, aIsNum := tryParseInt(aParts[i])
		bNum, bIsNum := tryParseInt(bParts[i])

		if aIsNum && bIsNum {
			if aNum != bNum {
				return intCompare(aNum, bNum)
			}
		} else if aIsNum {
			return -1 // Numeric < alphanumeric
		} else if bIsNum {
			return 1
		} else {
			if c := strings.Compare(aParts[i], bParts[i]); c != 0 {
				return c
			}
		}
	}

	return intCompare(len(aParts), len(bParts))
}

// Less returns true if v < other.
func (v Version) Less(other Version) bool {
	return v.Compare(other) < 0
}

func intCompare(a, b int) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

func comparePrerelease(a, b string) int {
	aParts := strings.Split(a, ".")
	bParts := strings.Split(b, ".")

	for i := range min(len(aParts), len(bParts)) {
		aNum, aIsNum := tryParseInt(aParts[i])
		bNum, bIsNum := tryParseInt(bParts[i])

		if aIsNum && bIsNum {
			if aNum != bNum {
				return intCompare(aNum, bNum)
			}
		} else if aIsNum {
			return -1 // Numeric < alphanumeric
		} else if bIsNum {
			return 1
		} else {
			if c := strings.Compare(aParts[i], bParts[i]); c != 0 {
				return c
			}
		}
	}

	return intCompare(len(aParts), len(bParts))
}

func tryParseInt(s string) (int, bool) {
	n, err := strconv.Atoi(s)
	return n, err == nil
}

// Versions is a sortable slice of Version.
type Versions []Version

func (v Versions) Len() int           { return len(v) }
func (v Versions) Swap(i, j int)      { v[i], v[j] = v[j], v[i] }
func (v Versions) Less(i, j int) bool { return v[i].Less(v[j]) }
