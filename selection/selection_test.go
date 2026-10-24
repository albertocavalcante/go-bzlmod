// Package selection implements Bazel's module version selection algorithm.
// Tests are based on Bazel's Selection.java behavior.
//
// Reference: https://github.com/bazelbuild/bazel/blob/master/src/main/java/com/google/devtools/build/lib/bazel/bzlmod/Selection.java
package selection

import (
	"errors"
	"sort"
	"strings"
	"testing"

	"github.com/albertocavalcante/go-bzlmod/selection/version"
)

// TestBasicMVS tests the basic case from Selection.java lines 51-58:
// "In the most basic case, only one version of each module is selected.
// The selected version is simply the highest among all existing versions
// in the dep graph."
func TestBasicMVS(t *testing.T) {
	// Given: A dependency graph with multiple versions of the same module
	//   root -> A@1.0 -> B@1.0
	//        -> C@1.0 -> B@2.0
	// Expected: B@2.0 is selected (highest version)
	graph := &DepGraph{
		Modules: map[ModuleKey]*Module{
			{Name: "<root>", Version: ""}: {
				Key:  ModuleKey{Name: "<root>", Version: ""},
				Deps: []DepSpec{{Name: "A", Version: "1.0"}, {Name: "C", Version: "1.0"}},
			},
			{Name: "A", Version: "1.0"}: {
				Key:  ModuleKey{Name: "A", Version: "1.0"},
				Deps: []DepSpec{{Name: "B", Version: "1.0"}},
			},
			{Name: "C", Version: "1.0"}: {
				Key:  ModuleKey{Name: "C", Version: "1.0"},
				Deps: []DepSpec{{Name: "B", Version: "2.0"}},
			},
			{Name: "B", Version: "1.0"}: {
				Key:  ModuleKey{Name: "B", Version: "1.0"},
				Deps: nil,
			},
			{Name: "B", Version: "2.0"}: {
				Key:  ModuleKey{Name: "B", Version: "2.0"},
				Deps: nil,
			},
		},
		RootKey: ModuleKey{Name: "<root>", Version: ""},
	}

	result, err := Run(graph, nil)
	if err != nil {
		t.Fatalf("Selection.Run() error = %v", err)
	}

	// Verify B@2.0 was selected
	bKey := ModuleKey{Name: "B", Version: "2.0"}
	if _, ok := result.ResolvedGraph[bKey]; !ok {
		t.Errorf("Expected B@2.0 to be selected, got keys: %v", keys(result.ResolvedGraph))
	}

	// Verify B@1.0 was removed
	b1Key := ModuleKey{Name: "B", Version: "1.0"}
	if _, ok := result.ResolvedGraph[b1Key]; ok {
		t.Errorf("Expected B@1.0 to be removed from resolved graph")
	}

	// Verify deps were rewritten to point to selected version
	aModule := result.ResolvedGraph[ModuleKey{Name: "A", Version: "1.0"}]
	if aModule == nil {
		t.Fatal("A@1.0 should be in resolved graph")
	}
	if len(aModule.Deps) != 1 || aModule.Deps[0].Version != "2.0" {
		t.Errorf("A's dep on B should be rewritten to 2.0, got %v", aModule.Deps)
	}
}

// TestUnreachableModuleRemoval tests Selection.java lines 58-59:
// "We also remove any module that becomes unreachable from the root module
// because of the removal of some other module."
func TestUnreachableModuleRemoval(t *testing.T) {
	// Given:
	//   root -> A@1.0 -> B@1.0 -> D@1.0
	//        -> A@2.0 (no deps)
	// After selection: A@2.0 selected, B and D become unreachable
	graph := &DepGraph{
		Modules: map[ModuleKey]*Module{
			{Name: "<root>", Version: ""}: {
				Key:  ModuleKey{Name: "<root>", Version: ""},
				Deps: []DepSpec{{Name: "A", Version: "1.0"}, {Name: "A", Version: "2.0"}},
			},
			{Name: "A", Version: "1.0"}: {
				Key:  ModuleKey{Name: "A", Version: "1.0"},
				Deps: []DepSpec{{Name: "B", Version: "1.0"}},
			},
			{Name: "A", Version: "2.0"}: {
				Key:  ModuleKey{Name: "A", Version: "2.0"},
				Deps: nil,
			},
			{Name: "B", Version: "1.0"}: {
				Key:  ModuleKey{Name: "B", Version: "1.0"},
				Deps: []DepSpec{{Name: "D", Version: "1.0"}},
			},
			{Name: "D", Version: "1.0"}: {
				Key:  ModuleKey{Name: "D", Version: "1.0"},
				Deps: nil,
			},
		},
		RootKey: ModuleKey{Name: "<root>", Version: ""},
	}

	result, err := Run(graph, nil)
	if err != nil {
		t.Fatalf("Selection.Run() error = %v", err)
	}

	// A@2.0 should be selected
	if _, ok := result.ResolvedGraph[ModuleKey{Name: "A", Version: "2.0"}]; !ok {
		t.Error("Expected A@2.0 to be selected")
	}

	// B and D should be removed (unreachable)
	if _, ok := result.ResolvedGraph[ModuleKey{Name: "B", Version: "1.0"}]; ok {
		t.Error("Expected B@1.0 to be removed (unreachable)")
	}
	if _, ok := result.ResolvedGraph[ModuleKey{Name: "D", Version: "1.0"}]; ok {
		t.Error("Expected D@1.0 to be removed (unreachable)")
	}
}

// TestCompatibilityLevelSelection tests Selection.java lines 60-63:
// "If versions of the same module but with different compatibility levels exist,
// then one version is selected for each compatibility level."
func TestCompatibilityLevelSelection(t *testing.T) {
	// Given: A@1.0 (compat=1), A@2.0 (compat=2) - different compat levels
	// Both can coexist initially, but only one remains after pruning
	graph := &DepGraph{
		Modules: map[ModuleKey]*Module{
			{Name: "<root>", Version: ""}: {
				Key:  ModuleKey{Name: "<root>", Version: ""},
				Deps: []DepSpec{{Name: "A", Version: "1.0"}, {Name: "B", Version: "1.0"}},
			},
			{Name: "A", Version: "1.0"}: {
				Key:         ModuleKey{Name: "A", Version: "1.0"},
				CompatLevel: 1,
				Deps:        nil,
			},
			{Name: "B", Version: "1.0"}: {
				Key:  ModuleKey{Name: "B", Version: "1.0"},
				Deps: []DepSpec{{Name: "A", Version: "2.0"}},
			},
			{Name: "A", Version: "2.0"}: {
				Key:         ModuleKey{Name: "A", Version: "2.0"},
				CompatLevel: 2,
				Deps:        nil,
			},
		},
		RootKey: ModuleKey{Name: "<root>", Version: ""},
	}

	// Without multiple_version_override, this should error because
	// two different compatibility levels of same module would exist
	_, err := Run(graph, nil)
	if err == nil {
		t.Error("Expected error due to different compatibility levels without override")
	}
}

// TestSingleVersionOverrideIsNotAppliedDuringSelection documents that
// selection.Run does NOT consume SingleVersionOverride.
//
// This test used to be named TestSingleVersionOverride and passed *vacuously*:
// Run only ever type-asserts *MultipleVersionOverride (see computeAllowedVersionSets
// and visit), so the override below is ignored entirely, and B@1.5 wins under
// plain MVS simply by being the highest B in the graph. The name claimed
// coverage that did not exist.
//
// That layering is correct and matches Bazel: single_version_override acts while
// the dependency graph is being constructed, not in Selection.java. In this
// module the pinning happens in the root package's resolver, which rewrites a
// dep's version before the graph reaches selection. Do not implement
// single-version handling here -- it would duplicate that logic and diverge from
// Bazel.
func TestSingleVersionOverrideIsNotAppliedDuringSelection(t *testing.T) {
	// Given: B@1.0 and B@2.0 in graph, override forces B@1.5
	graph := &DepGraph{
		Modules: map[ModuleKey]*Module{
			{Name: "<root>", Version: ""}: {
				Key:  ModuleKey{Name: "<root>", Version: ""},
				Deps: []DepSpec{{Name: "A", Version: "1.0"}},
			},
			{Name: "A", Version: "1.0"}: {
				Key:  ModuleKey{Name: "A", Version: "1.0"},
				Deps: []DepSpec{{Name: "B", Version: "1.0"}},
			},
			{Name: "B", Version: "1.0"}: {
				Key:  ModuleKey{Name: "B", Version: "1.0"},
				Deps: nil,
			},
			// The override version module must exist
			{Name: "B", Version: "1.5"}: {
				Key:  ModuleKey{Name: "B", Version: "1.5"},
				Deps: nil,
			},
		},
		RootKey: ModuleKey{Name: "<root>", Version: ""},
	}

	overrides := map[string]Override{
		"B": &SingleVersionOverride{Version: "1.5"},
	}

	result, err := Run(graph, overrides)
	if err != nil {
		t.Fatalf("Selection.Run() error = %v", err)
	}

	// B@1.5 should be selected due to override
	if _, ok := result.ResolvedGraph[ModuleKey{Name: "B", Version: "1.5"}]; !ok {
		t.Errorf("Expected B@1.5 to be selected due to override, got: %v", keys(result.ResolvedGraph))
	}
}

