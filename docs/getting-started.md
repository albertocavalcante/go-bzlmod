# Getting Started

Installation and basic usage of go-bzlmod.

## Installation

```bash
go get github.com/albertocavalcante/go-bzlmod
```

Requires Go 1.21+.

## Basic Usage

### Resolve from a File

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/albertocavalcante/go-bzlmod"
)

func main() {
    result, err := gobzlmod.Resolve(context.Background(),
        gobzlmod.FileSource("MODULE.bazel"),
    )
    if err != nil {
        log.Fatal(err)
    }

    fmt.Printf("Resolved %d modules\n", result.Summary.TotalModules)
    for _, m := range result.Modules {
        fmt.Printf("  %s@%s\n", m.Name, m.Version)
    }
}
```

### Resolve from Content

```go
content := `
module(name = "my_project", version = "1.0.0")

bazel_dep(name = "rules_go", version = "0.50.1")
bazel_dep(name = "gazelle", version = "0.40.0")
`

result, err := gobzlmod.Resolve(ctx, gobzlmod.ContentSource(content))
```

### Resolve a Registry Module

Fetch a module from the registry and resolve its dependencies:

```go
// Resolve rules_go and all its transitive dependencies
result, err := gobzlmod.Resolve(ctx, gobzlmod.RegistrySource{
    Name:    "rules_go",
    Version: "0.50.1",
})
// result.Modules[0] is rules_go@0.50.1 (Depth=0)
// result.Modules[1:] are its dependencies (Depth>=1)
```

Reference: [`api.go:104-126`](../api.go#L104-L126)

### Multiple Registries

Chain registries with priority ordering (first match wins):

```go
result, err := gobzlmod.Resolve(ctx,
    gobzlmod.FileSource("MODULE.bazel"),
    gobzlmod.WithRegistries(
        "https://my-private-registry.example.com",
        gobzlmod.DefaultRegistry,  // BCR fallback
    ),
)
```

Once a module is found in a registry, ALL versions of that module come from that registry. This matches Bazel's `--registry` flag behavior.

Reference: [Bazel registry documentation](https://bazel.build/external/registry)

### Excluding Dev Dependencies

```go
// By default, dev dependencies are excluded
result, err := gobzlmod.Resolve(ctx, gobzlmod.FileSource("MODULE.bazel"))

// Explicitly include dev dependencies
result, err := gobzlmod.Resolve(ctx,
    gobzlmod.FileSource("MODULE.bazel"),
    gobzlmod.WithDevDeps(),
)

fmt.Printf("Production: %d, Dev: %d\n",
    result.Summary.ProductionModules,
    result.Summary.DevModules)
```

## Understanding Results

The [`ResolutionList`](../types.go#L119-L139) contains:

```go
type ResolutionList struct {
    Modules  []ModuleToResolve  // All resolved modules, sorted by name
    Graph    *graph.Graph       // Dependency graph for queries
    Summary  ResolutionSummary  // Statistics
    Warnings []string           // Non-fatal issues
}
```

Each [`ModuleToResolve`](../types.go#L141-L197) includes:

| Field           | Description                                      |
| --------------- | ------------------------------------------------ |
| `Name`          | Module name (e.g., "rules_go")                   |
| `Version`       | Selected version (e.g., "0.50.1")                |
| `Registry`      | Source registry URL                              |
| `Depth`         | Distance from root (1 = direct, 2+ = transitive) |
| `DevDependency` | Is this a dev-only dependency?                   |
| `Dependencies`  | Direct dependencies of this module               |
| `RequiredBy`    | Modules that required this one                   |

## Convenience Methods

```go
// Get a specific module
m := result.Module("rules_go")
if m != nil {
    fmt.Printf("Found %s@%s\n", m.Name, m.Version)
}

// Filter by type
direct := result.DirectDeps()      // Depth == 1
transitive := result.TransitiveDeps()  // Depth > 1
prod := result.ProductionModules()     // DevDependency == false
dev := result.DevModules()             // DevDependency == true

// Check existence
if result.HasModule("protobuf") {
    fmt.Println("protobuf is in the dependency graph")
}
```

Reference: [`types.go:242-298`](../types.go#L242-L298)

## Error Handling

```go
result, err := gobzlmod.Resolve(ctx, src)
if err != nil {
    switch e := err.(type) {
    case *gobzlmod.YankedVersionsError:
        fmt.Printf("Yanked: %v\n", e.Modules)
    case *gobzlmod.DirectDepsMismatchError:
        fmt.Printf("Version mismatches: %v\n", e.Mismatches)
    case *gobzlmod.BazelIncompatibilityError:
        fmt.Printf("Incompatible with Bazel %s: %v\n", e.BazelVersion, e.Modules)
    case *gobzlmod.MaxDepthExceededError:
        fmt.Printf("Dependency too deep: %s\n", e.Path)
    default:
        fmt.Printf("Error: %v\n", err)
    }
}
```

Reference: [`types.go:661-791`](../types.go#L661-L791)

## Next Steps

- [Resolution Options](resolution-options.md) — All configuration options
- [Graph API](graph-api.md) — Query dependency relationships
- [Bazel Compatibility](bazel-compatibility.md) — Version validation
