# go-bzlmod

[![Go Reference](https://pkg.go.dev/badge/github.com/albertocavalcante/go-bzlmod.svg)](https://pkg.go.dev/github.com/albertocavalcante/go-bzlmod)
[![Go Report Card](https://goreportcard.com/badge/github.com/albertocavalcante/go-bzlmod)](https://goreportcard.com/report/github.com/albertocavalcante/go-bzlmod)
[![License](https://img.shields.io/badge/License-MIT%20OR%20Apache--2.0-blue.svg)](LICENSE)

A Go library for Bazel module dependency resolution. Implements [Minimal Version Selection](https://research.swtch.com/vgo-mvs) (MVS), parses `MODULE.bazel` files, and provides dependency graph analysis.

## Features

- **MVS Resolution** — Pure MVS with concurrent fetching ([resolver.go](resolver.go))
- **Multi-Registry** — Chain registries with priority ordering ([registry.go](registry.go))
- **Override Support** — `single_version_override`, `git_override`, `local_path_override`, `archive_override`
- **Graph Queries** — Dependency paths, explanations, cycle detection ([graph/](graph/))
- **Bazel Compatibility** — Validate `bazel_compatibility` constraints ([bazel_compat.go](bazel_compat.go))
- **Vendor Support** — Resolve from local vendor directories
- **MODULE.tools** — Inject Bazel's implicit dependencies ([bazeltools/](bazeltools/))

## Installation

```bash
go get github.com/albertocavalcante/go-bzlmod
```

Requires Go 1.21+.

## Quick Start

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/albertocavalcante/go-bzlmod"
)

func main() {
    // Resolve from a file
    result, err := gobzlmod.Resolve(context.Background(),
        gobzlmod.FileSource("MODULE.bazel"),
    )
    if err != nil {
        log.Fatal(err)
    }

    fmt.Printf("Resolved %d modules:\n", result.Summary.TotalModules)
    for _, m := range result.Modules {
        fmt.Printf("  %s@%s\n", m.Name, m.Version)
    }
}
```

## Module Sources

Three ways to specify input:

```go
// From file path
result, _ := gobzlmod.Resolve(ctx, gobzlmod.FileSource("MODULE.bazel"))

// From string content
result, _ := gobzlmod.Resolve(ctx, gobzlmod.ContentSource(`
    module(name = "my_project", version = "1.0.0")
    bazel_dep(name = "rules_go", version = "0.50.1")
`))

// From registry (fetch module by name@version)
result, _ := gobzlmod.Resolve(ctx, gobzlmod.RegistrySource{
    Name:    "rules_go",
    Version: "0.50.1",
})
```

## Options

Configure resolution with functional options:

```go
result, err := gobzlmod.Resolve(ctx, src,
    // Include dev_dependency modules
    gobzlmod.WithDevDeps(),

    // Use custom registries (first match wins)
    gobzlmod.WithRegistries(
        "https://my-registry.example.com",
        gobzlmod.DefaultRegistry,  // BCR fallback
    ),

    // Set request timeout
    gobzlmod.WithTimeout(30*time.Second),

    // Check for yanked versions
    gobzlmod.WithYankedCheck(true),
    gobzlmod.WithYankedBehavior(gobzlmod.YankedVersionWarn),
)
```

See [Resolution Options](docs/resolution-options.md) for all options.

## Dependency Graph

Query the resolved graph ([graph/query.go](graph/query.go)):

```go
result, _ := gobzlmod.Resolve(ctx, src)
g := result.Graph

// Explain why a module is at its version
explanation, _ := g.Explain("protobuf")
fmt.Println(explanation.RequestSummary)

// Find all paths from root to a module
chains, _ := g.WhyIncluded("protobuf")
for _, chain := range chains {
    fmt.Println(chain.String())  // root@1.0.0 -> rules_go@0.50.1 -> protobuf@3.19.6
}

// Check for cycles
if g.HasCycles() {
    cycles := g.FindCycles()
    fmt.Printf("Found %d cycles\n", len(cycles))
}

// Get graph statistics
stats := g.Stats()
fmt.Printf("Total: %d, Direct: %d, Transitive: %d\n",
    stats.TotalModules, stats.DirectDependencies, stats.TransitiveDependencies)

// Export formats
dotGraph := g.ToDOT()      // Graphviz DOT
jsonGraph, _ := g.ToJSON() // Bazel-compatible JSON
textGraph := g.ToText()    // Human-readable tree
```

See [Graph API](docs/graph-api.md) for complete documentation.

## Packages

| Package                     | Description                               |
| --------------------------- | ----------------------------------------- |
| [`gobzlmod`](.)             | Main API: `Resolve`, `Parse`, core types  |
| [`ast`](ast/)               | MODULE.bazel AST parsing                  |
| [`graph`](graph/)           | Dependency graph construction and queries |
| [`label`](label/)           | Bazel label parsing (`@repo//pkg:target`) |
| [`lockfile`](lockfile/)     | `MODULE.bazel.lock` parsing               |
| [`registry`](registry/)     | Registry client and types                 |
| [`selection`](selection/)   | MVS algorithm implementation              |
| [`bazeltools`](bazeltools/) | MODULE.tools implicit dependencies        |

See [Package Architecture](docs/packages.md) for details.

## Algorithm Note

This library implements pure MVS. Bazel's resolver includes additional heuristics that may produce different results:

```
Module: platforms
go-bzlmod: 0.0.4  (declared version, pure MVS)
Bazel:     0.0.7  (upgraded via compatibility mapping)
```

Use this library for dependency analysis and tooling. For exact Bazel parity, use `bazel mod graph`.

Reference: [Selection.java](https://github.com/bazelbuild/bazel/blob/master/src/main/java/com/google/devtools/build/lib/bazel/bzlmod/Selection.java)

## Documentation

- [Getting Started](docs/getting-started.md) — Installation and basic usage
- [Resolution Options](docs/resolution-options.md) — All configuration options
- [Graph API](docs/graph-api.md) — Query dependency relationships
- [Bazel Compatibility](docs/bazel-compatibility.md) — Version constraint validation
- [Package Architecture](docs/packages.md) — Package organization

## Testing

```bash
go test ./...              # Unit tests
go test ./e2e -v           # E2E tests against real Bazel
go test -cover ./...       # With coverage
go test -race ./...        # Race detection
```

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for development setup and guidelines.

## License

Licensed under [Apache 2.0](LICENSE-APACHE) or [MIT](LICENSE-MIT) at your option.
