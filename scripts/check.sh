#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."

test_root() {
  go build ./...
  go vet ./...
  go tool -modfile=tools.go.mod gotestsum --format pkgname-and-test-fails \
    --jsonfile test-output.json -- -race -count=1 -coverprofile=coverage.out ./...
}

lint() {
  go tool -modfile=tools/lint/go.mod golangci-lint run ./...
}

deps() {
  go mod verify
  local actual
  actual=$(go list -m -f '{{.Path}}' all)
  if [[ "$actual" != $'github.com/albertocavalcante/go-bzlmod\ngithub.com/albertocavalcante/go-bzlmod-ast' ]]; then
    echo "Unexpected runtime dependencies: $actual" >&2
    return 1
  fi
  go mod tidy -diff
  (cd e2e && go mod tidy -diff)
  (cd tools/lint && go mod tidy -diff)
}

e2e_harness() {
  # Live Bazel and corpus suites have explicit, separate entry points.
  (cd e2e && go test -short -race -count=1 ./...)
}

fmt_check() {
  local unformatted
  unformatted=$(gofmt -l .)
  if [[ -n "$unformatted" ]]; then
    echo "Run just fmt to format:" >&2
    echo "$unformatted" >&2
    return 1
  fi
}

case "${1:-all}" in
  test) test_root ;;
  lint) lint ;;
  deps) deps ;;
  fmt) fmt_check ;;
  e2e) e2e_harness ;;
  e2e-live) (cd e2e && go test -race -count=1 -timeout=5m ./...) ;;
  all)
    fmt_check
    deps
    test_root
    lint
    go tool -modfile=tools/lint/go.mod staticcheck ./...
    e2e_harness
    ;;
  *)
    echo "Usage: $0 [all|test|lint|deps|fmt|e2e|e2e-live]" >&2
    exit 2
    ;;
esac
