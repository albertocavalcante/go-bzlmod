package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gobzlmod "github.com/albertocavalcante/go-bzlmod"
	"github.com/albertocavalcante/go-bzlmod/graph"
)

// Type aliases for easier usage in tests
type ModuleToResolve = gobzlmod.ModuleToResolve
type ResolutionSummary = gobzlmod.ResolutionSummary
type ResolutionList = gobzlmod.ResolutionList

// Type aliases for Bazel graph types from graph package
type BazelDependency = graph.BazelDependency
type BazelModGraph = graph.BazelModGraph

// resolveDependencies uses our library API to resolve dependencies
func resolveDependencies(content, registry string, includeDevDeps bool) (*ResolutionList, error) {
	return resolveDependenciesWithBazelVersion(content, registry, includeDevDeps, "7.0.0")
}

// resolveDependenciesWithBazelVersion resolves with a specific Bazel version for MODULE.tools compat
func resolveDependenciesWithBazelVersion(content, registry string, includeDevDeps bool, bazelVersion string) (*ResolutionList, error) {
	opts := gobzlmod.ResolutionOptions{
		Registries:       []string{registry},
		IncludeDevDeps:   includeDevDeps,
		SubstituteYanked: true,
		BazelVersion:     bazelVersion,
	}
	ctx := context.Background()
	resolutionList, err := gobzlmod.Resolve(ctx, content, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve dependencies: %v", err)
	}

	return resolutionList, nil
}

// BazelModuleInfo represents a flattened module for easier comparison
type BazelModuleInfo struct {
	Key     string `json:"key"`
	Name    string `json:"name"`
	Version string `json:"version"`
}

// createTestWorkspace creates a temporary workspace with MODULE.bazel and WORKSPACE files
func createTestWorkspace(t *testing.T, moduleContent string) string {
	tmpDir, err := os.MkdirTemp("", "bazel-e2e-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}

	// Create MODULE.bazel file
	moduleFile := filepath.Join(tmpDir, "MODULE.bazel")
	err = os.WriteFile(moduleFile, []byte(moduleContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write MODULE.bazel: %v", err)
	}

	// Create empty WORKSPACE file (required for Bazel)
	workspaceFile := filepath.Join(tmpDir, "WORKSPACE")
	err = os.WriteFile(workspaceFile, []byte(""), 0644)
	if err != nil {
		t.Fatalf("Failed to write WORKSPACE: %v", err)
	}

	// Create .bazelversion file to ensure consistent Bazel version
	bazelVersionFile := filepath.Join(tmpDir, ".bazelversion")
	err = os.WriteFile(bazelVersionFile, []byte("7.0.0"), 0644)
	if err != nil {
		t.Fatalf("Failed to write .bazelversion: %v", err)
	}

	return tmpDir
}

// runBazelModGraph executes 'bazel mod graph --output=json' using bazelisk binary
func runBazelModGraph(t *testing.T, workspaceDir string) (*BazelModGraph, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	// Build bazelisk from the Go module
	bazeliskPath := filepath.Join(os.TempDir(), "bazelisk-e2e")
	buildCmd := exec.CommandContext(ctx, "go", "build", "-o", bazeliskPath, "github.com/bazelbuild/bazelisk")
	if err := buildCmd.Run(); err != nil {
		// Try to use system bazelisk or bazel if available
		if bazeliskBin, err := exec.LookPath("bazelisk"); err == nil {
			bazeliskPath = bazeliskBin
		} else if bazelBin, err := exec.LookPath("bazel"); err == nil {
			bazeliskPath = bazelBin
		} else {
			return nil, fmt.Errorf("failed to build bazelisk and no system bazel/bazelisk found: %v", err)
		}
	} else {
		defer os.Remove(bazeliskPath)
	}

	// Set up environment
	env := os.Environ()
	env = append(env, "USE_BAZEL_VERSION=7.0.0")

	// Run bazel mod graph --output=json using bazelisk
	args := []string{"mod", "graph", "--output=json"}
	cmd := exec.CommandContext(ctx, bazeliskPath, args...)
	cmd.Dir = workspaceDir
	cmd.Env = env

	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("bazel mod graph failed: %v\nStderr: %s\nStdout: %s", err, exitErr.Stderr, output)
		}
		return nil, fmt.Errorf("failed to run bazel mod graph: %v", err)
	}

	// Debug: log the raw output
	t.Logf("Bazel mod graph raw output: %s", string(output))

	// Parse JSON output
	var modGraph BazelModGraph
	if err := json.Unmarshal(output, &modGraph); err != nil {
		return nil, fmt.Errorf("failed to parse bazel mod graph JSON: %v\nOutput: %s", err, output)
	}

	return &modGraph, nil
}

