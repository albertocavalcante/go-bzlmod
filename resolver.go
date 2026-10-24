package gobzlmod

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sync"

	"github.com/albertocavalcante/go-bzlmod/bazeltools"
	"github.com/albertocavalcante/go-bzlmod/graph"
	"github.com/albertocavalcante/go-bzlmod/internal/compat"
	"github.com/albertocavalcante/go-bzlmod/selection"
	"github.com/albertocavalcante/go-bzlmod/selection/version"
)

const (
	defaultMaxConcurrency            = 5
	builtinBazelToolsModule          = "bazel_tools"
	builtinLocalConfigPlatformModule = "local_config_platform"

	overrideTypeSingleVersion = "single_version"
	overrideTypeMultiple      = "multiple_version"
	overrideTypeGit           = "git"
	overrideTypeLocalPath     = "local_path"
	overrideTypeArchive       = "archive"
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

// selectionResolver resolves dependencies using Bazel's complete selection algorithm.
// This is the sole resolution engine used by all public API functions.
type selectionResolver struct {
	registry Registry
	options  ResolutionOptions
}

// buildRegistry constructs a Registry from ResolutionOptions.
// Uses BCR as default when no registries are configured.
func buildRegistry(opts ResolutionOptions) Registry {
	urls := opts.Registries
	if len(urls) == 0 {
		urls = DefaultRegistries
	}
	return registryWithAllOptionsAndTrace(
		opts.HTTPClient,
		opts.Cache,
		opts.Timeout,
		opts.Logger,
		newRegistryTraceIfEnabled(opts.TraceRegistryFiles),
		urls...,
	)
}

// Resolve performs dependency resolution using Bazel's selection algorithm.
// It returns a ResolutionList with the resolved modules and optionally an
// unpruned view for debugging.
func (r *selectionResolver) Resolve(ctx context.Context, rootModule *ModuleInfo) (*ResolutionList, error) {
	if rootModule == nil {
		return nil, fmt.Errorf("root module is nil")
	}

	// Phase 1: Build the raw dependency graph by fetching all transitive deps
	depGraph, moduleInfoCache, err := r.buildDepGraph(ctx, rootModule)
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
	return r.buildResult(ctx, result, rootModule, moduleInfoCache)
}

// buildDepSpecs converts Dependency entries to selection.DepSpec, applying
// override-driven version rewrites (single_version pins the override version;
// git/local_path/archive overrides resolve to an empty version since they are
// not looked up in a registry). Non-root modules always ignore dev deps,
// matching Bazel's behavior that only the root module's dev_dependency=True
// bazel_dep entries participate in resolution.
func (r *selectionResolver) buildDepSpecs(deps []Dependency, isRoot bool, overrideMap map[string]Override) []selection.DepSpec {
	specs := make([]selection.DepSpec, 0, len(deps))
	for _, dep := range deps {
		// Match Bazel: non-root modules always ignore dev dependencies.
		if dep.DevDependency && (!isRoot || !r.options.IncludeDevDeps) {
			continue
		}

		depVersion := dep.Version
		if override, ok := overrideMap[dep.Name]; ok {
			switch override.Type {
			case overrideTypeSingleVersion:
				if override.Version != "" {
					depVersion = override.Version
				}
			case overrideTypeGit, overrideTypeLocalPath, overrideTypeArchive:
				// Match Bazel: non-registry overrides use empty version.
				depVersion = ""
			}
		}

		maxCL := dep.MaxCompatibilityLevel
		if maxCL == 0 {
			maxCL = -1
		}
		specs = append(specs, selection.DepSpec{
			Name:                  dep.Name,
			Version:               depVersion,
			MaxCompatibilityLevel: maxCL,
		})
	}
	return specs
}

// parseLocalPathOverrideModule parses the MODULE.bazel for a local_path
// override target, which may point directly at a MODULE.bazel file or at a
// directory containing one.
func parseLocalPathOverrideModule(path string) (*ModuleInfo, error) {
	moduleFile := path
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		moduleFile = filepath.Join(path, "MODULE.bazel")
	}
	return ParseModuleFile(moduleFile)
}