// TestDiamondDependency tests the classic diamond dependency pattern.
func TestDiamondDependency(t *testing.T) {
	// Given: Diamond pattern
	//   root -> A@1.0 -> C@1.0
	//        -> B@1.0 -> C@2.0
	// Expected: C@2.0 selected (highest), A and B deps rewritten
	graph := &DepGraph{
		Modules: map[ModuleKey]*Module{
			{Name: "<root>", Version: ""}: {
				Key:  ModuleKey{Name: "<root>", Version: ""},
				Deps: []DepSpec{{Name: "A", Version: "1.0"}, {Name: "B", Version: "1.0"}},
			},
			{Name: "A", Version: "1.0"}: {
				Key:  ModuleKey{Name: "A", Version: "1.0"},
				Deps: []DepSpec{{Name: "C", Version: "1.0"}},
			},
			{Name: "B", Version: "1.0"}: {
				Key:  ModuleKey{Name: "B", Version: "1.0"},
				Deps: []DepSpec{{Name: "C", Version: "2.0"}},
			},
			{Name: "C", Version: "1.0"}: {
				Key:  ModuleKey{Name: "C", Version: "1.0"},
				Deps: nil,
			},
			{Name: "C", Version: "2.0"}: {
				Key:  ModuleKey{Name: "C", Version: "2.0"},
				Deps: nil,
			},
		},
		RootKey: ModuleKey{Name: "<root>", Version: ""},
	}

	result, err := Run(graph, nil)
	if err != nil {
		t.Fatalf("Selection.Run() error = %v", err)
	}

	// C@2.0 should be selected
	if _, ok := result.ResolvedGraph[ModuleKey{Name: "C", Version: "2.0"}]; !ok {
		t.Error("Expected C@2.0 to be selected")
	}

	// C@1.0 should be removed
	if _, ok := result.ResolvedGraph[ModuleKey{Name: "C", Version: "1.0"}]; ok {
		t.Error("Expected C@1.0 to be removed")
	}

	// A's dep should be rewritten to C@2.0
	a := result.ResolvedGraph[ModuleKey{Name: "A", Version: "1.0"}]
	if a.Deps[0].Version != "2.0" {
		t.Errorf("Expected A's dep on C to be rewritten to 2.0, got %s", a.Deps[0].Version)
	}
}

// TestBFSOrder tests that the resolved graph maintains BFS iteration order.
// Reference: Selection.java line 91-92: "Final dep graph sorted in BFS iteration order"
func TestBFSOrder(t *testing.T) {
	graph := &DepGraph{
		Modules: map[ModuleKey]*Module{
			{Name: "<root>", Version: ""}: {
				Key:  ModuleKey{Name: "<root>", Version: ""},
				Deps: []DepSpec{{Name: "A", Version: "1.0"}, {Name: "B", Version: "1.0"}},
			},
			{Name: "A", Version: "1.0"}: {
				Key:  ModuleKey{Name: "A", Version: "1.0"},
				Deps: []DepSpec{{Name: "C", Version: "1.0"}},
			},
			{Name: "B", Version: "1.0"}: {
				Key:  ModuleKey{Name: "B", Version: "1.0"},
				Deps: []DepSpec{{Name: "D", Version: "1.0"}},
			},
			{Name: "C", Version: "1.0"}: {
				Key:  ModuleKey{Name: "C", Version: "1.0"},
				Deps: nil,
			},
			{Name: "D", Version: "1.0"}: {
				Key:  ModuleKey{Name: "D", Version: "1.0"},
				Deps: nil,
			},
		},
		RootKey: ModuleKey{Name: "<root>", Version: ""},
	}

	result, err := Run(graph, nil)
	if err != nil {
		t.Fatalf("Selection.Run() error = %v", err)
	}

	// Check that BFS order is maintained
	order := result.BFSOrder
	if len(order) != 5 {
		t.Fatalf("Expected 5 modules in BFS order, got %d", len(order))
	}
	// Root should be first
	if order[0].Name != "<root>" {
		t.Error("Root should be first in BFS order")
	}
	// A and B should come before C and D
	aIdx, bIdx, cIdx, dIdx := -1, -1, -1, -1
	for i, k := range order {
		switch k.Name {
		case "A":
			aIdx = i
		case "B":
			bIdx = i
		case "C":
			cIdx = i
		case "D":
			dIdx = i
		}
	}
	if aIdx > cIdx || bIdx > dIdx {
		t.Errorf("BFS order violated: A@%d, B@%d, C@%d, D@%d", aIdx, bIdx, cIdx, dIdx)
	}
}

// TestMissingModuleIsSkippedNotAnError records the current lower-layer walk
// behavior for sparse input. Discovery must separately reject missing required
// dependencies; this test does not establish that incomplete resolution is valid.
func TestMissingModuleIsSkippedNotAnError(t *testing.T) {
	graph := &DepGraph{
		Modules: map[ModuleKey]*Module{
			{Name: "<root>", Version: ""}: {
				Key: ModuleKey{Name: "<root>", Version: ""},
				// A@1.0 is referenced but deliberately absent from Modules.
				Deps: []DepSpec{{Name: "A", Version: "1.0"}},
			},
		},
		RootKey: ModuleKey{Name: "<root>", Version: ""},
	}

	result, err := Run(graph, nil)
	if err != nil {
		t.Fatalf("Run() = %v, want the missing module to be skipped silently", err)
	}

	// The missing module is simply absent from the resolved graph...
	if _, ok := result.ResolvedGraph[ModuleKey{Name: "A", Version: "1.0"}]; ok {
		t.Error("A@1.0 is not in the input graph, so it must not appear in the resolved graph")
	}
	// ...while the root still resolves.
	if _, ok := result.ResolvedGraph[ModuleKey{Name: "<root>", Version: ""}]; !ok {
		t.Error("the root module must still be resolved")
	}
}

// TestMissingRootModuleYieldsEmptyResult pins the degenerate case: a graph whose
// RootKey is absent from Modules resolves to nothing rather than erroring, for
// the same reason as above.
func TestMissingRootModuleYieldsEmptyResult(t *testing.T) {
	graph := &DepGraph{
		Modules: map[ModuleKey]*Module{},
		RootKey: ModuleKey{Name: "<root>", Version: ""},
	}

	result, err := Run(graph, nil)
	if err != nil {
		t.Fatalf("Run() = %v, want an empty result for a rootless graph", err)
	}
	if len(result.ResolvedGraph) != 0 {
		t.Errorf("ResolvedGraph has %d entries, want 0", len(result.ResolvedGraph))
	}
	if len(result.BFSOrder) != 0 {
		t.Errorf("BFSOrder has %d entries, want 0", len(result.BFSOrder))
	}
}

func keys(m map[ModuleKey]*Module) []ModuleKey {
	result := make([]ModuleKey, 0, len(m))
	for k := range m {
		result = append(result, k)
	}
	return result
}

// TestMaxCompatibilityLevel_Exceeded tests that max_compatibility_level is enforced.
func TestMaxCompatibilityLevel_Exceeded(t *testing.T) {
	// Given: A depends on B@1.0 with max_compatibility_level=1
	//        B@1.0 has compat_level=2 (exceeds max)
	// Expected: Error due to compat level exceeding max
	graph := &DepGraph{
		Modules: map[ModuleKey]*Module{
			{Name: "<root>", Version: ""}: {
				Key: ModuleKey{Name: "<root>", Version: ""},
				Deps: []DepSpec{{
					Name:                  "B",
					Version:               "1.0",
					MaxCompatibilityLevel: 1, // Max allowed is 1
				}},
			},
			{Name: "B", Version: "1.0"}: {
				Key:         ModuleKey{Name: "B", Version: "1.0"},
				CompatLevel: 2, // Has compat level 2, exceeds max
				Deps:        nil,
			},
		},
		RootKey: ModuleKey{Name: "<root>", Version: ""},
	}

	_, err := Run(graph, nil)
	if err == nil {
		t.Error("Expected error due to max_compatibility_level exceeded")
	}
	if selErr, ok := err.(*SelectionError); ok {
		if selErr.Code != "VERSION_RESOLUTION_ERROR" {
			t.Errorf("Expected VERSION_RESOLUTION_ERROR, got %s", selErr.Code)
		}
	}
}

