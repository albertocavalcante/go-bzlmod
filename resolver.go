package gobzlmod

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"sync"

	"github.com/albertocavalcante/go-bzlmod/bazeltools"
	"github.com/albertocavalcante/go-bzlmod/selection/version"
)

const (
	defaultMaxConcurrency = 5
	// taskBufferMultiplier scales the buffer size based on dependency count.
	taskBufferMultiplier = 2
	// minTaskBufferSize ensures a minimum buffer size for the task channel.
	minTaskBufferSize = 100
)

// DependencyResolver resolves Bazel module dependencies using Minimal Version Selection (MVS).
//
// The resolution algorithm proceeds in three phases:
//  1. Graph construction: Recursively fetches MODULE.bazel files from the registry
//     to build a complete dependency graph with all requested versions.
//  2. Override application: Applies single_version overrides to pin versions and
//     preserves git/local_path/archive overrides without fetching from the registry.
//  3. MVS selection: For each module, selects the highest version requested by
//     any dependent module.
//
// The resolver fetches dependencies concurrently (up to 5 at a time) and caches
// results to avoid redundant network requests.
type DependencyResolver struct {
	registry *RegistryClient
	options  ResolutionOptions
}

// NewDependencyResolver creates a new resolver with the given registry client.
// If includeDevDeps is false, dev_dependency=True modules are excluded from resolution.
func NewDependencyResolver(registry *RegistryClient, includeDevDeps bool) *DependencyResolver {
	return &DependencyResolver{
		registry: registry,
		options:  ResolutionOptions{IncludeDevDeps: includeDevDeps},
	}
}

// NewDependencyResolverWithOptions creates a resolver with full configuration control.
func NewDependencyResolverWithOptions(registry *RegistryClient, opts ResolutionOptions) *DependencyResolver {
	return &DependencyResolver{
		registry: registry,
		options:  opts,
	}
}

// ResolveDependencies resolves all transitive dependencies for a root module.
// It returns a ResolutionList containing all selected modules sorted by name.
//
// The method is safe for concurrent use and respects context cancellation.
func (r *DependencyResolver) ResolveDependencies(ctx context.Context, rootModule *ModuleInfo) (*ResolutionList, error) {
	if rootModule == nil {
		return nil, fmt.Errorf("root module is nil")
	}

	// Inject Bazel's MODULE.tools dependencies if a Bazel version is specified
	if r.options.BazelVersion != "" {
		injectBazelToolsDeps(rootModule, r.options.BazelVersion)
	}

	depGraph := make(map[string]map[string]*DepRequest)
	visiting := &sync.Map{}
	overrideIndex := indexOverrides(rootModule.Overrides)

	if err := r.buildDependencyGraphWithOverrides(ctx, rootModule, depGraph, visiting, []string{"<root>"}, overrideIndex); err != nil {
		return nil, fmt.Errorf("build dependency graph: %w", err)
	}

	// Substitute yanked versions if enabled
	if r.options.SubstituteYanked {
		r.substituteYankedVersionsInGraph(ctx, depGraph)
	}

	r.applyOverrides(depGraph, rootModule.Overrides)
	selectedVersions := r.applyMVS(depGraph)

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

	return r.buildResolutionList(ctx, selectedVersions, rootModule)
}

func (r *DependencyResolver) buildDependencyGraph(ctx context.Context, module *ModuleInfo, depGraph map[string]map[string]*DepRequest, visiting *sync.Map, path []string) error {
	return r.buildDependencyGraphWithOverrides(ctx, module, depGraph, visiting, path, nil)
}

