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
	"sort"
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
	resolver := newDependencyResolverWithOptions(reg, opts)
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
	resolver := newDependencyResolverWithOptions(reg, opts)
	return resolver.ResolveDependencies(ctx, moduleInfo)
}

// ResolveModule resolves a module from the registry and returns its complete dependency graph.
//
// Unlike Resolve/ResolveFile which resolve dependencies OF a root module (excluding the root),
// ResolveModule resolves a specific module BY its coordinate and includes it in the result.
//
// The target module appears first with Depth=0. Its direct dependencies have Depth=1, etc.
//
// This is useful for:
//   - Exploring what a module will bring into your project
//   - Pre-fetching all modules needed before adding a dependency
//   - Analyzing the complete dependency tree of any BCR module
//
// Example:
//
//	result, err := gobzlmod.ResolveModule(ctx, "rules_go", "0.50.0", opts)
//	// result.Modules[0] is rules_go@0.50.0 (Depth=0)
//	// result.Modules[1:] are its transitive dependencies (Depth>=1)
func ResolveModule(ctx context.Context, name, version string, opts ResolutionOptions) (*ResolutionList, error) {
	reg := registryFromOptions(opts)

	// Fetch the module's MODULE.bazel from registry
	moduleInfo, err := reg.GetModuleFile(ctx, name, version)
	if err != nil {
		return nil, fmt.Errorf("fetch module %s@%s: %w", name, version, err)
	}

	// Ensure the module info has the correct name/version
	// (Some MODULE.bazel files may not have module() directive)
	if moduleInfo.Name == "" {
		moduleInfo.Name = name
	}
	if moduleInfo.Version == "" {
		moduleInfo.Version = version
	}

	// Resolve dependencies (treats moduleInfo as root)
	resolver := newDependencyResolverWithOptions(reg, opts)
	result, err := resolver.ResolveDependencies(ctx, moduleInfo)
	if err != nil {
		return nil, err
	}

	// Determine the registry URL for the target module
	registryURL := reg.BaseURL()
	if chain, ok := reg.(*registryChain); ok {
		if moduleReg := chain.GetRegistryForModule(name); moduleReg != "" {
			registryURL = moduleReg
		}
	}

	// Build the target module's direct dependencies list (modules with Depth=1)
	var directDeps []string
	for _, m := range result.Modules {
		if m.Depth == 1 {
			directDeps = append(directDeps, m.Name)
		}
	}
	sort.Strings(directDeps)

	// Create the target module entry
	targetModule := ModuleToResolve{
		Name:         name,
		Version:      version,
		Registry:     registryURL,
		Depth:        0,
		Dependencies: directDeps,
		RequiredBy:   nil, // Root module isn't required by anything
	}

	// Prepend target module to the list
	result.Modules = append([]ModuleToResolve{targetModule}, result.Modules...)

	// Update summary
	result.Summary.TotalModules++
	result.Summary.ProductionModules++

	return result, nil
}

// registryFromOptions creates a registry from ResolutionOptions.
// Uses BCR if no registries are specified.
func registryFromOptions(opts ResolutionOptions) registryInterface {
	if len(opts.Registries) == 0 {
		return registryWithOptions(opts.HTTPClient, opts.Cache, opts.Timeout)
	}
	return registryWithOptions(opts.HTTPClient, opts.Cache, opts.Timeout, opts.Registries...)
}