// TestMaxCompatibilityLevel_Satisfied tests that max_compatibility_level allows valid deps.
func TestMaxCompatibilityLevel_Satisfied(t *testing.T) {
	// Given: A depends on B@1.0 with max_compatibility_level=2
	//        B@1.0 has compat_level=1 (within max)
	// Expected: Success
	graph := &DepGraph{
		Modules: map[ModuleKey]*Module{
			{Name: "<root>", Version: ""}: {
				Key: ModuleKey{Name: "<root>", Version: ""},
				Deps: []DepSpec{{
					Name:                  "B",
					Version:               "1.0",
					MaxCompatibilityLevel: 2, // Max allowed is 2
				}},
			},
			{Name: "B", Version: "1.0"}: {
				Key:         ModuleKey{Name: "B", Version: "1.0"},
				CompatLevel: 1, // Has compat level 1, within max
				Deps:        nil,
			},
		},
		RootKey: ModuleKey{Name: "<root>", Version: ""},
	}

	result, err := Run(graph, nil)
	if err != nil {
		t.Fatalf("Selection.Run() error = %v", err)
	}

	// B@1.0 should be selected
	if _, ok := result.ResolvedGraph[ModuleKey{Name: "B", Version: "1.0"}]; !ok {
		t.Error("Expected B@1.0 to be selected")
	}
}

// TestMaxCompatibilityLevel_NoConstraint tests that -1 means no constraint.
func TestMaxCompatibilityLevel_NoConstraint(t *testing.T) {
	// Given: A depends on B@1.0 with max_compatibility_level=-1 (no constraint)
	//        B@1.0 has compat_level=10
	// Expected: Success (no max constraint)
	graph := &DepGraph{
		Modules: map[ModuleKey]*Module{
			{Name: "<root>", Version: ""}: {
				Key: ModuleKey{Name: "<root>", Version: ""},
				Deps: []DepSpec{{
					Name:                  "B",
					Version:               "1.0",
					MaxCompatibilityLevel: -1, // No constraint
				}},
			},
			{Name: "B", Version: "1.0"}: {
				Key:         ModuleKey{Name: "B", Version: "1.0"},
				CompatLevel: 10, // High compat level
				Deps:        nil,
			},
		},
		RootKey: ModuleKey{Name: "<root>", Version: ""},
	}

	result, err := Run(graph, nil)
	if err != nil {
		t.Fatalf("Selection.Run() error = %v", err)
	}

	if _, ok := result.ResolvedGraph[ModuleKey{Name: "B", Version: "1.0"}]; !ok {
		t.Error("Expected B@1.0 to be selected")
	}
}

// TestNodepDeps_NotInResolvedGraph tests that nodep deps are excluded from final graph.
func TestNodepDeps_NotInResolvedGraph(t *testing.T) {
	// Given: root has regular dep on A, A has nodep dep on B
	// Expected: B is NOT in resolved graph (only reachable via nodep)
	graph := &DepGraph{
		Modules: map[ModuleKey]*Module{
			{Name: "<root>", Version: ""}: {
				Key:  ModuleKey{Name: "<root>", Version: ""},
				Deps: []DepSpec{{Name: "A", Version: "1.0"}},
			},
			{Name: "A", Version: "1.0"}: {
				Key:       ModuleKey{Name: "A", Version: "1.0"},
				Deps:      nil,
				NodepDeps: []DepSpec{{Name: "B", Version: "1.0"}}, // nodep dep
			},
			{Name: "B", Version: "1.0"}: {
				Key:  ModuleKey{Name: "B", Version: "1.0"},
				Deps: nil,
			},
		},
		RootKey: ModuleKey{Name: "<root>", Version: ""},
	}

	result, err := Run(graph, nil)
	if err != nil {
		t.Fatalf("Selection.Run() error = %v", err)
	}

	// A should be in resolved graph
	if _, ok := result.ResolvedGraph[ModuleKey{Name: "A", Version: "1.0"}]; !ok {
		t.Error("Expected A@1.0 to be in resolved graph")
	}

	// B should NOT be in resolved graph (only reachable via nodep)
	if _, ok := result.ResolvedGraph[ModuleKey{Name: "B", Version: "1.0"}]; ok {
		t.Error("Expected B@1.0 to NOT be in resolved graph (only nodep reachable)")
	}

	// B should be in unpruned graph
	if _, ok := result.UnprunedGraph[ModuleKey{Name: "B", Version: "1.0"}]; !ok {
		t.Error("Expected B@1.0 to be in unpruned graph")
	}
}

// TestNodepDeps_Validation tests that nodep deps are still validated.
func TestNodepDeps_Validation(t *testing.T) {
	// Given: A has nodep dep on B with max_compatibility_level=1
	//        B@1.0 has compat_level=2 (exceeds max)
	// Expected: Error (nodep deps are still validated)
	graph := &DepGraph{
		Modules: map[ModuleKey]*Module{
			{Name: "<root>", Version: ""}: {
				Key:  ModuleKey{Name: "<root>", Version: ""},
				Deps: []DepSpec{{Name: "A", Version: "1.0"}},
			},
			{Name: "A", Version: "1.0"}: {
				Key:  ModuleKey{Name: "A", Version: "1.0"},
				Deps: nil,
				NodepDeps: []DepSpec{{
					Name:                  "B",
					Version:               "1.0",
					MaxCompatibilityLevel: 1, // Max is 1
				}},
			},
			{Name: "B", Version: "1.0"}: {
				Key:         ModuleKey{Name: "B", Version: "1.0"},
				CompatLevel: 2, // Exceeds max
				Deps:        nil,
			},
		},
		RootKey: ModuleKey{Name: "<root>", Version: ""},
	}

	_, err := Run(graph, nil)
	if err == nil {
		t.Error("Expected error due to max_compatibility_level exceeded for nodep dep")
	}
}

// =============================================================================
// Phase 6: Strategy Enumeration Tests
// =============================================================================

// TestStrategyEnumeration_SingleStrategy tests that when there's no ambiguity,
// only one strategy is used (the default highest-version strategy).
func TestStrategyEnumeration_SingleStrategy(t *testing.T) {
	// Given: Simple graph with no max_compatibility_level constraints
	//   root -> A@1.0 -> B@1.0
	//        -> B@2.0
	// Expected: B@2.0 selected (standard MVS, single strategy)
	graph := &DepGraph{
		Modules: map[ModuleKey]*Module{
			{Name: "<root>", Version: ""}: {
				Key:  ModuleKey{Name: "<root>", Version: ""},
				Deps: []DepSpec{{Name: "A", Version: "1.0"}, {Name: "B", Version: "2.0"}},
			},
			{Name: "A", Version: "1.0"}: {
				Key:  ModuleKey{Name: "A", Version: "1.0"},
				Deps: []DepSpec{{Name: "B", Version: "1.0"}},
			},
			{Name: "B", Version: "1.0"}: {
				Key:  ModuleKey{Name: "B", Version: "1.0"},
				Deps: nil,
			},
			{Name: "B", Version: "2.0"}: {
				Key:  ModuleKey{Name: "B", Version: "2.0"},
				Deps: nil,
			},
		},
		RootKey: ModuleKey{Name: "<root>", Version: ""},
	}

	result, err := Run(graph, nil)
	if err != nil {
		t.Fatalf("Selection.Run() error = %v", err)
	}

	// B@2.0 should be selected
	if _, ok := result.ResolvedGraph[ModuleKey{Name: "B", Version: "2.0"}]; !ok {
		t.Error("Expected B@2.0 to be selected")
	}
}

