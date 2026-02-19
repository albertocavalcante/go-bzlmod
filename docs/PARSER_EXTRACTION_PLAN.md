# Starlark Parser Extraction Plan

## Current State Analysis

### go-bzlmod Vendoring (buildtools)

- **Source**: `bazelbuild/buildtools`
- **Location**: `third_party/buildtools/`
- **Size**: ~8,011 lines across 12 files
- **Packages**: `build/`, `labels/`, `tables/`
- **External Dependencies**: Zero (stdlib only)

### starlark-go Reference

- **Source**: `go.starlark.net/starlark`
- **Parser Location**: `syntax/` package
- **Size**: ~3,293 lines across 6 files
- **External Dependencies**: Zero (stdlib + `math/big`)

---

## Key Differences Between Parsers

| Aspect             | buildtools (current)          | starlark-go                 |
| ------------------ | ----------------------------- | --------------------------- |
| **Focus**          | BUILD/bzl file editing        | Full Starlark parsing       |
| **Parser Type**    | Yacc-generated                | Recursive-descent LL(1)     |
| **Indentation**    | No (BUILD files are flat)     | Yes (INDENT/OUTDENT tokens) |
| **Comments**       | CST-style (attached to nodes) | Optional retention          |
| **Formatting**     | Full pretty-printer           | None                        |
| **Rewriting**      | AST rewrite utilities         | None                        |
| **BUILD-specific** | Labels, tables, rules         | None                        |
| **String Types**   | Basic                         | Raw, triple-quoted, bytes   |

---

## Options for Extraction

### Option A: Extract buildtools Parser Only (Minimal)

Extract just the core parsing from buildtools, removing BUILD-specific features.

**What to Include:**

```
starlark-parser/
├── lex.go          (905 lines)   - Lexer
├── parse.y.go      (2,091 lines) - Parser
├── syntax.go       (830 lines)   - AST nodes
├── quote.go        (341 lines)   - String handling
├── walk.go         (275 lines)   - AST traversal
└── utils.go        (81 lines)    - Utilities
```

**Total**: ~4,523 lines

**What to Exclude:**

- `print.go` (formatting - BUILD-specific)
- `rewrite.go` (AST rewriting)
- `rule.go` (BUILD rule utilities)
- `labels/` (Bazel labels)
- `tables/` (Bazel metadata)

**Pros:**

- Preserves CST-like comment handling (comments attached to nodes)
- Familiar API for go-bzlmod (no migration needed)
- Proven production use in buildifier ecosystem
- Pretty-printing can be added back if needed

**Cons:**

- Yacc-generated parser is harder to maintain/modify
- No true indentation support (not needed for BUILD files)
- Smaller community than starlark-go

---

### Option B: Extract starlark-go syntax Package

Use starlark-go's syntax package as the foundation.

**What to Include:**

```
starlark-parser/
├── syntax.go       (529 lines)   - AST nodes
├── parse.go        (1,062 lines) - Parser
├── scan.go         (1,164 lines) - Scanner
├── quote.go        (309 lines)   - String handling
├── walk.go         (165 lines)   - AST traversal
└── options.go      (64 lines)    - Configuration
```

**Total**: ~3,293 lines

**Pros:**

- Official Starlark implementation (Google maintained)
- Hand-written recursive-descent parser (easier to understand/modify)
- Full indentation support (INDENT/OUTDENT tokens)
- Better string literal support (raw, triple-quoted, bytes)
- Larger community and ongoing maintenance
- Proper `math/big` for large integers

**Cons:**

- Different AST structure - requires migration in go-bzlmod
- Comment handling is optional, not CST-style
- No built-in formatting/printing
- No rewriting utilities

---

### Option C: Hybrid - starlark-go Parser + buildtools Formatting

Combine starlark-go syntax for parsing with buildtools features for formatting.

**What to Include:**

```
starlark-parser/
├── syntax/         # From starlark-go
│   ├── syntax.go
│   ├── parse.go
│   ├── scan.go
│   ├── quote.go
│   ├── walk.go
│   └── options.go
├── format/         # Adapted from buildtools
│   ├── print.go    (adapted)
│   └── rewrite.go  (adapted)
└── compat/         # Compatibility layer
    └── buildtools.go  # Adapter types
```

**Pros:**

- Best of both worlds
- Clean separation of concerns
- Can evolve formatting independently
- Provides migration path

**Cons:**

- More complex to maintain
- Need to write/maintain adapter layer
- Potentially two comment models

---

### Option D: New Parser from Scratch (CST-first)

Build a new parser optimized for tooling (CST with trivia preservation).

**Characteristics:**

- Concrete Syntax Tree (CST) that preserves ALL tokens
- Trivia (whitespace, comments) as first-class nodes
- Lossless round-tripping by design
- Modern error recovery

