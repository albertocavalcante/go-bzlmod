package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	gobzlmod "github.com/albertocavalcante/go-bzlmod"
)

// TestDiagnostic_VersionDifferences examines why we get different versions
func TestDiagnostic_VersionDifferences(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping diagnostic test in short mode")
	}

	// Let's examine a simple case - just rules_go@0.41.0
	moduleContent := `
module(name = "diagnostic_test", version = "1.0.0")

bazel_dep(name = "rules_go", version = "0.41.0")
`

	// Create test workspace
	workspaceDir := createTestWorkspace(t, moduleContent)
	defer os.RemoveAll(workspaceDir)

	// Get our library's output
	ourList, err := resolveDependencies(moduleContent, "https://bcr.bazel.build", false)
	if err != nil {
		t.Fatalf("Our dependency resolution failed: %v", err)
	}

	// Get Bazel's output
	bazelModGraph, err := runBazelModGraph(t, workspaceDir)
	if err != nil {
		t.Fatalf("Bazel mod graph failed: %v", err)
	}

	// Flatten Bazel's output
	bazelModules := flattenBazelGraph(bazelModGraph)

	t.Logf("=== DIAGNOSTIC ANALYSIS ===")
	t.Logf("Our modules count: %d", len(ourList.Modules))
	t.Logf("Bazel modules count: %d", len(bazelModules))

	// Print detailed comparison for each module
	ourMap := make(map[string]gobzlmod.ModuleToResolve)
	for _, m := range ourList.Modules {
		ourMap[m.Name] = m
	}

	bazelMap := make(map[string]BazelModuleInfo)
	for _, m := range bazelModules {
		if m.Name != "" && m.Name != "diagnostic_test" {
			bazelMap[m.Name] = m
		}
	}

	t.Logf("\n=== VERSION COMPARISON ===")
	allModules := make(map[string]bool)
	for name := range ourMap {
		allModules[name] = true
	}
	for name := range bazelMap {
		allModules[name] = true
	}

	for moduleName := range allModules {
		ourModule, hasOur := ourMap[moduleName]
		bazelModule, hasBazel := bazelMap[moduleName]

		if hasOur && hasBazel {
			if ourModule.Version != bazelModule.Version {
				t.Logf("VERSION MISMATCH: %s - Our: %s, Bazel: %s",
					moduleName, ourModule.Version, bazelModule.Version)

				// Let's fetch the MODULE.bazel for this module to see what it declares
				analyzeModuleDependencies(t, moduleName, ourModule.Version, bazelModule.Version)
			} else {
				t.Logf("VERSION MATCH: %s - %s", moduleName, ourModule.Version)
			}
		} else if hasOur {
			t.Logf("ONLY IN OUR OUTPUT: %s@%s", moduleName, ourModule.Version)
		} else if hasBazel {
			t.Logf("ONLY IN BAZEL OUTPUT: %s@%s", moduleName, bazelModule.Version)
		}
	}
}

// analyzeModuleDependencies fetches and compares the MODULE.bazel files for different versions
func analyzeModuleDependencies(t *testing.T, moduleName, ourVersion, bazelVersion string) {
	t.Logf("\n--- Analyzing %s: our=%s vs bazel=%s ---", moduleName, ourVersion, bazelVersion)

	// Fetch MODULE.bazel for both versions
	ourModuleContent := fetchModuleContent(t, moduleName, ourVersion)
	bazelModuleContent := fetchModuleContent(t, moduleName, bazelVersion)

	if ourModuleContent != "" {
		t.Logf("Our version (%s) MODULE.bazel:\n%s", ourVersion, ourModuleContent)
	}

	if bazelModuleContent != "" {
		t.Logf("Bazel version (%s) MODULE.bazel:\n%s", bazelVersion, bazelModuleContent)
	}

	// Compare dependency declarations
	if ourModuleContent != "" && bazelModuleContent != "" {
		compareDependencyDeclarations(t, moduleName, ourVersion, ourModuleContent, bazelVersion, bazelModuleContent)
	}
}

