package gobzlmod

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"slices"
	"sync"

	"github.com/albertocavalcante/go-bzlmod/bazeltools"
	"github.com/albertocavalcante/go-bzlmod/graph"
	"github.com/albertocavalcante/go-bzlmod/internal/compat"
	"github.com/albertocavalcante/go-bzlmod/selection/version"
)

const (
	// defaultMaxConcurrency limits concurrent module fetches from the registry.
	// Set to 5 to balance parallelism with resource usage and avoid overwhelming
	// the registry with too many simultaneous requests. This matches common HTTP
	// client concurrency limits and provides good performance without excessive
	// memory or connection overhead.
	defaultMaxConcurrency = 5

	// taskBufferMultiplier scales the task channel buffer size relative to dependency count.
	// Set to 2x to prevent deadlocks when dependencies spawn additional dependencies during
	// concurrent processing. For example, if a module has 10 deps, each of which might spawn
	// more tasks before being consumed, a 2x buffer (20) ensures the channel doesn't block
	// while workers are busy processing earlier tasks. This is especially important when
	// combined with MODULE.tools injection which can add many dependencies at once.
	taskBufferMultiplier = 2

	// minTaskBufferSize sets a minimum buffer size for the task channel.
	// Set to 100 to handle MODULE.tools dependency injection, which can add 8-14 implicit
	// dependencies depending on Bazel version (e.g., Bazel 6.6.0 adds 8 deps, Bazel 9.0.0
	// adds 14 deps). A minimum buffer ensures smooth concurrent processing even when the
	// root module has few explicit dependencies. Without this, MODULE.tools injection could
	// cause blocking if taskBufferMultiplier * small_dep_count < tools_dep_count.
	minTaskBufferSize = 100

	// maxDependencyDepth is the maximum allowed depth for dependency traversal.
	// This prevents stack overflow and resource exhaustion from extremely deep or
	// circular dependency chains. Set to 1000 to accommodate very deep but valid
	// dependency graphs while protecting against pathological cases.
	maxDependencyDepth = 1000
)

// dependencyResolver resolves Bazel module dependencies using Minimal Version Selection (MVS).
//
// This implementation follows Bazel's bzlmod resolution algorithm as defined in:
//   - Discovery: https://github.com/bazelbuild/bazel/blob/master/src/main/java/com/google/devtools/build/lib/bazel/bzlmod/Discovery.java
//   - Selection: https://github.com/bazelbuild/bazel/blob/master/src/main/java/com/google/devtools/build/lib/bazel/bzlmod/Selection.java
//
// The resolution algorithm proceeds in two phases (matching Bazel's architecture):
//
// Phase 1 - Discovery (see buildDependencyGraph):
// Recursively fetches MODULE.bazel files from the registry to build a complete
// dependency graph with all requested versions. Applies overrides during traversal.
//
// Phase 2 - Selection (see applyMVS):
// For each module, selects the highest version requested by any dependent module
// using Minimal Version Selection.
//
// The resolver fetches dependencies concurrently (up to 5 at a time) and caches
// results to avoid redundant network requests.
//
// Multi-registry support: When ResolutionOptions.Registries is set with multiple URLs,
// the resolver searches registries in order. The first registry where a module is found
// is used for ALL versions of that module.
type dependencyResolver struct {
	registry        Registry
	options         ResolutionOptions
	overrideMu      sync.RWMutex
	overrideModules map[string]*ModuleInfo
}

// graphBuildContext holds state during dependency graph construction.
// Grouping these fields reduces function parameter count and makes the
// relationship between these values explicit.
type graphBuildContext struct {
	// depGraph maps module name -> version -> request info
	depGraph map[string]map[string]*depRequest

	// moduleDeps maps "name@version" -> list of dependency names (for graph building).
	// Keyed by name@version to ensure the selected version's deps are used after MVS.
	moduleDeps map[string][]string

	// moduleInfoCache maps "name@version" -> ModuleInfo for Bazel compatibility checking.
	// This caches the parsed MODULE.bazel content to avoid refetching.
	moduleInfoCache map[string]*ModuleInfo

	// visiting tracks modules currently being processed to detect cycles
	visiting *sync.Map

	// overrides maps module name -> override configuration
	overrides map[string]Override

	// overrideModules contains pre-parsed MODULE.bazel for overridden modules
	overrideModules map[string]*ModuleInfo

	// unfulfilledNodepEdgeModuleNames tracks module names from nodep edges that could
	// not be satisfied in the current discovery round. These are nodep dependencies
	// that reference modules not yet in the dependency graph.
	//
	// Reference: Discovery.java lines 62-78
	// See: https://github.com/bazelbuild/bazel/blob/master/src/main/java/com/google/devtools/build/lib/bazel/bzlmod/Discovery.java
	unfulfilledNodepEdgeModuleNames map[string]bool

	// prevRoundModuleNames contains module names discovered in the previous round.
	// Used to determine if a nodep edge can be fulfilled in the current round.
	prevRoundModuleNames map[string]bool

	// explicitRootProdDepNames tracks production dependencies explicitly declared
	// in the root MODULE.bazel (before MODULE.tools injection).
	explicitRootProdDepNames map[string]bool

	// mu protects concurrent writes to depGraph, moduleDeps, moduleInfoCache, and unfulfilledNodepEdgeModuleNames
	mu sync.Mutex
}

