package gobzlmod

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"sync"

	"github.com/albertocavalcante/go-bzlmod/selection"
)

// Override type constants.
const (
	overrideTypeSingleVersion = "single_version"
	overrideTypeGit           = "git"
	overrideTypeLocalPath     = "local_path"
	overrideTypeArchive       = "archive"
)

// selectionResolver resolves dependencies using Bazel's complete selection algorithm.
// This provides full compatibility with Bazel's resolution including:
//   - Compatibility level enforcement
//   - Multiple version override support
//   - Proper pruning of unreachable modules
//
// For simpler MVS-only resolution, use dependencyResolver instead.
type selectionResolver struct {
	registry Registry
	options  ResolutionOptions
}

// newSelectionResolver creates a resolver using Bazel's full selection algorithm.
// The registry can be nil if opts.Registries is set, otherwise it's required.
// When opts.Registries is set, it takes precedence over the registry parameter.
func newSelectionResolver(registry Registry, opts ResolutionOptions) *selectionResolver {
	reg := registry

	// Registries in options takes precedence
	if len(opts.Registries) > 0 {
		reg = RegistryClient(opts.Registries...)
	} else if reg == nil {
		// No registry provided and no Registries in options, use BCR default
		reg = RegistryClient()
	}

	return &selectionResolver{
		registry: reg,
		options:  opts,
	}
}

// Resolve performs dependency resolution using Bazel's selection algorithm.
// It returns a ResolutionList with the resolved modules and optionally an
// unpruned view for debugging.
func (r *selectionResolver) Resolve(ctx context.Context, rootModule *ModuleInfo) (*selectionResult, error) {
	if rootModule == nil {
		return nil, fmt.Errorf("root module is nil")
	}

	// Phase 1: Build the raw dependency graph by fetching all transitive deps
	depGraph, err := r.buildDepGraph(ctx, rootModule)
	if err != nil {
		return nil, fmt.Errorf("build dependency graph: %w", err)
	}

	// Phase 2: Convert overrides to selection package format
	overrides := convertOverrides(rootModule.Overrides)

	// Phase 3: Run Bazel's selection algorithm
	result, err := selection.Run(depGraph, overrides)
	if err != nil {
		return nil, fmt.Errorf("selection algorithm: %w", err)
	}

	// Phase 4: Convert result to ResolutionList
	return r.buildResult(ctx, result, rootModule)
}

// selectionResult extends ResolutionList with additional debug information.
type selectionResult struct {
	// Resolved contains the final resolved modules (pruned).
	Resolved *ResolutionList

	// Unpruned contains all modules before pruning unreachable ones.
	// Useful for debugging why certain modules were excluded.
	Unpruned *ResolutionList

	// BFSOrder is the breadth-first traversal order of resolved modules.
	BFSOrder []string
}

