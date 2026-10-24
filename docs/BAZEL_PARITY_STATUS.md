# Validation evidence and known parity gaps

Last checked during the 2026-10-24 hardening work. This records bounded evidence, not a claim that
`go-bzlmod` implements all Bazel behavior. See [tracking issue #2](https://github.com/albertocavalcante/go-bzlmod/issues/2).

## Reproduced evidence

| Check                                             | Result and scope                                                                                            |
| ------------------------------------------------- | ----------------------------------------------------------------------------------------------------------- |
| Root race tests, pinned lint/staticcheck          | Pass on the hardening stack; `bash scripts/check.sh all` is the canonical command                           |
| Live E2E                                          | Pass against checksum-verified native Bazel 7.0.0; covers the enabled E2E fixtures, not every Bazel release |
| Complete local BCR snapshot                       | 954 resolutions, 25 asserted unsupported inputs, 0 unexpected failures, 11 documented workspace skips       |
| Saved visible-module matrix against that snapshot | 151 of 222 cases pass; 53 visible-set mismatches and 18 missing-coordinate errors remain                    |
| First-success strategy benchmark                  | 14 binary choices: 33,742,991 B/op eager versus 68,181 B/op lazy on Apple M4, Go 1.27.1, GOMAXPROCS=4       |

The BCR snapshot is pinned to `ab4017a7417cbd1e7eba34cb55aba99aa2be9808` (2026-02-19), with
990 module directories. The corpus resolves each module's latest non-yanked version from a clean,
fully materialized local checkout. Unsupported classifications assert exact directive, attribute
and transitive origin. Sparse or dirty checkouts and missing deterministic fixtures fail.
These 25 cases and 11 workspace skips are exclusions, not parity passes.

The matrix contains two fixtures, three visibility modes and 37 Bazel release versions. It compares
saved Bazel visible **module sets**, not graph edges or every Bazel output detail. Its 18 missing
coordinates include newer implicit `bazel_features@1.42.1` absent from the February snapshot. They
are fixture/registry limitations and must not be reported as successful comparisons. The remaining
53 failures concern the unused-module view. A fresh live-registry/live-Bazel run is needed before
claiming parity for a newer release.

## Reproduce

Run root checks with `just check`. That compiles the independent E2E harness in short mode;
external suites are explicitly skipped. For native Bazel and corpus commands see the
[README](../README.md#develop-and-validate).

From `e2e/`, reproduce the saved-golden matrix against an explicit registry:

```sh
GO_BZLMOD_E2E_RELEASE_MATRIX=1 \
GO_BZLMOD_E2E_RELEASE_MATRIX_REGISTRY=file:///path/to/bazel-central-registry \
go test -race -count=1 -timeout=5m -run TestE2E_BazelReleaseMatrix_VisibleGraphParity ./...
```

The registry option applies to both the library and native Bazel in live/refresh mode.
Omit `GO_BZLMOD_E2E_RELEASE_MATRIX_REGISTRY` to use the live BCR. This matrix is currently expected
to fail for the documented gaps. Library errors fail the test; they are never broadly skipped.
An explicit request combined with short mode also fails. Use the `FIXTURE`, `MODE` and `VERSION`
filters prefixed with `GO_BZLMOD_E2E_RELEASE_MATRIX_` to narrow an investigation.

`GO_BZLMOD_E2E_RELEASE_MATRIX_LIVE=1` runs Bazel for the selected versions instead of reading goldens.
It requires an appropriate Bazel/Bazelisk setup. `..._REFRESH=1` additionally writes new goldens;
refresh only after inspecting native output, registry context and the resulting diff. Never refresh
goldens merely to accept the library's output.

For the memory measurement:

```sh
GOMAXPROCS=4 go test -run '^$' -bench BenchmarkSelectionFirstSuccess -benchmem ./selection
```

The benchmark fixes an early-success workload. Lazy enumeration removes exponential retained
strategy state; unsuccessful searches can still require exponential CPU. Use `RunContext` or a
bounded resolution context for cancellation. Reproduce the eager comparison by applying the same
benchmark to the parent implementation, limiting it to 8 and 14 choices.

## Remaining work

1. Model Bazel's augmented/unpruned graph and builtin provenance accurately. `IncludeUnusedModules`
   currently exposes extra versions in 53 saved-set comparisons. Extend fixtures to compare edges,
   aliases and reachability, not just names/versions.
2. Refresh the registry fixture deliberately for newer implicit tool coordinates, record its exact
   revision, and rerun the full saved and live matrix. Separate unavailable fixtures from algorithm
   failures in the evidence.
3. Implement source acquisition and Starlark/include evaluation only behind explicit IO boundaries.
   Current unsupported selecting inputs return `errors.ErrUnsupported`; extension-generated
   repositories are outside this graph.
4. Automatic vendor and lockfile resolution modes remain reserved. A future lockfile integration
   must verify bytes before they drive transitive requests, preserve existing extension/unknown
   data, and commit updates atomically after all target checks succeed. Current non-default options
   fail explicitly before resolution IO.
5. Standalone lockfile serialization preserves its modeled schemas, not arbitrary unknown fields;
   allowed-yank bookkeeping and atomic replacement need dedicated validation before automatic use.

The historical extraction and parity plans are retained for context; their old package maps,
coverage counts and multiple-resolver descriptions are not current contracts. Exact PR review SHAs,
findings and hosted check state belong in each PR. A failed/unavailable CI check blocks merging;
native test evidence does not turn a failed SonarCloud check into a pass.
