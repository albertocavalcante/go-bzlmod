// Package gobzlmod provides a Go library for Bazel module dependency resolution
// using Minimal Version Selection (MVS).
//
// This library implements pure MVS as described in Russ Cox's research, which differs
// from Bazel's more complex algorithm that includes automatic version upgrades and
// compatibility mappings.
package gobzlmod

import (
	"context"
	"fmt"
)

// Example API demonstrating how to use the dependency resolver as a library

// ResolveDependenciesFromFile is a convenient API function that loads a MODULE.bazel file
// and resolves all its dependencies using Minimal Version Selection.
//
// Parameters:
//   - moduleFilePath: Path to the MODULE.bazel file to parse
//   - registryURL: URL of the Bazel registry (e.g., "https://bcr.bazel.build")
//   - includeDevDeps: Whether to include development dependencies in the resolution
//
// Returns a ResolutionList containing all resolved modules and their metadata.
func ResolveDependenciesFromFile(moduleFilePath string, registryURL string, includeDevDeps bool) (*ResolutionList, error) {
	// Parse the MODULE.bazel file
	moduleInfo, err := ParseModuleFile(moduleFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to parse module file: %v", err)
	}

	// Create registry client
	registry := NewRegistryClient(registryURL)

	// Create resolver
	resolver := NewDependencyResolver(registry, includeDevDeps)

	// Resolve dependencies
	ctx := context.Background()
	return resolver.ResolveDependencies(ctx, moduleInfo)
}

// ResolveDependenciesFromContent resolves dependencies from MODULE.bazel content string
// using Minimal Version Selection.
//
// Parameters:
//   - moduleContent: Raw content of a MODULE.bazel file as a string
//   - registryURL: URL of the Bazel registry (e.g., "https://bcr.bazel.build")
//   - includeDevDeps: Whether to include development dependencies in the resolution
//
// Returns a ResolutionList containing all resolved modules and their metadata.
func ResolveDependenciesFromContent(moduleContent string, registryURL string, includeDevDeps bool) (*ResolutionList, error) {
	// Parse the MODULE.bazel content
	moduleInfo, err := ParseModuleContent(moduleContent)
	if err != nil {
		return nil, fmt.Errorf("failed to parse module content: %v", err)
	}

	// Create registry client
	registry := NewRegistryClient(registryURL)

	// Create resolver
	resolver := NewDependencyResolver(registry, includeDevDeps)

	// Resolve dependencies
	ctx := context.Background()
	return resolver.ResolveDependencies(ctx, moduleInfo)
}

// ResolveDependenciesWithContext resolves dependencies from MODULE.bazel content with a custom context.
// This allows for timeout control and cancellation.
//
// Parameters:
//   - ctx: Context for cancellation and timeouts
//   - moduleContent: Raw content of a MODULE.bazel file as a string
//   - registryURL: URL of the Bazel registry (e.g., "https://bcr.bazel.build")
//   - includeDevDeps: Whether to include development dependencies in the resolution
//
// Returns a ResolutionList containing all resolved modules and their metadata.
func ResolveDependenciesWithContext(ctx context.Context, moduleContent string, registryURL string, includeDevDeps bool) (*ResolutionList, error) {
	// Parse the MODULE.bazel content
	moduleInfo, err := ParseModuleContent(moduleContent)
	if err != nil {
		return nil, fmt.Errorf("failed to parse module content: %v", err)
	}

	// Create registry client
	registry := NewRegistryClient(registryURL)

	// Create resolver
	resolver := NewDependencyResolver(registry, includeDevDeps)

	// Resolve dependencies
	return resolver.ResolveDependencies(ctx, moduleInfo)
}
