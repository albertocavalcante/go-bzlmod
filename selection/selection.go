package selection

import (
	"fmt"

	"github.com/albertocavalcante/go-bzlmod/selection/version"
)

// Run executes module selection (version resolution).
//
// This implements Bazel's Selection.run() from Selection.java lines 266-353.
// Reference: https://github.com/bazelbuild/bazel/blob/master/src/main/java/com/google/devtools/build/lib/bazel/bzlmod/Selection.java#L266
//
// The algorithm:
// 1. Compute allowed version sets for multiple-version overrides
// 2. Compute selection groups for each module
// 3. Select highest version for each selection group
// 4. Walk graph from root, removing unreachable modules
//
// Reference: Selection.java lines 44-84 describes the algorithm in detail.
// https://github.com/bazelbuild/bazel/blob/master/src/main/java/com/google/devtools/build/lib/bazel/bzlmod/Selection.java#L44
//
// KNOWN LIMITATION: Strategy Enumeration Not Implemented
//
// Bazel's Selection.java lines 249-264 implements enumerateStrategies() which computes
// the cartesian product of all possible resolutions when max_compatibility_level allows
// multiple valid versions. This is done via computePossibleResolutionResultsForOneDepSpec()
// (lines 182-228) which returns ALL valid versions for a DepSpec, not just the highest one.
//
// Reference: https://github.com/bazelbuild/bazel/blob/master/src/main/java/com/google/devtools/build/lib/bazel/bzlmod/Selection.java#L249
//
// This implementation does NOT enumerate strategies - it validates constraints but doesn't
// try alternative valid versions. Specifically:
//
//   - Bazel: For each DepSpec with max_compatibility_level, computes all modules where
//     version >= dep.version AND compatLevel <= max_compatibility_level, then enumerates
//     the cartesian product of all these possibilities across all deps.
//
//   - This implementation: Simply selects the highest version in each selection group and
//     validates that max_compatibility_level constraints are satisfied. If constraints fail,
//     an error is returned without attempting alternative valid versions.
//
// Impact: This implementation may fail with a max_compatibility_level error in cases where
// Bazel would succeed by trying an alternative resolution strategy. This is an acceptable
// trade-off for simplicity in most real-world use cases.
func Run(graph *DepGraph, overrides map[string]Override) (*Result, error) {
	// Step 1: For any multiple-version overrides, build a mapping from
	// (moduleName, compatibilityLevel) to the set of allowed versions.
	//
	// Reference: Selection.java lines 271-274
	// https://github.com/bazelbuild/bazel/blob/master/src/main/java/com/google/devtools/build/lib/bazel/bzlmod/Selection.java#L271
	allowedVersionSets, err := computeAllowedVersionSets(overrides, graph)
	if err != nil {
		return nil, err
	}

	// Step 2: For each module in the dep graph, pre-compute its selection group.
	// For most modules this is simply its (moduleName, compatibilityLevel) tuple.
	// For modules with multiple-version overrides, it additionally includes the
	// targetAllowedVersion.
	//
	// Reference: Selection.java lines 276-283
	// https://github.com/bazelbuild/bazel/blob/master/src/main/java/com/google/devtools/build/lib/bazel/bzlmod/Selection.java#L276
	selectionGroups := make(map[ModuleKey]SelectionGroup)
	for key, module := range graph.Modules {
		selectionGroups[key] = computeSelectionGroup(module, allowedVersionSets)
	}

	// Step 3: Figure out the version to select for every selection group.
	// This is the core of MVS: select the HIGHEST version in each group.
	//
	// Reference: Selection.java lines 285-291
	// https://github.com/bazelbuild/bazel/blob/master/src/main/java/com/google/devtools/build/lib/bazel/bzlmod/Selection.java#L285
	// "selectedVersions.merge(selectionGroup, key.version(), Comparators::max)"
	selectedVersions := make(map[SelectionGroup]string)
	for key, group := range selectionGroups {
		existing, ok := selectedVersions[group]
		if !ok || version.Compare(key.Version, existing) > 0 {
			selectedVersions[group] = key.Version
		}
	}

	// Step 4: Compute the resolution strategy - how each DepSpec resolves to a version.
	//
	// Reference: Selection.java lines 293-316
	// https://github.com/bazelbuild/bazel/blob/master/src/main/java/com/google/devtools/build/lib/bazel/bzlmod/Selection.java#L293
	//
	// NOTE: This is a simplified single-strategy approach. Bazel's actual implementation
	// uses computePossibleResolutionResultsForOneDepSpec() (lines 182-228) to compute
	// ALL valid versions for each DepSpec when max_compatibility_level is involved,
	// then enumerates the cartesian product via enumerateStrategies() (lines 249-264).
	// https://github.com/bazelbuild/bazel/blob/master/src/main/java/com/google/devtools/build/lib/bazel/bzlmod/Selection.java#L182
	// https://github.com/bazelbuild/bazel/blob/master/src/main/java/com/google/devtools/build/lib/bazel/bzlmod/Selection.java#L249
	//
	// This implementation only tries the highest version, which may fail in edge cases
	// where Bazel would succeed by trying an alternative valid version.
	resolutionStrategy := func(depSpec DepSpec) string {
		depKey := depSpec.ToModuleKey()
		group, ok := selectionGroups[depKey]
		if !ok {
			// Module not in graph, return original version
			return depSpec.Version
		}
		return selectedVersions[group]
	}

	// Step 5: Two-phase graph walking (Bazel 7.6+ behavior)
	//
	// Phase 1: Walk with nodep deps included (validation only).
	// This validates that all nodep dependencies exist and have valid versions,
	// but doesn't include them in the final resolved graph.
	//
	// Reference: Selection.java lines 317-353, 397-403
	// https://github.com/bazelbuild/bazel/blob/master/src/main/java/com/google/devtools/build/lib/bazel/bzlmod/Selection.java#L317
	walkerPhase1 := &depGraphWalker{
		oldGraph:        graph,
		overrides:       overrides,
		selectionGroups: selectionGroups,
		ignoreNodepDeps: false, // Include nodep deps for validation
	}

	// Run phase 1 to validate (we discard the result)
	_, _, err = walkerPhase1.walk(resolutionStrategy)
	if err != nil {
		return nil, err
	}

	// Phase 2: Walk without nodep deps (final pruning).
	// Modules only reachable via nodep edges are excluded from final graph.
	walkerPhase2 := &depGraphWalker{
		oldGraph:        graph,
		overrides:       overrides,
		selectionGroups: selectionGroups,
		ignoreNodepDeps: true, // Exclude nodep deps for pruning
	}

	resolvedGraph, bfsOrder, err := walkerPhase2.walk(resolutionStrategy)
	if err != nil {
		return nil, err
	}

	// Build unpruned graph with updated deps (includes all modules from phase 1)
	unprunedGraph := make(map[ModuleKey]*Module, len(graph.Modules))
	for key, module := range graph.Modules {
		newDeps := make([]DepSpec, len(module.Deps))
		for i, dep := range module.Deps {
			newDeps[i] = DepSpec{
				Name:                  dep.Name,
				Version:               resolutionStrategy(dep),
				MaxCompatibilityLevel: dep.MaxCompatibilityLevel,
			}
		}
		var newNodepDeps []DepSpec
		if len(module.NodepDeps) > 0 {
			newNodepDeps = make([]DepSpec, len(module.NodepDeps))
			for i, dep := range module.NodepDeps {
				newNodepDeps[i] = DepSpec{
					Name:                  dep.Name,
					Version:               resolutionStrategy(dep),
					MaxCompatibilityLevel: dep.MaxCompatibilityLevel,
				}
			}
		}
		unprunedGraph[key] = &Module{
			Key:         key,
			Deps:        newDeps,
			NodepDeps:   newNodepDeps,
			CompatLevel: module.CompatLevel,
		}
	}

	return &Result{
		ResolvedGraph: resolvedGraph,
		UnprunedGraph: unprunedGraph,
		BFSOrder:      bfsOrder,
	}, nil
}