// TestStrategyEnumeration_MultipleStrategies_FirstSucceeds tests that when
// multiple strategies are possible, the first successful one is used.
func TestStrategyEnumeration_MultipleStrategies_FirstSucceeds(t *testing.T) {
	// Given: Graph where max_compatibility_level allows multiple choices
	//   root -> A@1.0 (compat=1) with max_compatibility_level=2
	//   Both A@1.0 (compat=1) and A@2.0 (compat=2) exist and are valid
	// Expected: Either version works, first strategy should succeed
	graph := &DepGraph{
		Modules: map[ModuleKey]*Module{
			{Name: "<root>", Version: ""}: {
				Key: ModuleKey{Name: "<root>", Version: ""},
				Deps: []DepSpec{{
					Name:                  "A",
					Version:               "1.0",
					MaxCompatibilityLevel: 2, // Allows compat levels 1 and 2
				}},
			},
			{Name: "A", Version: "1.0"}: {
				Key:         ModuleKey{Name: "A", Version: "1.0"},
				CompatLevel: 1,
				Deps:        nil,
			},
			{Name: "A", Version: "2.0"}: {
				Key:         ModuleKey{Name: "A", Version: "2.0"},
				CompatLevel: 2,
				Deps:        nil,
			},
		},
		RootKey: ModuleKey{Name: "<root>", Version: ""},
	}

	result, err := Run(graph, nil)
	if err != nil {
		t.Fatalf("Selection.Run() error = %v", err)
	}

	// One of the versions should be selected
	hasA1 := false
	hasA2 := false
	for key := range result.ResolvedGraph {
		if key.Name == "A" {
			switch key.Version {
			case "1.0":
				hasA1 = true
			case "2.0":
				hasA2 = true
			}
		}
	}

	if !hasA1 && !hasA2 {
		t.Error("Expected either A@1.0 or A@2.0 to be selected")
	}
}

// TestStrategyEnumeration_FallbackToAlternative tests that when the first
// strategy fails, we fall back to alternative strategies.
func TestStrategyEnumeration_FallbackToAlternative(t *testing.T) {
	// This is a complex scenario where:
	// - root depends on A@1.0 with max_compatibility_level=2
	// - root also depends on B@1.0
	// - B@1.0 depends on A@2.0 (compat=2)
	// - A@1.0 has compat=1, A@2.0 has compat=2
	//
	// Without strategy enumeration, MVS would select A@2.0 (highest).
	// But A@2.0 has compat=2 which exceeds the max_compatibility_level=2...wait, 2<=2 is fine.
	// Let me think of a better scenario.
	//
	// Actually, let's create a scenario where:
	// - root -> C@1.0 (max_compat=1)
	// - Another path brings in C@2.0 (compat=2)
	// - C@1.0 has compat=1, C@2.0 has compat=2
	// - MVS picks C@2.0, but that violates max_compat=1
	// - Strategy enumeration should try C@1.0 which satisfies the constraint

	graph := &DepGraph{
		Modules: map[ModuleKey]*Module{
			{Name: "<root>", Version: ""}: {
				Key: ModuleKey{Name: "<root>", Version: ""},
				Deps: []DepSpec{
					{
						Name:                  "C",
						Version:               "1.0",
						MaxCompatibilityLevel: 1, // Only allows compat level 1
					},
					{Name: "B", Version: "1.0"},
				},
			},
			{Name: "B", Version: "1.0"}: {
				Key: ModuleKey{Name: "B", Version: "1.0"},
				// B depends on C@2.0, which MVS would prefer
				Deps: []DepSpec{{Name: "C", Version: "2.0"}},
			},
			{Name: "C", Version: "1.0"}: {
				Key:         ModuleKey{Name: "C", Version: "1.0"},
				CompatLevel: 1,
				Deps:        nil,
			},
			{Name: "C", Version: "2.0"}: {
				Key:         ModuleKey{Name: "C", Version: "2.0"},
				CompatLevel: 2, // This exceeds root's max_compat=1 for C
				Deps:        nil,
			},
		},
		RootKey: ModuleKey{Name: "<root>", Version: ""},
	}

	// With strategy enumeration, we should find that C@1.0 works
	// while C@2.0 (the MVS choice) violates the constraint
	_, err := Run(graph, nil)

	// This should fail because even with strategy enumeration:
	// - The only selected version for C in selection group (C, compat=1) is C@1.0
	// - The only selected version for C in selection group (C, compat=2) is C@2.0
	// - The dep C@1.0 with max_compat=1 can only resolve to selection groups with compat <= 1
	// - But B@1.0's dep C@2.0 (with implicit max_compat=2) would resolve to C@2.0
	// - This causes a conflict: same module name, different compat levels, no MVO
	//
	// Actually wait, this is a compat level conflict error, not a max_compat error.
	// The strategy enumeration wouldn't help here.
	if err == nil {
		t.Fatal("Expected error due to compatibility level conflict")
	}
}

// TestStrategyEnumeration_CartesianProduct tests that we correctly compute
// the cartesian product when multiple DepSpecs have multiple choices.
func TestStrategyEnumeration_CartesianProduct(t *testing.T) {
	// Create a graph where two different DepSpecs each have 2 possible resolutions
	// This should generate 2*2 = 4 strategies
	//
	// root -> A@1.0 (max_compat=2) -> has 2 valid versions
	// root -> B@1.0 (max_compat=2) -> has 2 valid versions

	graph := &DepGraph{
		Modules: map[ModuleKey]*Module{
			{Name: "<root>", Version: ""}: {
				Key: ModuleKey{Name: "<root>", Version: ""},
				Deps: []DepSpec{
					{Name: "A", Version: "1.0", MaxCompatibilityLevel: 2},
					{Name: "B", Version: "1.0", MaxCompatibilityLevel: 2},
				},
			},
			{Name: "A", Version: "1.0"}: {
				Key:         ModuleKey{Name: "A", Version: "1.0"},
				CompatLevel: 1,
				Deps:        nil,
			},
			{Name: "A", Version: "2.0"}: {
				Key:         ModuleKey{Name: "A", Version: "2.0"},
				CompatLevel: 2,
				Deps:        nil,
			},
			{Name: "B", Version: "1.0"}: {
				Key:         ModuleKey{Name: "B", Version: "1.0"},
				CompatLevel: 1,
				Deps:        nil,
			},
			{Name: "B", Version: "2.0"}: {
				Key:         ModuleKey{Name: "B", Version: "2.0"},
				CompatLevel: 2,
				Deps:        nil,
			},
		},
		RootKey: ModuleKey{Name: "<root>", Version: ""},
	}

	result, err := Run(graph, nil)
	if err != nil {
		t.Fatalf("Selection.Run() error = %v", err)
	}

	// Should succeed with some combination
	// Verify both A and B are in the result
	hasA := false
	hasB := false
	for key := range result.ResolvedGraph {
		if key.Name == "A" {
			hasA = true
		}
		if key.Name == "B" {
			hasB = true
		}
	}
	if !hasA || !hasB {
		t.Errorf("Expected both A and B in resolved graph, hasA=%v hasB=%v", hasA, hasB)
	}
}

// TestComputePossibleResolutionResults tests the computePossibleResolutionResultsForOneDepSpec function.
func TestComputePossibleResolutionResults(t *testing.T) {
	// Create a graph with modules at different compatibility levels
	graph := &DepGraph{
		Modules: map[ModuleKey]*Module{
			{Name: "<root>", Version: ""}: {
				Key:  ModuleKey{Name: "<root>", Version: ""},
				Deps: []DepSpec{{Name: "A", Version: "1.0", MaxCompatibilityLevel: 3}},
			},
			{Name: "A", Version: "1.0"}: {
				Key:         ModuleKey{Name: "A", Version: "1.0"},
				CompatLevel: 1,
				Deps:        nil,
			},
			{Name: "A", Version: "2.0"}: {
				Key:         ModuleKey{Name: "A", Version: "2.0"},
				CompatLevel: 2,
				Deps:        nil,
			},
			{Name: "A", Version: "3.0"}: {
				Key:         ModuleKey{Name: "A", Version: "3.0"},
				CompatLevel: 3,
				Deps:        nil,
			},
			{Name: "A", Version: "4.0"}: {
				Key:         ModuleKey{Name: "A", Version: "4.0"},
				CompatLevel: 4, // This is outside max_compat=3
				Deps:        nil,
			},
		},
		RootKey: ModuleKey{Name: "<root>", Version: ""},
	}

	// Compute selection groups
	selectionGroups := make(map[ModuleKey]SelectionGroup)
	for key, module := range graph.Modules {
		selectionGroups[key] = SelectionGroup{
			ModuleName:  module.Key.Name,
			CompatLevel: module.CompatLevel,
		}
	}

	// Compute selected versions (highest per group)
	selectedVersions := make(map[SelectionGroup]string)
	for key, group := range selectionGroups {
		existing, ok := selectedVersions[group]
		if !ok || version.Compare(key.Version, existing) > 0 {
			selectedVersions[group] = key.Version
		}
	}

	// Test: DepSpec with max_compat=3 should have 3 possible resolutions (compat 1, 2, 3)
	depSpec := DepSpec{Name: "A", Version: "1.0", MaxCompatibilityLevel: 3}
	results := computePossibleResolutionResultsForOneDepSpec(depSpec, graph, selectedVersions)

	// Should have results for compat levels 1, 2, and 3 (not 4)
	if len(results) != 3 {
		t.Errorf("Expected 3 possible resolutions, got %d: %+v", len(results), results)
	}

	// Verify compat levels
	compatLevels := make(map[int]bool)
	for _, r := range results {
		compatLevels[r.CompatLevel] = true
	}
	if !compatLevels[1] || !compatLevels[2] || !compatLevels[3] {
		t.Errorf("Expected compat levels 1, 2, 3 but got: %+v", results)
	}
	if compatLevels[4] {
		t.Error("Should not include compat level 4 (exceeds max)")
	}
}

