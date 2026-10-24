# Resolution Options

Complete reference for all `Resolve` configuration options.

Options are passed as functional arguments to `Resolve`:

```go
result, err := gobzlmod.Resolve(ctx, src,
    gobzlmod.WithDevDeps(),
    gobzlmod.WithTimeout(30*time.Second),
)
```

## Registry Options

### WithRegistries

```go
gobzlmod.WithRegistries(urls ...string)
```

Sets the registry chain. Modules are looked up in order; once found, that module version keeps its registry.

```go
gobzlmod.WithRegistries(
    "https://private.example.com",  // Check first
    "file:///local/registry",       // Then local
    gobzlmod.DefaultRegistry,       // BCR fallback
)
```

Supported URL schemes:

- `https://` — Remote registry
- `http://` — Remote (not recommended for production)
- `file://` — Local filesystem registry

Default: `["https://bcr.bazel.build"]`

Reference: [`types.go:484-497`](../types.go#L484-L497), [Bazel registry docs](https://bazel.build/external/registry)

### WithVendorDir

```go
gobzlmod.WithVendorDir(dir string)
```

Reserved: a nonempty directory returns `errors.ErrUnsupported` before IO.
For a directory with the registry `modules/{name}/{version}/MODULE.bazel`
layout, use `WithRegistries("file:///absolute/path")`. This is a local registry;
it does not implement Bazel's vendor mode.

### WithRegistryTrace

```go
gobzlmod.WithRegistryTrace()
```

Enables Bazel-style registry tracing during resolution.

When enabled:

- `ResolutionList.RegistryFileHashes` includes canonical registry URLs for
  `MODULE.bazel`, `source.json`, and `bazel_registry.json`.
- Hash values are raw SHA-256 hex digests.
- Nil values mean the URL was probed but not found, matching Bazel's
  `registryFileHashes` semantics for fallback misses.
- `ModuleToResolve.Source` is populated for registry-backed modules.
- `ResolutionList.ToLockfile()` can be used to convert the traced result into a
  lockfile-compatible snapshot.

This is the recommended mode for metadata-only mirroring and lockfile generation.

## Dependency Options

### WithDevDeps

```go
gobzlmod.WithDevDeps()
```

Include root-module `dev_dependency = True` modules in resolution. By default, dev dependencies are excluded. Transitive `dev_dependency` edges are ignored to match Bazel semantics.

Reference: [`types.go:438-439`](../types.go#L438-L439)

### WithDirectDepsMode

```go
gobzlmod.WithDirectDepsMode(mode DirectDepsCheckMode)
```

Compares effective root dependency edges before and after selection. Overrides
and dev-dependency filtering apply first; aliases and multiple-version edges are
checked independently. Versions differing only in build metadata compare equal.

| Mode              | Behavior                          |
| ----------------- | --------------------------------- |
| `DirectDepsOff`   | No validation (default)           |
| `DirectDepsWarn`  | Add warnings to `result.Warnings` |
| `DirectDepsError` | Return `DirectDepsMismatchError`  |

Reference: [`types.go:346-358`](../types.go#L346-L358)

## Yanked Version Options

### WithYankedCheck

```go
gobzlmod.WithYankedCheck(check bool)
```

Enable fetching metadata to detect yanked versions. When false, `Yanked` and `YankReason` fields are not populated.

Default: `false`

### WithYankedBehavior

```go
gobzlmod.WithYankedBehavior(behavior YankedVersionBehavior)
```

How to handle yanked versions when detected:

| Behavior             | Result                            |
| -------------------- | --------------------------------- |
| `YankedVersionAllow` | Populate info, no error (default) |
| `YankedVersionWarn`  | Add to `result.Warnings`          |
| `YankedVersionError` | Return `YankedVersionsError`      |

Reference: [`types.go:327-344`](../types.go#L327-L344)

### WithSubstituteYanked

```go
gobzlmod.WithSubstituteYanked(substitute bool)
```

Reserved: `true` returns `errors.ErrUnsupported`. Bazel does not automatically
substitute selected yanked versions. Use `WithYankedBehavior` to allow, warn, or fail.

Default: `false`

Reference: [`types.go:465-469`](../types.go#L465-L469)

### WithAllowedYankedVersions

```go
gobzlmod.WithAllowedYankedVersions(versions ...string)
```

Allow specific yanked versions. Format: `"module@version"` or `"all"` to allow all.

```go
gobzlmod.WithAllowedYankedVersions("protobuf@3.19.0", "rules_go@0.40.0")
```

Mirrors Bazel's `--allow_yanked_versions` flag.

Reference: [`types.go:450-454`](../types.go#L450-L454)

## Bazel Compatibility Options

### WithBazelVersion

```go
gobzlmod.WithBazelVersion(version string)
```

Target Bazel version. When set:

1. Injects that version's MODULE.tools dependencies
2. Enables `bazel_compatibility` constraint validation (if mode set)
3. Checks field version requirements (e.g., `mirror_urls` requires 7.7.0+)

```go
gobzlmod.WithBazelVersion("7.0.0")
```

Reference: [`types.go:478-482`](../types.go#L478-L482), [bazeltools/](../bazeltools/)

### WithBazelToolsLookup

```go
gobzlmod.WithBazelToolsLookup(func(version string) []bazeltools.ToolDep)
```

Overrides how implicit `MODULE.tools` dependencies are resolved for
`WithBazelVersion`.

Use this when:

- you need a Bazel release newer than the built-in table
- you are testing prerelease or HEAD builds
- you use a custom Bazel fork with different `MODULE.tools` contents

```go
gobzlmod.WithBazelToolsLookup(func(version string) []bazeltools.ToolDep {
    if version == "7.0.0-custom.1" {
        return []bazeltools.ToolDep{
            {Name: "fork_only_dep", Version: "0.1.0"},
        }
    }
    return bazeltools.LookupDeps(version)
})
```

Returning `nil` means no implicit `MODULE.tools` dependencies for that version.
Use `bazeltools.LookupDeps(version)` inside the callback when you want to extend
rather than replace the built-in mapping.

### WithBazelToolsTransformer

```go
gobzlmod.WithBazelToolsTransformer(func(version string, deps []bazeltools.ToolDep) []bazeltools.ToolDep)
```

Post-processes implicit `MODULE.tools` dependencies after lookup/default
resolution.

Use this when you want to:

- keep the built-in list but change one dependency version
- add one or more fork-specific implicit dependencies
- remove a dependency while preserving the rest of the list

```go
gobzlmod.WithBazelToolsTransformer(func(version string, deps []bazeltools.ToolDep) []bazeltools.ToolDep {
    deps = bazeltools.SetToolDep(deps, bazeltools.ToolDep{
        Name:    "rules_python",
        Version: "0.40.1-fork.1",
    })
    deps = bazeltools.RemoveToolDep(deps, "buildozer")
    return deps
})
```

The resolver passes a cloned slice, so the transformer may safely mutate or
replace it. Returning `nil` means no implicit `MODULE.tools` dependencies for
that version.

### WithBazelCompatibilityMode

```go
gobzlmod.WithBazelCompatibilityMode(mode BazelCompatibilityMode)
```

How to handle `bazel_compatibility` constraint failures:

| Mode                      | Behavior                           |
| ------------------------- | ---------------------------------- |
| `BazelCompatibilityOff`   | No validation (default)            |
| `BazelCompatibilityWarn`  | Add to `result.Warnings`           |
| `BazelCompatibilityError` | Return `BazelIncompatibilityError` |

Requires `WithBazelVersion` to be set.

Reference: [`types.go:360-375`](../types.go#L360-L375), [BazelModuleResolutionFunction.java](https://github.com/bazelbuild/bazel/blob/master/src/main/java/com/google/devtools/build/lib/bazel/bzlmod/BazelModuleResolutionFunction.java#L298-L333)

## HTTP Options

### WithTimeout

```go
gobzlmod.WithTimeout(d time.Duration)
```

HTTP request timeout for registry requests.

Default: 15 seconds

### WithHTTPClient

```go
gobzlmod.WithHTTPClient(client *http.Client)
```

Custom HTTP client for authentication, TLS, proxies, etc.

```go
// Example: Bearer token auth
type bearerTransport struct {
    token string
    base  http.RoundTripper
}

func (t *bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
    req = req.Clone(req.Context())
    req.Header.Set("Authorization", "Bearer "+t.token)
    return t.base.RoundTrip(req)
}

client := &http.Client{
    Transport: &bearerTransport{token: os.Getenv("TOKEN"), base: http.DefaultTransport},
}
gobzlmod.WithHTTPClient(client)
```

Reference: [`types.go:538-560`](../types.go#L538-L560)

## Caching Options

### WithCache

```go
gobzlmod.WithCache(cache ModuleCache)
```

External cache for MODULE.bazel content. Must implement:

```go
type ModuleCache interface {
    Get(ctx context.Context, name, version string) (content []byte, found bool, err error)
    Put(ctx context.Context, name, version string, content []byte) error
}
```

Cache errors are handled gracefully—failures fall back to registry fetch.

Reference: [`types.go:585-659`](../types.go#L585-L659)

Cache storage keys include a stable registry namespace, so sharing one cache across
private and public registries cannot reuse the wrong module definition. Old
unqualified entries are ignored. Clients apply the scope automatically. For
manual prepopulation, write through `gobzlmod.ScopeModuleCache(cache, registryURL)`
and pass the original cache to `WithCache`. Raw content and version values are
preserved; custom caches should treat the name argument as an opaque key.

## Progress Reporting

### WithProgress

```go
gobzlmod.WithProgress(fn func(ProgressEvent))
```

Callback for resolution progress. Must be thread-safe and return quickly.
For valid options, one resolve start/end pair surrounds parsing, discovery,
selection and final target checks. Every attempted registry module fetch has a
start/end pair, including cache hits and failures. End events carry errors in
`Message`. Invalid options fail before callbacks. Callback panics propagate.

```go
gobzlmod.WithProgress(func(e gobzlmod.ProgressEvent) {
    switch e.Type {
    case gobzlmod.ProgressResolveStart:
        fmt.Println("Starting resolution...")
    case gobzlmod.ProgressModuleFetchStart:
        fmt.Printf("Fetching %s@%s\n", e.Module, e.Version)
    case gobzlmod.ProgressResolveEnd:
        fmt.Println(e.Message)
    }
})
```

Event types: `ProgressResolveStart`, `ProgressResolveEnd`, `ProgressModuleFetchStart`, `ProgressModuleFetchEnd`

Reference: [`types.go:404-434`](../types.go#L404-L434)

## Logging

### WithLogger

```go
gobzlmod.WithLogger(l *slog.Logger)
```

Structured logger for resolution diagnostics.

```go
logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
    Level: slog.LevelDebug,
}))
gobzlmod.WithLogger(logger)
```

Reference: [`types.go:580-582`](../types.go#L580-L582)

## Lockfile Options

### WithLockfileMode

```go
gobzlmod.WithLockfileMode(mode LockfileMode)
```

Only `LockfileOff` is supported. `LockfileUpdate`, `LockfileError`, and
`LockfileRefresh` return `errors.ErrUnsupported` before resolution IO.
No automatic lockfile read, verification or write is performed.

### WithLockfilePath

A nonempty path returns `errors.ErrUnsupported`, including with `LockfileOff`.
For explicit operations use the [lockfile package](../lockfile/).
`ResolutionList.ToLockfile()` creates a new registry trace snapshot; it is not
an update of an existing lockfile and does not preserve extension evaluation data.

## Deprecated Options

### WithDeprecatedWarnings

```go
gobzlmod.WithDeprecatedWarnings(warn bool)
```

Add warnings for deprecated modules to `result.Warnings`.

Default: `false`
