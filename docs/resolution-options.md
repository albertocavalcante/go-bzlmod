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

Sets the registry chain. Modules are looked up in order; once found, all versions come from that registry.

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

Prepends a local vendor directory to the registry chain. Structure must match registry layout:

```
vendor/
└── modules/
    └── rules_go/
        └── 0.50.1/
            └── MODULE.bazel
```

Reference: [`types.go:499-507`](../types.go#L499-L507), [Bazel vendor docs](https://bazel.build/external/vendor)

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

Validates that direct dependency versions match resolved versions.

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

Automatically replace yanked versions with the next non-yanked version in the same compatibility level. Matches Bazel's default behavior.

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

## Progress Reporting

### WithProgress

```go
gobzlmod.WithProgress(fn func(ProgressEvent))
```

Callback for resolution progress. Must be thread-safe.

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

| Mode              | Behavior                                 |
| ----------------- | ---------------------------------------- |
| `LockfileOff`     | Ignore lockfile (default)                |
| `LockfileUpdate`  | Read existing, update after resolution   |
| `LockfileError`   | Fail if resolution differs from lockfile |
| `LockfileRefresh` | Ignore existing, create fresh lockfile   |

Reference: [`types.go:377-402`](../types.go#L377-L402), [Bazel lockfile docs](https://bazel.build/external/lockfile)

### WithLockfilePath

```go
gobzlmod.WithLockfilePath(path string)
```

Custom lockfile path. Default: `MODULE.bazel.lock` in same directory as MODULE.bazel.

## Deprecated Options

### WithDeprecatedWarnings

```go
gobzlmod.WithDeprecatedWarnings(warn bool)
```

Add warnings for deprecated modules to `result.Warnings`.

Default: `false`