// computeAllowedVersionSets computes a mapping from (moduleName, compatLevel)
// to the set of allowed versions for modules with multiple-version overrides.
//
// Reference: Selection.java lines 117-152
// https://github.com/bazelbuild/bazel/blob/master/src/main/java/com/google/devtools/build/lib/bazel/bzlmod/Selection.java#L117
func computeAllowedVersionSets(overrides map[string]Override, graph *DepGraph) (map[moduleNameAndCompatLevel][]string, error) {
	result := make(map[moduleNameAndCompatLevel][]string)

	for moduleName, override := range overrides {
		mvo, ok := override.(*MultipleVersionOverride)
		if !ok {
			continue
		}

		for _, allowedVersion := range mvo.Versions {
			key := ModuleKey{Name: moduleName, Version: allowedVersion}
			module, ok := graph.Modules[key]
			if !ok {
				return nil, &SelectionError{
					Code: "VERSION_RESOLUTION_ERROR",
					Message: fmt.Sprintf(
						"multiple_version_override for module %s contains version %s, "+
							"but it doesn't exist in the dependency graph",
						moduleName, allowedVersion),
				}
			}

			nameAndCompat := moduleNameAndCompatLevel{
				moduleName:  moduleName,
				compatLevel: module.CompatLevel,
			}
			result[nameAndCompat] = append(result[nameAndCompat], allowedVersion)
		}
	}

	// Sort allowed versions for each group
	for k := range result {
		version.Sort(result[k])
	}

	return result, nil
}