// TestEnumerateStrategies tests the enumerateStrategies function directly.
func TestEnumerateStrategies(t *testing.T) {
	// Create a simple graph where strategy enumeration would produce multiple strategies
	graph := &DepGraph{
		Modules: map[ModuleKey]*Module{
			{Name: "<root>", Version: ""}: {
				Key: ModuleKey{Name: "<root>", Version: ""},
				Deps: []DepSpec{{
					Name:                  "A",
					Version:               "1.0",
					MaxCompatibilityLevel: 2,
				}},
			},
			{Name: "A", Version: "1.0"}: {
				Key:         ModuleKey{Name: "A", Version: "1.0"},
				CompatLevel: 1,
				Deps:        nil,
			},
			{Name: "A", Version: "2.0"}: {
				Key:         ModuleKey{Name: "A", Version: "2.0"},
				CompatLevel: 2,
				Deps:        nil,
			},
		},
		RootKey: ModuleKey{Name: "<root>", Version: ""},
	}

	// Compute selection groups
	selectionGroups := make(map[ModuleKey]SelectionGroup)
	for key, module := range graph.Modules {
		selectionGroups[key] = SelectionGroup{
			ModuleName:  module.Key.Name,
			CompatLevel: module.CompatLevel,
		}
	}

	// Compute selected versions
	selectedVersions := make(map[SelectionGroup]string)
	for key, group := range selectionGroups {
		existing, ok := selectedVersions[group]
		if !ok || version.Compare(key.Version, existing) > 0 {
			selectedVersions[group] = key.Version
		}
	}

	strategies := enumerateStrategies(graph, selectionGroups, selectedVersions)

	// Should have 2 strategies: one using A@1.0, one using A@2.0
	if len(strategies) != 2 {
		t.Errorf("Expected 2 strategies, got %d", len(strategies))
	}

	// Verify strategies produce different results
	depSpec := DepSpec{Name: "A", Version: "1.0", MaxCompatibilityLevel: 2}
	versions := make(map[string]bool)
	for _, s := range strategies {
		v := s(depSpec)
		versions[v] = true
	}

	if len(versions) != 2 {
		t.Errorf("Expected 2 different versions from strategies, got %d", len(versions))
	}
}

// TestCartesianProduct tests the cartesianProduct helper function.
func TestCartesianProduct(t *testing.T) {
	keys := []depSpecKey{
		{Name: "A", Version: "1.0"},
		{Name: "B", Version: "1.0"},
	}
	allPossible := map[depSpecKey][]resolutionResult{
		{Name: "A", Version: "1.0"}: {
			{Version: "1.0", CompatLevel: 1},
			{Version: "2.0", CompatLevel: 2},
		},
		{Name: "B", Version: "1.0"}: {
			{Version: "1.0", CompatLevel: 1},
			{Version: "3.0", CompatLevel: 3},
		},
	}

	combos := cartesianProduct(keys, allPossible)

	// Should have 2 * 2 = 4 combinations
	if len(combos) != 4 {
		t.Errorf("Expected 4 combinations, got %d", len(combos))
	}

	// Verify all combinations are unique
	seen := make(map[string]bool)
	for _, combo := range combos {
		key := combo[depSpecKey{Name: "A", Version: "1.0"}] + ":" + combo[depSpecKey{Name: "B", Version: "1.0"}]
		if seen[key] {
			t.Errorf("Duplicate combination: %s", key)
		}
		seen[key] = true
	}
}

// TestCartesianProduct_Empty tests cartesian product with empty input.
func TestCartesianProduct_Empty(t *testing.T) {
	combos := cartesianProduct(nil, nil)

	// Should return one empty combination
	if len(combos) != 1 {
		t.Errorf("Expected 1 empty combination, got %d", len(combos))
	}
	if len(combos[0]) != 0 {
		t.Error("Expected empty combination")
	}
}

// TestStrategyEnumeration_NoMaxCompatLevel tests that without max_compatibility_level,
// no strategy enumeration happens (single default strategy).
func TestStrategyEnumeration_NoMaxCompatLevel(t *testing.T) {
	graph := &DepGraph{
		Modules: map[ModuleKey]*Module{
			{Name: "<root>", Version: ""}: {
				Key: ModuleKey{Name: "<root>", Version: ""},
				// No max_compatibility_level set (-1 is default, meaning no constraint)
				Deps: []DepSpec{{Name: "A", Version: "1.0", MaxCompatibilityLevel: -1}},
			},
			{Name: "A", Version: "1.0"}: {
				Key:         ModuleKey{Name: "A", Version: "1.0"},
				CompatLevel: 1,
				Deps:        nil,
			},
		},
		RootKey: ModuleKey{Name: "<root>", Version: ""},
	}

	selectionGroups := make(map[ModuleKey]SelectionGroup)
	for key, module := range graph.Modules {
		selectionGroups[key] = SelectionGroup{
			ModuleName:  module.Key.Name,
			CompatLevel: module.CompatLevel,
		}
	}

	selectedVersions := make(map[SelectionGroup]string)
	for key, group := range selectionGroups {
		existing, ok := selectedVersions[group]
		if !ok || version.Compare(key.Version, existing) > 0 {
			selectedVersions[group] = key.Version
		}
	}

	strategies := enumerateStrategies(graph, selectionGroups, selectedVersions)

	// Should have exactly 1 strategy (the default)
	if len(strategies) != 1 {
		t.Errorf("Expected 1 strategy when no max_compatibility_level, got %d", len(strategies))
	}
}

// TestMVSTieBreakPrefersSmallestSpelling verifies the canonical tie-break when
// distinct spellings compare as the same Bazel version.
func TestMVSTieBreakPrefersSmallestSpelling(t *testing.T) {
	for _, tc := range []struct{ name, first, second, want string }{
		{"build metadata", "1.0.0+build", "1.0.0", "1.0.0"},
		{"leading zeros", "1.0.0", "01.0.0", "01.0.0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := ModuleKey{Name: "root", Version: "1.0"}
			first := ModuleKey{Name: "B", Version: tc.first}
			second := ModuleKey{Name: "B", Version: tc.second}
			graph := &DepGraph{RootKey: root, Modules: map[ModuleKey]*Module{
				root: {Key: root, Deps: []DepSpec{
					{Name: "B", Version: tc.first, MaxCompatibilityLevel: -1},
					{Name: "B", Version: tc.second, MaxCompatibilityLevel: -1},
				}},
				first: {Key: first}, second: {Key: second},
			}}
			for range 20 {
				result, err := Run(graph, nil)
				if err != nil {
					t.Fatal(err)
				}
				var selected []string
				for key := range result.ResolvedGraph {
					if key.Name == "B" {
						selected = append(selected, key.Version)
					}
				}
				if len(selected) != 1 || selected[0] != tc.want {
					t.Fatalf("selected %v, want exactly [%s]", selected, tc.want)
				}
			}
		})
	}
}

// TestMultipleVersionOverrideCeiling covers computeSelectionGroup's ceiling
// lookup (selection.go:258-264), which had zero coverage: there was no
// MultipleVersionOverride test anywhere in the package.
//
// The `>=` is Bazel's ceiling semantics (Selection.java:174-179): snap a module
// to the LOWEST allowed version that is not below its own. A `>` mutation
// silently snaps an exactly-allowed version up to the next one, which would
// resolve a dependency the user explicitly allowed to a different version.
func TestMultipleVersionOverrideCeiling(t *testing.T) {
	rootKey := ModuleKey{Name: "root", Version: "1.0"}

	// B exists at 1.0 (exactly allowed), 1.2 (between allowed) and 2.0 (allowed).
	b10 := ModuleKey{Name: "B", Version: "1.0"}
	b12 := ModuleKey{Name: "B", Version: "1.2"}
	b20 := ModuleKey{Name: "B", Version: "2.0"}

	graph := &DepGraph{
		RootKey: rootKey,
		Modules: map[ModuleKey]*Module{
			rootKey: {Key: rootKey, Deps: []DepSpec{
				{Name: "B", Version: "1.0", MaxCompatibilityLevel: -1},
				{Name: "B", Version: "1.2", MaxCompatibilityLevel: -1},
				{Name: "B", Version: "2.0", MaxCompatibilityLevel: -1},
			}},
			b10: {Key: b10, CompatLevel: 1},
			b12: {Key: b12, CompatLevel: 1},
			b20: {Key: b20, CompatLevel: 1},
		},
	}
	overrides := map[string]Override{
		"B": &MultipleVersionOverride{Versions: []string{"1.0", "2.0"}},
	}

	result, err := Run(graph, overrides)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	selected := map[string]bool{}
	for k := range result.ResolvedGraph {
		if k.Name == "B" {
			selected[k.Version] = true
		}
	}

	// B@1.0 is exactly allowed, so it must stay at 1.0 -- NOT snap up to 2.0.
	// This is the assertion the `>` mutant fails.
	if !selected["1.0"] {
		t.Errorf("B@1.0 is exactly an allowed version and must be selected as-is; got %v", selected)
	}
	// B@2.0 is allowed and is the ceiling for itself.
	if !selected["2.0"] {
		t.Errorf("B@2.0 is an allowed version and must be selected; got %v", selected)
	}
	// B@1.2 is not allowed, so it snaps up to the ceiling 2.0 and does not
	// appear under its own version.
	if selected["1.2"] {
		t.Errorf("B@1.2 is not an allowed version and must not survive selection; got %v", selected)
	}
}

