# Graph API

The `graph` package provides dependency graph queries. Every `ResolutionList` includes a `Graph` field for analysis.

Reference: [`graph/`](../graph/)

## Basic Usage

```go
result, _ := gobzlmod.Resolve(ctx, src)
g := result.Graph
```

## Query Methods

### Explain

Get a detailed explanation of why a module is at its version:

```go
explanation, err := g.Explain("protobuf")
if err != nil {
    // Module not found
}

fmt.Println(explanation.RequestSummary)
// protobuf version selection:
//   3.19.2 requested by: rules_go@0.50.1 [SELECTED]
//   3.19.0 requested by: gazelle@0.40.0
// Strategy: mvs (highest version wins)

fmt.Printf("Selected: %s via %s\n",
    explanation.Selection.SelectedVersion,
    explanation.Selection.Strategy)
```

Reference: [`graph/query.go:185-218`](../graph/query.go#L185-L218)

### WhyIncluded

Find all dependency chains that cause a module to be included:

```go
chains, err := g.WhyIncluded("protobuf")
for _, chain := range chains {
    fmt.Println(chain.String())
    // my_project@1.0.0 -> rules_go@0.50.1 -> protobuf@3.19.2 (requested 3.19.2)
}
```

Reference: [`graph/query.go:246-260`](../graph/query.go#L246-L260)

### Path / AllPaths

Find dependency paths between modules:

```go
// ModuleKey identifies a module
from := graph.ModuleKey{Name: "my_project", Version: "1.0.0"}
to := graph.ModuleKey{Name: "protobuf", Version: "3.19.2"}

// Shortest path (BFS)
path := g.Path(from, to)
// [my_project@1.0.0, rules_go@0.50.1, protobuf@3.19.2]

// All paths (can be expensive for large graphs)
paths := g.AllPaths(from, to)
```

Reference: [`graph/query.go:111-183`](../graph/query.go#L111-L183)

### DirectDeps / DirectDependents

```go
key := graph.ModuleKey{Name: "rules_go", Version: "0.50.1"}

// What does this module depend on?
deps := g.DirectDeps(key)

// What directly depends on this module?
dependents := g.DirectDependents(key)
```

Reference: [`graph/query.go:35-49`](../graph/query.go#L35-L49)

### TransitiveDeps / TransitiveDependents

```go
// All transitive dependencies (BFS order)
allDeps := g.TransitiveDeps(key)

// All modules that transitively depend on this (reverse BFS)
allDependents := g.TransitiveDependents(key)
```

Reference: [`graph/query.go:51-109`](../graph/query.go#L51-L109)

## Lookup Methods

### Get / GetByName

```go
// By exact key
node := g.Get(graph.ModuleKey{Name: "rules_go", Version: "0.50.1"})

// By name only (any version)
node := g.GetByName("rules_go")
if node != nil {
    fmt.Printf("Found %s\n", node.Key.String())
}
```

Reference: [`graph/query.go:9-22`](../graph/query.go#L9-L22)

### Contains / ContainsName

```go
if g.ContainsName("protobuf") {
    fmt.Println("protobuf is in the graph")
}

if g.Contains(graph.ModuleKey{Name: "rules_go", Version: "0.50.1"}) {
    fmt.Println("Exact version found")
}
```

Reference: [`graph/query.go:24-33`](../graph/query.go#L24-L33)

## Cycle Detection

```go
// Quick check
if g.HasCycles() {
    fmt.Println("Graph contains cycles")
}

// Get all cycles
cycles := g.FindCycles()
for _, cycle := range cycles {
    // cycle is []ModuleKey forming the cycle
    fmt.Printf("Cycle: %v\n", cycle)
}
```

Reference: [`graph/query.go:343-428`](../graph/query.go#L343-L428)

## Graph Statistics

```go
stats := g.Stats()

fmt.Printf("Total modules: %d\n", stats.TotalModules)
fmt.Printf("Direct deps: %d\n", stats.DirectDependencies)
fmt.Printf("Transitive deps: %d\n", stats.TransitiveDependencies)
fmt.Printf("Max depth: %d\n", stats.MaxDepth)
fmt.Printf("Dev deps: %d\n", stats.DevDependencies)
```

Reference: [`graph/query.go:262-290`](../graph/query.go#L262-L290), [`graph/types.go:135-151`](../graph/types.go#L135-L151)

## Special Nodes

```go
// Root nodes (no dependents) - typically just one
roots := g.Roots()

// Leaf nodes (no dependencies)
leaves := g.Leaves()
```

Reference: [`graph/query.go:320-341`](../graph/query.go#L320-L341)

## Export Formats

### ToJSON

Bazel-compatible JSON format (matches `bazel mod graph --output=json`):

```go
jsonBytes, err := g.ToJSON()
os.WriteFile("graph.json", jsonBytes, 0644)
```

Reference: [`graph/format.go:35-39`](../graph/format.go#L35-L39)

### ToDOT

Graphviz DOT format for visualization:

```go
dot := g.ToDOT()
os.WriteFile("graph.dot", []byte(dot), 0644)
// Then: dot -Tpng graph.dot -o graph.png
```

Reference: [`graph/format.go:104-136`](../graph/format.go#L104-L136)

### ToText

Human-readable tree format:

```go
text := g.ToText()
fmt.Println(text)
// Dependency Graph (root: my_project@1.0.0)
// ============================================================
//
// Total modules: 15
// Direct dependencies: 3
// ...
//
// Dependency Tree:
// my_project@1.0.0
// ├── rules_go@0.50.1
// │   ├── protobuf@3.19.2
// │   └── ...
// └── gazelle@0.40.0
```

Reference: [`graph/format.go:138-174`](../graph/format.go#L138-L174)

### ToExplainText

Human-readable explanation for a specific module:

```go
text, err := g.ToExplainText("protobuf")
fmt.Println(text)
// Explanation for: protobuf@3.19.2
// ============================================================
//
// Version Selection:
//   Selected version: 3.19.2
//   Strategy: mvs
//   ...
```

Reference: [`graph/format.go:221-269`](../graph/format.go#L221-L269)

## Types

### ModuleKey

```go
type ModuleKey struct {
    Name    string
    Version string
}

key := graph.ModuleKey{Name: "rules_go", Version: "0.50.1"}
fmt.Println(key.String())  // "rules_go@0.50.1"
```

Reference: [`selection/types.go`](../selection/types.go)

### Node

```go
type Node struct {
    Key               ModuleKey
    Dependencies      []ModuleKey           // Direct deps
    Dependents        []ModuleKey           // Reverse edges
    RequestedVersions map[ModuleKey]string  // Who requested which version
    Selection         *SelectionInfo        // Why this version was selected
    IsRoot            bool
    DevDependency     bool
}
```

Reference: [`graph/types.go:25-47`](../graph/types.go#L25-L47)

### SelectionInfo

```go
type SelectionInfo struct {
    Strategy        SelectionStrategy  // "mvs", "override", "root"
    SelectedVersion string
    Candidates      []VersionCandidate
    DecidingFactor  string
}
```

Reference: [`graph/types.go:49-62`](../graph/types.go#L49-L62)
