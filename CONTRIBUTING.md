# Contributing to Go-bzlmod

## Getting Started

**Prerequisites:** Go 1.26.0+, Git, and Just for the command shortcuts.
Bazel/Bazelisk is required only for live E2E comparisons.

**Setup:**

```bash
git clone https://github.com/your-username/go-bzlmod.git
cd go-bzlmod
go mod download
go test ./...
cd e2e && go test ./...
```

## Testing

**Unit tests:**

```bash
go test ./...
go test -race ./...
go test -coverprofile=coverage.out ./...
```

**E2E tests:**

```bash
cd e2e && go test ./... -v
cd e2e && go test ./... -run="TestDiagnostic" -v
cd e2e && GO_BZLMOD_E2E_RELEASE_MATRIX=1 GO_BZLMOD_E2E_RELEASE_MATRIX_REFRESH=1 go test ./... -run TestE2E_BazelReleaseMatrix_ToolSelectionParity
```

Aim for >90% test coverage.

## Code Quality

Run the same checks used by CI:

```bash
just check
# Equivalent without Just:
bash scripts/check.sh
```

This checks dependencies, formatting, builds, race tests, pinned linters, and the
independent E2E module's short-mode harness. It does not run live Bazel, the BCR
corpus, or the opt-in release matrix; use the explicit commands above and in the
README for those suites. Go verifies downloaded module and tool dependencies
against the committed go.sum files and the Go checksum database.

CI also runs the full live E2E suite with a checksum-verified Bazel 7.0.0 binary.
To reproduce it on Linux x86_64 or macOS arm64:

```bash
bash scripts/install-bazel.sh /tmp/go-bzlmod-bazel
GO_BZLMOD_E2E_BAZEL=/tmp/go-bzlmod-bazel/bazel bash scripts/check.sh e2e-live
```

The installer pins each platform's digest. When upgrading Bazel, fetch the
matching `.sha256` files from `https://releases.bazel.build/VERSION/release/`,
cross-check the release asset, and update the version and digests together.

**Linting:**

```bash
just lint-all
```

**Formatting:**

```bash
go fmt ./...
goimports -w .
```

## Development Guidelines

Follow Go conventions. Document all public APIs. Keep functions small and focused. Use meaningful names. Test public APIs thoroughly with table-driven tests. Mock external dependencies.

## Bug Reports

Include Go version, OS/architecture, minimal reproduction case, expected vs actual behavior, and full error messages.

## Feature Requests

Describe the use case, provide API examples, consider alternatives, discuss breaking changes.

## Pull Request Process

Create an issue first for significant changes. Write tests. Update documentation. Run CI locally.

**Checklist:**

```
Tests pass: go test ./...
E2E tests pass: cd e2e && go test ./...
Canonical checks pass: just check
Documentation updated (if applicable)
Commits signed off: git commit -s
```

**Commit format:**

```
type(scope): description

Signed-off-by: Your Name <your.email@example.com>
```

Types: feat, fix, docs, style, refactor, test, chore

## Architecture

**Core components:**
api.go (public API), types.go (data structures), parser.go (MODULE.bazel parsing), registry.go (registry client), resolver.go (MVS algorithm)

**Design principles:**
Pure MVS implementation, thread-safe operations, context support, detailed error information.

## Resources

[Go Documentation](https://golang.org/doc/), [Effective Go](https://golang.org/doc/effective_go.html), [Bazel Module System](https://bazel.build/external/module), [Minimal Version Selection](https://research.swtch.com/vgo-mvs)

## License

MIT License applies to all contributions.