// TestMultipleVersionOverrideRejectsUnknownVersion covers
// computeAllowedVersionSets' error path (selection.go:196-207), also previously
// uncovered -- which is what let `return result` -> `return nil` survive.
func TestMultipleVersionOverrideRejectsUnknownVersion(t *testing.T) {
	rootKey := ModuleKey{Name: "root", Version: "1.0"}
	b10 := ModuleKey{Name: "B", Version: "1.0"}

	graph := &DepGraph{
		RootKey: rootKey,
		Modules: map[ModuleKey]*Module{
			rootKey: {Key: rootKey, Deps: []DepSpec{{Name: "B", Version: "1.0", MaxCompatibilityLevel: -1}}},
			b10:     {Key: b10, CompatLevel: 1},
		},
	}
	overrides := map[string]Override{
		// 9.9 is not in the graph at all.
		"B": &MultipleVersionOverride{Versions: []string{"1.0", "9.9"}},
	}

	_, err := Run(graph, overrides)
	if err == nil {
		t.Fatal("Run succeeded with an allowed version absent from the graph; want an error")
	}

	var se *SelectionError
	if !errors.As(err, &se) {
		t.Fatalf("error is %T, want *SelectionError", err)
	}
	if !strings.Contains(err.Error(), "9.9") {
		t.Errorf("error should name the missing version 9.9, got: %v", err)
	}
}

// TestMultipleVersionOverrideRejectsVersionAboveCeiling covers visit's
// TargetAllowedVersion == "" error (selection.go:430-444): a module whose
// version is above every allowed version has no ceiling to snap to.
func TestMultipleVersionOverrideRejectsVersionAboveCeiling(t *testing.T) {
	rootKey := ModuleKey{Name: "root", Version: "1.0"}
	b10 := ModuleKey{Name: "B", Version: "1.0"}
	b30 := ModuleKey{Name: "B", Version: "3.0"}

	graph := &DepGraph{
		RootKey: rootKey,
		Modules: map[ModuleKey]*Module{
			rootKey: {Key: rootKey, Deps: []DepSpec{
				{Name: "B", Version: "1.0", MaxCompatibilityLevel: -1},
				{Name: "B", Version: "3.0", MaxCompatibilityLevel: -1},
			}},
			b10: {Key: b10, CompatLevel: 1},
			b30: {Key: b30, CompatLevel: 1},
		},
	}
	overrides := map[string]Override{
		// B@3.0 is in the graph but above every allowed version.
		"B": &MultipleVersionOverride{Versions: []string{"1.0"}},
	}

	_, err := Run(graph, overrides)
	if err == nil {
		t.Fatal("Run succeeded with a module above every allowed version; want an error")
	}
	if !strings.Contains(err.Error(), "B") {
		t.Errorf("error should name module B, got: %v", err)
	}
}

// TestCompatibilityLevelConflictErrorIdentity tightens what
// TestCompatibilityLevelSelection only checked loosely.
//
// That test asserts err == nil, so a mutation inverting the conflict check
// (selection.go:453 `!=` -> `==`) still "passed": producing a *different* error,
// or none, was indistinguishable. This asserts the error's type, code and
// content.
func TestCompatibilityLevelConflictErrorIdentity(t *testing.T) {
	rootKey := ModuleKey{Name: "root", Version: "1.0"}
	// Two modules named B selected simultaneously at different compat levels.
	aKey := ModuleKey{Name: "A", Version: "1.0"}
	cKey := ModuleKey{Name: "C", Version: "1.0"}
	b1 := ModuleKey{Name: "B", Version: "1.0"}
	b2 := ModuleKey{Name: "B", Version: "2.0"}

	graph := &DepGraph{
		RootKey: rootKey,
		Modules: map[ModuleKey]*Module{
			rootKey: {Key: rootKey, Deps: []DepSpec{
				{Name: "A", Version: "1.0", MaxCompatibilityLevel: -1},
				{Name: "C", Version: "1.0", MaxCompatibilityLevel: -1},
			}},
			aKey: {Key: aKey, Deps: []DepSpec{{Name: "B", Version: "1.0", MaxCompatibilityLevel: -1}}},
			cKey: {Key: cKey, Deps: []DepSpec{{Name: "B", Version: "2.0", MaxCompatibilityLevel: -1}}},
			b1:   {Key: b1, CompatLevel: 1},
			b2:   {Key: b2, CompatLevel: 2},
		},
	}

	_, err := Run(graph, nil)
	if err == nil {
		t.Fatal("Run succeeded with B selected at two different compatibility levels; want a conflict error")
	}

	var se *SelectionError
	if !errors.As(err, &se) {
		t.Fatalf("error is %T, want *SelectionError", err)
	}
	if se.Code != "VERSION_RESOLUTION_ERROR" {
		t.Errorf("Code = %q, want %q", se.Code, "VERSION_RESOLUTION_ERROR")
	}
	// The message must identify both sides, or the user cannot tell which two
	// dependencies disagree.
	for _, want := range []string{"B", "compatibility level"} {
		if !strings.Contains(se.Message, want) {
			t.Errorf("message %q should mention %q", se.Message, want)
		}
	}
}

// TestResolutionIsDeterministic covers the two sort calls that keep strategy
// enumeration stable (selection.go:563 and :662, plus the :663 comparator).
//
// Those sorts exist because the inputs come from Go maps. Remove them and
// resolution order follows map iteration order, i.e. the SAME dependency graph
// can resolve differently between runs -- a flaky-build bug that no
// single-execution test can see.
func TestResolutionIsDeterministic(t *testing.T) {
	rootKey := ModuleKey{Name: "root", Version: "1.0"}
	// Several modules with max_compatibility_level constraints, so the strategy
	// enumeration has more than one dimension to order.
	mods := map[ModuleKey]*Module{
		rootKey: {Key: rootKey, Deps: []DepSpec{
			{Name: "A", Version: "1.0", MaxCompatibilityLevel: 2},
			{Name: "B", Version: "1.0", MaxCompatibilityLevel: 2},
			{Name: "C", Version: "1.0", MaxCompatibilityLevel: 2},
		}},
	}
	for _, name := range []string{"A", "B", "C"} {
		for _, v := range []string{"1.0", "2.0"} {
			k := ModuleKey{Name: name, Version: v}
			lvl := 1
			if v == "2.0" {
				lvl = 2
			}
			mods[k] = &Module{Key: k, CompatLevel: lvl}
		}
	}
	graph := &DepGraph{RootKey: rootKey, Modules: mods}

	fingerprint := func() string {
		res, err := Run(graph, nil)
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		ks := keys(res.ResolvedGraph)
		strs := make([]string, 0, len(ks))
		for _, k := range ks {
			strs = append(strs, k.String())
		}
		sort.Strings(strs)

		order := make([]string, 0, len(res.BFSOrder))
		for _, k := range res.BFSOrder {
			order = append(order, k.String())
		}
		// BFSOrder is deliberately NOT sorted: its order is the thing under test.
		return strings.Join(strs, ",") + "|" + strings.Join(order, ">")
	}

	want := fingerprint()
	for i := range 50 {
		if got := fingerprint(); got != want {
			t.Fatalf("resolution differed on run %d:\n got %s\nwant %s", i+2, got, want)
		}
	}
}

