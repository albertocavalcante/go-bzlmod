# Bazel Parity Remaining Plan

This is the concrete remaining-work plan for getting closer to full Bazel parity.

## Goal

Match Bazel in three layers, in this order:

1. selection correctness
2. internal graph correctness
3. output/formatter correctness

The key rule is: do not optimize for formatter parity if it hides unresolved algorithmic mismatches.

## Phase 1: Lock Down Algorithmic Contracts

### 1. Define exact parity targets

Write down the exact target behavior for:

- selected versions
- resolved pruned graph
- unpruned post-selection graph
- builtin module behavior
- nodep behavior
- dev-dependency behavior
- override behavior
- multiple-version override behavior
- compatibility-level constraints

### 2. Decide which internal path is authoritative

Today there are two meaningful resolution paths:

- the default dependency resolver
- the selection-based resolver

We need to decide whether to:

- migrate more parity-sensitive modes onto the selection-based path, or
- make the default path preserve all state required for parity-sensitive modes

Recommendation:

- keep using the simpler resolver for standard fast-path behavior only if it can remain semantically correct
- use the selection-based path as the parity-oriented source of truth for:
  - `include_unused`
  - override-heavy cases
  - multi-version cases

## Phase 2: Fix Internal Unpruned Graph Fidelity

### 3. Model unused-module semantics explicitly

Current state:

- unused modules can now be surfaced
- but the internal graph still likely lacks Bazel-equivalent used-vs-unused edge semantics

Required work:

- identify what Bazel stores in its augmented/unpruned graph
- map that to explicit internal structures
- preserve which edges were:
  - selected and used
  - updated to selected keys
  - unused after selection

### 4. Avoid name-collapsing in parity-sensitive paths

Any path that needs Bazel-faithful unused-module behavior must not collapse:

- module identity to name-only
- dependencies to name-only edges

Versioned keys must remain available end-to-end.

### 5. Revisit graph API assumptions

Audit places that assume:

- one visible version per module name
- `GetByName()` is sufficient
- `Dependencies []string` is enough

For parity-sensitive work, version-aware references should be the real source of truth.

## Phase 3: Expand Deterministic Parity Coverage

### 6. Add `include_unused` release matrix

Create checked-in parity goldens for:

- `implicit_tools_only`
- `explicit_rules_java_conflict`

Modes to add:

- `include_unused`
- `include_builtin + include_unused`

### 7. Add `verbose` parity coverage

Add a parity corpus for:

- `verbose`
- `include_builtin + verbose`
- `include_unused + verbose`
- `include_builtin + include_unused + verbose`

This should only happen after internal graph semantics are trustworthy.

### 8. Add more fixture families

Required new fixtures:

- single-version override conflict
- multiple-version override
- local path override
- git/archive override
- nodep only
- nodep plus explicit dep
- root dev deps
- non-root dev deps ignored
- compatibility level conflict
- builtin conflict cases beyond `rules_java`

## Phase 4: Structural Parity, Not Just Visible Module Sets

### 9. Compare more than flattened module keys

Current matrix compares visible module sets. That was enough for the builtin bug, but not enough for final parity.

Next comparisons should include:

- full graph node keys
- direct edges
- indirect edges
- cycle handling
- root direct children
- unused flags where applicable

### 10. Add Bazel JSON normalization layer

Before asserting parity, add normalization utilities so we compare semantic structure rather than harmless ordering/noise.

Likely normalizations:

- stable key ordering
- canonical empty version handling (`@_`)
- explicit separation of direct vs indirect edges

## Phase 5: Clean Up Public API After Semantics Stabilize

### 11. Document new parity-sensitive fields/options

Update docs for:

- `WithIncludeBuiltinModules`
- `WithIncludeUnusedModules`
- `ModuleToResolve.Unused`
- `ModuleToResolve.DependencyKeys`

### 12. Decide future API shape

Possible follow-up:

- add a version-aware query API for duplicate-name graphs
- de-emphasize name-only helpers when multiple versions are visible

## Definition Of Done

We should only claim near-full parity when:

- selected versions match Bazel across the full supported matrix
- pruned graph semantics match Bazel across representative fixtures
- unpruned graph semantics match Bazel across representative fixtures
- builtin and unused modes match Bazel deterministically from checked-in goldens
- output differences are limited to explicitly documented non-semantic gaps