func (r *DependencyResolver) buildDependencyGraphWithOverrides(ctx context.Context, module *ModuleInfo, depGraph map[string]map[string]*DepRequest, visiting *sync.Map, path []string, overrides map[string]Override) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var mu sync.Mutex
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

	// Use a larger buffer to prevent deadlocks when many deps are added at once
	// (e.g., MODULE.tools injection can add 10+ dependencies)
	bufferSize := len(module.Dependencies) * taskBufferMultiplier
	if bufferSize < minTaskBufferSize {
		bufferSize = minTaskBufferSize
	}
	tasks := make(chan depTask, bufferSize)
	var tasksWG sync.WaitGroup
	var workersWG sync.WaitGroup

	enqueue := func(depName, depVersion string, depPath []string) {
		if ctx.Err() != nil {
			return
		}
		depKey := depName + "@" + depVersion
		if _, visited := visiting.LoadOrStore(depKey, struct{}{}); visited {
			return
		}
		tasksWG.Add(1)
		select {
		case tasks <- depTask{name: depName, version: depVersion, path: depPath}:
		case <-ctx.Done():
			tasksWG.Done()
		}
	}

	processDeps := func(module *ModuleInfo, path []string) {
		for _, dep := range module.Dependencies {
			if dep.DevDependency && !r.options.IncludeDevDeps {
				continue
			}

			effectiveVersion := dep.Version
			skipFetch := false
			if override, ok := overrides[dep.Name]; ok {
				switch override.Type {
				case "single_version":
					if override.Version != "" {
						effectiveVersion = override.Version
					}
				case "git", "local_path", "archive":
					skipFetch = true
				}
			}

			mu.Lock()
			if depGraph[dep.Name] == nil {
				depGraph[dep.Name] = make(map[string]*DepRequest)
			}

			if existing, exists := depGraph[dep.Name][effectiveVersion]; exists {
				existing.RequiredBy = append(existing.RequiredBy, path[len(path)-1])
				if dep.DevDependency {
					existing.DevDependency = true
				}
			} else {
				depGraph[dep.Name][effectiveVersion] = &DepRequest{
					Version:       effectiveVersion,
					DevDependency: dep.DevDependency,
					RequiredBy:    []string{path[len(path)-1]},
				}
			}
			mu.Unlock()

			if skipFetch {
				continue
			}

			depPath := append(path[:len(path):len(path)], dep.Name+"@"+effectiveVersion)
			enqueue(dep.Name, effectiveVersion, depPath)
		}
	}

	worker := func() {
		defer workersWG.Done()
		for task := range tasks {
			if ctx.Err() != nil {
				tasksWG.Done()
				continue
			}

			transitiveDep, err := r.registry.GetModuleFile(ctx, task.name, task.version)
			if err != nil {
				if isNotFound(err) {
					mu.Lock()
					removeDependency(depGraph, task.name, task.version)
					mu.Unlock()
					tasksWG.Done()
					continue
				}
				setErr(fmt.Errorf("fetch module %s@%s: %w", task.name, task.version, err))
				tasksWG.Done()
				continue
			}

			processDeps(transitiveDep, task.path)
			tasksWG.Done()
		}
	}

	for i := 0; i < defaultMaxConcurrency; i++ {
		workersWG.Add(1)
		go worker()
	}

	processDeps(module, path)

	go func() {
		tasksWG.Wait()
		close(tasks)
	}()

	workersWG.Wait()

	if firstErr != nil {
		return firstErr
	}
	return ctx.Err()
}

