# Package boundaries

| Package                                    | Responsibility                                                                                          |
| ------------------------------------------ | ------------------------------------------------------------------------------------------------------- |
| [gobzlmod](../api.go)                      | Public source/options API, static declaration extraction, discovery IO and result assembly              |
| [selection](../selection/)                 | Pure version selection, overrides, pruning and lazy strategy search; `RunContext` supports cancellation |
| [selection/version](../selection/version/) | Bazel relaxed version parsing and comparison                                                            |
| [graph](../graph/)                         | Graph construction, queries and formatting                                                              |
| [registry](../registry/)                   | Registry data types and standalone HTTP client; root package composes resolution registries             |
| [lockfile](../lockfile/)                   | Explicit lockfile parsing, comparison, merge and serialization                                          |
| [bazeltools](../bazeltools/)               | Checked-in Bazel `MODULE.tools` dependency tables and lookup helpers                                    |

The source parser, AST utilities, buildtools fork and label package were extracted to
[`github.com/albertocavalcante/go-bzlmod-ast`](https://github.com/albertocavalcante/go-bzlmod-ast).
Old `go-bzlmod/ast` and `go-bzlmod/label` imports no longer exist. Inspect the extracted module's
public API for syntax-tree operations rather than using the resolver's static extractor as an
evaluator.

The root runtime dependency graph contains this module and that pinned parser module. E2E and
lint tools are isolated in nested modules. Consumer installation does not require the development
toolchain or a sibling source checkout. `just verify-deps` checks this boundary.

Resolution performs IO at the root API/registry/local-override boundaries. The selection package
uses only caller-supplied graphs and performs no IO. Callers own synchronization for mutable
options, returned graphs, custom caches and callbacks; registry clients support concurrent reads
and cache clearing.

For usage, see the [README](../README.md), [option reference](resolution-options.md), and
[graph API](graph-api.md).
