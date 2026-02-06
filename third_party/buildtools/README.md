# Vendored Starlark Parser

This directory contains a vendored subset of the [bazelbuild/buildtools](https://github.com/bazelbuild/buildtools) repository, specifically the Starlark/BUILD file parser.

## Vendored Version

| Field        | Value                              |
| ------------ | ---------------------------------- |
| **Source**   | `github.com/bazelbuild/buildtools` |
| **Ref**      | `b1e23f1025b8`                     |
| **Vendored** | `2026-02-02T21:32:10Z`             |

## Included Packages

Only the parser-related packages are vendored:

| Package   | Description                                                      |
| --------- | ---------------------------------------------------------------- |
| `build/`  | Core Starlark/BUILD file parser, AST types, and printer          |
| `labels/` | Bazel label parsing and manipulation utilities                   |
| `tables/` | Parser configuration tables for known Bazel rules and attributes |

These packages have **zero external dependencies** — they only use the Go standard library.

## What's Excluded

The full buildtools repository contains many other tools that are **not** included here:

- `buildifier` — BUILD file formatter
- `buildozer` — BUILD file refactoring tool
- `unused_deps` — Dependency analysis tool
- `generatetables` — Table generation utilities
- Test files and testdata directories

## License

This code is licensed under the **Apache License 2.0**, the same license as the original buildtools repository. See the [LICENSE](LICENSE) file in this directory.

**Attribution:** This code is Copyright © The Bazel Authors. It has been vendored with import paths rewritten for use in this project.

## Why Vendor?

This project vendors the parser to achieve **zero external runtime dependencies**:

1. **Minimal footprint** — Only ~8K lines of parser code, not the full buildtools module
2. **No transitive dependencies** — The parser packages only use stdlib
3. **Stability** — Vendored code won't change unexpectedly with upstream updates
4. **Reproducibility** — Exact version is tracked in the VERSION file

## Updating the Vendored Code

To update to a newer version of the parser:

```bash
# Update to a specific git tag
just vendor-parser v8.0.0

# Or use the tool directly
go run ./tools/vendor-parser -tag v8.0.0

# Update to a specific commit
go run ./tools/vendor-parser -commit abc123def

# Update to a Go module pseudo-version
go run ./tools/vendor-parser -version v0.0.0-20250602201422-abc123def
```

After updating:

1. Run `go build ./...` to verify compilation
2. Run `go test ./...` to ensure tests pass
3. Review the changes with `git diff`
4. Commit the updated vendored code

## Import Path

Code in this project imports the vendored parser as:

```go
import "github.com/albertocavalcante/go-bzlmod/third_party/buildtools/build"
```

The vendor tool automatically rewrites internal imports within the vendored code.

## Version File

The `VERSION` file contains JSON metadata about the vendored version:

```json
{
  "source": "github.com/bazelbuild/buildtools",
  "ref": "b1e23f1025b8",
  "vendored_at": "2026-02-02T21:32:10Z",
  "packages": ["build", "labels", "tables"]
}
```

Use `just vendor-version` to view this information.