func (r *DependencyResolver) applyOverrides(depGraph map[string]map[string]*DepRequest, overrides []Override) {
	for _, override := range overrides {
		switch override.Type {
		case "single_version":
			if override.Version != "" {
				if versions, exists := depGraph[override.ModuleName]; exists {
					newVersions := make(map[string]*DepRequest)
					if req, hasVersion := versions[override.Version]; hasVersion {
						newVersions[override.Version] = req
					} else {
						newVersions[override.Version] = &DepRequest{
							Version:       override.Version,
							DevDependency: false,
							RequiredBy:    []string{"<override>"},
						}
					}
					depGraph[override.ModuleName] = newVersions
				} else {
					// Create entry for nonexistent module
					depGraph[override.ModuleName] = map[string]*DepRequest{
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
func (r *DependencyResolver) applyMVS(depGraph map[string]map[string]*DepRequest) map[string]*DepRequest {
	selected := make(map[string]*DepRequest)

	for moduleName, versions := range depGraph {
		var maxReq *DepRequest
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
func (r *DependencyResolver) checkDirectDeps(rootModule *ModuleInfo, selected map[string]*DepRequest) []DirectDepMismatch {
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

func (r *DependencyResolver) buildResolutionList(ctx context.Context, selectedVersions map[string]*DepRequest, rootModule *ModuleInfo) (*ResolutionList, error) {
	list := &ResolutionList{
		Modules: make([]ModuleToResolve, 0, len(selectedVersions)),
	}

	defaultRegistry := r.registry.BaseURL()

	for moduleName, req := range selectedVersions {
		registryURL := defaultRegistry
		for _, override := range rootModule.Overrides {
			if override.ModuleName == moduleName && override.Registry != "" {
				registryURL = override.Registry
				break
			}
		}

		list.Modules = append(list.Modules, ModuleToResolve{
			Name:          moduleName,
			Version:       req.Version,
			Registry:      registryURL,
			DevDependency: req.DevDependency,
			RequiredBy:    req.RequiredBy,
		})
	}

	sort.Slice(list.Modules, func(i, j int) bool {
		return list.Modules[i].Name < list.Modules[j].Name
	})

	// Check for yanked/deprecated versions if enabled
	if r.options.CheckYanked || r.options.WarnDeprecated {
		r.checkModuleMetadata(ctx, list)
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

	// Add direct dependency mismatch warnings if enabled
	if r.options.DirectDepsMode == DirectDepsWarn {
		mismatches := r.checkDirectDeps(rootModule, selectedVersions)
		for _, m := range mismatches {
			list.Warnings = append(list.Warnings,
				fmt.Sprintf("direct dependency %s declared as %s but resolved to %s",
					m.Name, m.DeclaredVersion, m.ResolvedVersion))
		}
	}

	return list, nil
}

// checkModuleMetadata fetches metadata for all modules and marks yanked/deprecated status.
// Follows Bazel's fail-open pattern: if metadata.json fetch fails, resolution continues.
func (r *DependencyResolver) checkModuleMetadata(ctx context.Context, list *ResolutionList) {
	// Build allowed yanked versions set for quick lookup
	allowedYanked := buildAllowedYankedSet(r.options.AllowYankedVersions)

	type result struct {
		idx               int
		yanked            bool
		yankReason        string
		deprecated        bool
		deprecationReason string
	}

	results := make(chan result, len(list.Modules))
	var wg sync.WaitGroup

	for i := range list.Modules {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			module := &list.Modules[idx]
			metadata, err := r.registry.GetModuleMetadata(ctx, module.Name)
			if err != nil {
				// Bazel's fail-open pattern: metadata fetch failures don't block resolution.
				// This matches YankedVersionsFunction.java behavior (lines 47-62).
				return
			}

			res := result{idx: idx}

			// Check yanked status
			if metadata.IsYanked(module.Version) {
				res.yanked = true
				res.yankReason = metadata.YankReason(module.Version)
			}

			// Check deprecated status
			if metadata.IsDeprecated() {
				res.deprecated = true
				res.deprecationReason = metadata.Deprecated
			}

			if res.yanked || res.deprecated {
				results <- res
			}
		}(i)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	for res := range results {
		if res.yanked {
			// Check if this specific module@version is allowed
			moduleKey := list.Modules[res.idx].Name + "@" + list.Modules[res.idx].Version
			if !allowedYanked["all"] && !allowedYanked[moduleKey] {
				list.Modules[res.idx].Yanked = true
				list.Modules[res.idx].YankReason = res.yankReason
			}
		}
		if res.deprecated {
			list.Modules[res.idx].IsDeprecated = true
			list.Modules[res.idx].DeprecationReason = res.deprecationReason
		}
	}
}

// buildAllowedYankedSet creates a set from AllowYankedVersions for O(1) lookup.
func buildAllowedYankedSet(allowed []string) map[string]bool {
	if len(allowed) == 0 {
		return nil
	}
	set := make(map[string]bool, len(allowed))
	for _, v := range allowed {
		set[v] = true
	}
	return set
}

func isNotFound(err error) bool {
	var regErr *RegistryError
	return errors.As(err, &regErr) && regErr.StatusCode == http.StatusNotFound
}

func removeDependency(depGraph map[string]map[string]*DepRequest, moduleName, moduleVersion string) {
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
func (r *DependencyResolver) substituteYankedVersionsInGraph(ctx context.Context, depGraph map[string]map[string]*DepRequest) {
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
func (r *DependencyResolver) findNonYankedVersion(ctx context.Context, moduleName, requestedVersion string) string {
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

	// Look through versions to find a non-yanked replacement
	nonYankedVersions := metadata.NonYankedVersions()
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