// newDependencyResolver creates a new resolver with the given registry.
// If includeDevDeps is false, dev_dependency=True modules are excluded from resolution.
func newDependencyResolver(registry Registry, includeDevDeps bool) *dependencyResolver {
	return &dependencyResolver{
		registry: registry,
		options:  ResolutionOptions{IncludeDevDeps: includeDevDeps},
	}
}

// newDependencyResolverWithOptions creates a resolver with full configuration control.
// The registry can be nil if opts.Registries is set, otherwise it's required.
// When opts.Registries is set, it takes precedence over the registry parameter.
// When opts.VendorDir is set, a vendor registry is prepended to the chain.
func newDependencyResolverWithOptions(registry Registry, opts ResolutionOptions) *dependencyResolver {
	reg := registry

	// Registries in options takes precedence
	if len(opts.Registries) > 0 {
		reg = registryWithAllOptions(opts.HTTPClient, opts.Cache, opts.Timeout, opts.Logger, opts.Registries...)
	} else if reg == nil {
		// No registry provided and no Registries in options, use BCR default
		reg = registryWithAllOptions(opts.HTTPClient, opts.Cache, opts.Timeout, opts.Logger)
	}

	// Prepend vendor registry if VendorDir is set
	if opts.VendorDir != "" {
		vendorReg, err := newVendorRegistry(opts.VendorDir)
		if err == nil {
			// Create a chain with vendor registry first, then the configured registries
			reg = newVendorChain(vendorReg, reg)
		}
		// If vendor registry creation fails, silently continue with just the remote registries
		// This matches Bazel's behavior where an invalid vendor dir is not an error
	}

	return &dependencyResolver{
		registry: reg,
		options:  opts,
	}
}

// AddOverrideModuleContent registers MODULE.bazel content for a git/local/archive override.
// The content is parsed and used to hydrate transitive dependencies for that module.
func (r *dependencyResolver) AddOverrideModuleContent(moduleName, content string) error {
	if moduleName == "" {
		return fmt.Errorf("override module name is empty")
	}
	moduleInfo, err := ParseModuleContent(content)
	if err != nil {
		return fmt.Errorf("parse override module content for %s: %w", moduleName, err)
	}
	return r.AddOverrideModuleInfo(moduleName, moduleInfo)
}

// AddOverrideModuleInfo registers parsed module info for a git/local/archive override.
func (r *dependencyResolver) AddOverrideModuleInfo(moduleName string, moduleInfo *ModuleInfo) error {
	if moduleName == "" {
		return fmt.Errorf("override module name is empty")
	}
	if moduleInfo == nil {
		return fmt.Errorf("override module info is nil")
	}

	clone := *moduleInfo
	if clone.Name == "" {
		clone.Name = moduleName
	} else if clone.Name != moduleName {
		return fmt.Errorf("override module name mismatch: %s != %s", clone.Name, moduleName)
	}

	r.overrideMu.Lock()
	defer r.overrideMu.Unlock()
	if r.overrideModules == nil {
		r.overrideModules = make(map[string]*ModuleInfo)
	}
	r.overrideModules[moduleName] = &clone
	return nil
}

func (r *dependencyResolver) overrideModuleSnapshot() map[string]*ModuleInfo {
	r.overrideMu.RLock()
	defer r.overrideMu.RUnlock()
	if len(r.overrideModules) == 0 {
		return nil
	}
	return maps.Clone(r.overrideModules)
}

// emitProgress safely calls the OnProgress callback if configured.
func (r *dependencyResolver) emitProgress(event ProgressEvent) {
	if r.options.OnProgress != nil {
		r.options.OnProgress(event)
	}
}

// log returns the configured logger, or a no-op logger if none was set.
// This allows internal code to call logging methods without nil checks.
func (r *dependencyResolver) log() *slog.Logger {
	if r.options.Logger != nil {
		return r.options.Logger
	}
	// Return a logger that discards all output
	return slog.New(discardHandler{})
}

