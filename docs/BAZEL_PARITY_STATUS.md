# Bazel Parity Status

This document captures the current state of Bazel parity work in `go-bzlmod`.

## What Is Already Verified

### Builtin module behavior

- Builtin `MODULE.tools` dependencies participate in selection.
- Default visible output hides builtin-only modules.
- `IncludeBuiltinModules` exposes builtin modules without changing selected versions.
- This behavior was checked against live Bazel releases from `6.6.0` through `9.1.0` for the current release-matrix fixtures.

### Deterministic parity corpus

- `e2e/` is its own Go module via `e2e/go.mod`.
- Checked-in fixture workspaces exist under `e2e/testdata/fixtures`.
- Checked-in Bazel JSON goldens exist under `e2e/testdata/goldens`.
- Current matrix coverage includes:
  - `implicit_tools_only`
  - `explicit_rules_java_conflict`
  - modes:
    - default
    - include_builtin
  - Bazel versions:
    - every supported release from `6.6.0` through `9.1.0`

### API behavior

- `IncludeBuiltinModules` exists and is treated as visibility-only.
- `IncludeUnusedModules` now exists and exposes unused module versions through the public API.
- `ModuleToResolve` now carries:
  - `Unused`
  - `DependencyKeys`

## What Is High Confidence

- The previous bug that treated builtin `MODULE.tools` dependencies as ordinary visible root deps was real.
- The default/include-builtin visible-module parity for the checked fixtures is strong.
- The library can now expose multiple visible versions of the same module when unused modules are requested.

## What Is Not Yet Proven

### Algorithmic parity gaps

- Full unpruned graph parity is not yet proven.
- The current internal unpruned graph is not yet known to match Bazel's exact augmented/unpruned semantics.
- Unused-module edge structure is still likely less faithful than Bazel's.
- The root/default resolver path and selection-based path still represent different internal models.

### Coverage gaps

- No checked-in parity corpus yet for `include_unused`.
- No checked-in parity corpus yet for `verbose`.
- No checked-in parity corpus yet for combined modes such as:
  - `include_builtin + include_unused`
  - `include_builtin + include_unused + verbose`
- No checked-in parity corpus yet for additional fixture families:
  - override-heavy cases
  - nodep cases
  - dev-dependency cases
  - multiple-version override cases
  - local/git/archive override cases
  - compatibility-level edge cases
  - extension-driven cases

## Important Interpretation

`include_builtin` is mostly a visibility concern once selection semantics are correct.

`include_unused` is not merely presentation. If the library cannot expose the same unused modules and relationships that Bazel can, that indicates missing internal algorithm/state fidelity.

## Current Recommendation

Treat the next phase as an internal parity effort:

1. lock down exact algorithmic contracts
2. make the internal unpruned graph more faithful
3. add deterministic parity goldens for the missing modes and fixtures
4. only then tighten formatter-level parity