// addBuiltinBazelToolsDeps adds Bazel's built-in bazel_tools module (and, for
// Bazel versions before 9.0.0, the local_config_platform module it used to
// depend on) to modules, so the MODULE.tools dependencies participate in
// selection even though the root module never declares them directly. It
// returns rootDeps with a dep spec for bazel_tools appended, or rootDeps
// unchanged if no BazelVersion was configured.
func (r *selectionResolver) addBuiltinBazelToolsDeps(
	modules map[selection.ModuleKey]*selection.Module,
	buildDepSpecs func([]Dependency, bool) []selection.DepSpec,
	rootDeps []selection.DepSpec,
) []selection.DepSpec {
	if r.options.BazelVersion == "" {
		return rootDeps
	}

	builtinToolSpecs := buildDepSpecs(
		bazelToolsRootDeps(
			r.options.BazelVersion,
			r.options.BazelToolsLookup,
			r.options.BazelToolsTransformer,
		),
		true,
	)
	builtinDeps := make([]selection.DepSpec, 0, len(builtinToolSpecs)+1)
	if includeVisibleLocalConfigPlatform(r.options.BazelVersion) {
		localConfigKey := selection.ModuleKey{Name: builtinLocalConfigPlatformModule, Version: ""}
		modules[localConfigKey] = &selection.Module{Key: localConfigKey}
		builtinDeps = append(builtinDeps, selection.DepSpec{Name: builtinLocalConfigPlatformModule})
	}
	builtinDeps = append(builtinDeps, builtinToolSpecs...)

	bazelToolsKey := selection.ModuleKey{Name: builtinBazelToolsModule, Version: ""}
	modules[bazelToolsKey] = &selection.Module{
		Key:  bazelToolsKey,
		Deps: builtinDeps,
	}
	return append(rootDeps, selection.DepSpec{Name: builtinBazelToolsModule})
}

