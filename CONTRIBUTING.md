# Contributing to Go-bzlmod

## Getting Started

**Prerequisites:** Go 1.26.0+, Git, and Just for the command shortcuts.
Bazel/Bazelisk is required only for live E2E comparisons.

**Setup:**

```bash
git clone https://github.com/albertocavalcante/go-bzlmod.git
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
cd e2e && GO_BZLMOD_E2E_RELEASE_MATRIX=1 go test ./... -run TestE2E_BazelReleaseMatrix_VisibleGraphParity
```

Coverage is evidence about exercised paths, not a correctness or parity guarantee.
Record failures and explicit skips separately. See [current parity status](docs/BAZEL_PARITY_STATUS.md).

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

Create an issue first for significant changes. Use an isolated worktree, add meaningful regressions,
update documentation, and run the canonical check. Preserve unrelated working-tree changes.

Every PR needs an independent reviewer to inspect the complete final diff and relevant call paths,
tests, trust boundaries, failure/cancellation modes and rollback behavior. Address actionable
findings and obtain follow-up review of changed code. Record the exact reviewed head SHA, fixes,
validation and limits in the PR. Re-review rebased or modified heads. Green CI is additional evidence,
not a substitute for review. Unavailable or failed checks block merge; never skip or bypass them.

Pin GitHub Actions to full SHAs with the verified exact version tag on the same line. Once reviewed
and all required checks pass, squash merge using a handcrafted Conventional Commit subject of at
most 72 characters and a meaningful body, then verify the merged PR and target-branch commit.

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
Pure selection core with explicit discovery IO, context cancellation and detailed errors.
Registry clients support concurrent reads/cache clearing; callers synchronize mutable options,
results, custom caches and callbacks.

## Resources

[Go Documentation](https://golang.org/doc/), [Effective Go](https://golang.org/doc/effective_go.html), [Bazel Module System](https://bazel.build/external/module), [Minimal Version Selection](https://research.swtch.com/vgo-mvs)

## License

Contributions follow the repository's MIT OR Apache-2.0 license.
