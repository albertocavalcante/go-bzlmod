# Bazel Parity Ideas

This file is intentionally speculative. These are design ideas and follow-up directions, not committed decisions.

## Internal Model Ideas

### Keep two graph layers explicitly

Possible model:

- selected/pruned graph
- unpruned post-selection graph

Each should be first-class instead of derived ad hoc from a flattened module list.

### Track edge provenance explicitly

Potential edge metadata:

- requested version
- selected/resolved version
- whether the edge is builtin-derived
- whether the edge became unused after selection
- whether the edge originated from nodep
- whether the edge exists only because `include_unused` is enabled

This would make `verbose` parity easier and remove guesswork later.

### Treat builtins as a dedicated source class

Instead of inferring builtins from names, carry a source type such as:

- explicit root
- explicit transitive
- builtin implicit
- nodep
- override-synthetic

That would make visibility filtering and explanations less fragile.

## Testing Ideas

### Add fixture manifests

Each fixture could carry a small manifest file describing:

- what it is testing
- expected semantic invariants
- which Bazel modes are relevant
- whether duplicate versions are expected

This would make the parity corpus easier to maintain.

### Add invariant tests separate from Bazel goldens

Some checks should not depend on live Bazel output at all. Examples:

- enabling `include_builtin` must not change selected versions
- enabling `include_unused` must not change selected versions
- version-aware dependency keys must remain internally consistent

### Add targeted release-family probes

Not every fixture needs to run across every release forever. Some can be targeted:

- pre-7.6 nodep behavior
- 8.x builtin tool drift
- 9.x builtin visibility differences

## API Ideas

### Add a version-aware lookup helper

Current graph helpers like `GetByName()` become ambiguous once duplicate visible versions exist.

Possible additions:

- `GetAllByName(name string) []*Node`
- `ContainsExact(name, version string) bool`
- `ExplainKey(name, version string)`

### Add explicit parity/debug outputs

Potential future APIs:

- `ResolvedGraph()`
- `UnprunedGraph()`
- `ParitySnapshot()`

That would make test and tooling integrations cleaner than reverse-engineering from `ResolutionList.Modules`.

## E2E Ideas

### Cache live Bazel probes locally

For refresh workflows, a local cache of live Bazel outputs could reduce repeated expensive subprocess runs during development.

### Goldens for multiple output layers

Possible directory layout:

- `goldens/<fixture>/<mode>/<version>/visible_modules.json`
- `goldens/<fixture>/<mode>/<version>/graph.json`
- `goldens/<fixture>/<mode>/<version>/invariants.json`

This would allow gradual tightening without forcing every assertion into one monolithic comparison.

## Open Questions

### Unused-edge semantics

Questions to settle with source inspection and live Bazel:

- exactly which edges remain visible in unpruned/unused views
- how Bazel distinguishes direct, indirect, and unused edges in JSON
- whether our current graph type should grow that structure or whether parity-specific graph types are cleaner

### Module extensions

We still need to decide how much of Bazel's extension/repo surface is in scope for this library's parity claim.

Possible scopes:

- module-resolution parity only
- module graph parity plus extension visibility
- full `bazel mod` query parity

That scope decision matters before overbuilding the API.