// ResolveDependencies resolves all transitive dependencies for a root module.
// It returns a ResolutionList containing all selected modules sorted by name.
//
// The method is safe for concurrent use and respects context cancellation.
func (r *dependencyResolver) ResolveDependencies(ctx context.Context, rootModule *ModuleInfo) (*ResolutionList, error) {
	if rootModule == nil {
		return nil, fmt.Errorf("root module is nil")
	}

	logger := r.log()
	logger.Info("starting dependency resolution",
		"module", rootModule.Name,
		"version", rootModule.Version,
		"includeDevDeps", r.options.IncludeDevDeps)

	// Emit resolve_start event
	r.emitProgress(ProgressEvent{
		Type:    ProgressResolveStart,
		Message: "starting dependency resolution",
	})

	// Track explicit root production deps before MODULE.tools injection.
	explicitRootProdDepNames := make(map[string]bool)
	for _, dep := range rootModule.Dependencies {
		if !dep.DevDependency {
			explicitRootProdDepNames[dep.Name] = true
		}
	}

	// Inject Bazel's MODULE.tools dependencies if a Bazel version is specified
	if r.options.BazelVersion != "" {
		logger.Debug("injecting MODULE.tools dependencies", "bazelVersion", r.options.BazelVersion)
		injectBazelToolsDeps(rootModule, r.options.BazelVersion)
	}

	// Initialize graph build context with all state needed for traversal
	bc := &graphBuildContext{
		depGraph:                        make(map[string]map[string]*depRequest),
		moduleDeps:                      make(map[string][]string),
		moduleInfoCache:                 make(map[string]*ModuleInfo),
		visiting:                        &sync.Map{},
		overrides:                       indexOverrides(rootModule.Overrides),
		overrideModules:                 r.overrideModuleSnapshot(),
		unfulfilledNodepEdgeModuleNames: make(map[string]bool),
		prevRoundModuleNames:            map[string]bool{rootModule.Name: true},
		explicitRootProdDepNames:        explicitRootProdDepNames,
	}

	// Multi-round discovery loop for handling nodep edges.
	// Nodep dependencies (from use_extension) may reference modules not yet discovered.
	// After each round, we check if any unfulfilled nodep edges can now be satisfied
	// by newly discovered modules. If so, we run another round.
	//
	// Reference: Discovery.java lines 62-78
	// https://github.com/bazelbuild/bazel/blob/master/src/main/java/com/google/devtools/build/lib/bazel/bzlmod/Discovery.java
	for round := 1; ; round++ {
		logger.Debug("starting discovery round", "round", round)

		// Reset unfulfilled nodep edges for this round
		bc.mu.Lock()
		bc.unfulfilledNodepEdgeModuleNames = make(map[string]bool)
		bc.mu.Unlock()

		if err := r.buildDependencyGraph(ctx, rootModule, bc, []string{"<root>"}); err != nil {
			return nil, fmt.Errorf("build dependency graph (round %d): %w", round, err)
		}

		// Collect current round's module names
		currentModuleNames := make(map[string]bool)
		bc.mu.Lock()
		for moduleName := range bc.depGraph {
			currentModuleNames[moduleName] = true
		}
		unfulfilledNodeps := make(map[string]bool)
		for name := range bc.unfulfilledNodepEdgeModuleNames {
			unfulfilledNodeps[name] = true
		}
		bc.mu.Unlock()

		// Check if any unfulfilled nodep edge can now be fulfilled
		// (i.e., its module name is now in the current module set)
		canFulfillMore := false
		for nodepName := range unfulfilledNodeps {
			if currentModuleNames[nodepName] {
				canFulfillMore = true
				logger.Debug("unfulfilled nodep edge can now be fulfilled",
					"nodepModule", nodepName, "round", round)
				break
			}
		}

		if !canFulfillMore {
			logger.Debug("discovery complete", "rounds", round, "modules", len(currentModuleNames))
			break
		}

		// Update prev round module names for the next iteration
		bc.mu.Lock()
		bc.prevRoundModuleNames = currentModuleNames
		bc.mu.Unlock()

		// Safety limit to prevent infinite loops in case of bugs
		const maxDiscoveryRounds = 100
		if round >= maxDiscoveryRounds {
			logger.Warn("reached maximum discovery rounds", "maxRounds", maxDiscoveryRounds)
			break
		}
	}

	// Substitute yanked versions if enabled
	if r.options.SubstituteYanked {
		r.substituteYankedVersionsInGraph(ctx, bc.depGraph)
	}

	r.applyOverrides(bc.depGraph, rootModule.Overrides)
	selectedVersions := r.applyMVS(bc.depGraph)

	// Validate direct dependencies match resolved versions
	if r.options.DirectDepsMode != DirectDepsOff {
		mismatches := r.checkDirectDeps(rootModule, selectedVersions)
		if len(mismatches) > 0 {
			if r.options.DirectDepsMode == DirectDepsError {
				return nil, &DirectDepsMismatchError{Mismatches: mismatches}
			}
			// DirectDepsWarn: mismatches will be added as warnings in buildResolutionList
		}
	}

	result, err := r.buildResolutionList(ctx, selectedVersions, bc.moduleDeps, bc.moduleInfoCache, rootModule)
	if err != nil {
		return nil, err // Preserve error types (e.g., YankedVersionsError) without wrapping
	}

	logger.Info("resolution complete",
		"totalModules", len(result.Modules),
		"productionModules", result.Summary.ProductionModules,
		"devModules", result.Summary.DevModules)

	// Emit resolve_end event
	r.emitProgress(ProgressEvent{
		Type:    ProgressResolveEnd,
		Message: fmt.Sprintf("resolved %d modules", len(result.Modules)),
	})

	return result, nil
}

