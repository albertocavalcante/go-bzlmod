# Bazel Compatibility

go-bzlmod validates that modules are compatible with your target Bazel version.

## bazel_compatibility Constraints

Modules can declare Bazel version constraints in MODULE.bazel:

```python
module(
    name = "my_module",
    version = "1.0.0",
    bazel_compatibility = [">=7.0.0", "<9.0.0"],
)
```

Constraint syntax:

| Operator  | Meaning                     |
| --------- | --------------------------- |
| `>=X.Y.Z` | Minimum version (inclusive) |
| `<=X.Y.Z` | Maximum version (inclusive) |
| `>X.Y.Z`  | Greater than                |
| `<X.Y.Z`  | Less than                   |
| `-X.Y.Z`  | Exclude specific version    |

Reference: [Bazel module() docs](https://bazel.build/rules/lib/globals/module#module), [`bazel_compat.go`](../bazel_compat.go)

## Enabling Validation

```go
result, err := gobzlmod.Resolve(ctx, src,
    gobzlmod.WithBazelVersion("7.0.0"),
    gobzlmod.WithBazelCompatibilityMode(gobzlmod.BazelCompatibilityError),
)
```

### Modes

| Mode                      | Behavior                           |
| ------------------------- | ---------------------------------- |
| `BazelCompatibilityOff`   | No validation (default)            |
| `BazelCompatibilityWarn`  | Add warnings to `result.Warnings`  |
| `BazelCompatibilityError` | Return `BazelIncompatibilityError` |

Reference: [`types.go:360-375`](../types.go#L360-L375)

## Checking Results

Incompatible modules are flagged in the result:

```go
for _, m := range result.Modules {
    if m.IsBazelIncompatible {
        fmt.Printf("%s@%s: %s\n",
            m.Name, m.Version, m.BazelIncompatibilityReason)
    }
}

fmt.Printf("Incompatible modules: %d\n", result.Summary.IncompatibleModules)
```

## MODULE.tools Injection

When `WithBazelVersion` is set, the resolver injects Bazel's implicit MODULE.tools dependencies. These are tool dependencies that Bazel includes automatically.

```go
// Includes Bazel 7.0.0's implicit tool dependencies
result, _ := gobzlmod.Resolve(ctx, src,
    gobzlmod.WithBazelVersion("7.0.0"),
)
```

The injected dependencies vary by Bazel version. See [`bazeltools/tools.go`](../bazeltools/tools.go) for the full list per version.

Reference: [Bazel MODULE.tools](https://github.com/bazelbuild/bazel/blob/master/MODULE.tools), [`bazeltools/`](../bazeltools/)

## Field Version Requirements

Some MODULE.bazel and source.json fields require minimum Bazel versions:

| Field                     | Minimum Version | Location     |
| ------------------------- | --------------- | ------------ |
| `max_compatibility_level` | 7.0.0           | MODULE.bazel |
| `use_repo_rule`           | 7.0.0           | MODULE.bazel |
| `include`                 | 7.2.0           | MODULE.bazel |
| `mirror_urls`             | 7.7.0           | source.json  |
| `override_repo`           | 8.0.0           | MODULE.bazel |
| `inject_repo`             | 8.0.0           | MODULE.bazel |

When a field is used with an incompatible Bazel version, warnings are added to `result.Summary.FieldWarnings`.

Reference: [`internal/compat/fields.go`](../internal/compat/fields.go)

## Example: Full Validation

```go
result, err := gobzlmod.Resolve(ctx,
    gobzlmod.FileSource("MODULE.bazel"),
    gobzlmod.WithBazelVersion("7.0.0"),
    gobzlmod.WithBazelCompatibilityMode(gobzlmod.BazelCompatibilityWarn),
)
if err != nil {
    log.Fatal(err)
}

// Check for compatibility issues
if result.Summary.IncompatibleModules > 0 {
    fmt.Printf("Warning: %d modules incompatible with Bazel 7.0.0\n",
        result.Summary.IncompatibleModules)
    for _, m := range result.Modules {
        if m.IsBazelIncompatible {
            fmt.Printf("  - %s@%s: %s\n",
                m.Name, m.Version, m.BazelIncompatibilityReason)
        }
    }
}

// Check field compatibility
for _, warning := range result.Summary.FieldWarnings {
    fmt.Printf("Field warning: %s\n", warning)
}
```

## Bazel Source References

The compatibility checking implementation follows Bazel's behavior:

- [BazelModuleResolutionFunction.java](https://github.com/bazelbuild/bazel/blob/master/src/main/java/com/google/devtools/build/lib/bazel/bzlmod/BazelModuleResolutionFunction.java) — Compatibility validation (lines 298-333)
- [ModuleFileGlobals.java](https://github.com/bazelbuild/bazel/blob/master/src/main/java/com/google/devtools/build/lib/bazel/bzlmod/ModuleFileGlobals.java) — bazel_compatibility parsing
- [MODULE.tools](https://github.com/bazelbuild/bazel/blob/master/MODULE.tools) — Implicit tool dependencies
