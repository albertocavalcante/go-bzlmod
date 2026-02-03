# Package Architecture

go-bzlmod is organized into focused packages with clear boundaries.

## Package Overview

```
go-bzlmod/
├── gobzlmod          # Main API (root package)
├── ast/              # MODULE.bazel AST parsing
├── graph/            # Dependency graph queries
├── label/            # Bazel label types
├── lockfile/         # MODULE.bazel.lock parsing
├── registry/         # Registry client and types
├── selection/        # MVS algorithm
│   └── version/      # Version comparison
├── bazeltools/       # MODULE.tools dependencies
└── internal/
    ├── buildutil/    # AST utilities
    └── compat/       # Field version compatibility
```

## Public Packages

### gobzlmod (root)

Main entry point. Import as:

```go
import "github.com/albertocavalcante/go-bzlmod"
```

Key exports:

- `Resolve()` — Primary resolution API
- `ContentSource`, `FileSource`, `RegistrySource` — Input types
- `With*` options — Configuration
- `ResolutionList`, `ModuleToResolve` — Result types
- `ParseModuleContent()`, `ParseModuleFile()` — Direct parsing

Reference: [`api.go`](../api.go), [`types.go`](../types.go)

### ast

Low-level MODULE.bazel parsing with full AST access.

```go
import "github.com/albertocavalcante/go-bzlmod/ast"

file, err := ast.ParseFile("MODULE.bazel")
info := ast.Extract(file)
```

Use when you need:

- Access to raw AST nodes
- Comment extraction
- Source location information
- Custom parsing beyond ModuleInfo

Reference: [`ast/`](../ast/)

### graph

Dependency graph construction and queries.

```go
import "github.com/albertocavalcante/go-bzlmod/graph"

// Usually accessed via result.Graph
g := result.Graph
explanation, _ := g.Explain("protobuf")
```

Key types: `Graph`, `Node`, `ModuleKey`, `Explanation`

Key methods: `Explain()`, `WhyIncluded()`, `Path()`, `AllPaths()`, `Stats()`

Reference: [`graph/`](../graph/), [Graph API docs](graph-api.md)

### label

Bazel label parsing and manipulation.

```go
import "github.com/albertocavalcante/go-bzlmod/label"

lbl, err := label.Parse("@rules_go//go:def.bzl")
fmt.Println(lbl.Repo)     // "rules_go"
fmt.Println(lbl.Package)  // "go"
fmt.Println(lbl.Target)   // "def.bzl"
```

Reference: [`label/`](../label/), [Bazel labels docs](https://bazel.build/concepts/labels)

### lockfile

Parse and work with MODULE.bazel.lock files.

```go
import "github.com/albertocavalcante/go-bzlmod/lockfile"

lock, err := lockfile.Parse("MODULE.bazel.lock")
```

Reference: [`lockfile/`](../lockfile/), [Bazel lockfile docs](https://bazel.build/external/lockfile)

### registry

Registry client and types for source.json, metadata.json.

```go
import "github.com/albertocavalcante/go-bzlmod/registry"

client := registry.NewClient("https://bcr.bazel.build")
meta, err := client.GetMetadata(ctx, "rules_go")
source, err := client.GetSource(ctx, "rules_go", "0.50.1")
```

Reference: [`registry/`](../registry/), [BCR docs](https://bazel.build/external/registry)

### selection

MVS algorithm implementation.

```go
import "github.com/albertocavalcante/go-bzlmod/selection"

// ModuleKey is used throughout
key := selection.ModuleKey{Name: "rules_go", Version: "0.50.1"}
```

Reference: [`selection/`](../selection/), [MVS paper](https://research.swtch.com/vgo-mvs)

### selection/version

Semantic version parsing and comparison.

```go
import "github.com/albertocavalcante/go-bzlmod/selection/version"

cmp := version.Compare("1.2.3", "1.2.4")  // -1
```

Reference: [`selection/version/`](../selection/version/)

### bazeltools

MODULE.tools implicit dependency data by Bazel version.

```go
import "github.com/albertocavalcante/go-bzlmod/bazeltools"

deps := bazeltools.GetDeps("7.0.0")
for _, dep := range deps {
    fmt.Printf("%s@%s\n", dep.Name, dep.Version)
}
```

Reference: [`bazeltools/`](../bazeltools/), [Bazel MODULE.tools](https://github.com/bazelbuild/bazel/blob/master/MODULE.tools)

## Internal Packages

### internal/buildutil

AST extraction utilities. Used by the ast package.

### internal/compat

Field version compatibility checking. Validates that MODULE.bazel fields are supported in the target Bazel version.

Reference: [`internal/compat/`](../internal/compat/)

## Import Graph

```
label (zero dependencies)
   ↑
   ├── ast
   ├── graph ← selection
   ├── selection ← selection/version
   ├── registry ← internal/compat ← selection/version
   ├── bazeltools ← selection/version
   └── internal/compat ← selection/version

gobzlmod (root) imports all of the above
```

## Design Principles

1. **Clear boundaries** — Each package has a single purpose
2. **Minimal dependencies** — Packages depend only on what they need
3. **Internal hiding** — Implementation details in `internal/`
4. **Bazel alignment** — Types and behavior match Bazel where possible
5. **Source links** — Code references Bazel source for traceability
