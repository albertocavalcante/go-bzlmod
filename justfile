# go-bzlmod development commands
# Run `just --list` to see available commands

# Default recipe: run tests
default: test

# Build all packages
build:
    go build ./...

# Run all tests
test:
    go test ./...

# Run tests with verbose output
test-v:
    go test -v ./...

# Run tests with race detector
test-race:
    go test -race ./...

# Run benchmarks
bench:
    go test -bench=. -benchmem ./...

# Run golangci-lint (uses tools/lint module)
lint:
    go run -modfile=tools/lint/go.mod github.com/golangci/golangci-lint/v2/cmd/golangci-lint run ./...

# Run staticcheck (uses tools/lint module)
staticcheck:
    go run -modfile=tools/lint/go.mod honnef.co/go/tools/cmd/staticcheck ./...

# Run all linters
lint-all: lint staticcheck

# Tidy all go.mod files
tidy:
    go mod tidy
    cd tools/lint && go mod tidy

# Verify the module has no external dependencies
verify-deps:
    @echo "Checking for direct dependencies..."
    @deps=$(go list -m -f '{{{{if not .Indirect}}{{{{.Path}}{{{{end}}' all 2>/dev/null | grep -v "^github.com/albertocavalcante/go-bzlmod$" || true); \
    if [ -z "$deps" ]; then \
        echo "✓ No direct dependencies!"; \
    else \
        echo "✗ Found direct dependencies:"; \
        echo "$deps"; \
        exit 1; \
    fi

# Run go vet
vet:
    go vet ./...

# Format code
fmt:
    go fmt ./...

# Check if code is formatted
fmt-check:
    @test -z "$(gofmt -l .)" || (echo "Code is not formatted. Run 'just fmt'" && exit 1)

# Clean build cache
clean:
    go clean -cache

# Vendor the buildtools parser (updates third_party/buildtools)
vendor-parser tag="":
    @if [ -z "{{tag}}" ]; then \
        go run ./tools/vendor-parser; \
    else \
        go run ./tools/vendor-parser -tag {{tag}}; \
    fi

# Show vendored parser version
vendor-version:
    @cat third_party/buildtools/VERSION | jq .

# Run all checks (CI)
ci: build test lint-all vet fmt-check

# Development workflow: format, lint, test
dev: fmt lint test