type moduleNameAndCompatLevel struct {
	moduleName  string
	compatLevel int
}

// computeSelectionGroup computes the SelectionGroup for a module.
//
// Reference: Selection.java lines 154-180
// https://github.com/bazelbuild/bazel/blob/master/src/main/java/com/google/devtools/build/lib/bazel/bzlmod/Selection.java#L154
// "If the module has a multiple-version override, information in there will be
// used to compute its targetAllowedVersion."
func computeSelectionGroup(module *Module, allowedVersionSets map[moduleNameAndCompatLevel][]string) SelectionGroup {
	nameAndCompat := moduleNameAndCompatLevel{
		moduleName:  module.Key.Name,
		compatLevel: module.CompatLevel,
	}

	allowedVersions, hasOverride := allowedVersionSets[nameAndCompat]
	if !hasOverride {
		// No multiple-version override - just use module name and compat level
		return SelectionGroup{
			ModuleName:           module.Key.Name,
			CompatLevel:          module.CompatLevel,
			TargetAllowedVersion: "",
		}
	}

	// Find the lowest allowed version >= module's version (ceiling).
	//
	// Reference: Selection.java lines 174-179
	// https://github.com/bazelbuild/bazel/blob/master/src/main/java/com/google/devtools/build/lib/bazel/bzlmod/Selection.java#L174
	// "We use the ceiling method here to quickly locate the lowest allowed version
	// that's still no lower than this module's version."
	targetVersion := ""
	for _, av := range allowedVersions {
		if version.Compare(av, module.Key.Version) >= 0 {
			targetVersion = av
			break
		}
	}

	return SelectionGroup{
		ModuleName:           module.Key.Name,
		CompatLevel:          module.CompatLevel,
		TargetAllowedVersion: targetVersion,
	}
}

// depGraphWalker walks the dependency graph from the root, collecting reachable nodes.
//
// Reference: Selection.java lines 355-479, DepGraphWalker class
// https://github.com/bazelbuild/bazel/blob/master/src/main/java/com/google/devtools/build/lib/bazel/bzlmod/Selection.java#L355
type depGraphWalker struct {
	oldGraph        *DepGraph
	overrides       map[string]Override
	selectionGroups map[ModuleKey]SelectionGroup
	// ignoreNodepDeps when true excludes nodep dependencies from graph traversal.
	// This is used in the second phase of two-phase walking to prune modules
	// that are only reachable via nodep edges.
	ignoreNodepDeps bool
}