// buildDepGraph fetches all transitive dependencies and builds a selection.DepGraph.
func (r *selectionResolver) buildDepGraph(ctx context.Context, rootModule *ModuleInfo) (*selection.DepGraph, error) {
	modules := make(map[selection.ModuleKey]*selection.Module)
	overrideIndex := indexOverrides(rootModule.Overrides)

	// Create root module entry
	rootKey := selection.ModuleKey{
		Name:    rootModule.Name,
		Version: rootModule.Version,
	}

	rootDeps := make([]selection.DepSpec, 0, len(rootModule.Dependencies))
	for _, dep := range rootModule.Dependencies {
		if dep.DevDependency && !r.options.IncludeDevDeps {
			continue
		}
		// MaxCompatibilityLevel: 0 means not set in MODULE.bazel, use -1 for selection
		maxCL := dep.MaxCompatibilityLevel
		if maxCL == 0 {
			maxCL = -1
		}
		rootDeps = append(rootDeps, selection.DepSpec{
			Name:                  dep.Name,
			Version:               dep.Version,
			MaxCompatibilityLevel: maxCL,
		})
	}

	modules[rootKey] = &selection.Module{
		Key:         rootKey,
		Deps:        rootDeps,
		CompatLevel: rootModule.CompatibilityLevel,
	}

	// BFS to fetch all transitive dependencies
	visited := make(map[selection.ModuleKey]bool)
	visited[rootKey] = true

	queue := make([]selection.DepSpec, len(rootDeps))
	copy(queue, rootDeps)

	var mu sync.Mutex
	var wg sync.WaitGroup
	errCh := make(chan error, 1)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Worker pool for concurrent fetching
	sem := make(chan struct{}, defaultMaxConcurrency)

	for {
		// Process all current queue items
		for len(queue) > 0 {
			dep := queue[0]
			queue = queue[1:]

			key := dep.ToModuleKey()

			// Check if this should skip registry fetch (git/local/archive override)
			if override, ok := overrideIndex[dep.Name]; ok {
				switch override.Type {
				case overrideTypeGit, overrideTypeLocalPath, overrideTypeArchive:
					mu.Lock()
					if !visited[key] {
						visited[key] = true
						// Add placeholder for non-registry modules
						modules[key] = &selection.Module{
							Key:         key,
							Deps:        nil,
							CompatLevel: 0,
						}
					}
					mu.Unlock()
					continue
				case overrideTypeSingleVersion:
					if override.Version != "" {
						key = selection.ModuleKey{Name: dep.Name, Version: override.Version}
					}
				}
			}

			mu.Lock()
			if visited[key] {
				mu.Unlock()
				continue
			}
			visited[key] = true
			mu.Unlock()

			// Fetch module info from registry
			wg.Add(1)
			go func(k selection.ModuleKey) {
				defer wg.Done()

				select {
				case sem <- struct{}{}:
					defer func() { <-sem }()
				case <-ctx.Done():
					return
				}

				moduleInfo, err := r.registry.GetModuleFile(ctx, k.Name, k.Version)
				if err != nil {
					if !isNotFound(err) {
						select {
						case errCh <- fmt.Errorf("fetch %s@%s: %w", k.Name, k.Version, err):
							cancel()
						default:
						}
					}
					return
				}

				deps := make([]selection.DepSpec, 0, len(moduleInfo.Dependencies))
				for _, d := range moduleInfo.Dependencies {
					if d.DevDependency && !r.options.IncludeDevDeps {
						continue
					}
					// Apply single_version_override to transitive deps
					depVersion := d.Version
					if override, ok := overrideIndex[d.Name]; ok && override.Type == overrideTypeSingleVersion && override.Version != "" {
						depVersion = override.Version
					}
					// MaxCompatibilityLevel: 0 means not set, use -1 for selection
					maxCL := d.MaxCompatibilityLevel
					if maxCL == 0 {
						maxCL = -1
					}
					deps = append(deps, selection.DepSpec{
						Name:                  d.Name,
						Version:               depVersion,
						MaxCompatibilityLevel: maxCL,
					})
				}

				mu.Lock()
				modules[k] = &selection.Module{
					Key:         k,
					Deps:        deps,
					CompatLevel: moduleInfo.CompatibilityLevel,
				}
				// Add new deps to queue
				for _, d := range deps {
					dk := d.ToModuleKey()
					if !visited[dk] {
						queue = append(queue, d)
					}
				}
				mu.Unlock()
			}(key)
		}

		// Wait for all workers to finish processing current batch
		wg.Wait()

		// Check if any new items were added to the queue
		mu.Lock()
		hasMore := len(queue) > 0
		mu.Unlock()

		if !hasMore {
			break
		}
	}

	// Check for errors
	select {
	case err := <-errCh:
		return nil, err
	default:
	}

	return &selection.DepGraph{
		Modules: modules,
		RootKey: rootKey,
	}, nil
}

// convertOverrides converts gobzlmod.Override to selection.Override.
func convertOverrides(overrides []Override) map[string]selection.Override {
	result := make(map[string]selection.Override)
	for _, o := range overrides {
		switch o.Type {
		case overrideTypeSingleVersion:
			result[o.ModuleName] = &selection.SingleVersionOverride{
				Version:  o.Version,
				Registry: o.Registry,
			}
		case overrideTypeGit, overrideTypeLocalPath, overrideTypeArchive:
			result[o.ModuleName] = &selection.NonRegistryOverride{
				Type: o.Type,
			}
			// Note: multiple_version_override would need additional fields in gobzlmod.Override
		}
	}
	return result
}

