package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	gobzlmod "github.com/albertocavalcante/go-bzlmod"
)

const (
	bcrCorpusEnv = "GO_BZLMOD_E2E_BCR_CORPUS"
	bcrPathEnv   = "GO_BZLMOD_E2E_BCR_PATH"
	defaultBCRPath = "/Volumes/T9/dev/refs/bazel-central-registry"
)

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
	// Walk versions in reverse (latest first), skip yanked
	for i := len(meta.Versions) - 1; i >= 0; i-- {
		v := meta.Versions[i]
		if _, yanked := meta.YankedVersions[v]; !yanked {
			return v, nil
		}
	}
	// All versions yanked — use the latest anyway
	return meta.Versions[len(meta.Versions)-1], nil
}

func hasLocalPathOverride(moduleContent string) bool {
	return strings.Contains(moduleContent, "local_path_override")
}

func hasArchiveOverride(moduleContent string) bool {
	return strings.Contains(moduleContent, "archive_override")
}

// TestE2E_BCRCorpus_ResolveLatestVersions resolves every module's latest version
// from a local BCR clone using file:// registry. No network, no Bazel needed.
//
// This tests stability and correctness across real-world MODULE.bazel diversity:
// 990 modules with varying complexity, overrides, extensions, and dep patterns.
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

			// Skip modules with local_path_override — they reference workspace-relative
			// paths that don't exist in the BCR clone.
			if hasLocalPathOverride(string(content)) {
				skipped.Add(1)
				t.Skipf("skip %s@%s: has local_path_override", moduleName, version)
			}

			// Skip modules with archive_override — they reference external archives
			// that the file:// registry can't resolve.
			if hasArchiveOverride(string(content)) {
				skipped.Add(1)
				t.Skipf("skip %s@%s: has archive_override", moduleName, version)
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

	// Basic structural checks
	for _, m := range result.Modules {
		if m.Name == "" {
			t.Errorf("%s@%s: resolved module with empty name", moduleName, version)
		}
		// Non-override modules should have non-empty versions
		if m.Version == "" && m.Registry != "" {
			t.Errorf("%s@%s: module %s has empty version but non-empty registry %s",
				moduleName, version, m.Name, m.Registry)
		}
	}

	// Summary should be consistent
	if result.Summary.TotalModules != len(result.Modules) {
		t.Errorf("%s@%s: summary.TotalModules=%d but len(Modules)=%d",
			moduleName, version, result.Summary.TotalModules, len(result.Modules))
	}

	// Modules should be sorted by name
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

	// Test a few well-known modules for determinism
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
