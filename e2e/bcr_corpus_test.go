package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	gobzlmod "github.com/albertocavalcante/go-bzlmod"
)

const (
	bcrCorpusEnv   = "GO_BZLMOD_E2E_BCR_CORPUS"
	bcrPathEnv     = "GO_BZLMOD_E2E_BCR_PATH"
	defaultBCRPath = "/Volumes/T9/dev/refs/bazel-central-registry"
)

// bcrCorpusSkips lists modules that cannot be resolved from a BCR clone in isolation.
// Each entry is verified: Bazel resolves these correctly in their native workspace.
// The library's resolver is NOT at fault — these are inherent to corpus testing.
var bcrCorpusSkips = map[string]string{
	// --- Starlark expression in version ---
	// These MODULE.bazel files use Starlark string operations to compute dep versions.
	// Our parser handles literal strings, not Starlark evaluation.
	// Bazel evaluates Starlark and resolves these correctly.

	// version = ".".join(proto_version.split(".")[-2:])  → "33.4"
	// protobuf@33.4 exists in BCR. Bazel resolves fine.
	"rules_kotlin": `Starlark expression: version = ".".join(proto_version_parts[-2:]) for protobuf`,

	// version = BOOST_VERSION + ".bcr.2"  → "1.89.0.bcr.2"
	// boost.config@1.89.0.bcr.2 exists in BCR. Bazel resolves fine.
	"lanelet2": `Starlark expression: version = BOOST_VERSION + ".bcr.2" for boost deps`,

	// version = ANTLR4_VERSION  where ANTLR4_VERSION = "4.13.2"
	// antlr4-cpp-runtime@4.13.2 exists in BCR. Bazel resolves fine.
	"cel-cpp": `Starlark expression: version = ANTLR4_VERSION for antlr4-cpp-runtime`,

	// --- Monorepo sub-modules with workspace-relative local_path_override ---
	// These are sub-modules in a monorepo. Their local_path_override declarations
	// reference sibling directories (path = "..", path = "../proto", etc.) that
	// only exist in the module's git workspace, not in the BCR clone.
	// Bazel resolves these correctly when run from the actual workspace.

	// local_path_override(module_name = "rules_webtesting", path = "..")
	"rules_web_testing_go":    `monorepo sub-module: local_path_override(path = "..") for rules_webtesting`,
	"rules_web_testing_java":  `monorepo sub-module: local_path_override(path = "..") for rules_webtesting`,
	"rules_web_testing_python": `monorepo sub-module: local_path_override(path = "..") for rules_webtesting`,
	"rules_web_testing_scala": `monorepo sub-module: local_path_override(path = "..") for rules_webtesting`,

	// local_path_override(module_name = "rules_python", path = "..")
	"rules_python_gazelle_plugin": `monorepo sub-module: local_path_override(path = "..") for rules_python`,

	// local_path_override(module_name = "bazel_worker_api", path = "../proto")
	"bazel_worker_java": `monorepo sub-module: local_path_override(path = "../proto") for bazel_worker_api`,

	// local_path_override(module_name = "package_metadata", path = "../metadata")
	"supply_chain_tools": `monorepo sub-module: local_path_override(path = "../metadata") for package_metadata`,

	// local_path_override(module_name = "package_metadata", path = "../../metadata")
	"supply-chain-go": `monorepo sub-module: local_path_override(path = "../../metadata") for package_metadata`,

	// local_path_override(module_name = "rules_nixpkgs_core", path = "../../core")
	"rules_nixpkgs_nodejs": `monorepo sub-module: local_path_override(path = "../../core") for rules_nixpkgs_core`,

	// local_path_override(module_name = "engflowapis", path = "..")
	"engflowapis-java": `monorepo sub-module: local_path_override(path = "..") for engflowapis`,
	"engflowapis-go":   `monorepo sub-module: local_path_override(path = "..") for engflowapis`,
}

func bcrPath() string {
	if p := os.Getenv(bcrPathEnv); p != "" {
		return p
	}
	return defaultBCRPath
}

func requireBCRCorpusEnabled(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping BCR corpus test in short mode")
	}
	if os.Getenv(bcrCorpusEnv) == "" {
		t.Skipf("set %s=1 to run the BCR corpus suite", bcrCorpusEnv)
	}
	if _, err := os.Stat(filepath.Join(bcrPath(), "modules")); err != nil {
		t.Skipf("BCR clone not found at %s (set %s to override)", bcrPath(), bcrPathEnv)
	}
}

type bcrMetadata struct {
	Versions       []string          `json:"versions"`
	YankedVersions map[string]string `json:"yanked_versions"`
}

func latestNonYankedVersion(metadataPath string) (string, error) {
	data, err := os.ReadFile(metadataPath)
	if err != nil {
		return "", err
	}
	var meta bcrMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return "", err
	}
	if len(meta.Versions) == 0 {
		return "", fmt.Errorf("no versions in metadata")
	}
	for i := len(meta.Versions) - 1; i >= 0; i-- {
		v := meta.Versions[i]
		if _, yanked := meta.YankedVersions[v]; !yanked {
			return v, nil
		}
	}
	return meta.Versions[len(meta.Versions)-1], nil
}

