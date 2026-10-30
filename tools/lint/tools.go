//go:build tools

// Package lint contains development tool dependencies.
// This is a separate module to keep the main go.mod free of tool dependencies.
//
// Usage from project root:
//
//	go tool -modfile=tools/lint/go.mod golangci-lint run ./...
//	go tool -modfile=tools/lint/go.mod staticcheck ./...
//
// Or use the Justfile:
//
//	just lint
//	just staticcheck
package lint