// buildDependencyGraph constructs the dependency graph by recursively fetching
// and processing MODULE.bazel files. Uses bc (graphBuildContext) to accumulate state.
//
// This method handles both regular dependencies and nodep dependencies (from use_extension).
// Nodep deps are tracked separately and only fulfilled if their target module already
// exists in the dependency graph. Unfulfilled nodep edges are recorded for potential
// resolution in subsequent discovery rounds.
//
// Reference: Discovery.java lines 47-79
// https://github.com/bazelbuild/bazel/blob/master/src/main/java/com/google/devtools/build/lib/bazel/bzlmod/Discovery.java#L47-L79
func (r *dependencyResolver) buildDependencyGraph(ctx context.Context, module *ModuleInfo, bc *graphBuildContext, path []string) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var errOnce sync.Once
	var firstErr error

	setErr := func(err error) {
		if err == nil {
			return
		}
		errOnce.Do(func() {
			firstErr = err
			cancel()
		})
	}

	type depTask struct {
		name    string
		version string
		path    []string
	}

	// Unbounded task queue guarded by mutex/condition to avoid producer deadlocks.
	// Workers both consume and discover new work; a bounded channel can deadlock when all
	// workers are blocked sending and no receiver is available.
	var (
		queueMu    sync.Mutex
		queueCond  = sync.NewCond(&queueMu)
		taskQueue  []depTask
		queueClose bool
	)
	var tasksWG sync.WaitGroup
	var workersWG sync.WaitGroup

	// checkDepth ensures we don't exceed maximum dependency depth.
	// This protects against pathologically deep dependency chains.
	checkDepth := func(depPath []string) error {
		if len(depPath) > maxDependencyDepth {
			return &MaxDepthExceededError{
				Depth:    len(depPath),
				MaxDepth: maxDependencyDepth,
				Path:     depPath,
			}
		}
		return nil
	}

	enqueue := func(depName, depVersion string, depPath []string) {
		if ctx.Err() != nil {
			return
		}
		depKey := depName + "@" + depVersion

		// Check if already visited globally. This is the key mechanism that prevents
		// infinite loops in mutual dependencies (like rules_go <-> gazelle).
		// Following Bazel's approach: if a module is already visited, skip it silently.
		// This allows A -> B -> A patterns because when B tries to add A, A is already
		// in the visited set, so we just skip it - no reprocessing, no infinite loop.
		// See: Selection.java DepGraphWalker.walk() in Bazel source.
		if _, visited := bc.visiting.LoadOrStore(depKey, struct{}{}); visited {
			return
		}

		// Check depth limit
		if err := checkDepth(depPath); err != nil {
			setErr(err)
			return
		}

		tasksWG.Add(1)
		queueMu.Lock()
		if queueClose || ctx.Err() != nil {
			queueMu.Unlock()
			tasksWG.Done()
			return
		}
		taskQueue = append(taskQueue, depTask{name: depName, version: depVersion, path: depPath})
		queueCond.Signal()
		queueMu.Unlock()
	}

	var processDeps func(module *ModuleInfo, path []string) error
	processDeps = func(module *ModuleInfo, path []string) error {
		// Capture this module's dependencies for graph building (O(n) - just collect names)
		var deps []string
		for _, dep := range module.Dependencies {
			if dep.DevDependency && !r.options.IncludeDevDeps {
				continue
			}
			deps = append(deps, dep.Name)
		}
		if len(deps) > 0 && module.Name != "" {
			depsKey := module.Name + "@" + module.Version
			bc.mu.Lock()
			bc.moduleDeps[depsKey] = deps
			bc.mu.Unlock()
		}

		for _, dep := range module.Dependencies {
			if dep.DevDependency && !r.options.IncludeDevDeps {
				continue
			}

			effectiveVersion := dep.Version
			skipFetch := false
			if override, ok := bc.overrides[dep.Name]; ok {
				switch override.Type {
				case "single_version":
					if override.Version != "" {
						effectiveVersion = override.Version
					}
				case "git", "local_path", "archive":
					skipFetch = true
				}
			}
			if effectiveVersion == "" && !skipFetch {
				return fmt.Errorf("bazel_dep(%s) has empty version and no non-registry override", dep.Name)
			}

			bc.mu.Lock()
			if bc.depGraph[dep.Name] == nil {
				bc.depGraph[dep.Name] = make(map[string]*depRequest)
			}

			if existing, exists := bc.depGraph[dep.Name][effectiveVersion]; exists {
				existing.RequiredBy = append(existing.RequiredBy, path[len(path)-1])
				if !dep.DevDependency {
					existing.DevDependency = false
				}
			} else {
				bc.depGraph[dep.Name][effectiveVersion] = &depRequest{
					Version:       effectiveVersion,
					DevDependency: dep.DevDependency,
					RequiredBy:    []string{path[len(path)-1]},
				}
			}
			bc.mu.Unlock()

			if skipFetch {
				if overrideModule, ok := bc.overrideModules[dep.Name]; ok {
					depKey := dep.Name + "@" + effectiveVersion
					depPath := append(path[:len(path):len(path)], dep.Name+"@"+effectiveVersion)

					// Check depth limit
					if err := checkDepth(depPath); err != nil {
						return err
					}

					// Try to mark as visited and process if this is the first visit.
					// This prevents infinite loops in mutual dependencies (like rules_go <-> gazelle).
					// Following Bazel's approach: if already visited, skip silently - no error.
					if _, visited := bc.visiting.LoadOrStore(depKey, struct{}{}); !visited {
						if err := processDeps(overrideModule, depPath); err != nil {
							return err
						}
					}
				}
				continue
			}

			depPath := append(path[:len(path):len(path)], dep.Name+"@"+effectiveVersion)
			enqueue(dep.Name, effectiveVersion, depPath)
		}

		// Process nodep dependencies
		// Nodep deps only get added to the graph if their target module already exists
		// (either in the graph already or was in the previous round).
		// Otherwise, they're tracked as unfulfilled for potential resolution in later rounds.
		//
		// Reference: Discovery.java lines 62-78
		for _, nodepDep := range module.NodepDependencies {
			if nodepDep.DevDependency && !r.options.IncludeDevDeps {
				continue
			}

			effectiveVersion := nodepDep.Version
			if override, ok := bc.overrides[nodepDep.Name]; ok {
				if override.Type == "single_version" && override.Version != "" {
					effectiveVersion = override.Version
				}
			}

			depKey := nodepDep.Name + "@" + effectiveVersion

			bc.mu.Lock()
			// Check if the nodep target already exists in the dependency graph
			_, existsInGraph := bc.depGraph[nodepDep.Name]
			// Or if it was in the previous round (can be fulfilled)
			_, inPrevRound := bc.prevRoundModuleNames[nodepDep.Name]

			if existsInGraph || inPrevRound {
				// Nodep can be fulfilled - add to graph for version selection only
				// (but don't enqueue for transitive traversal)
				if bc.depGraph[nodepDep.Name] == nil {
					bc.depGraph[nodepDep.Name] = make(map[string]*depRequest)
				}
				if existing, exists := bc.depGraph[nodepDep.Name][effectiveVersion]; exists {
					existing.RequiredBy = append(existing.RequiredBy, path[len(path)-1]+" (nodep)")
				} else {
					bc.depGraph[nodepDep.Name][effectiveVersion] = &depRequest{
						Version:       effectiveVersion,
						DevDependency: nodepDep.DevDependency,
						RequiredBy:    []string{path[len(path)-1] + " (nodep)"},
					}
				}
				// Mark as visited to prevent re-processing, but don't traverse transitively
				bc.visiting.LoadOrStore(depKey, struct{}{})
			} else {
				// Cannot be fulfilled - track as unfulfilled for potential later resolution
				bc.unfulfilledNodepEdgeModuleNames[nodepDep.Name] = true
			}
			bc.mu.Unlock()
		}

		return nil
	}

	worker := func() {
		defer workersWG.Done()
		logger := r.log()
		for {
			queueMu.Lock()
			for len(taskQueue) == 0 && !queueClose {
				queueCond.Wait()
			}
			if len(taskQueue) == 0 && queueClose {
				queueMu.Unlock()
				return
			}
			task := taskQueue[0]
			taskQueue = taskQueue[1:]
			queueMu.Unlock()

			if ctx.Err() != nil {
				tasksWG.Done()
				continue
			}

			logger.Debug("fetching module", "name", task.name, "version", task.version)

			// Emit module_fetch_start event
			r.emitProgress(ProgressEvent{
				Type:    ProgressModuleFetchStart,
				Module:  task.name,
				Version: task.version,
			})

			// Check if there's a registry override for this module
			registryToUse := r.registry
			if override, ok := bc.overrides[task.name]; ok && override.Registry != "" {
				logger.Debug("using registry override", "name", task.name, "registry", override.Registry)
				// Use the overridden registry for this specific module with the configured timeout
				registryToUse = newRegistryClientWithTimeout(override.Registry, r.options.Timeout)
			}

			transitiveDep, err := registryToUse.GetModuleFile(ctx, task.name, task.version)

			// Emit module_fetch_end event
			r.emitProgress(ProgressEvent{
				Type:    ProgressModuleFetchEnd,
				Module:  task.name,
				Version: task.version,
			})

			if err != nil {
				if isNotFound(err) {
					logger.Debug("module not found", "name", task.name, "version", task.version)
					missingRequiredByRootProduction := false
					bc.mu.Lock()
					if versions, ok := bc.depGraph[task.name]; ok {
						if req, ok := versions[task.version]; ok {
							// Missing direct production deps from root should fail resolution.
							if !req.DevDependency && bc.explicitRootProdDepNames[task.name] {
								for _, rb := range req.RequiredBy {
									if rb == "<root>" {
										missingRequiredByRootProduction = true
										break
									}
								}
							}
						}
					}
					removeDependency(bc.depGraph, task.name, task.version)
					bc.mu.Unlock()
					if missingRequiredByRootProduction {
						setErr(fmt.Errorf("fetch module %s@%s: %w", task.name, task.version, err))
					}
					tasksWG.Done()
					continue
				}
				logger.Debug("fetch error", "name", task.name, "version", task.version, "error", err)
				setErr(fmt.Errorf("fetch module %s@%s: %w", task.name, task.version, err))
				tasksWG.Done()
				continue
			}

			logger.Debug("fetched module", "name", task.name, "version", task.version,
				"dependencies", len(transitiveDep.Dependencies))

			// Cache module info for Bazel compatibility checking
			if len(transitiveDep.BazelCompatibility) > 0 {
				cacheKey := task.name + "@" + task.version
				bc.mu.Lock()
				bc.moduleInfoCache[cacheKey] = transitiveDep
				bc.mu.Unlock()
			}

			if err := processDeps(transitiveDep, task.path); err != nil {
				setErr(err)
			}
			tasksWG.Done()
		}
	}

	for range defaultMaxConcurrency {
		workersWG.Add(1)
		go worker()
	}

	if err := processDeps(module, path); err != nil {
		setErr(err)
	}

	go func() {
		tasksWG.Wait()
		queueMu.Lock()
		queueClose = true
		queueCond.Broadcast()
		queueMu.Unlock()
	}()

	workersWG.Wait()

	if firstErr != nil {
		return firstErr
	}
	return ctx.Err()
}