// flattenBazelGraph recursively flattens the Bazel dependency graph into a list of modules
func flattenBazelGraph(graph *BazelModGraph) []BazelModuleInfo {
	modules := make(map[string]BazelModuleInfo)

	// Skip root module - we only compare dependencies
	// The root module has Key="<root>" and is not a dependency

	// Recursively process dependencies
	var processDeps func(deps []BazelDependency)
	processDeps = func(deps []BazelDependency) {
		for _, dep := range deps {
			if dep.Key != "" && !dep.Unexpanded {
				// Parse module name and version from key (format: "name@version")
				parts := strings.Split(dep.Key, "@")
				if len(parts) == 2 {
					modules[dep.Key] = BazelModuleInfo{
						Key:     dep.Key,
						Name:    parts[0],
						Version: parts[1],
					}
				}
			}
			// Recursively process nested dependencies
			processDeps(dep.Dependencies)
			processDeps(dep.IndirectDependencies)
		}
	}

	processDeps(graph.Dependencies)
	processDeps(graph.IndirectDependencies)

	// Convert map to slice
	var result []BazelModuleInfo
	for _, module := range modules {
		result = append(result, module)
	}

	return result
}

// normalizeModuleName normalizes module names for comparison (removes version suffixes if any)
func normalizeModuleName(name string) string {
	// Remove any version suffixes that might be in the name
	if idx := strings.Index(name, "@"); idx != -1 {
		return name[:idx]
	}
	return name
}

// compareModuleLists compares our module list with Bazel's module list
func compareModuleLists(t *testing.T, ourModules []ModuleToResolve, bazelModules []BazelModuleInfo) {
	// Create maps for easier comparison
	ourModuleMap := make(map[string]ModuleToResolve)
	for _, module := range ourModules {
		key := normalizeModuleName(module.Name)
		ourModuleMap[key] = module
	}

	bazelModuleMap := make(map[string]BazelModuleInfo)
	for _, module := range bazelModules {
		key := normalizeModuleName(module.Name)
		// Skip empty names and internal references
		if key == "" || strings.Contains(key, "<root>") || strings.Contains(key, "@@") {
			continue
		}
		bazelModuleMap[key] = module
	}

	t.Logf("Our modules: %v", getModuleNames(ourModules))
	t.Logf("Bazel modules: %v", getBazelModuleNames(bazelModules))

	// Check if we have all the modules that Bazel has (excluding root)
	for moduleName, bazelModule := range bazelModuleMap {
		ourModule, exists := ourModuleMap[moduleName]
		if !exists {
			t.Errorf("Module %s found in Bazel output but not in our output", moduleName)
			continue
		}

		// Compare versions if both have them
		if bazelModule.Version != "" && ourModule.Version != "" {
			if bazelModule.Version != ourModule.Version {
				t.Errorf("Version mismatch for module %s: our=%s, bazel=%s",
					moduleName, ourModule.Version, bazelModule.Version)
			}
		}

		// Note: Bazel mod graph doesn't include registry info, so we skip registry comparison
	}

	// Check for extra modules in our output (might be expected for transitive deps)
	for moduleName := range ourModuleMap {
		if _, exists := bazelModuleMap[moduleName]; !exists {
			t.Logf("Module %s found in our output but not in Bazel output (might be filtered)", moduleName)
		}
	}
}

