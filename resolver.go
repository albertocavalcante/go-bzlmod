package gobzlmod

import (
	"errors"
	"fmt"
	"net/http"
	"slices"

	"github.com/albertocavalcante/go-bzlmod/bazeltools"
	"github.com/albertocavalcante/go-bzlmod/internal/compat"
	"github.com/albertocavalcante/go-bzlmod/selection/version"
)

const (
	// defaultMaxConcurrency limits concurrent module fetches from the registry.
	defaultMaxConcurrency = 5

	builtinBazelToolsModule          = "bazel_tools"
	builtinLocalConfigPlatformModule = "local_config_platform"
)

func includeVisibleLocalConfigPlatform(bazelVersion string) bool {
	if bazelVersion == "" {
		return false
	}
	return version.Compare(bazelVersion, "9.0.0") < 0
}

func isNotFound(err error) bool {
	var regErr *RegistryError
	return errors.As(err, &regErr) && regErr.StatusCode == http.StatusNotFound
}

// bazelToolsRootDeps returns Bazel's built-in MODULE.tools dependencies as
// implicit root deps. They participate in selection but are hidden from the
// default visible graph.
func bazelToolsRootDeps(
	bazelVersion string,
	lookup BazelToolsLookup,
	transformer BazelToolsTransformer,
) []Dependency {
	if lookup == nil {
		lookup = bazeltools.LookupDeps
	}

	deps := lookup(bazelVersion)
	if transformer != nil {
		deps = transformer(bazelVersion, slices.Clone(deps))
	}
	if deps == nil {
		return nil
	}

	rootDeps := make([]Dependency, 0, len(deps))
	for _, toolDep := range deps {
		rootDeps = append(rootDeps, Dependency{
			Name:       toolDep.Name,
			Version:    toolDep.Version,
			IsNodepDep: true,
		})
	}
	return rootDeps
}

// checkFieldCompatibility checks if bzlmod fields used in the root module are
// compatible with the target Bazel version.
func checkFieldCompatibility(rootModule *ModuleInfo, bazelVersion string) []string {
	if bazelVersion == "" {
		return nil
	}

	var warnings []string
	for _, dep := range rootModule.Dependencies {
		if dep.MaxCompatibilityLevel > 0 {
			if w := compat.CheckField(bazelVersion, "max_compatibility_level"); w != nil {
				warnings = append(warnings,
					fmt.Sprintf("bazel_dep(%s): %s", dep.Name, w.String()))
				break
			}
		}
	}
	return warnings
}
