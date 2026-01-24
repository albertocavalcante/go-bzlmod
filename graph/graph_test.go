package graph

import (
	"encoding/json"
	"strings"
	"testing"
)

// Helper to create a test graph:
//
//	root@1.0.0
//	├── a@1.0.0
//	│   └── c@2.0.0
//	└── b@1.0.0
//	    └── c@2.0.0 (shared)
func createTestGraph() *Graph {
	root := ModuleKey{Name: "root", Version: "1.0.0"}
	a := ModuleKey{Name: "a", Version: "1.0.0"}
	b := ModuleKey{Name: "b", Version: "1.0.0"}
	c := ModuleKey{Name: "c", Version: "2.0.0"}

	return Build(root, []SimpleModule{
		{Name: "root", Version: "1.0.0", Dependencies: []ModuleKey{a, b}},
		{Name: "a", Version: "1.0.0", Dependencies: []ModuleKey{c}},
		{Name: "b", Version: "1.0.0", Dependencies: []ModuleKey{c}},
		{Name: "c", Version: "2.0.0", Dependencies: nil},
	})
}

func TestModuleKey_String(t *testing.T) {
	tests := []struct {
		key  ModuleKey
		want string
	}{
		{ModuleKey{Name: "foo", Version: "1.0.0"}, "foo@1.0.0"},
		{ModuleKey{Name: "bar", Version: ""}, "bar@_"},
		{ModuleKey{Name: "baz", Version: "2.0.0-rc1"}, "baz@2.0.0-rc1"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.key.String(); got != tt.want {
				t.Errorf("ModuleKey.String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuild(t *testing.T) {
	g := createTestGraph()

	// Check root
	if g.Root.Name != "root" || g.Root.Version != "1.0.0" {
		t.Errorf("unexpected root: %v", g.Root)
	}

	// Check module count
	if len(g.Modules) != 4 {
		t.Errorf("expected 4 modules, got %d", len(g.Modules))
	}

	// Check root's dependencies
	rootNode := g.Modules[g.Root]
	if rootNode == nil {
		t.Fatal("root node not found")
	}
	if len(rootNode.Dependencies) != 2 {
		t.Errorf("root should have 2 dependencies, got %d", len(rootNode.Dependencies))
	}

	// Check c has two dependents (a and b)
	cKey := ModuleKey{Name: "c", Version: "2.0.0"}
	cNode := g.Modules[cKey]
	if cNode == nil {
		t.Fatal("c node not found")
	}
	if len(cNode.Dependents) != 2 {
		t.Errorf("c should have 2 dependents, got %d", len(cNode.Dependents))
	}
}

func TestGraph_Get(t *testing.T) {
	g := createTestGraph()

	// Existing module
	key := ModuleKey{Name: "a", Version: "1.0.0"}
	if node := g.Get(key); node == nil {
		t.Error("Get() returned nil for existing module")
	}

	// Non-existing module
	nonExistent := ModuleKey{Name: "x", Version: "1.0.0"}
	if node := g.Get(nonExistent); node != nil {
		t.Error("Get() should return nil for non-existing module")
	}
}

func TestGraph_GetByName(t *testing.T) {
	g := createTestGraph()

	// Existing module
	if node := g.GetByName("a"); node == nil {
		t.Error("GetByName() returned nil for existing module")
	}

	// Non-existing module
	if node := g.GetByName("x"); node != nil {
		t.Error("GetByName() should return nil for non-existing module")
	}
}

func TestGraph_Contains(t *testing.T) {
	g := createTestGraph()

	key := ModuleKey{Name: "a", Version: "1.0.0"}
	if !g.Contains(key) {
		t.Error("Contains() should return true for existing module")
	}

	nonExistent := ModuleKey{Name: "x", Version: "1.0.0"}
	if g.Contains(nonExistent) {
		t.Error("Contains() should return false for non-existing module")
	}
}

func TestGraph_ContainsName(t *testing.T) {
	g := createTestGraph()

	if !g.ContainsName("a") {
		t.Error("ContainsName() should return true for existing module")
	}

	if g.ContainsName("x") {
		t.Error("ContainsName() should return false for non-existing module")
	}
}

func TestGraph_DirectDeps(t *testing.T) {
	g := createTestGraph()

	// Root has 2 direct deps
	deps := g.DirectDeps(g.Root)
	if len(deps) != 2 {
		t.Errorf("root should have 2 direct deps, got %d", len(deps))
	}

	// c has no deps
	cKey := ModuleKey{Name: "c", Version: "2.0.0"}
	deps = g.DirectDeps(cKey)
	if len(deps) != 0 {
		t.Errorf("c should have 0 direct deps, got %d", len(deps))
	}

	// Non-existing module
	nonExistent := ModuleKey{Name: "x", Version: "1.0.0"}
	deps = g.DirectDeps(nonExistent)
	if deps != nil {
		t.Error("DirectDeps() should return nil for non-existing module")
	}
}

func TestGraph_DirectDependents(t *testing.T) {
	g := createTestGraph()

	// c has 2 dependents
	cKey := ModuleKey{Name: "c", Version: "2.0.0"}
	deps := g.DirectDependents(cKey)
	if len(deps) != 2 {
		t.Errorf("c should have 2 dependents, got %d", len(deps))
	}

	// root has no dependents
	deps = g.DirectDependents(g.Root)
	if len(deps) != 0 {
		t.Errorf("root should have 0 dependents, got %d", len(deps))
	}
}

func TestGraph_TransitiveDeps(t *testing.T) {
	g := createTestGraph()

	// Root's transitive deps should include a, b, c
	deps := g.TransitiveDeps(g.Root)
	if len(deps) != 3 {
		t.Errorf("root should have 3 transitive deps, got %d", len(deps))
	}

	// Check all expected deps are present
	found := make(map[string]bool)
	for _, dep := range deps {
		found[dep.Name] = true
	}
	for _, name := range []string{"a", "b", "c"} {
		if !found[name] {
			t.Errorf("missing transitive dep: %s", name)
		}
	}
}

func TestGraph_TransitiveDependents(t *testing.T) {
	g := createTestGraph()

	// c's transitive dependents should include a, b, root
	cKey := ModuleKey{Name: "c", Version: "2.0.0"}
	deps := g.TransitiveDependents(cKey)
	if len(deps) != 3 {
		t.Errorf("c should have 3 transitive dependents, got %d", len(deps))
	}

	// Check all expected dependents are present
	found := make(map[string]bool)
	for _, dep := range deps {
		found[dep.Name] = true
	}
	for _, name := range []string{"a", "b", "root"} {
		if !found[name] {
			t.Errorf("missing transitive dependent: %s", name)
		}
	}
}

func TestGraph_Path(t *testing.T) {
	g := createTestGraph()

	cKey := ModuleKey{Name: "c", Version: "2.0.0"}

	// Path from root to c
	path := g.Path(g.Root, cKey)
	if path == nil {
		t.Fatal("Path() returned nil for existing path")
	}
	if len(path) != 3 {
		t.Errorf("expected path length 3, got %d", len(path))
	}
	if path[0] != g.Root {
		t.Error("path should start with root")
	}
	if path[len(path)-1] != cKey {
		t.Error("path should end with c")
	}

	// Path from root to root
	path = g.Path(g.Root, g.Root)
	if len(path) != 1 || path[0] != g.Root {
		t.Error("path to self should be [self]")
	}

	// No path exists
	nonExistent := ModuleKey{Name: "x", Version: "1.0.0"}
	path = g.Path(g.Root, nonExistent)
	if path != nil {
		t.Error("Path() should return nil when no path exists")
	}
}

func TestGraph_AllPaths(t *testing.T) {
	g := createTestGraph()

	cKey := ModuleKey{Name: "c", Version: "2.0.0"}

	// All paths from root to c (should be 2: root->a->c and root->b->c)
	paths := g.AllPaths(g.Root, cKey)
	if len(paths) != 2 {
		t.Errorf("expected 2 paths, got %d", len(paths))
	}

	// Each path should have 3 nodes
	for i, path := range paths {
		if len(path) != 3 {
			t.Errorf("path %d: expected length 3, got %d", i, len(path))
		}
	}
}

func TestGraph_Stats(t *testing.T) {
	g := createTestGraph()

	stats := g.Stats()

	if stats.TotalModules != 4 {
		t.Errorf("TotalModules: expected 4, got %d", stats.TotalModules)
	}
	if stats.DirectDependencies != 2 {
		t.Errorf("DirectDependencies: expected 2, got %d", stats.DirectDependencies)
	}
	if stats.TransitiveDependencies != 1 { // Only c is transitive
		t.Errorf("TransitiveDependencies: expected 1, got %d", stats.TransitiveDependencies)
	}
	if stats.MaxDepth != 2 {
		t.Errorf("MaxDepth: expected 2, got %d", stats.MaxDepth)
	}
}

func TestGraph_Roots(t *testing.T) {
	g := createTestGraph()

	roots := g.Roots()
	if len(roots) != 1 {
		t.Errorf("expected 1 root, got %d", len(roots))
	}
	if roots[0] != g.Root {
		t.Errorf("expected root to be %v, got %v", g.Root, roots[0])
	}
}

func TestGraph_Leaves(t *testing.T) {
	g := createTestGraph()

	leaves := g.Leaves()
	if len(leaves) != 1 {
		t.Errorf("expected 1 leaf, got %d", len(leaves))
	}
	if leaves[0].Name != "c" {
		t.Errorf("expected leaf to be 'c', got %v", leaves[0])
	}
}

func TestGraph_HasCycles(t *testing.T) {
	// Test graph without cycles
	g := createTestGraph()
	if g.HasCycles() {
		t.Error("test graph should not have cycles")
	}

	// Create graph with cycle: a -> b -> a
	a := ModuleKey{Name: "a", Version: "1.0.0"}
	b := ModuleKey{Name: "b", Version: "1.0.0"}

	cyclicGraph := Build(a, []SimpleModule{
		{Name: "a", Version: "1.0.0", Dependencies: []ModuleKey{b}},
		{Name: "b", Version: "1.0.0", Dependencies: []ModuleKey{a}},
	})

	if !cyclicGraph.HasCycles() {
		t.Error("cyclic graph should have cycles")
	}
}

func TestGraph_FindCycles(t *testing.T) {
	// Test graph without cycles
	g := createTestGraph()
	cycles := g.FindCycles()
	if len(cycles) != 0 {
		t.Errorf("test graph should have 0 cycles, got %d", len(cycles))
	}

	// Create graph with cycle
	a := ModuleKey{Name: "a", Version: "1.0.0"}
	b := ModuleKey{Name: "b", Version: "1.0.0"}

	cyclicGraph := Build(a, []SimpleModule{
		{Name: "a", Version: "1.0.0", Dependencies: []ModuleKey{b}},
		{Name: "b", Version: "1.0.0", Dependencies: []ModuleKey{a}},
	})

	cycles = cyclicGraph.FindCycles()
	if len(cycles) == 0 {
		t.Error("cyclic graph should have at least 1 cycle")
	}
}

func TestGraph_Explain(t *testing.T) {
	g := createTestGraph()

	explanation, err := g.Explain("c")
	if err != nil {
		t.Fatalf("Explain() error: %v", err)
	}

	if explanation.Module.Name != "c" {
		t.Errorf("expected module 'c', got '%s'", explanation.Module.Name)
	}

	// c should have 2 dependency chains (via a and via b)
	if len(explanation.DependencyChains) != 2 {
		t.Errorf("expected 2 dependency chains, got %d", len(explanation.DependencyChains))
	}

	// Non-existing module
	_, err = g.Explain("nonexistent")
	if err == nil {
		t.Error("Explain() should return error for non-existing module")
	}
}

func TestGraph_WhyIncluded(t *testing.T) {
	g := createTestGraph()

	chains, err := g.WhyIncluded("c")
	if err != nil {
		t.Fatalf("WhyIncluded() error: %v", err)
	}

	// c should have 2 paths from root
	if len(chains) != 2 {
		t.Errorf("expected 2 chains, got %d", len(chains))
	}

	// Non-existing module
	_, err = g.WhyIncluded("nonexistent")
	if err == nil {
		t.Error("WhyIncluded() should return error for non-existing module")
	}
}

func TestGraph_ToJSON(t *testing.T) {
	g := createTestGraph()

	jsonBytes, err := g.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON() error: %v", err)
	}

	// Verify it's valid JSON
	var result BazelModGraph
	if err := json.Unmarshal(jsonBytes, &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	// Check root
	if result.Key != "root@1.0.0" {
		t.Errorf("expected key 'root@1.0.0', got '%s'", result.Key)
	}
	if !result.Root {
		t.Error("Root should be true")
	}
}

func TestGraph_ToDOT(t *testing.T) {
	g := createTestGraph()

	dot := g.ToDOT()

	// Check basic DOT structure
	if !strings.Contains(dot, "digraph dependencies") {
		t.Error("missing 'digraph dependencies'")
	}
	if !strings.Contains(dot, "rankdir=LR") {
		t.Error("missing 'rankdir=LR'")
	}
	if !strings.Contains(dot, `"root@1.0.0"`) {
		t.Error("missing root node")
	}
	if !strings.Contains(dot, "->") {
		t.Error("missing edges")
	}
}

func TestGraph_ToText(t *testing.T) {
	g := createTestGraph()

	text := g.ToText()

	// Check basic text structure
	if !strings.Contains(text, "Dependency Graph") {
		t.Error("missing 'Dependency Graph' header")
	}
	if !strings.Contains(text, "root@1.0.0") {
		t.Error("missing root in output")
	}
	if !strings.Contains(text, "Total modules: 4") {
		t.Error("missing module count")
	}
	if !strings.Contains(text, "Dependency Tree") {
		t.Error("missing 'Dependency Tree' section")
	}
}

func TestGraph_ToExplainText(t *testing.T) {
	g := createTestGraph()

	text, err := g.ToExplainText("c")
	if err != nil {
		t.Fatalf("ToExplainText() error: %v", err)
	}

	if !strings.Contains(text, "Explanation for:") {
		t.Error("missing 'Explanation for:' header")
	}
	if !strings.Contains(text, "c@2.0.0") {
		t.Error("missing module key in output")
	}

	// Non-existing module
	_, err = g.ToExplainText("nonexistent")
	if err == nil {
		t.Error("ToExplainText() should return error for non-existing module")
	}
}

func TestGraph_ToModuleList(t *testing.T) {
	g := createTestGraph()

	modules := g.ToModuleList()

	// Should have 3 modules (excluding root)
	if len(modules) != 3 {
		t.Errorf("expected 3 modules, got %d", len(modules))
	}

	// Check sorting by name
	if modules[0].Name != "a" || modules[1].Name != "b" || modules[2].Name != "c" {
		t.Error("modules should be sorted by name")
	}
}

func TestDependencyChain_String(t *testing.T) {
	chain := DependencyChain{
		Path: []ModuleKey{
			{Name: "root", Version: "1.0.0"},
			{Name: "a", Version: "1.0.0"},
			{Name: "b", Version: "2.0.0"},
		},
		RequestedVersion: "1.5.0",
	}

	str := chain.String()
	expected := "root@1.0.0 -> a@1.0.0 -> b@2.0.0 (requested 1.5.0)"
	if str != expected {
		t.Errorf("got %q, want %q", str, expected)
	}

	// Empty chain
	emptyChain := DependencyChain{}
	if emptyChain.String() != "" {
		t.Error("empty chain should return empty string")
	}
}

func TestBuilder_RecordRequest(t *testing.T) {
	b := NewBuilder()

	b.RecordRequest("foo", "1.0.0", "root")
	b.RecordRequest("foo", "1.0.0", "a")
	b.RecordRequest("foo", "2.0.0", "b")

	if len(b.PreSelectionRequests["foo"]) != 2 {
		t.Errorf("expected 2 versions, got %d", len(b.PreSelectionRequests["foo"]))
	}

	if len(b.PreSelectionRequests["foo"]["1.0.0"]) != 2 {
		t.Errorf("expected 2 requesters for 1.0.0, got %d", len(b.PreSelectionRequests["foo"]["1.0.0"]))
	}
}

func TestBuilder_RecordOverride(t *testing.T) {
	b := NewBuilder()

	b.RecordOverride("foo", "3.0.0")

	if b.Overrides["foo"] != "3.0.0" {
		t.Errorf("expected override version 3.0.0, got %s", b.Overrides["foo"])
	}
}

func TestParseModuleKey(t *testing.T) {
	tests := []struct {
		input   string
		want    ModuleKey
		wantStr string
	}{
		{"foo@1.0.0", ModuleKey{Name: "foo", Version: "1.0.0"}, "foo@1.0.0"},
		{"bar@_", ModuleKey{Name: "bar", Version: ""}, "bar@_"},
		{"baz", ModuleKey{Name: "baz", Version: ""}, "baz@_"},
		{"org@example@2.0.0", ModuleKey{Name: "org@example", Version: "2.0.0"}, "org@example@2.0.0"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseModuleKey(tt.input)
			if got.Name != tt.want.Name || got.Version != tt.want.Version {
				t.Errorf("parseModuleKey(%q) = %v, want %v", tt.input, got, tt.want)
			}
			if got.String() != tt.wantStr {
				t.Errorf("parseModuleKey(%q).String() = %q, want %q", tt.input, got.String(), tt.wantStr)
			}
		})
	}
}

func TestGraph_DevDependency(t *testing.T) {
	root := ModuleKey{Name: "root", Version: "1.0.0"}
	dev := ModuleKey{Name: "dev", Version: "1.0.0"}

	g := Build(root, []SimpleModule{
		{Name: "root", Version: "1.0.0", Dependencies: []ModuleKey{dev}},
		{Name: "dev", Version: "1.0.0", DevDependency: true},
	})

	devNode := g.Modules[dev]
	if !devNode.DevDependency {
		t.Error("dev module should be marked as dev dependency")
	}

	stats := g.Stats()
	if stats.DevDependencies != 1 {
		t.Errorf("expected 1 dev dependency, got %d", stats.DevDependencies)
	}

	// Check text output includes dev marker
	text := g.ToText()
	if !strings.Contains(text, "Dev dependencies: 1") {
		t.Error("text output should include dev dependencies count")
	}
}

func TestGraph_EmptyGraph(t *testing.T) {
	g := &Graph{
		Root:    ModuleKey{Name: "empty", Version: "1.0.0"},
		Modules: make(map[ModuleKey]*Node),
	}

	// Stats on empty graph
	stats := g.Stats()
	if stats.TotalModules != 0 {
		t.Errorf("expected 0 modules, got %d", stats.TotalModules)
	}

	// JSON on empty graph
	jsonBytes, err := g.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON() error on empty graph: %v", err)
	}

	var result BazelModGraph
	if err := json.Unmarshal(jsonBytes, &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
}

func TestBuilder_SelectionInfo(t *testing.T) {
	b := NewBuilder()

	// Record multiple version requests
	b.RecordRequest("foo", "1.0.0", "root@1.0.0")
	b.RecordRequest("foo", "1.5.0", "a@1.0.0")
	b.RecordRequest("foo", "2.0.0", "b@1.0.0")

	// Build selection info
	info := b.buildSelectionInfo("foo", "2.0.0")

	if info.Strategy != StrategyMVS {
		t.Errorf("expected MVS strategy, got %s", info.Strategy)
	}

	if len(info.Candidates) != 3 {
		t.Errorf("expected 3 candidates, got %d", len(info.Candidates))
	}

	// Check that selected version is marked
	var selectedCount int
	for _, c := range info.Candidates {
		if c.Selected {
			selectedCount++
			if c.Version != "2.0.0" {
				t.Errorf("wrong version selected: %s", c.Version)
			}
		}
	}
	if selectedCount != 1 {
		t.Errorf("expected 1 selected candidate, got %d", selectedCount)
	}
}

func TestBuilder_OverrideSelection(t *testing.T) {
	b := NewBuilder()

	b.RecordOverride("foo", "3.0.0")

	info := b.buildSelectionInfo("foo", "3.0.0")

	if info.Strategy != StrategyOverride {
		t.Errorf("expected override strategy, got %s", info.Strategy)
	}

	if info.DecidingFactor != "single_version_override" {
		t.Errorf("expected 'single_version_override', got %s", info.DecidingFactor)
	}
}
