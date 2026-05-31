module github.com/albertocavalcante/go-bzlmod

go 1.26.0

// Sibling checkout; relative path so the workspace tree is portable.
// Drop once go-bzlmod-ast ships a tagged version.
replace github.com/albertocavalcante/go-bzlmod-ast => ../go-bzlmod-ast

require github.com/albertocavalcante/go-bzlmod-ast v0.0.0-00010101000000-000000000000