func (r *dependencyResolver) applyOverrides(depGraph map[string]map[string]*depRequest, overrides []Override) {
	for _, override := range overrides {
		switch override.Type {
		case "single_version":
			if override.Version != "" {
				if versions, exists := depGraph[override.ModuleName]; exists {
					newVersions := make(map[string]*depRequest)
					if req, hasVersion := versions[override.Version]; hasVersion {
						newVersions[override.Version] = req
					} else {
						newVersions[override.Version] = &depRequest{
							Version:       override.Version,
							DevDependency: false,
							RequiredBy:    []string{"<override>"},
						}
					}
					depGraph[override.ModuleName] = newVersions
				} else {
					// Create entry for nonexistent module
					depGraph[override.ModuleName] = map[string]*depRequest{
						override.Version: {
							Version:       override.Version,
							DevDependency: false,
							RequiredBy:    []string{"<override>"},
						},
					}
				}
			}
		case "git", "local_path", "archive":
			continue
		}
	}
}

// applyMVS implements Minimal Version Selection: for each module, select the
// highest version requested by any dependent.
//
// Reference: Selection.java lines 285-291 (mergeWithMax logic)
// https://github.com/bazelbuild/bazel/blob/master/src/main/java/com/google/devtools/build/lib/bazel/bzlmod/Selection.java#L285-L291
//
// This implements the core MVS algorithm: iterate through all requested versions
// for each module and select the maximum version. This matches Bazel's behavior
// where the highest requested version wins.
func (r *dependencyResolver) applyMVS(depGraph map[string]map[string]*depRequest) map[string]*depRequest {
	selected := make(map[string]*depRequest)

	for moduleName, versions := range depGraph {
		var maxReq *depRequest
		for _, req := range versions {
			if maxReq == nil || version.Compare(req.Version, maxReq.Version) > 0 {
				maxReq = req
			}
		}
		if maxReq != nil {
			selected[moduleName] = maxReq
		}
	}

	return selected
}

