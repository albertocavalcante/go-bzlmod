// Package gobzlmod provides a Go library for Bazel module dependency resolution
// using Minimal Version Selection (MVS).
//
// This library implements pure MVS as described in Russ Cox's research
// (https://research.swtch.com/vgo-mvs), which selects the minimum version that
// satisfies all requirements in the dependency graph.
//
// # Overview
//
// The package provides three main components:
//
//   - Parser: Parses MODULE.bazel files to extract module information
//   - Registry: Fetches module metadata from Bazel Central Registry (BCR)
//   - Resolver: Resolves transitive dependencies using MVS
//
// # Quick Start
//
// The simplest way to resolve dependencies is using the high-level API:
//
//	result, err := gobzlmod.ResolveDependenciesFromFile(
//	    "MODULE.bazel",
//	    "https://bcr.bazel.build",
//	    false, // include dev dependencies
//	)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	for _, mod := range result.Modules {
//	    fmt.Printf("%s@%s\n", mod.Name, mod.Version)
//	}
//
// # Differences from Bazel's Algorithm
//
// Bazel's actual resolution algorithm includes additional features not implemented here:
//   - Compatibility level checking and automatic upgrades
//   - Yanked version handling
//   - Multiple version override support
//   - Module extension resolution
//
// For production use requiring full Bazel compatibility, see the selection package
// which implements Bazel's complete algorithm.
//
// # Thread Safety
//
// All public types in this package are safe for concurrent use.
package gobzlmod

import (
	"context"
	"fmt"
)

// Example API demonstrating how to use the dependency resolver as a library

// ResolveDependenciesFromFile loads a MODULE.bazel file and resolves all dependencies using MVS.
func ResolveDependenciesFromFile(moduleFilePath, registryURL string, includeDevDeps bool) (*ResolutionList, error) {
	moduleInfo, err := ParseModuleFile(moduleFilePath)
	if err != nil {
		return nil, fmt.Errorf("parse module file: %w", err)
	}

	registry := NewRegistryClient(registryURL)
	resolver := NewDependencyResolver(registry, includeDevDeps)
	return resolver.ResolveDependencies(context.Background(), moduleInfo)
}

// ResolveDependenciesFromContent resolves dependencies from MODULE.bazel content using MVS.
func ResolveDependenciesFromContent(moduleContent, registryURL string, includeDevDeps bool) (*ResolutionList, error) {
	return ResolveDependenciesWithContext(context.Background(), moduleContent, registryURL, includeDevDeps)
}

// ResolveDependenciesWithContext resolves dependencies with a custom context for timeout/cancellation.
func ResolveDependenciesWithContext(ctx context.Context, moduleContent, registryURL string, includeDevDeps bool) (*ResolutionList, error) {
	moduleInfo, err := ParseModuleContent(moduleContent)
	if err != nil {
		return nil, fmt.Errorf("parse module content: %w", err)
	}

	registry := NewRegistryClient(registryURL)
	resolver := NewDependencyResolver(registry, includeDevDeps)
	return resolver.ResolveDependencies(ctx, moduleInfo)
}
