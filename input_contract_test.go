package gobzlmod

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestComputedCoordinatesReturnUnsupported(t *testing.T) {
	for _, declaration := range []string{
		`bazel_dep(name="alpha", version=VERSION)`,
		`bazel_dep(name="alpha", version="1." + "0")`,
		`bazel_dep(name="alpha", version=".".join(["1", "0"]))`,
		`bazel_dep(name=NAME, version="1.0")`,
		`single_version_override(module_name="alpha", version=VERSION)`,
		`multiple_version_override(module_name="alpha", versions=VERSIONS)`,
		`multiple_version_override(module_name="alpha", versions=["1.0", VERSION])`,
		`local_path_override(module_name="alpha", path=PATH)`,
		`bazel_dep(name="alpha",version="1.0",dev_dependency=DEV)`,
		`bazel_dep(name="alpha",version="1.0",max_compatibility_level=LEVEL)`,
	} {
		t.Run(declaration, func(t *testing.T) {
			_, err := ParseModuleContent(discoveryModule("root", "1.0") + declaration)
			var unsupported *UnsupportedInputError
			if !errors.Is(err, errors.ErrUnsupported) || !errors.As(err, &unsupported) || unsupported.Attribute == "" {
				t.Fatalf("want actionable unsupported attribute, got %v", err)
			}
		})
	}
}

func TestIncludedFileIsNotSilentlyIgnored(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "MODULE.bazel"), []byte(discoveryModule("root", "1.0")+`include("//:deps.MODULE.bazel")`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "deps.MODULE.bazel"), []byte(discoveryDep("alpha", "1.0")), 0600); err != nil {
		t.Fatal(err)
	}
	result, err := Resolve(context.Background(), FileSource(filepath.Join(dir, "MODULE.bazel")))
	if result != nil || !errors.Is(err, errors.ErrUnsupported) {
		t.Fatalf("included dependencies were not rejected explicitly: result=%v err=%v", result, err)
	}
}

func TestRemoteSourceOverridesReturnUnsupported(t *testing.T) {
	for _, directive := range []string{"git_override", "archive_override"} {
		t.Run(directive, func(t *testing.T) {
			root := discoveryModule("root", "1.0") + discoveryDep("alpha", "1.0") + directive + `(module_name="alpha")`
			result, err := Resolve(context.Background(), ContentSource(root))
			if result != nil || !errors.Is(err, errors.ErrUnsupported) {
				t.Fatalf("want explicit unsupported source discovery, got result=%v err=%v", result, err)
			}
		})
	}
}

func TestExplicitZeroCompatibilityConstraint(t *testing.T) {
	for _, tc := range []struct {
		maximum, level int
		wantError      bool
	}{{0, 0, false}, {0, 1, true}, {-1, 1, false}} {
		t.Run(fmt.Sprintf("max_%d_level_%d", tc.maximum, tc.level), func(t *testing.T) {
			root := discoveryModule("root", "1.0") + fmt.Sprintf("bazel_dep(name='alpha',version='1.0',max_compatibility_level=%d)", tc.maximum)
			parsed, err := ParseModuleContent(root)
			if err != nil {
				t.Fatal(err)
			}
			if !parsed.Dependencies[0].MaxCompatibilityLevelSet || parsed.Dependencies[0].MaxCompatibilityLevel != tc.maximum {
				t.Fatal("explicit compatibility constraint intent lost")
			}
			files := map[string]string{"/modules/alpha/1.0/MODULE.bazel": fmt.Sprintf("module(name='alpha',version='1.0',compatibility_level=%d)", tc.level)}
			_, err = Resolve(context.Background(), ContentSource(root), WithRegistries("http://registry.test"), WithHTTPClient(discoveryHTTPClient(files, nil)))
			if (err != nil) != tc.wantError {
				t.Fatalf("resolution error=%v, wantError=%v", err, tc.wantError)
			}
		})
	}
}

func TestRegistryCoordinateAllowsComputedSelfVersion(t *testing.T) {
	files := map[string]string{"/modules/alpha/1.0/MODULE.bazel": "module(name='alpha',version=BUILD_VERSION)"}
	result, err := Resolve(context.Background(), RegistrySource{Name: "alpha", Version: "1.0"}, WithRegistries("http://registry.test"), WithHTTPClient(discoveryHTTPClient(files, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if m := result.Module("alpha"); m == nil || m.Version != "1.0" {
		t.Fatalf("registry coordinate was not authoritative: %v", m)
	}
}