// Helper function to get module names for logging
func getModuleNames(modules []ModuleToResolve) []string {
	names := make([]string, len(modules))
	for i, m := range modules {
		names[i] = fmt.Sprintf("%s@%s", m.Name, m.Version)
	}
	return names
}

func getBazelModuleNames(modules []BazelModuleInfo) []string {
	var names []string
	for _, m := range modules {
		if m.Name != "" && !strings.Contains(m.Name, "<root>") && !strings.Contains(m.Name, "@@") {
			names = append(names, fmt.Sprintf("%s@%s", m.Name, m.Version))
		}
	}
	return names
}

func TestE2E_SimpleModuleDependencies(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}

	moduleContent := `
module(name = "test_project", version = "1.0.0")

bazel_dep(name = "rules_go", version = "0.41.0")
bazel_dep(name = "gazelle", version = "0.32.0")
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

	// Flatten Bazel's dependency graph into a list of modules
	bazelModules := flattenBazelGraph(bazelModGraph)

	// Compare outputs
	t.Logf("Our library found %d modules", len(ourList.Modules))
	t.Logf("Bazel found %d modules", len(bazelModules))

	compareModuleLists(t, ourList.Modules, bazelModules)
}

func TestE2E_ModuleWithOverrides(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}

	moduleContent := `
module(name = "test_project", version = "1.0.0")

bazel_dep(name = "rules_go", version = "0.41.0")

single_version_override(
    module_name = "rules_go",
    version = "0.40.0",
)
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

	// Flatten Bazel's dependency graph into a list of modules
	bazelModules := flattenBazelGraph(bazelModGraph)

	// Compare outputs - should show the overridden version
	t.Logf("Our library found %d modules", len(ourList.Modules))
	t.Logf("Bazel found %d modules", len(bazelModules))

	// Verify that the override was applied
	var foundRulesGo bool
	for _, module := range ourList.Modules {
		if module.Name == "rules_go" {
			foundRulesGo = true
			if module.Version != "0.40.0" {
				t.Errorf("Expected rules_go version 0.40.0 due to override, got %s", module.Version)
			}
			break
		}
	}
	if !foundRulesGo {
		t.Error("rules_go module not found in our output")
	}

	compareModuleLists(t, ourList.Modules, bazelModules)
}

func TestE2E_JSONOutputComparison(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}

	moduleContent := `
module(name = "comparison_test", version = "1.0.0")

bazel_dep(name = "rules_go", version = "0.41.0")
`

	// Create test workspace
	workspaceDir := createTestWorkspace(t, moduleContent)
	defer os.RemoveAll(workspaceDir)

	// Get our library's JSON output
	ourList, err := resolveDependencies(moduleContent, "https://bcr.bazel.build", false)
	if err != nil {
		t.Fatalf("Our dependency resolution failed: %v", err)
	}

	ourJSON, err := json.MarshalIndent(ourList, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal our output to JSON: %v", err)
	}

	// Get Bazel's output
	bazelModGraph, err := runBazelModGraph(t, workspaceDir)
	if err != nil {
		t.Fatalf("Bazel mod graph failed: %v", err)
	}

	bazelJSON, err := json.MarshalIndent(bazelModGraph, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal Bazel output to JSON: %v", err)
	}

	// Flatten Bazel's dependency graph for comparison
	bazelModules := flattenBazelGraph(bazelModGraph)

	// Log both outputs for comparison
	t.Logf("Our JSON output:\n%s", ourJSON)
	t.Logf("Bazel JSON output:\n%s", bazelJSON)
	t.Logf("Flattened Bazel modules: %v", getBazelModuleNames(bazelModules))

	// The JSON structures are different by design, but we can still compare core modules
	compareModuleLists(t, ourList.Modules, bazelModules)
}
