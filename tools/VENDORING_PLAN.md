# Starlark Parser Vendoring Plan

This document outlines two approaches to eliminate the `bazelbuild/buildtools` dependency:

1. **Vendoring automation** - Tool to vendor the parser into this repo
2. **Fork plan** - Create a standalone parser module

---

## Current State

```
go-bzlmod
└── depends on: github.com/bazelbuild/buildtools (full module)
    └── we only use: build/, labels/, tables/ packages (~8K LOC, stdlib only)
```

The `build` package (Starlark parser) has **zero external dependencies** - it only uses stdlib.
The starlark-go/protobuf deps in buildtools are for buildifier/buildozer, not the parser.

---

## Option 1: Vendoring Automation

### Target Structure

```
go-bzlmod/
├── internal/
│   └── starlark/                    # Vendored parser
│       ├── build/                   # AST types, parser, printer
│       ├── labels/                  # Bazel label parsing
│       ├── tables/                  # Known rules/attributes
│       └── VERSION                  # Tracks vendored version
├── tools/
│   └── vendor-parser/
│       ├── main.go                  # Vendoring tool
│       └── README.md
```

### Vendoring Tool Design

**Command:**

```bash
go run ./tools/vendor-parser -version v0.0.0-20250602201422-b1e23f1025b8
# or
go run ./tools/vendor-parser -commit b1e23f1025b8
# or
go run ./tools/vendor-parser -tag v7.1.2
```

**Tool Workflow:**

```
┌─────────────────────────────────────────────────────────────────────┐
│ 1. DOWNLOAD                                                         │
│    - Fetch tarball from GitHub at specified version/commit/tag      │
│    - URL: github.com/bazelbuild/buildtools/archive/{ref}.tar.gz     │
│    - Verify download (checksum if provided)                         │
└─────────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────────┐
│ 2. EXTRACT                                                          │
│    - Extract only: build/, labels/, tables/                         │
│    - Skip: *_test.go files (optional, configurable)                 │
│    - Skip: testdata/ directories                                    │
└─────────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────────┐
│ 3. REWRITE IMPORTS                                                  │
│    - Old: github.com/bazelbuild/buildtools/build                    │
│    - New: github.com/albertocavalcante/go-bzlmod/internal/starlark/build │
│    - Same for: labels, tables                                       │
│    - Use go/ast or simple string replacement                        │
└─────────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────────┐
│ 4. WRITE VERSION FILE                                               │
│    internal/starlark/VERSION:                                       │
│    {                                                                 │
│      "source": "github.com/bazelbuild/buildtools",                  │
│      "version": "v0.0.0-20250602201422-b1e23f1025b8",               │
│      "commit": "b1e23f1025b8...",                                   │
│      "vendored_at": "2026-02-02T12:00:00Z",                         │
│      "packages": ["build", "labels", "tables"],                     │
│      "checksum": "sha256:..."                                       │
│    }                                                                 │
└─────────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────────┐
│ 5. UPDATE INTERNAL IMPORTS                                          │
│    - Update go-bzlmod source files to use internal/starlark/...    │
│    - Files: parser.go, ast/parser.go, internal/buildutil/attr.go   │
│    - Run: go mod tidy (removes buildtools dependency)               │
└─────────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────────┐
│ 6. VERIFY                                                           │
│    - go build ./...                                                 │
│    - go test ./...                                                  │
│    - Confirm buildtools removed from go.mod                         │
└─────────────────────────────────────────────────────────────────────┘
```

### Tool Implementation Notes

```go
// tools/vendor-parser/main.go

package main

import (
    "archive/tar"
    "compress/gzip"
    "flag"
    "go/ast"
    "go/parser"
    "go/printer"
    "go/token"
    "io"
    "net/http"
    "os"
    "path/filepath"
    "strings"
)

const (
    buildtoolsRepo = "bazelbuild/buildtools"
    targetDir      = "internal/starlark"
    oldImportPath  = "github.com/bazelbuild/buildtools"
    newImportPath  = "github.com/albertocavalcante/go-bzlmod/internal/starlark"
)

var packages = []string{"build", "labels", "tables"}

func main() {
    version := flag.String("version", "", "Go module version (e.g., v0.0.0-20250602...)")
    commit := flag.String("commit", "", "Git commit hash")
    tag := flag.String("tag", "", "Git tag (e.g., v7.1.2)")
    keepTests := flag.Bool("keep-tests", false, "Keep test files")
    flag.Parse()

    // ... implementation
}
```

### Key Functions Needed

1. **`downloadTarball(ref string) (io.ReadCloser, error)`**
   - Fetch from `https://github.com/{repo}/archive/{ref}.tar.gz`

2. **`extractPackages(tarball io.Reader, packages []string, dest string) error`**
   - Extract only specified packages from tarball

3. **`rewriteImports(dir string, oldPath, newPath string) error`**
   - Parse Go files with go/ast
   - Rewrite import paths
   - Write back with go/printer

