# go-bzlmod Documentation

Bazel module dependency resolution using Minimal Version Selection (MVS).

## Contents

- [Getting Started](getting-started.md) — Installation and basic usage
- [Resolution Options](resolution-options.md) — All configuration options
- [Graph API](graph-api.md) — Query dependency relationships
- [Bazel Compatibility](bazel-compatibility.md) — Version constraint validation
- [Package Architecture](packages.md) — Package organization

## Quick Example

```go
package main

import (
    "context"
    "fmt"

    "github.com/albertocavalcante/go-bzlmod"
)

func main() {
    result, _ := gobzlmod.Resolve(context.Background(),
        gobzlmod.FileSource("MODULE.bazel"),
        gobzlmod.WithBazelVersion("7.0.0"),
    )

    for _, m := range result.Modules {
        fmt.Printf("%s@%s\n", m.Name, m.Version)
    }
}
```

## Links

- [GitHub Repository](https://github.com/albertocavalcante/go-bzlmod)
- [Go Package Docs](https://pkg.go.dev/github.com/albertocavalcante/go-bzlmod)
- [Bazel Central Registry](https://bcr.bazel.build)
- [Bazel bzlmod Documentation](https://bazel.build/external/module)
- [MVS Algorithm](https://research.swtch.com/vgo-mvs)
