//go:build tools

// Package lint contains development tool dependencies.
// This is a separate module to keep the main go.mod free of tool dependencies.
//
// Usage from project root:
//
//	go run -modfile=tools/lint/go.mod github.com/golangci/golangci-lint/v2/cmd/golangci-lint run ./...
//	go run -modfile=tools/lint/go.mod honnef.co/go/tools/cmd/staticcheck ./...
//
// Or use the Makefile:
//
//	make lint
//	make staticcheck
package lint
