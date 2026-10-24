# Getting started

The [README](../README.md#install) is the canonical installation and runnable resolution example.
Requires Go 1.26.0+. Bazel is needed only for live comparisons, not library use.

1. Install `github.com/albertocavalcante/go-bzlmod@latest` in your Go module.
2. Choose `FileSource`, `ContentSource` or `RegistrySource` and call `Resolve` with a context.
3. Check the returned error before reading `Modules` or `Graph`.
4. Read the [supported input boundary](../README.md#resolution-behavior) before relying on results
   for files that compute dependency values or acquire sources through overrides.

`FileSource` anchors relative local override paths beside the module file. `ContentSource` uses the
process working directory. Content/file resolution excludes the root from `Modules`; registry
resolution includes the requested target at depth 0. Results are sorted by name.

Defaults exclude dev dependencies and do not check yank status. See the
[option reference](resolution-options.md) for explicit policy. Automatic vendor and lockfile modes
and yanked substitution are unsupported and return errors before resolution IO.

For syntax trees, editing and labels, use the separate
[go-bzlmod-ast module](https://github.com/albertocavalcante/go-bzlmod-ast).

For contributors, start with [CONTRIBUTING.md](../CONTRIBUTING.md) and `just check`.
The default check compiles the E2E harness but skips its external suites. Run the explicit
[live and corpus commands](../README.md#develop-and-validate) for that coverage.
