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
// The simplest way to resolve dependencies:
//
//	// Zero-config: uses BCR by default
//	result, err := gobzlmod.Resolve(ctx, moduleContent, gobzlmod.ResolutionOptions{})
//
//	// From a file
//	result, err := gobzlmod.ResolveFile(ctx, "MODULE.bazel", gobzlmod.ResolutionOptions{})
//
//	// With a private registry + BCR fallback
//	result, err := gobzlmod.Resolve(ctx, moduleContent, gobzlmod.ResolutionOptions{
//	    Registries: []string{"https://registry.example.com", gobzlmod.DefaultRegistry},
//	})
//
// # Registry Configuration
//
// By default, the Bazel Central Registry (BCR) is used. For private registries:
//
//	opts := gobzlmod.ResolutionOptions{
//	    Registries: []string{
//	        "https://private.example.com",  // Try first
//	        gobzlmod.DefaultRegistry,        // Fall back to BCR
//	    },
//	}
//
// # Yanked Version Handling
//
// The library supports detecting and handling yanked versions:
//
//	opts := gobzlmod.ResolutionOptions{
//	    CheckYanked:    true,                        // Enable yanked version checking
//	    YankedBehavior: gobzlmod.YankedVersionError, // Fail if yanked versions selected
//	}
//
// # Thread Safety
//
// All public types in this package are safe for concurrent use.
package gobzlmod

import (
	"context"
	"fmt"
)

// Resolve resolves dependencies from MODULE.bazel content.
//
// Uses BCR by default if opts.Registries is empty.
// This is the recommended entry point for dependency resolution.
func Resolve(ctx context.Context, moduleContent string, opts ResolutionOptions) (*ResolutionList, error) {
	moduleInfo, err := ParseModuleContent(moduleContent)
	if err != nil {
		return nil, fmt.Errorf("parse module content: %w", err)
	}

	reg := registryFromOptions(opts)
	resolver := NewDependencyResolverWithOptions(reg, opts)
	return resolver.ResolveDependencies(ctx, moduleInfo)
}

// ResolveFile resolves dependencies from a MODULE.bazel file.
//
// Uses BCR by default if opts.Registries is empty.
func ResolveFile(ctx context.Context, moduleFilePath string, opts ResolutionOptions) (*ResolutionList, error) {
	moduleInfo, err := ParseModuleFile(moduleFilePath)
	if err != nil {
		return nil, fmt.Errorf("parse module file: %w", err)
	}

	reg := registryFromOptions(opts)
	resolver := NewDependencyResolverWithOptions(reg, opts)
	return resolver.ResolveDependencies(ctx, moduleInfo)
}

// registryFromOptions creates a registry from ResolutionOptions.
// Uses BCR if no registries are specified.
func registryFromOptions(opts ResolutionOptions) RegistryInterface {
	if len(opts.Registries) == 0 {
		return Registry()
	}
	return Registry(opts.Registries...)
}

// --- Legacy API (deprecated, kept for backwards compatibility) ---

// ResolveDependenciesFromFile loads a MODULE.bazel file and resolves all dependencies using MVS.
//
// Deprecated: Use ResolveFile instead.
func ResolveDependenciesFromFile(moduleFilePath, registryURL string, includeDevDeps bool) (*ResolutionList, error) {
	moduleInfo, err := ParseModuleFile(moduleFilePath)
	if err != nil {
		return nil, fmt.Errorf("parse module file: %w", err)
	}

	reg := Registry(registryURL)
	resolver := NewDependencyResolver(reg, includeDevDeps)
	return resolver.ResolveDependencies(context.Background(), moduleInfo)
}

// ResolveDependenciesFromContent resolves dependencies from MODULE.bazel content using MVS.
//
// Deprecated: Use Resolve instead.
func ResolveDependenciesFromContent(moduleContent, registryURL string, includeDevDeps bool) (*ResolutionList, error) {
	return ResolveDependenciesWithContext(context.Background(), moduleContent, registryURL, includeDevDeps)
}

// ResolveDependenciesFromContentWithOverrides resolves dependencies and hydrates git/local/archive overrides.
//
// Deprecated: Use Resolve with OverrideModules in ResolutionOptions instead.
func ResolveDependenciesFromContentWithOverrides(moduleContent, registryURL string, includeDevDeps bool, overrideModules map[string]string) (*ResolutionList, error) {
	return ResolveDependenciesWithContextAndOverrides(context.Background(), moduleContent, registryURL, includeDevDeps, overrideModules)
}

