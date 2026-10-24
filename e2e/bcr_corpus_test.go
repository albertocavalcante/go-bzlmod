package e2e

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
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
	// --- Monorepo sub-modules with workspace-relative local_path_override ---
	// These are sub-modules in a monorepo. Their local_path_override declarations
	// reference sibling directories (path = "..", path = "../proto", etc.) that
	// only exist in the module's git workspace, not in the BCR clone.
	// Bazel resolves these correctly when run from the actual workspace.

	// local_path_override(module_name = "rules_webtesting", path = "..")
	"rules_web_testing_go":     `monorepo sub-module: local_path_override(path = "..") for rules_webtesting`,
	"rules_web_testing_java":   `monorepo sub-module: local_path_override(path = "..") for rules_webtesting`,
	"rules_web_testing_python": `monorepo sub-module: local_path_override(path = "..") for rules_webtesting`,
	"rules_web_testing_scala":  `monorepo sub-module: local_path_override(path = "..") for rules_webtesting`,

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

// These are explicit static-resolution exclusions for the pinned corpus. Each
// must continue to return its specific unsupported-input error; a new success or
// a different error requires review, rather than an unconditional skip.
var bcrUnsupportedInputs = map[string]struct{ directive, attribute, via string }{
	"rules_kotlin":        {"bazel_dep", "version", ""},
	"lanelet2":            {"bazel_dep", "version", ""},
	"cel-cpp":             {"bazel_dep", "version", ""},
	"protovalidate-cc":    {"bazel_dep", "version", "cel-cpp@0.11.0"},
	"rules_pitest":        {"bazel_dep", "version", "contrib_rules_jvm@0.24.0"},
	"rules_cc_hdrs_map":   {"archive_override", "", "rules_cc"},
	"or-tools":            {"git_override", "", "pybind11_abseil"},
	"jsinterop_base":      {"archive_override", "", "j2cl"},
	"jsinterop_generator": {"archive_override", "", "j2cl"},
	"elemental2":          {"archive_override", "", "j2cl"},
	"rules_java":          {"archive_override", "", "rules_cc"},
	"aspect_rules_py":     {"archive_override", "", "toolchains_llvm_bootstrapped"},
}

const bcrCorpusRevision = "ab4017a7417cbd1e7eba34cb55aba99aa2be9808"

func bcrPath() string {
	if p := os.Getenv(bcrPathEnv); p != "" {
		return p
	}
	return defaultBCRPath
}

func requireBCRCorpusEnabled(t *testing.T) {
	t.Helper()
	if testing.Short() {
		if os.Getenv(bcrCorpusEnv) != "" {
			t.Fatal("explicit corpus request cannot run in short mode")
		}
		t.Skip("skipping BCR corpus test in short mode")
	}
	if os.Getenv(bcrCorpusEnv) == "" {
		t.Skipf("set %s=1 to run the BCR corpus suite", bcrCorpusEnv)
	}
	if _, err := os.Stat(filepath.Join(bcrPath(), "modules")); err != nil {
		t.Fatalf("BCR clone not found at %s (set %s to override)", bcrPath(), bcrPathEnv)
	}
	expected := os.Getenv("GO_BZLMOD_E2E_BCR_REVISION")
	if expected == "" {
		expected = bcrCorpusRevision
	}
	head, err := exec.Command("git", "-C", bcrPath(), "rev-parse", "HEAD").Output()
	if err != nil || strings.TrimSpace(string(head)) != expected {
		t.Fatalf("BCR corpus must be at %s; got %s (%v)", expected, strings.TrimSpace(string(head)), err)
	}
	status, err := exec.Command("git", "-C", bcrPath(), "status", "--porcelain").Output()
	if err != nil || len(status) != 0 {
		t.Fatal("BCR corpus must be a complete, clean git checkout")
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

	var passed, failed, skipped, unsupported atomic.Int32
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
				failed.Add(1)
				t.Fatalf("missing or invalid metadata for %s: %v", moduleName, err)
			}

			moduleFile := filepath.Join(modulesDir, moduleName, version, "MODULE.bazel")
			content, err := os.ReadFile(moduleFile)
			if err != nil {
				failed.Add(1)
				t.Fatalf("missing MODULE.bazel for %s@%s: %v", moduleName, version, err)
			}

			result, err := gobzlmod.Resolve(ctx,
				gobzlmod.ContentSource(string(content)),
				gobzlmod.WithRegistries(fileURL),
			)
			if expected, ok := bcrUnsupportedInputs[moduleName]; ok {
				var detail *gobzlmod.UnsupportedInputError
				if !errors.As(err, &detail) || detail.Directive != expected.directive || detail.Attribute != expected.attribute || !strings.Contains(err.Error(), expected.via) {
					failed.Add(1)
					t.Fatalf("unsupported-input classification drift: want %+v; got %v", expected, err)
				}
				unsupported.Add(1)
				t.Logf("expected unsupported input: %v", err)
				return
			}

			if err != nil {
				failed.Add(1)
				t.Errorf("%s@%s resolution failed: %v", moduleName, version, err)
				return
			}

			validateResolutionResult(t, moduleName, version, result)
			if t.Failed() {
				failed.Add(1)
			} else {
				passed.Add(1)
			}
		})
	}

	t.Cleanup(func() {
		t.Logf("BCR corpus: %d resolved, %d expected unsupported, %d failed, %d skipped in %s",
			passed.Load(), unsupported.Load(), failed.Load(), skipped.Load(), time.Since(start).Round(time.Millisecond))
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