// buildDepGraph fetches all transitive dependencies and builds a selection.DepGraph.
// It also returns a moduleInfoCache mapping "name@version" to *ModuleInfo for modules
// that declare bazel_compatibility constraints, enabling post-resolution compat checks.
func (r *selectionResolver) buildDepGraph(ctx context.Context, rootModule *ModuleInfo) (*selection.DepGraph, map[string]*ModuleInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	modules := make(map[selection.ModuleKey]*selection.Module)
	moduleInfoCache := make(map[string]*ModuleInfo)
	overrideMap := overrideIndex(rootModule.Overrides)

	buildDepSpecs := func(deps []Dependency, isRoot bool) []selection.DepSpec {
		specs := r.buildDepSpecs(deps, isRoot, overrideMap)
		// Bazel rewrites every reference to the root module to the actual
		// root key, even when a dependency requests a different version.
		for i := range specs {
			if specs[i].Name == rootModule.Name {
				specs[i].Version = rootModule.Version
				specs[i].MaxCompatibilityLevel = -1
			}
		}
		return specs
	}

	// Create root module entry
	rootKey := selection.ModuleKey{
		Name:    rootModule.Name,
		Version: rootModule.Version,
	}

	rootDeps := buildDepSpecs(rootModule.Dependencies, true)
	rootDeps = r.addBuiltinBazelToolsDeps(modules, buildDepSpecs, rootDeps)

	rootNodepDeps := buildDepSpecs(rootModule.NodepDependencies, true)
	modules[rootKey] = &selection.Module{
		Key:         rootKey,
		Deps:        rootDeps,
		NodepDeps:   rootNodepDeps,
		CompatLevel: rootModule.CompatibilityLevel,
	}

	// BFS to fetch all transitive dependencies
	visited := make(map[selection.ModuleKey]bool)
	visited[rootKey] = true
	knownNames := map[string]bool{rootKey.Name: true}
	for _, dep := range rootDeps {
		knownNames[dep.Name] = true
	}
	pendingNodep := slices.Clone(rootNodepDeps)

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
		for {
			mu.Lock()
			if len(queue) == 0 {
				mu.Unlock()
				break
			}
			dep := queue[0]
			queue = queue[1:]
			mu.Unlock()

			key := dep.ToModuleKey()

			mu.Lock()
			predefinedModule, predefined := modules[key]
			mu.Unlock()
			if predefined {
				mu.Lock()
				if !visited[key] {
					visited[key] = true
					for _, d := range predefinedModule.Deps {
						knownNames[d.Name] = true
						dk := d.ToModuleKey()
						if !visited[dk] {
							queue = append(queue, d)
						}
					}
					pendingNodep = append(pendingNodep, predefinedModule.NodepDeps...)
				}
				mu.Unlock()
				continue
			}

			// Check if this should skip registry fetch (git/local/archive override)
			if override, ok := overrideMap[dep.Name]; ok {
				switch override.Type {
				case overrideTypeGit, overrideTypeLocalPath, overrideTypeArchive:
					// Match Bazel: non-registry overrides resolve to empty version.
					key = selection.ModuleKey{Name: dep.Name, Version: ""}

					if override.Type == overrideTypeLocalPath && override.Path != "" {
						localModule, err := parseLocalPathOverrideModule(override.Path)
						if err != nil {
							cancel()
							wg.Wait()
							return nil, nil, fmt.Errorf("parse local_path override for %s: %w", dep.Name, err)
						}
						localDeps := buildDepSpecs(localModule.Dependencies, false)
						localNodepDeps := buildDepSpecs(localModule.NodepDependencies, false)

						mu.Lock()
						if !visited[key] {
							visited[key] = true
							modules[key] = &selection.Module{
								Key:         key,
								Deps:        localDeps,
								NodepDeps:   localNodepDeps,
								CompatLevel: localModule.CompatibilityLevel,
							}
							for _, d := range localDeps {
								knownNames[d.Name] = true
								dk := d.ToModuleKey()
								if !visited[dk] {
									queue = append(queue, d)
								}
							}
							pendingNodep = append(pendingNodep, localNodepDeps...)
						}
						mu.Unlock()
						continue
					}

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

				// Check if there's a registry override for this module.
				registryToUse := r.registry
				if override, ok := overrideMap[k.Name]; ok && override.Registry != "" {
					registryToUse = registryWithAllOptionsAndTrace(
						r.options.HTTPClient,
						r.options.Cache,
						r.options.Timeout,
						r.options.Logger,
						sharedRegistryFileTrace(r.registry),
						override.Registry,
					)
				}
				moduleInfo, err := registryToUse.GetModuleFile(ctx, k.Name, k.Version)
				if err != nil {
					select {
					case errCh <- fmt.Errorf("fetch %s@%s: %w", k.Name, k.Version, err):
						cancel()
					default:
					}
					return
				}

				deps := buildDepSpecs(moduleInfo.Dependencies, false)
				nodepDeps := buildDepSpecs(moduleInfo.NodepDependencies, false)

				mu.Lock()
				// Cache module info for post-resolution Bazel compatibility checking.
				if len(moduleInfo.BazelCompatibility) > 0 {
					moduleInfoCache[k.Name+"@"+k.Version] = moduleInfo
				}
				modules[k] = &selection.Module{
					Key:         k,
					Deps:        deps,
					NodepDeps:   nodepDeps,
					CompatLevel: moduleInfo.CompatibilityLevel,
				}
				// Add new deps to queue
				for _, d := range deps {
					knownNames[d.Name] = true
					dk := d.ToModuleKey()
					if !visited[dk] {
						queue = append(queue, d)
					}
				}
				pendingNodep = append(pendingNodep, nodepDeps...)
				mu.Unlock()
			}(key)
		}

		// Wait for all workers to finish processing current batch
		wg.Wait()

		// A nodep requirement becomes eligible only after its module name is
		// discovered through an ordinary edge. Repeat after every batch so a
		// delayed ordinary dependency can activate an earlier nodep requirement.
		mu.Lock()
		remaining := pendingNodep[:0]
		for _, dep := range pendingNodep {
			if knownNames[dep.Name] {
				if !visited[dep.ToModuleKey()] {
					queue = append(queue, dep)
				}
			} else {
				remaining = append(remaining, dep)
			}
		}
		pendingNodep = remaining
		hasMore := len(queue) > 0
		mu.Unlock()

		if !hasMore {
			break
		}
	}

	// Check for errors
	select {
	case err := <-errCh:
		return nil, nil, err
	default:
	}

	// Propagate context cancellation/timeout.
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}

	if err := r.verifyDirectDepsFetched(rootModule, overrideMap, modules); err != nil {
		return nil, nil, err
	}

	return &selection.DepGraph{
		Modules: modules,
		RootKey: rootKey,
	}, moduleInfoCache, nil
}