// checkDirectDeps validates that direct dependencies' declared versions match resolved versions.
// Returns a list of mismatches for reporting or error handling.
func (r *dependencyResolver) checkDirectDeps(rootModule *ModuleInfo, selected map[string]*depRequest) []DirectDepMismatch {
	var mismatches []DirectDepMismatch

	for _, dep := range rootModule.Dependencies {
		if dep.DevDependency && !r.options.IncludeDevDeps {
			continue
		}

		resolved, ok := selected[dep.Name]
		if !ok {
			// Module not in graph - likely has non-registry override
			continue
		}

		if resolved.Version != dep.Version {
			mismatches = append(mismatches, DirectDepMismatch{
				Name:            dep.Name,
				DeclaredVersion: dep.Version,
				ResolvedVersion: resolved.Version,
			})
		}
	}

	return mismatches
}

func (r *dependencyResolver) buildResolutionList(ctx context.Context, selectedVersions map[string]*depRequest, moduleDeps map[string][]string, moduleInfoCache map[string]*ModuleInfo, rootModule *ModuleInfo) (*ResolutionList, error) {
	list := &ResolutionList{
		Modules: make([]ModuleToResolve, 0, len(selectedVersions)),
	}

	defaultRegistry := r.registry.BaseURL()

	// Build a set of selected module names for filtering dependencies
	selectedNames := make(map[string]bool, len(selectedVersions))
	for name := range selectedVersions {
		selectedNames[name] = true
	}

	// Extract root module's direct dependencies for depth calculation
	rootDeps := make([]string, 0, len(rootModule.Dependencies))
	for _, dep := range rootModule.Dependencies {
		if dep.DevDependency && !r.options.IncludeDevDeps {
			continue
		}
		rootDeps = append(rootDeps, dep.Name)
	}

	// Resolve moduleDeps (keyed by name@version) to a name-only map using selected versions.
	// This ensures each module's dependencies reflect the version MVS actually selected.
	resolvedModuleDeps := make(map[string][]string, len(selectedVersions))
	for name, req := range selectedVersions {
		depsKey := name + "@" + req.Version
		if deps, ok := moduleDeps[depsKey]; ok {
			resolvedModuleDeps[name] = deps
		}
	}

	// Calculate depth for each module using BFS
	moduleDepths := calculateModuleDepths(rootDeps, resolvedModuleDeps, selectedNames)

	for moduleName, req := range selectedVersions {
		registryURL := defaultRegistry

		// Check for registry override in root module
		for _, override := range rootModule.Overrides {
			if override.ModuleName == moduleName && override.Registry != "" {
				registryURL = override.Registry
				break
			}
		}

		// For multi-registry chains, get the actual registry that provided this module
		if chain, ok := r.registry.(*registryChain); ok && registryURL == defaultRegistry {
			if moduleRegistry := chain.GetRegistryForModule(moduleName); moduleRegistry != "" {
				registryURL = moduleRegistry
			}
		}

		// Get dependencies for this module, filtered to only include selected modules
		var deps []string
		if rawDeps, ok := resolvedModuleDeps[moduleName]; ok {
			for _, dep := range rawDeps {
				if selectedNames[dep] {
					deps = append(deps, dep)
				}
			}
		}

		list.Modules = append(list.Modules, ModuleToResolve{
			Name:          moduleName,
			Version:       req.Version,
			Registry:      registryURL,
			Depth:         moduleDepths[moduleName],
			DevDependency: req.DevDependency,
			Dependencies:  deps,
			RequiredBy:    req.RequiredBy,
		})
	}

	slices.SortFunc(list.Modules, func(a, b ModuleToResolve) int {
		return cmp.Compare(a.Name, b.Name)
	})

	// Check for yanked/deprecated versions if enabled
	if r.options.CheckYanked || r.options.WarnDeprecated {
		checkModuleMetadata(ctx, r.registry, r.options, list)
	}

	// Check Bazel compatibility if enabled and a Bazel version is specified
	if r.options.BazelCompatibilityMode != BazelCompatibilityOff && r.options.BazelVersion != "" {
		checkModuleBazelCompatibility(list.Modules, moduleInfoCache, r.options.BazelVersion)
	}

	// Check field version compatibility if a Bazel version is specified
	if r.options.BazelVersion != "" {
		fieldWarnings := checkFieldCompatibility(rootModule, r.options.BazelVersion)
		list.Summary.FieldWarnings = append(list.Summary.FieldWarnings, fieldWarnings...)
	}

	// Compute summary statistics
	list.Summary.TotalModules = len(list.Modules)
	for _, module := range list.Modules {
		if module.DevDependency {
			list.Summary.DevModules++
		} else {
			list.Summary.ProductionModules++
		}
		if module.Yanked {
			list.Summary.YankedModules++
		}
		if module.IsDeprecated {
			list.Summary.DeprecatedModules++
		}
		if module.IsBazelIncompatible {
			list.Summary.IncompatibleModules++
		}
	}

	// Handle yanked version behavior
	if list.Summary.YankedModules > 0 {
		switch r.options.YankedBehavior {
		case YankedVersionAllow:
			// Yanked info is populated but no warnings or errors
		case YankedVersionWarn:
			for _, m := range list.Modules {
				if m.Yanked {
					list.Warnings = append(list.Warnings,
						fmt.Sprintf("module %s@%s is yanked: %s", m.Name, m.Version, m.YankReason))
				}
			}
		case YankedVersionError:
			yankedModules := make([]ModuleToResolve, 0, list.Summary.YankedModules)
			for _, m := range list.Modules {
				if m.Yanked {
					yankedModules = append(yankedModules, m)
				}
			}
			return nil, &YankedVersionsError{Modules: yankedModules}
		}
	}

	// Add deprecated module warnings if enabled
	if r.options.WarnDeprecated && list.Summary.DeprecatedModules > 0 {
		for _, m := range list.Modules {
			if m.IsDeprecated {
				list.Warnings = append(list.Warnings,
					fmt.Sprintf("module %s is deprecated: %s", m.Name, m.DeprecationReason))
			}
		}
	}

	// Handle Bazel compatibility behavior
	if list.Summary.IncompatibleModules > 0 {
		switch r.options.BazelCompatibilityMode {
		case BazelCompatibilityOff:
			// Incompatibility info is populated but no warnings or errors
		case BazelCompatibilityWarn:
			for _, m := range list.Modules {
				if m.IsBazelIncompatible {
					list.Warnings = append(list.Warnings,
						fmt.Sprintf("module %s@%s is incompatible with Bazel %s: %s",
							m.Name, m.Version, r.options.BazelVersion, m.BazelIncompatibilityReason))
				}
			}
		case BazelCompatibilityError:
			incompatibleModules := make([]ModuleToResolve, 0, list.Summary.IncompatibleModules)
			for _, m := range list.Modules {
				if m.IsBazelIncompatible {
					incompatibleModules = append(incompatibleModules, m)
				}
			}
			return nil, &BazelIncompatibilityError{
				BazelVersion: r.options.BazelVersion,
				Modules:      incompatibleModules,
			}
		}
	}

	// Add direct dependency mismatch warnings if enabled
	if r.options.DirectDepsMode == DirectDepsWarn {
		mismatches := r.checkDirectDeps(rootModule, selectedVersions)
		for _, m := range mismatches {
			list.Warnings = append(list.Warnings,
				fmt.Sprintf("direct dependency %s declared as %s but resolved to %s",
					m.Name, m.DeclaredVersion, m.ResolvedVersion))
		}
	}

	// Build dependency graph - O(n) where n = number of modules
	list.Graph = buildGraph(rootModule, list.Modules)

	return list, nil
}

