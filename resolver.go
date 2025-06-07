package gobzlmod

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"golang.org/x/mod/semver"
)

// DependencyResolver handles dependency resolution using MVS (Minimal Version Selection)
type DependencyResolver struct {
	registry       *RegistryClient
	includeDevDeps bool
	visiting       sync.Map
}

// NewDependencyResolver creates a new dependency resolver
func NewDependencyResolver(registry *RegistryClient, includeDevDeps bool) *DependencyResolver {
	return &DependencyResolver{
		registry:       registry,
		includeDevDeps: includeDevDeps,
	}
}

// ResolveDependencies resolves all dependencies for a given root module
func (r *DependencyResolver) ResolveDependencies(ctx context.Context, rootModule *ModuleInfo) (*ResolutionList, error) {
	// Phase 1: Build complete dependency graph with ALL version requirements
	depGraph := make(map[string]map[string]*DepRequest)

	err := r.buildDependencyGraph(ctx, rootModule, depGraph, true, []string{"<root>"})
	if err != nil {
		return nil, fmt.Errorf("failed to build dependency graph: %v", err)
	}

	// Phase 2: Apply overrides (this can change version requirements)
	r.applyOverrides(depGraph, rootModule.Overrides)

	// Phase 3: Apply MVS to the complete graph (not per-module)
	selectedVersions := r.applyMVS(depGraph)

	return r.buildResolutionList(selectedVersions, rootModule)
}

// buildDependencyGraph recursively builds the dependency graph
func (r *DependencyResolver) buildDependencyGraph(ctx context.Context, module *ModuleInfo, depGraph map[string]map[string]*DepRequest, isRoot bool, path []string) error {
	return r.buildDependencyGraphWithMutex(ctx, module, depGraph, isRoot, path, &sync.Mutex{})
}

// buildDependencyGraphWithMutex is the internal implementation with shared mutex
func (r *DependencyResolver) buildDependencyGraphWithMutex(ctx context.Context, module *ModuleInfo, depGraph map[string]map[string]*DepRequest, isRoot bool, path []string, mu *sync.Mutex) error {
	// Process dependencies with semaphore for controlled concurrency
	semaphore := make(chan struct{}, 5) // Limit to 5 concurrent fetches
	var wg sync.WaitGroup

	for _, dep := range module.Dependencies {
		if dep.DevDependency && !isRoot && !r.includeDevDeps {
			continue
		}
		if dep.DevDependency && !r.includeDevDeps {
			continue
		}

		// Add THIS SPECIFIC version requirement to the graph
		mu.Lock()
		if depGraph[dep.Name] == nil {
			depGraph[dep.Name] = make(map[string]*DepRequest)
		}

		if existing, exists := depGraph[dep.Name][dep.Version]; exists {
			// Merge requiredBy lists
			existing.RequiredBy = append(existing.RequiredBy, path[len(path)-1])
			if dep.DevDependency {
				existing.DevDependency = true
			}
		} else {
			// Add this version requirement
			depGraph[dep.Name][dep.Version] = &DepRequest{
				Version:       dep.Version,
				DevDependency: dep.DevDependency,
				RequiredBy:    []string{path[len(path)-1]},
			}
		}
		mu.Unlock()

		// Process transitively - fetch the module at the REQUESTED version
		depKey := dep.Name + "@" + dep.Version
		if _, visited := r.visiting.LoadOrStore(depKey, true); !visited {
			wg.Add(1)
			go func(depName, depVersion string, depPath []string, devDep bool) {
				defer wg.Done()
				semaphore <- struct{}{}
				defer func() { <-semaphore }()

				// Fetch the module at the SPECIFIC requested version
				transitiveDep, err := r.registry.GetModuleFile(ctx, depName, depVersion)
				if err != nil {
					fmt.Printf("Warning: Failed to fetch %s@%s: %v\n", depName, depVersion, err)
					// Remove failed dependency from graph
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

				// Recursively collect dependencies from this specific version
				newPath := append(depPath, depName+"@"+depVersion)
				r.buildDependencyGraphWithMutex(ctx, transitiveDep, depGraph, false, newPath, mu)
			}(dep.Name, dep.Version, path, dep.DevDependency)
		}
	}

	wg.Wait()
	return nil
}

// applyOverrides applies module overrides to the dependency graph
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

// applyMVS applies Minimal Version Selection to choose the best version for each module
func (r *DependencyResolver) applyMVS(depGraph map[string]map[string]*DepRequest) map[string]*DepRequest {
	selected := make(map[string]*DepRequest)

	for moduleName, versions := range depGraph {
		var maxVersion string
		var maxReq *DepRequest

		for version, req := range versions {
			if maxVersion == "" || semver.Compare("v"+version, "v"+maxVersion) > 0 {
				maxVersion = version
				maxReq = req
			}
		}

		if maxVersion != "" {
			selected[moduleName] = maxReq
		}
	}

	return selected
}

// buildResolutionList creates the final resolution list from selected versions
func (r *DependencyResolver) buildResolutionList(selectedVersions map[string]*DepRequest, rootModule *ModuleInfo) (*ResolutionList, error) {
	list := &ResolutionList{
		Modules: []ModuleToResolve{},
	}

	for moduleName, req := range selectedVersions {
		module := ModuleToResolve{
			Name:          moduleName,
			Version:       req.Version,
			Registry:      "https://bcr.bazel.build",
			DevDependency: req.DevDependency,
			RequiredBy:    req.RequiredBy,
		}

		for _, override := range rootModule.Overrides {
			if override.ModuleName == moduleName && override.Registry != "" {
				module.Registry = override.Registry
				break
			}
		}

		list.Modules = append(list.Modules, module)
	}

	sort.Slice(list.Modules, func(i, j int) bool {
		return list.Modules[i].Name < list.Modules[j].Name
	})

	list.Summary = ResolutionSummary{
		TotalModules: len(list.Modules),
	}

	for _, module := range list.Modules {
		if module.DevDependency {
			list.Summary.DevModules++
		} else {
			list.Summary.ProductionModules++
		}
	}

	return list, nil
}