4. **`writeVersionFile(dest string, meta VersionMeta) error`**
   - Write JSON metadata about vendored version

5. **`updateProjectImports(projectRoot string) error`**
   - Update imports in main project files

### Usage After Implementation

```bash
# Vendor a specific version
go run ./tools/vendor-parser -version v0.0.0-20250602201422-b1e23f1025b8

# Vendor from a commit
go run ./tools/vendor-parser -commit b1e23f1025b8

# Vendor from a tag
go run ./tools/vendor-parser -tag v7.1.2

# Check what's currently vendored
cat internal/starlark/VERSION

# Update to latest
go run ./tools/vendor-parser -tag $(gh api repos/bazelbuild/buildtools/releases/latest -q .tag_name)
```

### Maintenance Workflow

```bash
# 1. Check for updates
gh api repos/bazelbuild/buildtools/releases/latest

# 2. Review changelog
gh release view v7.2.0 -R bazelbuild/buildtools

# 3. Vendor new version
go run ./tools/vendor-parser -tag v7.2.0

# 4. Run tests
go test ./...

# 5. Commit
git add internal/starlark/
git commit -m "build: vendor buildtools parser v7.2.0"
```

---

## Option 2: Fork Plan

Create a standalone module: `github.com/albertocavalcante/starlark-parser`

### Fork Structure

```
starlark-parser/
├── build/              # Parser, AST, printer (from buildtools)
├── labels/             # Label parsing (from buildtools)
├── tables/             # Known rules (from buildtools)
├── go.mod              # module github.com/albertocavalcante/starlark-parser
├── LICENSE             # Apache 2.0 (same as buildtools)
├── README.md           # Attribution, usage
└── .github/
    └── workflows/
        └── sync.yml    # Automation to sync from upstream
```

### go.mod (Zero Dependencies!)

```go
module github.com/albertocavalcante/starlark-parser

go 1.21
```

### Sync Automation (.github/workflows/sync.yml)

```yaml
name: Sync from upstream

on:
  schedule:
    - cron: "0 0 * * 0" # Weekly
  workflow_dispatch:
    inputs:
      upstream_ref:
        description: "Upstream ref to sync (tag, commit, or branch)"
        default: "master"

jobs:
  sync:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Fetch upstream
        run: |
          git clone --depth=1 --filter=blob:none --sparse \
            https://github.com/bazelbuild/buildtools.git upstream
          cd upstream
          git sparse-checkout set build labels tables

      - name: Copy packages
        run: |
          rm -rf build labels tables
          cp -r upstream/build upstream/labels upstream/tables .
          rm -rf upstream

      - name: Rewrite imports
        run: |
          find . -name "*.go" -exec sed -i \
            's|github.com/bazelbuild/buildtools|github.com/albertocavalcante/starlark-parser|g' {} +

      - name: Test
        run: go test ./...

      - name: Create PR
        uses: peter-evans/create-pull-request@v5
        with:
          title: "sync: update from upstream buildtools"
          body: "Automated sync from bazelbuild/buildtools"
          branch: sync-upstream
```

### Usage in go-bzlmod

```go
// go.mod
require github.com/albertocavalcante/starlark-parser v1.0.0

// parser.go
import "github.com/albertocavalcante/starlark-parser/build"
```

### Fork Versioning Strategy

| Upstream          | Fork Version | Notes        |
| ----------------- | ------------ | ------------ |
| v7.1.2            | v1.0.0       | Initial fork |
| v7.2.0            | v1.1.0       | Sync update  |
| v8.0.0 (breaking) | v2.0.0       | Major sync   |

### Fork Maintenance Checklist

- [ ] Initial fork with packages extracted
- [ ] Import paths rewritten
- [ ] Tests passing
- [ ] LICENSE file (Apache 2.0)
- [ ] README with attribution
- [ ] GitHub Actions for automated sync
- [ ] Tagged release (v1.0.0)
- [ ] Update go-bzlmod to use fork

---

## Comparison

| Aspect               | Vendoring             | Fork                   |
| -------------------- | --------------------- | ---------------------- |
| **Dependency count** | 0 (internal)          | 1 (your module)        |
| **Maintenance**      | Manual sync           | Automated PR           |
| **Reusability**      | Only this project     | Any project            |
| **Import path**      | Long internal path    | Clean module path      |
| **Setup effort**     | Medium (tool once)    | Medium (repo setup)    |
| **Ongoing effort**   | Run tool occasionally | Merge PRs occasionally |

---

## Recommendation

**Start with vendoring** (Option 1):

1. Faster to implement
2. Zero dependencies immediately
3. Can convert to fork later if needed

**Consider fork later** if:

- You have other projects needing the parser
- You want to contribute improvements upstream
- Community interest in a minimal parser module

---

## Next Steps

1. [ ] Implement `tools/vendor-parser/main.go`
2. [ ] Test vendoring workflow
3. [ ] Remove buildtools dependency
4. [ ] Document in README
5. [ ] (Future) Create fork if beneficial
