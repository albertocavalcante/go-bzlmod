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
//   - Resolver: Resolves transitive dependencies using MVS or Bazel's selection algorithm
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
// # Selection API (Full Bazel Compatibility)
//
// For full Bazel compatibility including compatibility levels and proper pruning:
//
//	opts := gobzlmod.ResolutionOptions{
//	    IncludeDevDeps: false,
//	    CheckYanked:    true,
//	    YankedBehavior: gobzlmod.YankedVersionWarn,
//	}
//	result, err := gobzlmod.ResolveWithSelection(ctx, moduleContent, registryURL, opts)
//
// # Yanked Version Handling
//
// The library supports detecting and handling yanked versions from the registry:
//
//	opts := gobzlmod.ResolutionOptions{
//	    CheckYanked:    true,                        // Enable yanked version checking
//	    YankedBehavior: gobzlmod.YankedVersionError, // Fail if yanked versions selected
//	}
//
// YankedBehavior options:
//   - YankedVersionAllow: Populate yanked info but don't warn or error
//   - YankedVersionWarn: Include warnings in result for yanked versions
//   - YankedVersionError: Return error if any yanked version is selected
//
// # Differences from Bazel's Algorithm
//
// The simple MVS resolver differs from Bazel's algorithm in:
//   - Compatibility level checking and automatic upgrades
//   - Multiple version override support
//   - Module extension resolution
//
// Use ResolveWithSelection for compatibility level enforcement and proper pruning.
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

// ResolveDependenciesWithOptions resolves dependencies with full configuration control.
// This allows enabling yanked version checking and other advanced options.
func ResolveDependenciesWithOptions(ctx context.Context, moduleContent, registryURL string, opts ResolutionOptions) (*ResolutionList, error) {
	moduleInfo, err := ParseModuleContent(moduleContent)
	if err != nil {
		return nil, fmt.Errorf("parse module content: %w", err)
	}

	registry := NewRegistryClient(registryURL)
	resolver := NewDependencyResolverWithOptions(registry, opts)
	return resolver.ResolveDependencies(ctx, moduleInfo)
}

// ResolveWithSelection resolves dependencies using Bazel's complete selection algorithm.
// This provides full compatibility with Bazel including:
//   - Compatibility level enforcement
//   - Multiple version override support (when available in Override type)
//   - Proper pruning of unreachable modules
//
// Returns a SelectionResult with both resolved and unpruned views.
func ResolveWithSelection(ctx context.Context, moduleContent, registryURL string, opts ResolutionOptions) (*SelectionResult, error) {
	moduleInfo, err := ParseModuleContent(moduleContent)
	if err != nil {
		return nil, fmt.Errorf("parse module content: %w", err)
	}

	registry := NewRegistryClient(registryURL)
	resolver := NewSelectionResolver(registry, opts)
	return resolver.Resolve(ctx, moduleInfo)
}

// ResolveWithSelectionFromFile loads a MODULE.bazel file and resolves using the selection algorithm.
func ResolveWithSelectionFromFile(moduleFilePath, registryURL string, opts ResolutionOptions) (*SelectionResult, error) {
	moduleInfo, err := ParseModuleFile(moduleFilePath)
	if err != nil {
		return nil, fmt.Errorf("parse module file: %w", err)
	}

	registry := NewRegistryClient(registryURL)
	resolver := NewSelectionResolver(registry, opts)
	return resolver.Resolve(context.Background(), moduleInfo)
}