// verifyDirectDepsFetched checks that every direct production dependency of
// the root module (i.e. not a dev dep when dev deps are excluded, and not a
// non-registry override) was actually fetched into modules. A missing direct
// dep means the registry returned 404 for it, which should fail resolution
// outright rather than silently proceeding without it (matching Bazel
// behavior).
func (r *selectionResolver) verifyDirectDepsFetched(
	rootModule *ModuleInfo,
	overrideMap map[string]Override,
	modules map[selection.ModuleKey]*selection.Module,
) error {
	for _, dep := range rootModule.Dependencies {
		if dep.DevDependency && !r.options.IncludeDevDeps {
			continue
		}
		// Non-registry overrides (git/local/archive) don't need registry lookup.
		if override, ok := overrideMap[dep.Name]; ok {
			switch override.Type {
			case overrideTypeGit, overrideTypeLocalPath, overrideTypeArchive:
				continue
			}
		}
		depKey := selection.ModuleKey{Name: dep.Name, Version: dep.Version}
		if override, ok := overrideMap[dep.Name]; ok && override.Type == overrideTypeSingleVersion && override.Version != "" {
			depKey.Version = override.Version
		}
		if _, found := modules[depKey]; !found {
			return fmt.Errorf("fetch %s@%s: registry returned status 404 for module %s@%s",
				dep.Name, depKey.Version, dep.Name, depKey.Version)
		}
	}
	return nil
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
		case overrideTypeMultiple:
			result[o.ModuleName] = &selection.MultipleVersionOverride{
				Versions: o.Versions,
				Registry: o.Registry,
			}
		case overrideTypeGit, overrideTypeLocalPath, overrideTypeArchive:
			result[o.ModuleName] = &selection.NonRegistryOverride{
				Type: o.Type,
				Path: o.Path,
			}
		}
	}
	return result
}