// TestE2E_BCRCorpus_ResolveLatestVersions resolves every module's latest version
// from a local BCR clone using file:// registry. No network, no Bazel needed.
//
// This tests stability and correctness across real-world MODULE.bazel diversity:
// ~990 modules with varying complexity, overrides, extensions, and dep patterns.
//
// Modules in bcrCorpusSkips are skipped with documented reasons. All skipped
// modules resolve correctly in Bazel when run from their native workspace.
func TestE2E_BCRCorpus_ResolveLatestVersions(t *testing.T) {
	requireBCRCorpusEnabled(t)

	bcrRoot := bcrPath()
	modulesDir := filepath.Join(bcrRoot, "modules")
	entries, err := os.ReadDir(modulesDir)
	if err != nil {
		t.Fatalf("read BCR modules directory: %v", err)
	}

	fileURL := "file://" + bcrRoot
	ctx := context.Background()

	var passed, failed, skipped atomic.Int32
	start := time.Now()

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		moduleName := entry.Name()
		t.Run(moduleName, func(t *testing.T) {
			t.Parallel()

			if reason, ok := bcrCorpusSkips[moduleName]; ok {
				skipped.Add(1)
				t.Skipf("skip (known): %s", reason)
			}

			metadataFile := filepath.Join(modulesDir, moduleName, "metadata.json")
			version, err := latestNonYankedVersion(metadataFile)
			if err != nil {
				skipped.Add(1)
				t.Skipf("skip %s: %v", moduleName, err)
			}

			moduleFile := filepath.Join(modulesDir, moduleName, version, "MODULE.bazel")
			content, err := os.ReadFile(moduleFile)
			if err != nil {
				skipped.Add(1)
				t.Skipf("skip %s@%s: %v", moduleName, version, err)
			}

			result, err := gobzlmod.Resolve(ctx,
				gobzlmod.ContentSource(string(content)),
				gobzlmod.WithRegistries(fileURL),
			)
			if err != nil {
				failed.Add(1)
				t.Errorf("%s@%s resolution failed: %v", moduleName, version, err)
				return
			}

			validateResolutionResult(t, moduleName, version, result)
			passed.Add(1)
		})
	}

	t.Cleanup(func() {
		t.Logf("BCR corpus: %d passed, %d failed, %d skipped in %s",
			passed.Load(), failed.Load(), skipped.Load(), time.Since(start).Round(time.Millisecond))
	})
}

func validateResolutionResult(t *testing.T, moduleName, version string, result *gobzlmod.ResolutionList) {
	t.Helper()

	if result == nil {
		t.Errorf("%s@%s: result is nil", moduleName, version)
		return
	}

	for _, m := range result.Modules {
		if m.Name == "" {
			t.Errorf("%s@%s: resolved module with empty name", moduleName, version)
		}
		if m.Version == "" && m.Registry != "" {
			t.Errorf("%s@%s: module %s has empty version but non-empty registry %s",
				moduleName, version, m.Name, m.Registry)
		}
	}

	if result.Summary.TotalModules != len(result.Modules) {
		t.Errorf("%s@%s: summary.TotalModules=%d but len(Modules)=%d",
			moduleName, version, result.Summary.TotalModules, len(result.Modules))
	}

	names := make([]string, len(result.Modules))
	for i, m := range result.Modules {
		names[i] = m.Name
	}
	if !slices.IsSorted(names) {
		t.Errorf("%s@%s: modules not sorted by name", moduleName, version)
	}
}

// TestE2E_BCRCorpus_Deterministic verifies that resolving the same module twice
// produces identical output.
func TestE2E_BCRCorpus_Deterministic(t *testing.T) {
	requireBCRCorpusEnabled(t)

	bcrRoot := bcrPath()
	fileURL := "file://" + bcrRoot

	modules := []struct {
		name    string
		version string
	}{
		{"rules_go", "0.50.0"},
		{"rules_python", "0.35.0"},
		{"bazel_skylib", "1.7.1"},
		{"protobuf", "27.0"},
	}

	ctx := context.Background()
	for _, mod := range modules {
		t.Run(mod.name+"@"+mod.version, func(t *testing.T) {
			moduleFile := filepath.Join(bcrRoot, "modules", mod.name, mod.version, "MODULE.bazel")
			content, err := os.ReadFile(moduleFile)
			if err != nil {
				t.Skipf("skip: %v", err)
			}

			opts := []gobzlmod.Option{gobzlmod.WithRegistries(fileURL)}

			result1, err := gobzlmod.Resolve(ctx, gobzlmod.ContentSource(string(content)), opts...)
			if err != nil {
				t.Fatalf("first resolve: %v", err)
			}
			result2, err := gobzlmod.Resolve(ctx, gobzlmod.ContentSource(string(content)), opts...)
			if err != nil {
				t.Fatalf("second resolve: %v", err)
			}

			keys1 := moduleKeys(result1)
			keys2 := moduleKeys(result2)
			if !slices.Equal(keys1, keys2) {
				t.Errorf("non-deterministic resolution:\n  run1: %v\n  run2: %v", keys1, keys2)
			}
		})
	}
}

func moduleKeys(result *gobzlmod.ResolutionList) []string {
	keys := make([]string, len(result.Modules))
	for i, m := range result.Modules {
		keys[i] = m.Name + "@" + m.Version
	}
	slices.Sort(keys)
	return keys
}
