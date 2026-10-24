# go-bzlmod

[![Go Reference](https://pkg.go.dev/badge/github.com/albertocavalcante/go-bzlmod.svg)](https://pkg.go.dev/github.com/albertocavalcante/go-bzlmod)

A Go library for inspecting `MODULE.bazel` files and resolving registry module dependencies.
It discovers transitive requirements, applies version selection and root overrides, and builds a
queryable dependency graph without running Bazel.

This is a **static resolver**, with a defined subset of Bazel behavior. It does not evaluate
Starlark or module extensions, fetch source archives, or perform a Bazel build. Full Bazel parity
is still work in progress; see [validation and known gaps](docs/BAZEL_PARITY_STATUS.md).

## Install

Requires **Go 1.26.0 or newer**. Bazel is unnecessary for library use.

```sh
go get github.com/albertocavalcante/go-bzlmod@latest
```

The runtime module depends on
[`go-bzlmod-ast`](https://github.com/albertocavalcante/go-bzlmod-ast), pinned in `go.mod`.
No sibling checkout or local `replace` is required. Development tools and E2E dependencies live in
separate modules and are not runtime dependencies. For unreleased changes on the default branch,
use `@main` instead of `@latest`.

## Resolve a module file

```go
package main

import (
    "context"
    "fmt"
    "log"
    "time"

    gobzlmod "github.com/albertocavalcante/go-bzlmod"
)

func main() {
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()

    result, err := gobzlmod.Resolve(ctx, gobzlmod.FileSource("MODULE.bazel"),
        gobzlmod.WithYankedCheck(true),
        gobzlmod.WithYankedBehavior(gobzlmod.YankedVersionError),
    )
    if err != nil {
        log.Fatal(err)
    }
    for _, module := range result.Modules {
        fmt.Printf("%s@%s (depth %d)\n", module.Name, module.Version, module.Depth)
    }
}
```

Three input types use the same `Resolve` API:

| Source                                                | Result                                                                                                |
| ----------------------------------------------------- | ----------------------------------------------------------------------------------------------------- |
| `FileSource("MODULE.bazel")`                          | Dependencies of the file's root module; local override paths are relative to that file                |
| `ContentSource(content)`                              | Dependencies of the supplied root; local override paths are relative to the process working directory |
| `RegistrySource{Name: "rules_go", Version: "0.50.0"}` | The requested module at depth 0, plus its dependencies                                                |

`Modules` is sorted by name. Use `result.Module("rules_go")` to find a module rather than assuming
an array position. Multiple-version and unused-module views can contain several versions of one
name; use the graph's module keys for version-specific queries.

Use `ParseModuleContent` or `ParseModuleFile` to extract declarations without registry resolution.
Low-level AST and label packages now live in
[`go-bzlmod-ast`](https://github.com/albertocavalcante/go-bzlmod-ast), not under this module's import path.

## Resolution behavior

Discovery fetches required `MODULE.bazel` files, including eligible nodep constraints. Missing
required dependencies fail resolution. A nodep requirement participates only once its module name
is reachable through an ordinary dependency; it does not introduce that name on its own.
Selection chooses the highest requested version within each selection group, applies overrides,
and prunes unreachable modules. Ambiguous compatibility choices are tried lazily in deterministic
order; selection honors context cancellation.

| Capability                                                          | Support                                                                                                   |
| ------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------- |
| Literal top-level `module` and `bazel_dep`                          | Supported, including explicit zero compatibility constraints                                              |
| Root `single_version_override` / `multiple_version_override`        | Version selection and registry overrides                                                                  |
| Root `local_path_override`                                          | Reads the local target's real dependencies; FileSource resolves relative paths beside the root file       |
| Used `git_override` / `archive_override`                            | Returns `errors.ErrUnsupported`; source acquisition is not implemented                                    |
| `include`, computed selecting arguments, selecting-function aliases | Returns `errors.ErrUnsupported`; no silent empty dependency result                                        |
| Module extension and toolchain declarations                         | Not evaluated; their generated repositories are outside the resolved graph                                |
| Bazel version emulation                                             | Optional `WithBazelVersion`; uses the checked-in `MODULE.tools` table, with custom lookup/transform hooks |
| Compatibility levels                                                | Legacy behavior without a Bazel version; ignored for the 8.6 branch and 9.1+, retained in the 9.0 branch  |
| Graph queries                                                       | `result.Graph`, with module keys, paths, reverse dependencies and explanations                            |

The extractor accepts unrelated extension declarations, so successful parsing is not proof that
Bazel would accept or evaluate the complete file. Unsupported static selecting inputs expose
`UnsupportedInputError`; use `errors.Is(err, errors.ErrUnsupported)` to classify them.
Patches and source-rule evaluation are not performed.

## Configure registries and policy

```go
result, err := gobzlmod.Resolve(ctx, src,
    gobzlmod.WithRegistries("https://private.example.com", gobzlmod.DefaultRegistry),
    gobzlmod.WithDevDeps(),
    gobzlmod.WithDirectDepsMode(gobzlmod.DirectDepsError),
)
```

Registries are tried in priority order. **Only a 404 permits initial fallback.** Once a registry
provides `name@version`, that coordinate's source and metadata stay with that origin; other versions
may come from other registries. Authentication, server, TLS and timeout failures are returned.
For a local registry layout, use `WithRegistries("file:///absolute/path/to/registry")`.
Local registry reads are confined to that directory, including symlink handling.

Defaults exclude root dev dependencies, omit implicit Bazel tools unless a Bazel version is set,
and leave yank/deprecation checks off. Transitive dev dependencies are always excluded.
`DefaultOptions()` is an optional bundle enabling yank warnings, deprecation warnings and a
15-second request timeout; these are library defaults, not an exact copy of Bazel defaults.
Yank metadata fetch failures follow Bazel's fail-open behavior. For strict policy, account for that
limitation. Allowed yanked versions can be configured with `WithAllowedYankedVersions`.

Direct-dependency checks compare effective root edges after overrides against selection results.
`DirectDepsWarn` adds warnings; `DirectDepsError` returns `DirectDepsMismatchError`.
Progress callbacks receive balanced resolution and attempted-module-fetch start/end events,
including failures. They must be thread-safe and return quickly.

These reserved options now fail explicitly **before resolution IO**, rather than silently doing
nothing:

| Option                 | Accepted value                                                          |
| ---------------------- | ----------------------------------------------------------------------- |
| `WithVendorDir`        | Empty only; use a `file://` registry for the registry layout            |
| `WithLockfileMode`     | `LockfileOff` only                                                      |
| `WithLockfilePath`     | Empty only                                                              |
| `WithSubstituteYanked` | `false` only; selected yanked versions are never automatically replaced |

See the [full option reference](docs/resolution-options.md) for custom HTTP clients, external caches,
Bazel hooks and warning policies. Shared external caches are automatically namespaced by registry;
old unqualified entries become misses. For manual prepopulation, use `ScopeModuleCache`.

## Registry traces and lockfiles

`WithRegistryTrace()` records raw SHA-256 hashes for accessed registry `MODULE.bazel`, `source.json`
and optional `bazel_registry.json` files, and hydrates each registry-backed module's `Source`.
Missing probed files have nil hashes. It fetches metadata, not the module's source archive.

`result.ToLockfile()` creates a new snapshot. The [lockfile package](lockfile/) provides explicit
parsing, comparison and serialization. Neither path implements Bazel's automatic lockfile modes,
module-extension evaluation, or an update preserving all unknown fields of an existing lockfile.
Local override and file-registry content is outside the portable HTTP registry hash trace.
Do not treat a newly generated snapshot as a verified replacement for an existing Bazel lockfile.

## Develop and validate

```sh
just check
# Equivalent without Just:
bash scripts/check.sh all
```

This checks formatting, module tidiness and the runtime dependency graph, builds/vets, runs the root
race suite, pinned linters and staticcheck, and compiles the separate E2E harness in short mode.
**Short mode skips live Bazel and corpus tests.** Run those explicitly:

```sh
# Native Bazel 7.0.0 E2E comparison (verified installer pins version + SHA-256):
bash scripts/install-bazel.sh /tmp/go-bzlmod-bazel
GO_BZLMOD_E2E_BAZEL=/tmp/go-bzlmod-bazel/bazel bash scripts/check.sh e2e-live

# Complete pinned local BCR corpus; see e2e/bcr_corpus_test.go for its revision:
cd e2e
GO_BZLMOD_E2E_BCR_CORPUS=1 \
GO_BZLMOD_E2E_BCR_PATH=/path/to/bazel-central-registry \
go test -race -count=1 -timeout=5m -run TestE2E_BCRCorpus ./...
```

The pinned 990-module snapshot currently yields **954 resolutions, 25 asserted unsupported inputs,
0 unexpected failures and 11 documented monorepo skips**. This measures static resolution, not
full Bazel parity. The harness rejects dirty, wrong-revision or incomplete sparse checkouts.
[Parity evidence](docs/BAZEL_PARITY_STATUS.md) records the remaining visible-graph mismatches and
validation limits. [CONTRIBUTING.md](CONTRIBUTING.md) documents the review workflow.

## Packages and license

See [package boundaries](docs/packages.md), [graph queries](docs/graph-api.md), and
[Bazel compatibility checks](docs/bazel-compatibility.md).

Dual licensed under [MIT](LICENSE-MIT) or [Apache-2.0](LICENSE-APACHE), at your option.