// buildResult converts selection.Result to selectionResult.
func (r *selectionResolver) buildResult(ctx context.Context, result *selection.Result, rootModule *ModuleInfo, moduleInfoCache map[string]*ModuleInfo) (*ResolutionList, error) {
	defaultRegistry := r.registry.BaseURL()
	overridesByModule := overrideIndex(rootModule.Overrides)

	sourceGraph := result.ResolvedGraph
	if r.options.IncludeUnusedModules {
		sourceGraph = result.UnprunedGraph
	}

	// Compute reachability from root's production, dev, and builtin fronts.
	// A module is dev-only iff reachable from dev deps and not from prod deps.
	rootKey := selection.ModuleKey{Name: rootModule.Name, Version: rootModule.Version}
	prodStarts, devStarts, builtinStarts := computeReachabilityFronts(rootModule, sourceGraph, rootKey)
	prodReachable := computeReachableKeys(sourceGraph, prodStarts)
	devReachable := computeReachableKeys(sourceGraph, devStarts)
	builtinReachable := computeReachableKeys(sourceGraph, builtinStarts)
	usedKeys := make(map[selection.ModuleKey]bool, len(result.ResolvedGraph))
	for key := range result.ResolvedGraph {
		usedKeys[key] = true
	}
	allVisible := make(map[selection.ModuleKey]bool, len(sourceGraph))
	if r.options.IncludeUnusedModules {
		for key := range sourceGraph {
			if key != rootKey {
				allVisible[key] = true
			}
		}
	}

	visibility := moduleVisibility{
		rootKey:               rootKey,
		includeUnusedModules:  r.options.IncludeUnusedModules,
		includeBuiltinModules: r.options.IncludeBuiltinModules,
		allVisible:            allVisible,
		prodReachable:         prodReachable,
		devReachable:          devReachable,
		builtinReachable:      builtinReachable,
	}
	isVisible := visibility.isVisible

	// Pre-compute reverse-dependency index in O(n*d) for efficient requiredBy lookup.
	reverseIndex := make(map[selection.ModuleKey][]selection.ModuleKey)
	for depKey, depModule := range sourceGraph {
		if depKey == rootKey {
			continue
		}
		for _, dep := range depModule.Deps {
			dk := dep.ToModuleKey()
			reverseIndex[dk] = append(reverseIndex[dk], depKey)
		}
	}

	resolved := &ResolutionList{
		Modules: make([]ModuleToResolve, 0, len(sourceGraph)),
	}

	for key, module := range sourceGraph {
		if !isVisible(key) {
			continue
		}

		registryURL := registryURLForModule(defaultRegistry, key.Name, overridesByModule)
		// For multi-registry chains, get the actual registry that provided this module.
		if chain, ok := r.registry.(*registryChain); ok && registryURL == defaultRegistry {
			if moduleRegistry := chain.GetRegistryForModule(key.Name); moduleRegistry != "" {
				registryURL = moduleRegistry
			}
		}
		if key.Name == builtinBazelToolsModule || key.Name == builtinLocalConfigPlatformModule {
			registryURL = ""
		}

		dependencies := make([]string, 0, len(module.Deps))
		dependencyKeys := make([]string, 0, len(module.Deps))
		for _, dep := range module.Deps {
			depKey := dep.ToModuleKey()
			if !isVisible(depKey) {
				continue
			}
			dependencies = append(dependencies, dep.Name)
			dependencyKeys = append(dependencyKeys, depKey.String())
		}

		// Look up requiredBy from pre-computed reverse index.
		requiredBy := make([]string, 0)
		for _, reqKey := range reverseIndex[key] {
			if isVisible(reqKey) {
				requiredBy = append(requiredBy, reqKey.String())
			}
		}

		isDevDep := devReachable[key] && !prodReachable[key]
		unused := !usedKeys[key]

		resolved.Modules = append(resolved.Modules, ModuleToResolve{
			Name:           key.Name,
			Version:        key.Version,
			Registry:       registryURL,
			DevDependency:  isDevDep,
			Dependencies:   dependencies,
			DependencyKeys: dependencyKeys,
			RequiredBy:     requiredBy,
			Unused:         unused,
		})
	}

	slices.SortFunc(resolved.Modules, func(a, b ModuleToResolve) int {
		if c := cmp.Compare(a.Name, b.Name); c != 0 {
			return c
		}
		return cmp.Compare(a.Version, b.Version)
	})

	resolved.Graph = buildSelectionGraph(rootModule, sourceGraph, prodStarts, devStarts, builtinStarts, prodReachable, devReachable, builtinReachable, r.options.IncludeUnusedModules, r.options.IncludeBuiltinModules)
	moduleDepths := calculateModuleDepthsSelection(resolved.Graph)
	for i := range resolved.Modules {
		key := graph.ModuleKey{Name: resolved.Modules[i].Name, Version: resolved.Modules[i].Version}
		resolved.Modules[i].Depth = moduleDepths[key]
	}

	// Check yanked/deprecated versions if enabled
	if r.options.CheckYanked || r.options.WarnDeprecated {
		checkModuleMetadata(ctx, r.registry, r.options, resolved)
	}

	// Check Bazel compatibility if enabled and a Bazel version is specified
	if r.options.BazelCompatibilityMode != BazelCompatibilityOff && r.options.BazelVersion != "" {
		checkModuleBazelCompatibility(resolved.Modules, moduleInfoCache, r.options.BazelVersion)
	}

	// Check field version compatibility if a Bazel version is specified
	if r.options.BazelVersion != "" {
		fieldWarnings := checkFieldCompatibility(rootModule, r.options.BazelVersion)
		resolved.Summary.FieldWarnings = append(resolved.Summary.FieldWarnings, fieldWarnings...)
	}

	// Compute summary and apply configured behaviors.
	computeSummary(resolved)
	if err := applyYankedBehavior(resolved, r.options); err != nil {
		return nil, err
	}
	applyDeprecatedWarnings(resolved, r.options)
	if err := applyBazelCompatBehavior(resolved, r.options); err != nil {
		return nil, err
	}

	if err := enrichResolutionList(ctx, r.registry, r.options, rootModule.Overrides, resolved); err != nil {
		return nil, err
	}

	return resolved, nil
}

