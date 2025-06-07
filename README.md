# Go-bzlmod

[![Go Reference](https://pkg.go.dev/badge/github.com/albertocavalcante/go-bzlmod.svg)](https://pkg.go.dev/github.com/albertocavalcante/go-bzlmod)
[![Go Report Card](https://goreportcard.com/badge/github.com/albertocavalcante/go-bzlmod)](https://goreportcard.com/report/github.com/albertocavalcante/go-bzlmod)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

A Go library for Bazel module dependency resolution using Minimal Version Selection (MVS). Parses `MODULE.bazel` files, resolves transitive dependencies, and applies MVS algorithm to determine the minimal set of module versions.

## Features

Pure MVS implementation following [Russ Cox's research](https://research.swtch.com/vgo-mvs). Concurrent dependency resolution with override support. Fetches metadata from Bazel Central Registry or custom registries. Handles `single_version_override`, `git_override`, `local_path_override`, and `archive_override`.

## Algorithm Differences

This library implements pure MVS. Bazel uses an enhanced algorithm with automatic version upgrades and compatibility mappings.

```
Module: platforms
Our MVS: v0.0.4  (declared in MODULE.bazel)
Bazel:   v0.0.7  (upgraded for compatibility)
```

Use this library for understanding MVS behavior, research, or building dependency analysis tools. Don't use it for exact Bazel build reproduction.

## Installation

```bash
go get github.com/albertocavalcante/go-bzlmod
```

## Usage

### Basic Example

```go
package main

import (
    "fmt"
    "log"
    
    "github.com/albertocavalcante/go-bzlmod"
)

func main() {
    resolutionList, err := gobzlmod.ResolveDependenciesFromFile(
        "MODULE.bazel",
        "https://bcr.bazel.build",
        false, // include dev dependencies
    )
    if err != nil {
        log.Fatalf("Failed to resolve dependencies: %v", err)
    }

    fmt.Printf("Resolved %d modules:\n", resolutionList.Summary.TotalModules)
    for _, module := range resolutionList.Modules {
        fmt.Printf("  %s@%s\n", module.Name, module.Version)
    }
}
```

### With Context and Content

```go
package main

import (
    "context"
    "fmt"
    "log"
    "time"
    
    "github.com/albertocavalcante/go-bzlmod"
)

func main() {
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()

    moduleContent := `
module(name = "my_project", version = "1.0.0")

bazel_dep(name = "rules_go", version = "0.41.0")
bazel_dep(name = "gazelle", version = "0.31.0")
bazel_dep(name = "protobuf", version = "3.19.2")

single_version_override(
    module_name = "protobuf",
    version = "3.19.6",
)
`

    resolutionList, err := gobzlmod.ResolveDependenciesWithContext(
        ctx,
        moduleContent,
        "https://bcr.bazel.build",
        true, // include dev dependencies
    )
    if err != nil {
        log.Fatalf("Failed to resolve dependencies: %v", err)
    }
    
    fmt.Printf("Resolved %d modules with overrides\n", resolutionList.Summary.TotalModules)
}
```

### Advanced Usage

```go
package main

import (
    "context"
    "fmt"
    "log"
    
    "github.com/albertocavalcante/go-bzlmod"
)

func main() {
    ctx := context.Background()
    content := `module(name = "my_project", version = "1.0.0")`
    
    // Parse MODULE.bazel manually
    moduleInfo, err := gobzlmod.ParseModuleContent(content)
    if err != nil {
        log.Fatalf("Failed to parse module: %v", err)
    }

    // Create custom registry client
    registry := gobzlmod.NewRegistryClient("https://my-registry.com")

    // Create resolver with options
    resolver := gobzlmod.NewDependencyResolver(registry, true) // includeDevDeps

    // Resolve with full control
    resolutionList, err := resolver.ResolveDependencies(ctx, moduleInfo)
    if err != nil {
        log.Fatalf("Failed to resolve: %v", err)
    }
    
    fmt.Printf("Advanced resolution complete: %d modules\n", resolutionList.Summary.TotalModules)
}
```

## Output Structure

```go
type ResolutionList struct {
    Modules []ModuleToResolve `json:"modules"`
    Summary ResolutionSummary `json:"summary"`
}

type ModuleToResolve struct {
    Name          string   `json:"name"`           // e.g., "rules_go"
    Version       string   `json:"version"`        // e.g., "0.41.0"
    Registry      string   `json:"registry"`       // source registry URL
    DevDependency bool     `json:"dev_dependency"` // whether it's a dev dependency
    RequiredBy    []string `json:"required_by"`    // modules that require this
}

type ResolutionSummary struct {
    TotalModules      int `json:"total_modules"`
    ProductionModules int `json:"production_modules"`
    DevModules        int `json:"dev_modules"`
}
```

## API Functions

### Core Functions

```go
// Resolve dependencies from a MODULE.bazel file
func ResolveDependenciesFromFile(
    moduleFilePath string,
    registryURL string, 
    includeDevDeps bool,
) (*ResolutionList, error)

// Resolve dependencies from MODULE.bazel content string
func ResolveDependenciesFromContent(
    moduleContent string,
    registryURL string,
    includeDevDeps bool,
) (*ResolutionList, error)

// Resolve dependencies with custom context for timeout control
func ResolveDependenciesWithContext(
    ctx context.Context,
    moduleContent string,
    registryURL string,
    includeDevDeps bool,
) (*ResolutionList, error)
```

### Utility Functions

```go
// Parse MODULE.bazel content into structured data
func ParseModuleContent(content string) (*ModuleInfo, error)

// Create a new registry client with caching
func NewRegistryClient(baseURL string) *RegistryClient

// Create a dependency resolver with custom options
func NewDependencyResolver(
    registry *RegistryClient,
    includeDevDeps bool,
) *DependencyResolver
```

## MVS Algorithm

The library implements canonical Minimal Version Selection:

1. **Build dependency graph**: Discover all version requirements from transitive closure
2. **Apply overrides**: Process single_version_override and other override types
3. **Select minimal versions**: Choose highest version among all requirements for each module
4. **Filter dev dependencies**: Include/exclude based on configuration

The algorithm is deterministic and reproducible - same inputs always produce same outputs.

## Project Structure

```
├── api.go           # Public API functions
├── types.go         # Core data structures
├── parser.go        # MODULE.bazel parsing
├── registry.go      # Registry client with caching
├── resolver.go      # MVS dependency resolution
├── *_test.go        # Comprehensive unit tests
└── e2e/             # End-to-end tests vs real Bazel
```

## Testing

### Running Tests

```bash
# Run all unit tests
go test ./...

# Run end-to-end tests with verbose output
go test ./e2e -v

# Run specific diagnostic tests
go test ./e2e -run="TestDiagnostic" -v

# Run tests with coverage
go test -cover ./...

# Run tests with race detection
go test -race ./...
```

The e2e tests compare results against actual `bazel mod graph` output to validate correctness within expected algorithm differences.

## Performance

Concurrent fetching with HTTP caching. Registry responses cached in memory. Configurable concurrency limits prevent overwhelming registries.

Typical performance: 20-30 modules in 1-2 seconds, 50-100 modules in 3-5 seconds. Performance is network-bound by registry response times.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for development setup, testing guidelines, and contribution process.

## License

MIT License. See [LICENSE](LICENSE) file.