// TestMaxCompatibilityLevelZero covers the four sites that branch on
// MaxCompatibilityLevel being non-negative (selection.go:329, :359, :514, :600).
//
// No test anywhere used the value 0 -- only -1, 1, 2 and 3 -- so `>= 0` -> `> 0`
// and `< 0` -> `<= 0` all survived. Zero is meaningful: it constrains a dep to
// compatibility level 0.
func TestMaxCompatibilityLevelZero(t *testing.T) {
	rootKey := ModuleKey{Name: "root", Version: "1.0"}
	bKey := ModuleKey{Name: "B", Version: "1.0"}

	t.Run("satisfied when the module is at compat level 0", func(t *testing.T) {
		graph := &DepGraph{
			RootKey: rootKey,
			Modules: map[ModuleKey]*Module{
				rootKey: {Key: rootKey, Deps: []DepSpec{{Name: "B", Version: "1.0", MaxCompatibilityLevel: 0}}},
				bKey:    {Key: bKey, CompatLevel: 0},
			},
		}
		if _, err := Run(graph, nil); err != nil {
			t.Errorf("max_compatibility_level 0 with a level-0 module should resolve, got: %v", err)
		}
	})

	t.Run("violated when the module exceeds level 0", func(t *testing.T) {
		graph := &DepGraph{
			RootKey: rootKey,
			Modules: map[ModuleKey]*Module{
				rootKey: {Key: rootKey, Deps: []DepSpec{{Name: "B", Version: "1.0", MaxCompatibilityLevel: 0}}},
				bKey:    {Key: bKey, CompatLevel: 1},
			},
		}
		_, err := Run(graph, nil)
		if err == nil {
			t.Fatal("max_compatibility_level 0 with a level-1 module must fail; a `> 0` guard would skip the check entirely")
		}
		if !strings.Contains(err.Error(), "max_compatibility_level") {
			t.Errorf("error should explain the constraint, got: %v", err)
		}
	})

	t.Run("applies to nodep deps too", func(t *testing.T) {
		graph := &DepGraph{
			RootKey: rootKey,
			Modules: map[ModuleKey]*Module{
				rootKey: {Key: rootKey, NodepDeps: []DepSpec{{Name: "B", Version: "1.0", MaxCompatibilityLevel: 0}}},
				bKey:    {Key: bKey, CompatLevel: 1},
			},
		}
		if _, err := Run(graph, nil); err == nil {
			t.Error("max_compatibility_level 0 must be validated for nodep deps as well")
		}
	})
}

// TestModuleKeyStringRendersEmptyVersion covers types.go:22, where an empty
// version -- the non-registry-override sentinel -- renders as "name@_".
func TestModuleKeyStringRendersEmptyVersion(t *testing.T) {
	if got, want := (ModuleKey{Name: "a", Version: ""}).String(), "a@_"; got != want {
		t.Errorf("empty version renders %q, want %q", got, want)
	}
	if got, want := (ModuleKey{Name: "a", Version: "1.0"}).String(), "a@1.0"; got != want {
		t.Errorf("normal version renders %q, want %q", got, want)
	}
}

// TestCartesianProductExactContentAndOrder pins cartesianProduct's output
// precisely. The existing TestCartesianProduct only counts combinations and
// checks uniqueness, so it cannot tell a correct product from one that pairs the
// wrong values together or emits them in an unstable order -- and enumeration
// order decides which strategy Run tries first.
func TestCartesianProductExactContentAndOrder(t *testing.T) {
	a := depSpecKey{Name: "A", Version: "1.0"}
	b := depSpecKey{Name: "B", Version: "1.0"}
	keys := []depSpecKey{a, b}
	allPossible := map[depSpecKey][]resolutionResult{
		a: {{Version: "1.0", CompatLevel: 1}, {Version: "2.0", CompatLevel: 2}},
		b: {{Version: "1.0", CompatLevel: 1}, {Version: "3.0", CompatLevel: 3}},
	}

	got := cartesianProduct(keys, allPossible)

	// Keys are iterated in slice order and possibilities in slice order, so the
	// product is fully determined.
	want := []map[depSpecKey]string{
		{a: "1.0", b: "1.0"},
		{a: "1.0", b: "3.0"},
		{a: "2.0", b: "1.0"},
		{a: "2.0", b: "3.0"},
	}

	if len(got) != len(want) {
		t.Fatalf("got %d combinations, want %d: %v", len(got), len(want), got)
	}
	for i, w := range want {
		if len(got[i]) != len(w) {
			t.Errorf("combination %d has %d entries, want %d: %v", i, len(got[i]), len(w), got[i])
			continue
		}
		for k, v := range w {
			if got[i][k] != v {
				t.Errorf("combination %d: %v -> %q, want %q (full: %v)", i, k, got[i][k], v, got[i])
			}
		}
	}
}

// TestResolutionResultsSortedByCompatLevel covers the sort at selection.go:563.
// Its removal leaves the results in Go map iteration order, making the order in
// which Run tries resolutions nondeterministic.
func TestResolutionResultsSortedByCompatLevel(t *testing.T) {
	rootKey := ModuleKey{Name: "root", Version: "1.0"}
	b1 := ModuleKey{Name: "B", Version: "1.0"}
	b2 := ModuleKey{Name: "B", Version: "2.0"}
	b3 := ModuleKey{Name: "B", Version: "3.0"}

	graph := &DepGraph{
		RootKey: rootKey,
		Modules: map[ModuleKey]*Module{
			rootKey: {Key: rootKey, Deps: []DepSpec{
				{Name: "B", Version: "1.0", MaxCompatibilityLevel: 3},
				{Name: "B", Version: "2.0", MaxCompatibilityLevel: 3},
				{Name: "B", Version: "3.0", MaxCompatibilityLevel: 3},
			}},
			b1: {Key: b1, CompatLevel: 1},
			b2: {Key: b2, CompatLevel: 2},
			b3: {Key: b3, CompatLevel: 3},
		},
	}

	_, selectedVersions := groupsAndVersionsForTest(t, graph)
	dep := DepSpec{Name: "B", Version: "1.0", MaxCompatibilityLevel: 3}
	results := computePossibleResolutionResultsForOneDepSpec(dep, graph, selectedVersions)

	if len(results) < 2 {
		t.Fatalf("expected several possible resolutions, got %v", results)
	}
	// Ascending compat level: the comment at the sort says lower levels are
	// preferred so simpler resolutions are tried first.
	for i := 1; i < len(results); i++ {
		if results[i-1].CompatLevel > results[i].CompatLevel {
			t.Errorf("results not sorted by ascending compat level: %v", results)
			break
		}
	}
}

// TestUnconstrainedDepHasPossibleResolutions covers selection.go:519's
// `minCompatLevel > maxCompatLevel`. With MaxCompatibilityLevel -1, max equals
// min, so a `>=` mutation returns nil for the *common* unconstrained case.
func TestUnconstrainedDepHasPossibleResolutions(t *testing.T) {
	rootKey := ModuleKey{Name: "root", Version: "1.0"}
	b1 := ModuleKey{Name: "B", Version: "1.0"}

	graph := &DepGraph{
		RootKey: rootKey,
		Modules: map[ModuleKey]*Module{
			rootKey: {Key: rootKey, Deps: []DepSpec{{Name: "B", Version: "1.0", MaxCompatibilityLevel: -1}}},
			b1:      {Key: b1, CompatLevel: 1},
		},
	}

	_, selectedVersions := groupsAndVersionsForTest(t, graph)
	dep := DepSpec{Name: "B", Version: "1.0", MaxCompatibilityLevel: -1}
	results := computePossibleResolutionResultsForOneDepSpec(dep, graph, selectedVersions)

	if len(results) == 0 {
		t.Fatal("an unconstrained dep (min == max compat level) must still have a possible resolution")
	}
	if results[0].Version != "1.0" {
		t.Errorf("resolution = %q, want 1.0", results[0].Version)
	}
}

// TestPossibleResolutionsIgnoreOtherModules covers the `continue` at
// selection.go:531. Without it, another module's selection groups leak into this
// dep's candidate list.
func TestPossibleResolutionsIgnoreOtherModules(t *testing.T) {
	rootKey := ModuleKey{Name: "root", Version: "1.0"}
	b1 := ModuleKey{Name: "B", Version: "1.0"}
	c1 := ModuleKey{Name: "C", Version: "9.0"}

	graph := &DepGraph{
		RootKey: rootKey,
		Modules: map[ModuleKey]*Module{
			rootKey: {Key: rootKey, Deps: []DepSpec{
				{Name: "B", Version: "1.0", MaxCompatibilityLevel: -1},
				{Name: "C", Version: "9.0", MaxCompatibilityLevel: -1},
			}},
			// Same compat level, so only the name guard separates them.
			b1: {Key: b1, CompatLevel: 1},
			c1: {Key: c1, CompatLevel: 1},
		},
	}

	_, selectedVersions := groupsAndVersionsForTest(t, graph)
	dep := DepSpec{Name: "B", Version: "1.0", MaxCompatibilityLevel: -1}
	results := computePossibleResolutionResultsForOneDepSpec(dep, graph, selectedVersions)

	for _, r := range results {
		if r.Version == "9.0" {
			t.Errorf("C@9.0 leaked into B's possible resolutions: %v", results)
		}
	}
}