// moduleVisibility decides whether a module key should appear in buildResult's
// output, based on reachability from the root's production/dev/builtin
// dependency fronts and the IncludeUnusedModules/IncludeBuiltinModules
// options. The root itself is never visible (it's the implicit entry point,
// not a resolved dependency).
type moduleVisibility struct {
	rootKey               selection.ModuleKey
	includeUnusedModules  bool
	includeBuiltinModules bool
	allVisible            map[selection.ModuleKey]bool
	prodReachable         map[selection.ModuleKey]bool
	devReachable          map[selection.ModuleKey]bool
	builtinReachable      map[selection.ModuleKey]bool
}

func (v moduleVisibility) isVisible(key selection.ModuleKey) bool {
	if key == v.rootKey {
		return false
	}
	if v.includeUnusedModules {
		if !v.allVisible[key] {
			return false
		}
	} else if !v.prodReachable[key] && !v.devReachable[key] && !v.builtinReachable[key] {
		return false
	}
	if !v.includeBuiltinModules && v.builtinReachable[key] && !v.prodReachable[key] && !v.devReachable[key] {
		return false
	}
	return true
}

// computeReachabilityFronts classifies the root module's direct dependencies
// into production, dev, and builtin (bazel_tools) "fronts" -- the BFS
// starting points computeReachableKeys uses to determine, for every module in
// sourceGraph, whether it is a production dependency, a dev-only dependency,
// or only reachable through the builtin bazel_tools module. A dep counts as
// dev-only when it is declared with dev_dependency=True and is not also
// declared as a production dep (Bazel allows both on the same module name).
func computeReachabilityFronts(
	rootModule *ModuleInfo,
	sourceGraph map[selection.ModuleKey]*selection.Module,
	rootKey selection.ModuleKey,
) (prodStarts, devStarts, builtinStarts []selection.ModuleKey) {
	rootDevDeps := make(map[string]bool)
	rootProdDeps := make(map[string]bool)
	for _, dep := range rootModule.Dependencies {
		if dep.DevDependency {
			rootDevDeps[dep.Name] = true
		} else {
			rootProdDeps[dep.Name] = true
		}
	}

	rootNode, ok := sourceGraph[rootKey]
	if !ok {
		return nil, nil, nil
	}
	for _, dep := range rootNode.Deps {
		depKey := dep.ToModuleKey()
		if rootProdDeps[dep.Name] {
			prodStarts = append(prodStarts, depKey)
		}
		if rootDevDeps[dep.Name] && !rootProdDeps[dep.Name] {
			devStarts = append(devStarts, depKey)
		}
		if dep.Name == builtinBazelToolsModule {
			builtinStarts = append(builtinStarts, depKey)
		}
	}
	return prodStarts, devStarts, builtinStarts
}

func computeReachableKeys(
	depGraph map[selection.ModuleKey]*selection.Module,
	starts []selection.ModuleKey,
) map[selection.ModuleKey]bool {
	reachable := make(map[selection.ModuleKey]bool, len(starts))
	queue := make([]selection.ModuleKey, 0, len(starts))
	for _, k := range starts {
		if !reachable[k] {
			reachable[k] = true
			queue = append(queue, k)
		}
	}

	for len(queue) > 0 {
		key := queue[0]
		queue = queue[1:]
		module, ok := depGraph[key]
		if !ok {
			continue
		}
		for _, dep := range module.Deps {
			depKey := dep.ToModuleKey()
			if !reachable[depKey] {
				reachable[depKey] = true
				queue = append(queue, depKey)
			}
		}
	}
	return reachable
}