// fetchModuleContent fetches the MODULE.bazel content for a specific module version
func fetchModuleContent(t *testing.T, moduleName, version string) string {
	url := fmt.Sprintf("https://bcr.bazel.build/modules/%s/%s/MODULE.bazel", moduleName, version)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		t.Logf("Failed to create request for %s@%s: %v", moduleName, version, err)
		return ""
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Logf("Failed to fetch %s@%s: %v", moduleName, version, err)
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Logf("HTTP %d for %s@%s", resp.StatusCode, moduleName, version)
		return ""
	}

	content, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Logf("Failed to read content for %s@%s: %v", moduleName, version, err)
		return ""
	}

	return string(content)
}

// compareDependencyDeclarations compares the dependencies declared in two MODULE.bazel files
func compareDependencyDeclarations(t *testing.T, moduleName, ourVersion, ourContent, bazelVersion, bazelContent string) {
	t.Logf("\n--- Dependency Analysis for %s ---", moduleName)

	// Parse both MODULE.bazel contents (simplified parsing)
	ourDeps := extractDependencies(ourContent)
	bazelDeps := extractDependencies(bazelContent)

	t.Logf("Dependencies in our version (%s): %v", ourVersion, ourDeps)
	t.Logf("Dependencies in bazel version (%s): %v", bazelVersion, bazelDeps)

	// Find differences
	for dep, ourVer := range ourDeps {
		if bazelVer, exists := bazelDeps[dep]; exists {
			if ourVer != bazelVer {
				t.Logf("  DEPENDENCY VERSION DIFF: %s requires %s@%s, bazel requires %s@%s",
					moduleName, dep, ourVer, dep, bazelVer)
			}
		} else {
			t.Logf("  DEPENDENCY ONLY IN OUR VERSION: %s@%s", dep, ourVer)
		}
	}

	for dep, bazelVer := range bazelDeps {
		if _, exists := ourDeps[dep]; !exists {
			t.Logf("  DEPENDENCY ONLY IN BAZEL VERSION: %s@%s", dep, bazelVer)
		}
	}
}

// extractDependencies extracts bazel_dep declarations from MODULE.bazel content
func extractDependencies(content string) map[string]string {
	deps := make(map[string]string)

	// This is a very simplified parser - just for diagnostic purposes
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "bazel_dep(") {
			// Extract name and version (very basic parsing)
			if nameStart := strings.Index(line, "name = \""); nameStart != -1 {
				nameStart += 8
				nameEnd := strings.Index(line[nameStart:], "\"")
				if nameEnd != -1 {
					name := line[nameStart : nameStart+nameEnd]

					if versionStart := strings.Index(line, "version = \""); versionStart != -1 {
						versionStart += 11
						versionEnd := strings.Index(line[versionStart:], "\"")
						if versionEnd != -1 {
							version := line[versionStart : versionStart+versionEnd]
							deps[name] = version
						}
					}
				}
			}
		}
	}

	return deps
}

// TestDiagnostic_RegistryConsistency checks if we're reading the same data as Bazel
func TestDiagnostic_RegistryConsistency(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping diagnostic test in short mode")
	}

	// Let's check what versions are available for modules that show differences
	modulesToCheck := []string{"platforms", "rules_java", "zlib", "rules_cc", "rules_license"}

	for _, moduleName := range modulesToCheck {
		t.Logf("\n=== Checking available versions for %s ===", moduleName)

		// Fetch metadata.json to see available versions
		url := fmt.Sprintf("https://bcr.bazel.build/modules/%s/metadata.json", moduleName)

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)

		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			t.Logf("Failed to create request for %s metadata: %v", moduleName, err)
			cancel()
			continue
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Logf("Failed to fetch %s metadata: %v", moduleName, err)
			cancel()
			continue
		}

		if resp.StatusCode == 200 {
			content, err := io.ReadAll(resp.Body)
			if err == nil {
				var metadata map[string]interface{}
				if json.Unmarshal(content, &metadata) == nil {
					if versions, ok := metadata["versions"]; ok {
						t.Logf("Available versions for %s: %v", moduleName, versions)
					}
				}
			}
		} else {
			t.Logf("HTTP %d for %s metadata", resp.StatusCode, moduleName)
		}

		resp.Body.Close()
		cancel()
	}
}