// TestCollectDepKeepsMostPermissiveMaxCompat covers selection.go:604, where the
// same (name, version) dep declared by two modules with different
// max_compatibility_level must resolve to the most permissive. The comment at
// :591-597 promises this and commit 241f344 fixed it, but nothing tested it.
func TestCollectDepKeepsMostPermissiveMaxCompat(t *testing.T) {
	rootKey := ModuleKey{Name: "root", Version: "1.0"}
	aKey := ModuleKey{Name: "A", Version: "1.0"}
	cKey := ModuleKey{Name: "C", Version: "1.0"}
	b1 := ModuleKey{Name: "B", Version: "1.0"}
	b3 := ModuleKey{Name: "B", Version: "3.0"}

	graph := &DepGraph{
		RootKey: rootKey,
		Modules: map[ModuleKey]*Module{
			rootKey: {Key: rootKey, Deps: []DepSpec{
				{Name: "A", Version: "1.0", MaxCompatibilityLevel: -1},
				{Name: "C", Version: "1.0", MaxCompatibilityLevel: -1},
			}},
			// Same dep (B@1.0), different max_compatibility_level.
			aKey: {Key: aKey, Deps: []DepSpec{{Name: "B", Version: "1.0", MaxCompatibilityLevel: 1}}},
			cKey: {Key: cKey, Deps: []DepSpec{{Name: "B", Version: "1.0", MaxCompatibilityLevel: 3}}},
			b1:   {Key: b1, CompatLevel: 1},
			b3:   {Key: b3, CompatLevel: 3},
		},
	}

	_, selectedVersions := groupsAndVersionsForTest(t, graph)
	all := computeAllPossibleResolutions(graph, selectedVersions)

	key := depSpecKey{Name: "B", Version: "1.0"}
	results, ok := all[key]
	if !ok {
		t.Fatalf("B@1.0 has two differing max_compatibility_level declarations, so it should have multiple possible resolutions; got %v", all)
	}

	// The permissive declaration (max 3) must be the one that survives, which
	// means compat level 3 is reachable.
	sawLevel3 := false
	for _, r := range results {
		if r.CompatLevel == 3 {
			sawLevel3 = true
		}
	}
	if !sawLevel3 {
		t.Errorf("the most permissive max_compatibility_level (3) must win, so compat level 3 should be a candidate; got %v", results)
	}
}

// groupsAndVersionsForTest reproduces the selection-group and selected-version
// computation Run performs before strategy enumeration, so the unexported
// helpers can be tested directly.
func groupsAndVersionsForTest(t *testing.T, graph *DepGraph) (map[ModuleKey]SelectionGroup, map[SelectionGroup]string) {
	t.Helper()

	allowed, err := computeAllowedVersionSets(nil, graph)
	if err != nil {
		t.Fatalf("computeAllowedVersionSets: %v", err)
	}

	groups := make(map[ModuleKey]SelectionGroup, len(graph.Modules))
	for key, module := range graph.Modules {
		groups[key] = computeSelectionGroup(module, allowed)
	}

	selected := make(map[SelectionGroup]string)
	for key, group := range groups {
		existing, ok := selected[group]
		if !ok || version.Compare(key.Version, existing) > 0 {
			selected[group] = key.Version
		}
	}
	return groups, selected
}

// TestMaxCompatBelowModuleLevelHasNoResolution covers selection.go:558's
// `depSpec.MaxCompatibilityLevel >= 0`.
//
// The `> 0` mutation only diverges when max_compatibility_level is exactly 0 AND
// the target module sits above level 0: the real code then computes
// maxCompatLevel 0 against minCompatLevel 1 and correctly reports no possible
// resolution, while the mutant leaves max at min and happily proceeds.
func TestMaxCompatBelowModuleLevelHasNoResolution(t *testing.T) {
	rootKey := ModuleKey{Name: "root", Version: "1.0"}
	b1 := ModuleKey{Name: "B", Version: "1.0"}

	graph := &DepGraph{
		RootKey: rootKey,
		Modules: map[ModuleKey]*Module{
			rootKey: {Key: rootKey, Deps: []DepSpec{{Name: "B", Version: "1.0", MaxCompatibilityLevel: 0}}},
			b1:      {Key: b1, CompatLevel: 1},
		},
	}

	_, selectedVersions := groupsAndVersionsForTest(t, graph)
	dep := DepSpec{Name: "B", Version: "1.0", MaxCompatibilityLevel: 0}
	results := computePossibleResolutionResultsForOneDepSpec(dep, graph, selectedVersions)

	if len(results) != 0 {
		t.Errorf("max_compatibility_level 0 against a level-1 module admits no resolution, got %v", results)
	}
}

// TestPossibleResolutionsSkipOtherModulesThatWouldWin covers the `continue` at
// selection.go:575 (the `group.ModuleName != depSpec.Name` guard).
//
// An earlier attempt at this test used a foreign module with a HIGHER version,
// which the "keep the lower version" rule at :592 filtered out anyway, so
// removing the guard made no observable difference. The foreign version has to
// land strictly between the dep's declared version and the legitimately selected
// one: high enough to survive the MVS floor check at :584, low enough to win the
// keep-the-lower comparison.
func TestPossibleResolutionsSkipOtherModulesThatWouldWin(t *testing.T) {
	rootKey := ModuleKey{Name: "root", Version: "1.0"}
	b1 := ModuleKey{Name: "B", Version: "1.0"}
	b2 := ModuleKey{Name: "B", Version: "2.0"}
	// C@1.5 sits between B's dep version (1.0) and B's selected version (2.0).
	c15 := ModuleKey{Name: "C", Version: "1.5"}

	graph := &DepGraph{
		RootKey: rootKey,
		Modules: map[ModuleKey]*Module{
			rootKey: {Key: rootKey, Deps: []DepSpec{
				{Name: "B", Version: "1.0", MaxCompatibilityLevel: -1},
				{Name: "B", Version: "2.0", MaxCompatibilityLevel: -1},
				{Name: "C", Version: "1.5", MaxCompatibilityLevel: -1},
			}},
			// Same compat level, so only the module-name guard separates them.
			b1:  {Key: b1, CompatLevel: 1},
			b2:  {Key: b2, CompatLevel: 1},
			c15: {Key: c15, CompatLevel: 1},
		},
	}

	_, selectedVersions := groupsAndVersionsForTest(t, graph)
	dep := DepSpec{Name: "B", Version: "1.0", MaxCompatibilityLevel: -1}
	results := computePossibleResolutionResultsForOneDepSpec(dep, graph, selectedVersions)

	for _, r := range results {
		if r.Version == "1.5" {
			t.Fatalf("C@1.5 leaked into B's possible resolutions: %v", results)
		}
	}
	if len(results) != 1 || results[0].Version != "2.0" {
		t.Errorf("B should resolve only to its own selected version 2.0, got %v", results)
	}
}

// TestMaxCompatEqualToModuleLevelIsAllowedForNodepDeps covers selection.go:392.
//
// TestMaxCompatibilityLevelZero only exercises the violating direction (module
// level above the max), which both `>` and `>=` reject. The equality case --
// compatibility level exactly equal to max_compatibility_level -- must be
// ALLOWED, and that is what distinguishes them.
func TestMaxCompatEqualToModuleLevelIsAllowedForNodepDeps(t *testing.T) {
	rootKey := ModuleKey{Name: "root", Version: "1.0"}
	bKey := ModuleKey{Name: "B", Version: "1.0"}

	for _, tc := range []struct {
		name  string
		nodep bool
	}{{"regular dep", false}, {"nodep dep", true}} {
		t.Run(tc.name, func(t *testing.T) {
			root := &Module{Key: rootKey}
			spec := DepSpec{Name: "B", Version: "1.0", MaxCompatibilityLevel: 2}
			if tc.nodep {
				root.NodepDeps = []DepSpec{spec}
			} else {
				root.Deps = []DepSpec{spec}
			}

			graph := &DepGraph{
				RootKey: rootKey,
				Modules: map[ModuleKey]*Module{
					rootKey: root,
					// Exactly at the limit: allowed.
					bKey: {Key: bKey, CompatLevel: 2},
				},
			}
			if _, err := Run(graph, nil); err != nil {
				t.Errorf("compatibility level equal to max_compatibility_level must be allowed, got: %v", err)
			}
		})
	}
}