func buildSelectionGraph(
	rootModule *ModuleInfo,
	sourceGraph map[selection.ModuleKey]*selection.Module,
	prodStarts, devStarts, builtinStarts []selection.ModuleKey,
	prodReachable, devReachable, builtinReachable map[selection.ModuleKey]bool,
	includeUnused bool,
	includeBuiltins bool,
) *graph.Graph {
	rootKey := graph.ModuleKey{Name: rootModule.Name, Version: rootModule.Version}
	visible := make(map[selection.ModuleKey]bool, len(sourceGraph))
	if includeUnused {
		for key := range sourceGraph {
			if key == (selection.ModuleKey{Name: rootModule.Name, Version: rootModule.Version}) {
				continue
			}
			if !includeBuiltins && builtinReachable[key] && !prodReachable[key] && !devReachable[key] {
				continue
			}
			visible[key] = true
		}
	} else {
		mapsCopyInto(visible, prodReachable)
		mapsCopyInto(visible, devReachable)
		mapsCopyInto(visible, builtinReachable)
	}

	rootDeps := make([]graph.ModuleKey, 0, len(prodStarts)+len(devStarts)+len(builtinStarts))
	rootSeen := make(map[graph.ModuleKey]bool)
	appendRootDep := func(key selection.ModuleKey) {
		// graph.ModuleKey is a type alias for selection.ModuleKey (see
		// graph/types.go), so this is a same-type assignment, not a
		// conversion.
		gk := key
		if !rootSeen[gk] {
			rootSeen[gk] = true
			rootDeps = append(rootDeps, gk)
		}
	}
	for _, key := range prodStarts {
		appendRootDep(key)
	}
	for _, key := range devStarts {
		appendRootDep(key)
	}
	if includeBuiltins {
		for _, key := range builtinStarts {
			appendRootDep(key)
		}
	}

	modules := []graph.SimpleModule{{
		Name:         rootModule.Name,
		Version:      rootModule.Version,
		Dependencies: rootDeps,
	}}
	for key, module := range sourceGraph {
		if key == (selection.ModuleKey{Name: rootModule.Name, Version: rootModule.Version}) {
			continue
		}
		if !visible[key] {
			continue
		}
		deps := make([]graph.ModuleKey, 0, len(module.Deps))
		for _, dep := range module.Deps {
			depKey := dep.ToModuleKey()
			if !visible[depKey] {
				continue
			}
			// depKey is already a graph.ModuleKey: the two types are aliased.
			deps = append(deps, depKey)
		}
		modules = append(modules, graph.SimpleModule{
			Name:          key.Name,
			Version:       key.Version,
			Dependencies:  deps,
			DevDependency: false,
		})
	}
	return graph.Build(rootKey, modules)
}

func calculateModuleDepthsSelection(g *graph.Graph) map[graph.ModuleKey]int {
	// The nil check has to come first: len(g.Modules) dereferences g, so this
	// guard used to sit one line below the dereference it was protecting and a
	// nil graph panicked before ever reaching it.
	if g == nil {
		return map[graph.ModuleKey]int{}
	}
	depths := make(map[graph.ModuleKey]int, len(g.Modules))
	queue := []graph.ModuleKey{g.Root}
	depths[g.Root] = 0
	for len(queue) > 0 {
		key := queue[0]
		queue = queue[1:]
		node := g.Get(key)
		if node == nil {
			continue
		}
		for _, dep := range node.Dependencies {
			if _, seen := depths[dep]; seen {
				continue
			}
			depths[dep] = depths[key] + 1
			queue = append(queue, dep)
		}
	}
	return depths
}

func mapsCopyInto(dst, src map[selection.ModuleKey]bool) {
	for key := range src {
		dst[key] = true
	}
}
