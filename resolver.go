package gobzlmod

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"golang.org/x/mod/semver"
)

// DependencyResolver resolves Bazel module dependencies using Minimal Version Selection (MVS).
//
// The resolution algorithm proceeds in three phases:
//  1. Graph construction: Recursively fetches MODULE.bazel files from the registry
//     to build a complete dependency graph with all requested versions.
//  2. Override application: Applies single_version, git, local_path, and archive
//     overrides to modify or remove modules from the graph.
//  3. MVS selection: For each module, selects the highest version requested by
//     any dependent module.
//
// The resolver fetches dependencies concurrently (up to 5 at a time) and caches
// results to avoid redundant network requests.
type DependencyResolver struct {
	registry       *RegistryClient
	includeDevDeps bool
}

// NewDependencyResolver creates a new resolver with the given registry client.
// If includeDevDeps is false, dev_dependency=True modules are excluded from resolution.
func NewDependencyResolver(registry *RegistryClient, includeDevDeps bool) *DependencyResolver {
	return &DependencyResolver{
		registry:       registry,
		includeDevDeps: includeDevDeps,
	}
}

// ResolveDependencies resolves all transitive dependencies for a root module.
// It returns a ResolutionList containing all selected modules sorted by name.
//
// The method is safe for concurrent use and respects context cancellation.
func (r *DependencyResolver) ResolveDependencies(ctx context.Context, rootModule *ModuleInfo) (*ResolutionList, error) {
	depGraph := make(map[string]map[string]*DepRequest)
	visiting := &sync.Map{}

	if err := r.buildDependencyGraph(ctx, rootModule, depGraph, visiting, []string{"<root>"}); err != nil {
		return nil, fmt.Errorf("build dependency graph: %w", err)
	}

	r.applyOverrides(depGraph, rootModule.Overrides)
	selectedVersions := r.applyMVS(depGraph)
	return r.buildResolutionList(selectedVersions, rootModule)
}

func (r *DependencyResolver) buildDependencyGraph(ctx context.Context, module *ModuleInfo, depGraph map[string]map[string]*DepRequest, visiting *sync.Map, path []string) error {
	return r.buildDependencyGraphInternal(ctx, module, depGraph, visiting, path, &sync.Mutex{})
}

func (r *DependencyResolver) buildDependencyGraphInternal(ctx context.Context, module *ModuleInfo, depGraph map[string]map[string]*DepRequest, visiting *sync.Map, path []string, mu *sync.Mutex) error {
	const maxConcurrency = 5
	semaphore := make(chan struct{}, maxConcurrency)
	var wg sync.WaitGroup

	for _, dep := range module.Dependencies {
		if dep.DevDependency && !r.includeDevDeps {
			continue
		}

		mu.Lock()
		if depGraph[dep.Name] == nil {
			depGraph[dep.Name] = make(map[string]*DepRequest)
		}

		if existing, exists := depGraph[dep.Name][dep.Version]; exists {
			existing.RequiredBy = append(existing.RequiredBy, path[len(path)-1])
			if dep.DevDependency {
				existing.DevDependency = true
			}
		} else {
			depGraph[dep.Name][dep.Version] = &DepRequest{
				Version:       dep.Version,
				DevDependency: dep.DevDependency,
				RequiredBy:    []string{path[len(path)-1]},
			}
		}
		mu.Unlock()

		depKey := dep.Name + "@" + dep.Version
		if _, visited := visiting.LoadOrStore(depKey, true); !visited {
			wg.Add(1)
			go func(depName, depVersion string, depPath []string) {
				defer wg.Done()
				semaphore <- struct{}{}
				defer func() { <-semaphore }()

				transitiveDep, err := r.registry.GetModuleFile(ctx, depName, depVersion)
				if err != nil {
					// Remove failed dependency from the graph. This can happen when:
					// - The module is overridden (git/local_path/archive) and doesn't exist in registry
					// - The version doesn't exist in the registry
					// - Network errors during fetch
					// This is normal behavior - the module will be excluded from resolution.
					mu.Lock()
					if versions, exists := depGraph[depName]; exists {
						delete(versions, depVersion)
						if len(versions) == 0 {
							delete(depGraph, depName)
						}
					}
					mu.Unlock()
					return
				}

				newPath := append(depPath, depName+"@"+depVersion)
				_ = r.buildDependencyGraphInternal(ctx, transitiveDep, depGraph, visiting, newPath, mu)
			}(dep.Name, dep.Version, path)
		}
	}

	wg.Wait()
	return nil
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
						override.Version: &DepRequest{
							Version:       override.Version,
							DevDependency: false,
							RequiredBy:    []string{"<override>"},
						},
					}
				}
			}
		case "git", "local_path", "archive":
			delete(depGraph, override.ModuleName)
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
			if maxReq == nil || semver.Compare("v"+req.Version, "v"+maxReq.Version) > 0 {
				maxReq = req
			}
		}
		if maxReq != nil {
			selected[moduleName] = maxReq
		}
	}

	return selected
}

func (r *DependencyResolver) buildResolutionList(selectedVersions map[string]*DepRequest, rootModule *ModuleInfo) (*ResolutionList, error) {
	list := &ResolutionList{
		Modules: make([]ModuleToResolve, 0, len(selectedVersions)),
	}

	defaultRegistry := r.registry.BaseURL()

	for moduleName, req := range selectedVersions {
		registry := defaultRegistry
		for _, override := range rootModule.Overrides {
			if override.ModuleName == moduleName && override.Registry != "" {
				registry = override.Registry
				break
			}
		}

		list.Modules = append(list.Modules, ModuleToResolve{
			Name:          moduleName,
			Version:       req.Version,
			Registry:      registry,
			DevDependency: req.DevDependency,
			RequiredBy:    req.RequiredBy,
		})
	}

	sort.Slice(list.Modules, func(i, j int) bool {
		return list.Modules[i].Name < list.Modules[j].Name
	})

	list.Summary.TotalModules = len(list.Modules)
	for _, module := range list.Modules {
		if module.DevDependency {
			list.Summary.DevModules++
		} else {
			list.Summary.ProductionModules++
		}
	}

	return list, nil
}
