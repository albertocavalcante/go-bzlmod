// Package selection implements Bazel's module version selection algorithm.
// Tests are based on Bazel's Selection.java behavior.
//
// Reference: https://github.com/bazelbuild/bazel/blob/master/src/main/java/com/google/devtools/build/lib/bazel/bzlmod/Selection.java
package selection

import (
	"testing"
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

// TestSingleVersionOverride tests that single_version_override forces a specific version.
func TestSingleVersionOverride(t *testing.T) {
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

func keys(m map[ModuleKey]*Module) []ModuleKey {
	result := make([]ModuleKey, 0, len(m))
	for k := range m {
		result = append(result, k)
	}
	return result
}