// buildGraph constructs a graph.Graph from resolution results.
// This is O(n) where n is the number of modules.
func buildGraph(rootModule *ModuleInfo, modules []ModuleToResolve) *graph.Graph {
	// Create module index for O(1) version lookup
	moduleVersions := make(map[string]string, len(modules))
	for _, m := range modules {
		moduleVersions[m.Name] = m.Version
	}

	// Build root dependencies (filtered to selected modules)
	var rootDeps []graph.ModuleKey
	for _, dep := range rootModule.Dependencies {
		if ver, ok := moduleVersions[dep.Name]; ok {
			rootDeps = append(rootDeps, graph.ModuleKey{Name: dep.Name, Version: ver})
		}
	}

	// Build SimpleModule list for graph.Build - O(n)
	simpleModules := make([]graph.SimpleModule, 0, len(modules)+1)

	// Add root module
	simpleModules = append(simpleModules, graph.SimpleModule{
		Name:         rootModule.Name,
		Version:      rootModule.Version,
		Dependencies: rootDeps,
	})

	// Add resolved modules
	for _, m := range modules {
		// Convert dependency names to ModuleKeys
		deps := make([]graph.ModuleKey, 0, len(m.Dependencies))
		for _, depName := range m.Dependencies {
			if ver, ok := moduleVersions[depName]; ok {
				deps = append(deps, graph.ModuleKey{Name: depName, Version: ver})
			}
		}

		simpleModules = append(simpleModules, graph.SimpleModule{
			Name:          m.Name,
			Version:       m.Version,
			Dependencies:  deps,
			DevDependency: m.DevDependency,
		})
	}

	rootKey := graph.ModuleKey{Name: rootModule.Name, Version: rootModule.Version}
	return graph.Build(rootKey, simpleModules)
}

func isNotFound(err error) bool {
	var regErr *RegistryError
	return errors.As(err, &regErr) && regErr.StatusCode == http.StatusNotFound
}

func removeDependency(depGraph map[string]map[string]*depRequest, moduleName, moduleVersion string) {
	if versions, exists := depGraph[moduleName]; exists {
		delete(versions, moduleVersion)
		if len(versions) == 0 {
			delete(depGraph, moduleName)
		}
	}
}