// buildResult converts selection.Result to selectionResult.
func (r *selectionResolver) buildResult(ctx context.Context, result *selection.Result, rootModule *ModuleInfo) (*selectionResult, error) {
	defaultRegistry := r.registry.BaseURL()

	// Build set of root's direct dev dependencies for tracking
	rootDevDeps := make(map[string]bool)
	for _, dep := range rootModule.Dependencies {
		if dep.DevDependency {
			rootDevDeps[dep.Name] = true
		}
	}

	resolved := &ResolutionList{
		Modules: make([]ModuleToResolve, 0, len(result.ResolvedGraph)),
	}

	for key, module := range result.ResolvedGraph {
		// Skip root module
		if key.Name == rootModule.Name && key.Version == rootModule.Version {
			continue
		}

		registryURL := defaultRegistry
		for _, override := range rootModule.Overrides {
			if override.ModuleName == key.Name && override.Registry != "" {
				registryURL = override.Registry
				break
			}
		}

		requiredBy := make([]string, 0)
		// Find who requires this module
		for depKey, depModule := range result.ResolvedGraph {
			for _, dep := range depModule.Deps {
				if dep.Name == key.Name && dep.Version == key.Version {
					requiredBy = append(requiredBy, depKey.String())
				}
			}
		}

		// Check if this is a dev dependency (currently only tracks direct deps from root)
		// TODO: Full dev dependency tracking requires graph reachability analysis
		isDevDep := rootDevDeps[key.Name] && len(requiredBy) == 1 && requiredBy[0] == rootModule.Name+"@"+rootModule.Version

		resolved.Modules = append(resolved.Modules, ModuleToResolve{
			Name:          key.Name,
			Version:       key.Version,
			Registry:      registryURL,
			DevDependency: isDevDep,
			RequiredBy:    requiredBy,
		})

		// Check compat level for debugging
		_ = module.CompatLevel
	}

	slices.SortFunc(resolved.Modules, func(a, b ModuleToResolve) int {
		return cmp.Compare(a.Name, b.Name)
	})

	// Check yanked/deprecated versions if enabled
	if r.options.CheckYanked || r.options.WarnDeprecated {
		checkModuleMetadata(ctx, r.registry, r.options, resolved)
	}

	// Compute summary
	resolved.Summary.TotalModules = len(resolved.Modules)
	for _, m := range resolved.Modules {
		if m.DevDependency {
			resolved.Summary.DevModules++
		} else {
			resolved.Summary.ProductionModules++
		}
		if m.Yanked {
			resolved.Summary.YankedModules++
		}
		if m.IsDeprecated {
			resolved.Summary.DeprecatedModules++
		}
	}

	// Handle yanked version behavior
	if resolved.Summary.YankedModules > 0 {
		switch r.options.YankedBehavior {
		case YankedVersionAllow:
			// Yanked info is populated but no warnings or errors
		case YankedVersionWarn:
			for _, m := range resolved.Modules {
				if m.Yanked {
					resolved.Warnings = append(resolved.Warnings,
						fmt.Sprintf("module %s@%s is yanked: %s", m.Name, m.Version, m.YankReason))
				}
			}
		case YankedVersionError:
			yankedModules := make([]ModuleToResolve, 0, resolved.Summary.YankedModules)
			for _, m := range resolved.Modules {
				if m.Yanked {
					yankedModules = append(yankedModules, m)
				}
			}
			return nil, &YankedVersionsError{Modules: yankedModules}
		}
	}

	// Add deprecated module warnings if enabled
	if r.options.WarnDeprecated && resolved.Summary.DeprecatedModules > 0 {
		for _, m := range resolved.Modules {
			if m.IsDeprecated {
				resolved.Warnings = append(resolved.Warnings,
					fmt.Sprintf("module %s is deprecated: %s", m.Name, m.DeprecationReason))
			}
		}
	}

	// Build unpruned list
	unpruned := &ResolutionList{
		Modules: make([]ModuleToResolve, 0, len(result.UnprunedGraph)),
	}
	for key := range result.UnprunedGraph {
		if key.Name == rootModule.Name && key.Version == rootModule.Version {
			continue
		}
		unpruned.Modules = append(unpruned.Modules, ModuleToResolve{
			Name:    key.Name,
			Version: key.Version,
		})
	}
	slices.SortFunc(unpruned.Modules, func(a, b ModuleToResolve) int {
		return cmp.Compare(a.Name, b.Name)
	})
	unpruned.Summary.TotalModules = len(unpruned.Modules)

	// Build BFS order
	bfsOrder := make([]string, 0, len(result.BFSOrder))
	for _, key := range result.BFSOrder {
		if key.Name == rootModule.Name && key.Version == rootModule.Version {
			continue
		}
		bfsOrder = append(bfsOrder, key.String())
	}

	return &selectionResult{
		Resolved: resolved,
		Unpruned: unpruned,
		BFSOrder: bfsOrder,
	}, nil
}