// ResolveDependenciesWithContext resolves dependencies with a custom context.
//
// Deprecated: Use Resolve instead.
func ResolveDependenciesWithContext(ctx context.Context, moduleContent, registryURL string, includeDevDeps bool) (*ResolutionList, error) {
	moduleInfo, err := ParseModuleContent(moduleContent)
	if err != nil {
		return nil, fmt.Errorf("parse module content: %w", err)
	}

	reg := Registry(registryURL)
	resolver := NewDependencyResolver(reg, includeDevDeps)
	return resolver.ResolveDependencies(ctx, moduleInfo)
}

// ResolveDependenciesWithContextAndOverrides resolves dependencies with a custom context and overrides.
//
// Deprecated: Use Resolve with OverrideModules in ResolutionOptions instead.
func ResolveDependenciesWithContextAndOverrides(ctx context.Context, moduleContent, registryURL string, includeDevDeps bool, overrideModules map[string]string) (*ResolutionList, error) {
	moduleInfo, err := ParseModuleContent(moduleContent)
	if err != nil {
		return nil, fmt.Errorf("parse module content: %w", err)
	}

	reg := Registry(registryURL)
	resolver := NewDependencyResolver(reg, includeDevDeps)
	if err := addOverrideModules(resolver, overrideModules); err != nil {
		return nil, err
	}
	return resolver.ResolveDependencies(ctx, moduleInfo)
}

// ResolveDependenciesWithOptions resolves dependencies with full configuration control.
//
// Deprecated: Use Resolve instead.
func ResolveDependenciesWithOptions(ctx context.Context, moduleContent, registryURL string, opts ResolutionOptions) (*ResolutionList, error) {
	moduleInfo, err := ParseModuleContent(moduleContent)
	if err != nil {
		return nil, fmt.Errorf("parse module content: %w", err)
	}

	reg := Registry(registryURL)
	resolver := NewDependencyResolverWithOptions(reg, opts)
	return resolver.ResolveDependencies(ctx, moduleInfo)
}

// ResolveDependenciesWithOptionsAndOverrides resolves dependencies with full configuration and overrides.
//
// Deprecated: Use Resolve with OverrideModules in ResolutionOptions instead.
func ResolveDependenciesWithOptionsAndOverrides(ctx context.Context, moduleContent, registryURL string, opts ResolutionOptions, overrideModules map[string]string) (*ResolutionList, error) {
	moduleInfo, err := ParseModuleContent(moduleContent)
	if err != nil {
		return nil, fmt.Errorf("parse module content: %w", err)
	}

	reg := Registry(registryURL)
	resolver := NewDependencyResolverWithOptions(reg, opts)
	if err := addOverrideModules(resolver, overrideModules); err != nil {
		return nil, err
	}
	return resolver.ResolveDependencies(ctx, moduleInfo)
}

func addOverrideModules(resolver *DependencyResolver, overrideModules map[string]string) error {
	if len(overrideModules) == 0 {
		return nil
	}
	for moduleName, content := range overrideModules {
		if err := resolver.AddOverrideModuleContent(moduleName, content); err != nil {
			return err
		}
	}
	return nil
}

// ResolveWithSelection resolves dependencies using Bazel's complete selection algorithm.
//
// Deprecated: Use Resolve with ResolutionOptions instead.
func ResolveWithSelection(ctx context.Context, moduleContent, registryURL string, opts ResolutionOptions) (*SelectionResult, error) {
	moduleInfo, err := ParseModuleContent(moduleContent)
	if err != nil {
		return nil, fmt.Errorf("parse module content: %w", err)
	}

	reg := Registry(registryURL)
	resolver := NewSelectionResolver(reg, opts)
	return resolver.Resolve(ctx, moduleInfo)
}

// ResolveWithSelectionFromFile loads a MODULE.bazel file and resolves using the selection algorithm.
//
// Deprecated: Use ResolveFile with ResolutionOptions instead.
func ResolveWithSelectionFromFile(moduleFilePath, registryURL string, opts ResolutionOptions) (*SelectionResult, error) {
	moduleInfo, err := ParseModuleFile(moduleFilePath)
	if err != nil {
		return nil, fmt.Errorf("parse module file: %w", err)
	}

	reg := Registry(registryURL)
	resolver := NewSelectionResolver(reg, opts)
	return resolver.Resolve(context.Background(), moduleInfo)
}
