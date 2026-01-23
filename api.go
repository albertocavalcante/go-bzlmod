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