// walk traverses the graph from root, building a new graph with only reachable modules.
// Returns the resolved graph, BFS order, and any error.
//
// Reference: Selection.java lines 374-408
// https://github.com/bazelbuild/bazel/blob/master/src/main/java/com/google/devtools/build/lib/bazel/bzlmod/Selection.java#L374
func (w *depGraphWalker) walk(resolutionStrategy func(DepSpec) string) (map[ModuleKey]*Module, []ModuleKey, error) {
	moduleByName := make(map[string]existingModule)
	newGraph := make(map[ModuleKey]*Module)
	known := make(map[ModuleKey]bool)
	bfsOrder := []ModuleKey{}

	// BFS queue: (moduleKey, dependent)
	type queueItem struct {
		key       ModuleKey
		dependent *ModuleKey
	}
	queue := []queueItem{{key: w.oldGraph.RootKey, dependent: nil}}
	known[w.oldGraph.RootKey] = true

	for len(queue) > 0 {
		item := queue[0]
		queue = queue[1:]

		oldModule := w.oldGraph.Modules[item.key]
		if oldModule == nil {
			continue
		}

		// Transform deps using resolution strategy
		newDeps := make([]DepSpec, len(oldModule.Deps))
		for i, dep := range oldModule.Deps {
			resolvedVersion := resolutionStrategy(dep)
			newDeps[i] = DepSpec{
				Name:                  dep.Name,
				Version:               resolvedVersion,
				MaxCompatibilityLevel: dep.MaxCompatibilityLevel,
			}

			// Validate MaxCompatibilityLevel constraint
			// Reference: Bazel enforces that resolved module's compat level <= max_compatibility_level
			// https://github.com/bazelbuild/bazel/blob/master/src/main/java/com/google/devtools/build/lib/bazel/bzlmod/Selection.java#L182
			// See computePossibleResolutionResultsForOneDepSpec() for the full constraint logic.
			if dep.MaxCompatibilityLevel >= 0 {
				resolvedKey := ModuleKey{Name: dep.Name, Version: resolvedVersion}
				if resolvedModule, ok := w.oldGraph.Modules[resolvedKey]; ok {
					if resolvedModule.CompatLevel > dep.MaxCompatibilityLevel {
						return nil, nil, &SelectionError{
							Code: "VERSION_RESOLUTION_ERROR",
							Message: fmt.Sprintf(
								"%v depends on %s with max_compatibility_level %d, "+
									"but %s@%s has compatibility_level %d which is higher",
								item.key, dep.Name, dep.MaxCompatibilityLevel,
								dep.Name, resolvedVersion, resolvedModule.CompatLevel),
						}
					}
				}
			}
		}

		// Transform nodep deps using resolution strategy (for validation and consistency)
		var newNodepDeps []DepSpec
		if len(oldModule.NodepDeps) > 0 {
			newNodepDeps = make([]DepSpec, len(oldModule.NodepDeps))
			for i, dep := range oldModule.NodepDeps {
				resolvedVersion := resolutionStrategy(dep)
				newNodepDeps[i] = DepSpec{
					Name:                  dep.Name,
					Version:               resolvedVersion,
					MaxCompatibilityLevel: dep.MaxCompatibilityLevel,
				}

				// Validate MaxCompatibilityLevel for nodep deps too
				if dep.MaxCompatibilityLevel >= 0 {
					resolvedKey := ModuleKey{Name: dep.Name, Version: resolvedVersion}
					if resolvedModule, ok := w.oldGraph.Modules[resolvedKey]; ok {
						if resolvedModule.CompatLevel > dep.MaxCompatibilityLevel {
							return nil, nil, &SelectionError{
								Code: "VERSION_RESOLUTION_ERROR",
								Message: fmt.Sprintf(
									"%v has nodep_dep on %s with max_compatibility_level %d, "+
										"but %s@%s has compatibility_level %d which is higher",
									item.key, dep.Name, dep.MaxCompatibilityLevel,
									dep.Name, resolvedVersion, resolvedModule.CompatLevel),
							}
						}
					}
				}
			}
		}

		module := &Module{
			Key:         item.key,
			Deps:        newDeps,
			NodepDeps:   newNodepDeps,
			CompatLevel: oldModule.CompatLevel,
		}

		// Check for conflicts
		if err := w.visit(item.key, module, item.dependent, moduleByName); err != nil {
			return nil, nil, err
		}

		// Add deps to queue (regular deps are always followed)
		for _, dep := range module.Deps {
			depKey := dep.ToModuleKey()
			if !known[depKey] {
				known[depKey] = true
				queue = append(queue, queueItem{key: depKey, dependent: &item.key})
			}
		}

		// Add nodep deps to queue only in phase 1 (when not ignoring them)
		// In phase 2, nodep deps are ignored for pruning
		if !w.ignoreNodepDeps {
			for _, dep := range module.NodepDeps {
				depKey := dep.ToModuleKey()
				if !known[depKey] {
					known[depKey] = true
					queue = append(queue, queueItem{key: depKey, dependent: &item.key})
				}
			}
		}

		newGraph[item.key] = module
		bfsOrder = append(bfsOrder, item.key)
	}

	return newGraph, bfsOrder, nil
}

