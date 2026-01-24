// Package bazeltools provides Bazel version-specific MODULE.tools dependencies.
// These are the implicit dependencies that Bazel adds to every resolution.
package bazeltools

// ToolDep represents a dependency from Bazel's MODULE.tools file.
type ToolDep struct {
	Name    string
	Version string
}

// VersionConfig contains the MODULE.tools dependencies for a specific Bazel version.
type VersionConfig struct {
	// BazelVersion is the Bazel version (e.g., "7.0.0").
	BazelVersion string
	// Deps are the dependencies declared in MODULE.tools.
	Deps []ToolDep
}

// bazelConfigs maps Bazel versions to their MODULE.tools dependencies.
//
// Note on Bazel 6: Version 6.6.0 is the final release in the Bazel 6.x series.
// Bazel 6 reached end-of-life with the release of Bazel 9.
// See: https://blog.bazel.build/2026/01/20/bazel-9.html#bazel-6-deprecation
var bazelConfigs = map[string]VersionConfig{
	// 6.6.0 is the final Bazel 6.x release (no further 6.x versions will be released)
	"6.6.0": {
		BazelVersion: "6.6.0",
		Deps: []ToolDep{
			{"rules_cc", "0.0.9"},
			{"rules_java", "5.5.1"},
			{"rules_license", "0.0.3"},
			{"rules_proto", "4.0.0"},
			{"rules_python", "0.4.0"},
			{"platforms", "0.0.7"},
			{"protobuf", "3.19.6"},
			{"zlib", "1.2.13"},
		},
	},
	"7.0.0": {
		BazelVersion: "7.0.0",
		Deps: []ToolDep{
			{"rules_cc", "0.0.9"},
			{"rules_java", "7.1.0"},
			{"rules_license", "0.0.3"},
			{"rules_proto", "4.0.0"},
			{"rules_python", "0.4.0"},
			{"platforms", "0.0.7"},
			{"protobuf", "3.19.6"},
			{"zlib", "1.3"},
			{"apple_support", "1.5.0"},
		},
	},
	"7.1.0": {
		BazelVersion: "7.1.0",
		Deps: []ToolDep{
			{"rules_cc", "0.0.9"},
			{"rules_java", "7.4.0"},
			{"rules_license", "0.0.7"},
			{"rules_proto", "5.3.0-21.7"},
			{"rules_python", "0.31.0"},
			{"platforms", "0.0.8"},
			{"protobuf", "21.7"},
			{"zlib", "1.3.1"},
			{"apple_support", "1.11.1"},
		},
	},
	"7.2.0": {
		BazelVersion: "7.2.0",
		Deps: []ToolDep{
			{"rules_cc", "0.0.9"},
			{"rules_java", "7.6.1"},
			{"rules_license", "0.0.7"},
			{"rules_proto", "6.0.0"},
			{"rules_python", "0.32.2"},
			{"platforms", "0.0.9"},
			{"protobuf", "27.0"},
			{"zlib", "1.3.1.bcr.1"},
			{"apple_support", "1.15.1"},
		},
	},
	"8.0.0": {
		BazelVersion: "8.0.0",
		Deps: []ToolDep{
			{"rules_license", "1.0.0"},
			{"buildozer", "7.1.2"},
			{"platforms", "0.0.10"},
			{"zlib", "1.3.1.bcr.3"},
			{"rules_proto", "7.0.2"},
			{"bazel_features", "1.21.0"},
			{"protobuf", "29.0"},
			{"rules_java", "8.6.1"},
			{"rules_cc", "0.0.16"},
			{"rules_python", "0.40.0"},
			{"rules_shell", "0.2.0"},
		},
	},
	"9.0.0": {
		BazelVersion: "9.0.0",
		Deps: []ToolDep{
			{"rules_license", "1.0.0"},
			{"buildozer", "8.2.1"},
			{"platforms", "1.0.0"},
			{"zlib", "1.3.1.bcr.5"},
			{"bazel_features", "1.30.0"},
			{"protobuf", "33.4"},
			{"rules_java", "9.0.3"},
			{"rules_cc", "0.2.14"},
			{"rules_python", "1.7.0"},
			{"rules_shell", "0.6.1"},
			{"apple_support", "1.24.2"},
			{"rules_apple", "4.1.0"},
			{"rules_swift", "3.1.2"},
			{"abseil-cpp", "20250814.1"},
		},
	},
}

// GetConfig returns the MODULE.tools configuration for a Bazel version.
// Returns nil if the version is not supported.
// Use ClosestVersion to find the closest matching version.
func GetConfig(version string) *VersionConfig {
	if cfg, ok := bazelConfigs[version]; ok {
		return &cfg
	}
	return nil
}

// GetDeps returns the MODULE.tools dependencies for a Bazel version.
// Returns nil if the version is not supported.
func GetDeps(version string) []ToolDep {
	if cfg := GetConfig(version); cfg != nil {
		return cfg.Deps
	}
	return nil
}

// SupportedVersions returns all supported Bazel versions.
func SupportedVersions() []string {
	versions := make([]string, 0, len(bazelConfigs))
	for v := range bazelConfigs {
		versions = append(versions, v)
	}
	return versions
}

const (
	// versionMinLenMajorMinor is the minimum length for major.minor pattern (e.g., "7.0.x").
	versionMinLenMajorMinor = 5
)

// ClosestVersion finds the closest supported version for a given Bazel version.
// For example, "7.0.1" would return "7.0.0", "7.1.2" would return "7.1.0".
// Returns empty string if no suitable version is found.
func ClosestVersion(version string) string {
	// Exact match
	if _, ok := bazelConfigs[version]; ok {
		return version
	}

	// Try major.minor.0 pattern - find the first two dots
	// For "7.0.1" -> "7.0.0", for "7.1.2" -> "7.1.0"
	if len(version) >= versionMinLenMajorMinor {
		firstDot := -1
		secondDot := -1
		for i, c := range version {
			if c == '.' {
				if firstDot == -1 {
					firstDot = i
				} else {
					secondDot = i
					break
				}
			}
		}
		if firstDot > 0 && secondDot > firstDot {
			majorMinor := version[:secondDot] + ".0" // e.g., "7.0.1" -> "7.0.0"
			if _, ok := bazelConfigs[majorMinor]; ok {
				return majorMinor
			}
		}
	}

	// Try major.0.0 pattern
	if len(version) >= 1 {
		// Find first dot to get major version
		firstDot := -1
		for i, c := range version {
			if c == '.' {
				firstDot = i
				break
			}
		}
		if firstDot > 0 {
			major := version[:firstDot] + ".0.0" // e.g., "7.x.x" -> "7.0.0"
			if _, ok := bazelConfigs[major]; ok {
				return major
			}
		}
	}

	return ""
}
