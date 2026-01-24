package gobzlmod

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"sync"

	"github.com/albertocavalcante/go-bzlmod/selection/version"
)

const defaultMaxConcurrency = 5

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
	if rootModule == nil {
		return nil, fmt.Errorf("root module is nil")
	}

	depGraph := make(map[string]map[string]*DepRequest)
	visiting := &sync.Map{}
	overrideIndex := indexOverrides(rootModule.Overrides)

	if err := r.buildDependencyGraphWithOverrides(ctx, rootModule, depGraph, visiting, []string{"<root>"}, overrideIndex); err != nil {
		return nil, fmt.Errorf("build dependency graph: %w", err)
	}

	r.applyOverrides(depGraph, rootModule.Overrides)
	selectedVersions := r.applyMVS(depGraph)
	return r.buildResolutionList(selectedVersions, rootModule)
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

	tasks := make(chan depTask, defaultMaxConcurrency)
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
			if dep.DevDependency && !r.includeDevDeps {
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