// existingModule tracks modules we've already visited, for conflict detection.
type existingModule struct {
	key         ModuleKey
	compatLevel int
	dependent   *ModuleKey
}

// visit checks for conflicts when adding a module to the resolved graph.
//
// Reference: Selection.java lines 410-472
// https://github.com/bazelbuild/bazel/blob/master/src/main/java/com/google/devtools/build/lib/bazel/bzlmod/Selection.java#L410
func (w *depGraphWalker) visit(key ModuleKey, module *Module, from *ModuleKey, moduleByName map[string]existingModule) error {
	// Check for multiple-version override conflicts
	if override, ok := w.overrides[key.Name].(*MultipleVersionOverride); ok {
		group := w.selectionGroups[key]
		if group.TargetAllowedVersion == "" {
			// No valid target allowed version - error
			//
			// Reference: Selection.java lines 416-429
			// https://github.com/bazelbuild/bazel/blob/master/src/main/java/com/google/devtools/build/lib/bazel/bzlmod/Selection.java#L416
			return &SelectionError{
				Code: "VERSION_RESOLUTION_ERROR",
				Message: fmt.Sprintf(
					"%v depends on %v which is not allowed by the multiple_version_override on %s, "+
						"which allows only %v",
					from, key, key.Name, override.Versions),
			}
		}
	} else {
		// Check for compatibility level conflicts
		//
		// Reference: Selection.java lines 431-451
		// https://github.com/bazelbuild/bazel/blob/master/src/main/java/com/google/devtools/build/lib/bazel/bzlmod/Selection.java#L431
		// "This has to mean that a module with the same name but a different
		// compatibility level was also selected."
		existing, ok := moduleByName[module.Key.Name]
		if ok && existing.compatLevel != module.CompatLevel {
			return &SelectionError{
				Code: "VERSION_RESOLUTION_ERROR",
				Message: fmt.Sprintf(
					"%v depends on %v with compatibility level %d, but %v depends on %v "+
						"with compatibility level %d which is different",
					from, key, module.CompatLevel,
					existing.dependent, existing.key, existing.compatLevel),
			}
		}
		moduleByName[module.Key.Name] = existingModule{
			key:         key,
			compatLevel: module.CompatLevel,
			dependent:   from,
		}
	}

	return nil
}