**Example CST Structure:**

```go
type SyntaxToken struct {
    Kind       TokenKind
    Text       string
    Span       Span
    LeadingTrivia  []Trivia  // whitespace, comments before
    TrailingTrivia []Trivia  // whitespace, comments after
}

type CallExpr struct {
    Func       Expr
    OpenParen  SyntaxToken
    Args       []Argument
    CloseParen SyntaxToken
}
```

**Pros:**

- Perfect for tooling (formatters, refactoring, IDE support)
- Lossless representation
- Modern design (inspired by Roslyn, tree-sitter)
- Full control over design decisions

**Cons:**

- Significant development effort
- Need to write and test from scratch
- Higher memory usage (all trivia preserved)
- No existing ecosystem to leverage

---

## Recommended Approach

### Phase 1: Extract buildtools Parser (Option A) - Quick Win

1. Create new repo: `github.com/albertocavalcante/starlark-syntax`
2. Extract minimal buildtools parser (~4,523 lines)
3. Update go-bzlmod to use the new package
4. Keep buildtools-compatible API

**Timeline**: 1-2 days

### Phase 2: Add starlark-go Compatibility Layer

1. Add starlark-go syntax as alternative parser
2. Create adapter to convert starlark-go AST → buildtools AST
3. Allow users to choose parser backend

**Timeline**: 3-5 days

### Phase 3: Evolve to CST (Long-term)

1. Design proper CST with trivia preservation
2. Build new parser or enhance existing
3. Provide AST view on top of CST
4. Enable advanced tooling (format-preserving transforms)

**Timeline**: 2-4 weeks

---

## Package Structure Recommendation

```
github.com/albertocavalcante/starlark-syntax/
├── LICENSE                 # Apache 2.0
├── README.md
├── go.mod
├── go.sum
│
├── ast/                    # Abstract Syntax Tree
│   ├── nodes.go           # All AST node types
│   ├── expr.go            # Expression nodes
│   ├── stmt.go            # Statement nodes
│   └── walk.go            # AST traversal
│
├── parser/                 # Parser implementation
│   ├── parser.go          # Main parser API
│   ├── lexer.go           # Tokenizer
│   └── errors.go          # Parse errors
│
├── token/                  # Token types
│   ├── token.go           # Token enum
│   └── position.go        # Source positions
│
├── format/                 # (Optional) Formatting
│   ├── printer.go         # AST printer
│   └── options.go         # Format options
│
└── testdata/               # Test files
    └── *.star
```

---

## API Design Considerations

### Simple Parse API

```go
// Parse parses Starlark source and returns an AST.
func Parse(filename string, src []byte) (*ast.File, error)

// ParseExpr parses a single Starlark expression.
func ParseExpr(src []byte) (ast.Expr, error)

// Options for parsing
type ParseOptions struct {
    RetainComments bool  // Keep comments in AST
    Mode           Mode  // Parse mode (Module, Expression, etc.)
}
```

### AST Node Interface

```go
// Node is the interface implemented by all AST nodes.
type Node interface {
    Span() (start, end Position)  // Source location
}

// Expr is implemented by all expression nodes.
type Expr interface {
    Node
    exprNode()
}

// Stmt is implemented by all statement nodes.
type Stmt interface {
    Node
    stmtNode()
}
```

---

## Decision Matrix

| Criteria               | Option A | Option B | Option C | Option D  |
| ---------------------- | -------- | -------- | -------- | --------- |
| **Development Effort** | Low      | Low      | Medium   | High      |
| **Migration Effort**   | None     | Medium   | Medium   | High      |
| **Future Flexibility** | Medium   | High     | High     | Highest   |
| **Community Support**  | Medium   | High     | Medium   | None      |
| **Tooling Support**    | Good     | Basic    | Good     | Excellent |
| **Maintainability**    | Medium   | High     | Medium   | High      |

---

## Recommendation Summary

**For immediate needs**: Start with **Option A** (buildtools extraction)

- Minimal effort
- No breaking changes to go-bzlmod
- Gets you a standalone parser package quickly

**For long-term vision**: Plan for **Option D** (CST-first parser)

- Best foundation for advanced tooling
- Format-preserving transformations
- IDE-quality error recovery

**Middle ground**: **Option C** (hybrid) if you need both:

- starlark-go parser quality
- buildtools formatting capabilities

---

## Next Steps

1. [ ] Decide on initial approach (A, B, C, or D)
2. [ ] Create new GitHub repository
3. [ ] Set up Go module structure
4. [ ] Extract/adapt parser code
5. [ ] Write minimal test suite
6. [ ] Update go-bzlmod imports
7. [ ] Document API and migration guide