// TestDiagnostic_DetailedMVSTrace traces exactly what our MVS algorithm is seeing
func TestDiagnostic_DetailedMVSTrace(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping diagnostic test in short mode")
	}

	// Let's look at a smaller case to trace the exact issue
	moduleContent := `
module(name = "mvs_trace", version = "1.0.0")

bazel_dep(name = "rules_go", version = "0.41.0")
`

	// Get our library's output with detailed tracing
	ourList, err := resolveDependencies(moduleContent, "https://bcr.bazel.build", false)
	if err != nil {
		t.Fatalf("Our dependency resolution failed: %v", err)
	}

	t.Logf("=== MVS TRACE ANALYSIS ===")
	t.Logf("Total modules resolved: %d", len(ourList.Modules))

	// Create a map to see what we resolved
	ourMap := make(map[string]string)
	for _, m := range ourList.Modules {
		ourMap[m.Name] = m.Version
	}

	// Now let's manually trace what should happen:
	// 1. rules_go@0.41.0 requires:
	//    - bazel_skylib@1.3.0
	//    - gazelle@0.32.0
	//    - platforms@0.0.7
	//    - protobuf@3.19.6
	//    - rules_proto@4.0.0

	expectedFromBazel := map[string]string{
		"rules_go":      "0.41.0",
		"bazel_skylib":  "1.3.0",
		"gazelle":       "0.32.0",
		"platforms":     "0.0.7",
		"protobuf":      "3.19.6",
		"rules_proto":   "4.0.0",
		"rules_cc":      "0.0.9",
		"rules_java":    "7.1.0",
		"rules_python":  "0.4.0",
		"zlib":          "1.3",
		"rules_license": "0.0.7",
	}

	t.Logf("\n=== EXPECTED vs ACTUAL ===")
	for moduleName, expectedVersion := range expectedFromBazel {
		if actualVersion, exists := ourMap[moduleName]; exists {
			if actualVersion == expectedVersion {
				t.Logf("✅ %s: %s (matches)", moduleName, actualVersion)
			} else {
				t.Logf("❌ %s: our=%s, expected=%s", moduleName, actualVersion, expectedVersion)

				// Let's investigate why we got the wrong version
				analyzeVersionSelection(t, moduleName, actualVersion, expectedVersion)
			}
		} else {
			t.Logf("❌ %s: MISSING (expected %s)", moduleName, expectedVersion)
		}
	}

	// Check for extra modules
	for moduleName, actualVersion := range ourMap {
		if _, expected := expectedFromBazel[moduleName]; !expected {
			t.Logf("⚠️  %s: %s (extra module)", moduleName, actualVersion)
		}
	}
}

// analyzeVersionSelection investigates why we selected the wrong version
func analyzeVersionSelection(t *testing.T, moduleName, ourVersion, expectedVersion string) {
	t.Logf("\n🔍 Analyzing %s version selection:", moduleName)

	// Let's check what each version requires
	ourContent := fetchModuleContent(t, moduleName, ourVersion)
	expectedContent := fetchModuleContent(t, moduleName, expectedVersion)

	if ourContent != "" && expectedContent != "" {
		ourDeps := extractDependencies(ourContent)
		expectedDeps := extractDependencies(expectedContent)

		t.Logf("  Our version (%s) deps: %v", ourVersion, ourDeps)
		t.Logf("  Expected version (%s) deps: %v", expectedVersion, expectedDeps)

		// The key insight: WHY didn't our MVS pick the higher version?
		// This suggests that the higher version wasn't in our dependency graph at all!
		t.Logf("  🔑 KEY QUESTION: Was %s@%s even discovered during dependency resolution?", moduleName, expectedVersion)
	}
}