// injectBazelToolsDeps adds Bazel's MODULE.tools dependencies to the root module.
// This ensures resolution matches Bazel's behavior for a given version.
func injectBazelToolsDeps(rootModule *ModuleInfo, bazelVersion string) {
	closestVersion := bazeltools.ClosestVersion(bazelVersion)
	if closestVersion == "" {
		return
	}

	deps := bazeltools.GetDeps(closestVersion)
	if deps == nil {
		return
	}

	// Create a map of existing dependencies for deduplication
	existingDeps := make(map[string]bool)
	for _, dep := range rootModule.Dependencies {
		existingDeps[dep.Name] = true
	}

	// Add MODULE.tools deps that aren't already declared
	for _, toolDep := range deps {
		if !existingDeps[toolDep.Name] {
			rootModule.Dependencies = append(rootModule.Dependencies, Dependency{
				Name:    toolDep.Name,
				Version: toolDep.Version,
			})
		}
	}
}

// substituteYankedVersionsInGraph iterates through the dependency graph and replaces
// yanked versions with non-yanked alternatives in the same compatibility level.
func (r *dependencyResolver) substituteYankedVersionsInGraph(ctx context.Context, depGraph map[string]map[string]*depRequest) {
	for moduleName, versions := range depGraph {
		// Collect replacements to avoid modifying map during iteration
		replacements := make(map[string]string)
		for ver := range versions {
			replacement := r.findNonYankedVersion(ctx, moduleName, ver)
			if replacement != ver {
				replacements[ver] = replacement
			}
		}

		// Apply replacements
		for oldVer, newVer := range replacements {
			req := versions[oldVer]
			delete(versions, oldVer)
			req.Version = newVer
			versions[newVer] = req
		}
	}
}

// findNonYankedVersion finds a non-yanked replacement for a yanked version.
// Returns the original version if not yanked or no replacement is found.
// The replacement must be in the same compatibility level.
func (r *dependencyResolver) findNonYankedVersion(ctx context.Context, moduleName, requestedVersion string) string {
	// Fetch metadata to check yanked status
	metadata, err := r.registry.GetModuleMetadata(ctx, moduleName)
	if err != nil {
		// If we can't fetch metadata, use the original version
		return requestedVersion
	}

	if !metadata.IsYanked(requestedVersion) {
		return requestedVersion
	}

	// Find the next non-yanked version
	// First, get the compatibility level of the requested version
	requestedModule, err := r.registry.GetModuleFile(ctx, moduleName, requestedVersion)
	if err != nil {
		// Can't get the compatibility level, use the original version
		return requestedVersion
	}
	requestedCompatLevel := requestedModule.CompatibilityLevel

	// Look through versions to find a non-yanked replacement.
	// Sort to ensure we find the closest (lowest) valid replacement.
	nonYankedVersions := metadata.NonYankedVersions()
	version.Sort(nonYankedVersions)
	for _, candidateVersion := range nonYankedVersions {
		// Skip versions older than requested
		if version.Compare(candidateVersion, requestedVersion) < 0 {
			continue
		}

		// Check if the candidate has the same compatibility level
		candidateModule, err := r.registry.GetModuleFile(ctx, moduleName, candidateVersion)
		if err != nil {
			continue
		}

		if candidateModule.CompatibilityLevel == requestedCompatLevel {
			return candidateVersion
		}
	}

	// No suitable replacement found, return original
	return requestedVersion
}

func indexOverrides(overrides []Override) map[string]Override {
	if len(overrides) == 0 {
		return nil
	}
	index := make(map[string]Override, len(overrides))
	for _, override := range overrides {
		if override.ModuleName == "" {
			continue
		}
		index[override.ModuleName] = override
	}
	return index
}

// checkFieldCompatibility checks if bzlmod fields used in the root module are
// compatible with the target Bazel version. Returns warning messages for any
// unsupported fields.
//
// Currently checks:
// - max_compatibility_level (requires Bazel 7.0.0+)
func checkFieldCompatibility(rootModule *ModuleInfo, bazelVersion string) []string {
	if bazelVersion == "" {
		return nil
	}

	var warnings []string

	// Check max_compatibility_level usage in dependencies
	for _, dep := range rootModule.Dependencies {
		if dep.MaxCompatibilityLevel > 0 {
			if w := compat.CheckField(bazelVersion, "max_compatibility_level"); w != nil {
				warnings = append(warnings,
					fmt.Sprintf("bazel_dep(%s): %s", dep.Name, w.String()))
				// Only warn once for this field type
				break
			}
		}
	}

	return warnings
}

// calculateModuleDepths computes the shortest path length from root to each module using BFS.
// Returns a map from module name to depth (1 = direct dependency, 2+ = transitive).
func calculateModuleDepths(rootDeps []string, moduleDeps map[string][]string, selected map[string]bool) map[string]int {
	depths := make(map[string]int)
	queue := make([]string, 0, len(rootDeps))

	// Root's direct deps have depth 1
	for _, dep := range rootDeps {
		if selected[dep] {
			if _, seen := depths[dep]; !seen {
				depths[dep] = 1
				queue = append(queue, dep)
			}
		}
	}

	// BFS for minimum depth
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		currentDepth := depths[current]

		for _, depName := range moduleDeps[current] {
			if selected[depName] {
				if _, seen := depths[depName]; !seen {
					depths[depName] = currentDepth + 1
					queue = append(queue, depName)
				}
			}
		}
	}
	return depths
}
