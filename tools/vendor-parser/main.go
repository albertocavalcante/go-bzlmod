// Command vendor-parser vendors the buildtools parser packages into the project.
//
// It downloads the specified version from GitHub, extracts only the needed packages,
// rewrites imports to use the local vendored path, and writes a VERSION file.
//
// Usage:
//
//	go run ./tools/vendor-parser -version v0.0.0-20250602201422-b1e23f1025b8
//	go run ./tools/vendor-parser -tag v7.1.2
//	go run ./tools/vendor-parser -commit b1e23f1025b8
package main

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	sourceRepo     = "github.com/bazelbuild/buildtools"
	destImportPath = "github.com/albertocavalcante/go-bzlmod/third_party/buildtools"
	destDir        = "third_party/buildtools"
)

var packagesToVendor = []string{"build", "labels", "tables"}

// VersionInfo holds metadata about the vendored code.
type VersionInfo struct {
	Source     string   `json:"source"`
	Ref        string   `json:"ref"`
	VendoredAt string   `json:"vendored_at"`
	Packages   []string `json:"packages"`
	Note       string   `json:"note"`
}

func main() {
	version := flag.String("version", "", "Go module version (e.g., v0.0.0-20250602201422-b1e23f1025b8)")
	commit := flag.String("commit", "", "Git commit hash")
	tag := flag.String("tag", "", "Git tag (e.g., v7.1.2)")
	keepTests := flag.Bool("keep-tests", false, "Keep test files")
	flag.Parse()

	// Determine the ref to use
	ref := ""
	switch {
	case *version != "":
		ref = extractCommitFromVersion(*version)
		if ref == "" {
			ref = *version
		}
	case *commit != "":
		ref = *commit
	case *tag != "":
		ref = *tag
	default:
		fmt.Fprintln(os.Stderr, "Error: one of -version, -commit, or -tag is required")
		flag.Usage()
		os.Exit(1)
	}

	fmt.Printf("Vendoring buildtools parser (ref: %s)\n", ref)

	// Find project root (where go.mod is)
	root, err := findProjectRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error finding project root: %v\n", err)
		os.Exit(1)
	}

	// Download tarball
	url := fmt.Sprintf("https://github.com/bazelbuild/buildtools/archive/%s.tar.gz", ref)
	fmt.Printf("Downloading from: %s\n", url)

	resp, err := http.Get(url)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error downloading: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "Error: HTTP %d from %s\n", resp.StatusCode, url)
		os.Exit(1)
	}

	// Create destination directory
	destPath := filepath.Join(root, destDir)
	if err := os.RemoveAll(destPath); err != nil {
		fmt.Fprintf(os.Stderr, "Error cleaning destination: %v\n", err)
		os.Exit(1)
	}
	if err := os.MkdirAll(destPath, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "Error creating destination: %v\n", err)
		os.Exit(1)
	}

	// Extract and process tarball
	if err := extractTarball(resp.Body, destPath, *keepTests); err != nil {
		fmt.Fprintf(os.Stderr, "Error extracting: %v\n", err)
		os.Exit(1)
	}

	// Rewrite imports in all .go files
	if err := rewriteImports(destPath); err != nil {
		fmt.Fprintf(os.Stderr, "Error rewriting imports: %v\n", err)
		os.Exit(1)
	}

	// Write VERSION file
	versionInfo := VersionInfo{
		Source:     sourceRepo,
		Ref:        ref,
		VendoredAt: time.Now().UTC().Format(time.RFC3339),
		Packages:   packagesToVendor,
		Note:       "Subset of buildtools containing only the Starlark parser",
	}
	versionFile := filepath.Join(destPath, "VERSION")
	versionData, err := json.MarshalIndent(versionInfo, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error marshaling version info: %v\n", err)
		os.Exit(1)
	}
	if err := os.WriteFile(versionFile, append(versionData, '\n'), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing VERSION file: %v\n", err)
		os.Exit(1)
	}

	// Fetch LICENSE from upstream
	if err := fetchLicense(destPath); err != nil {
		fmt.Fprintf(os.Stderr, "Error fetching LICENSE: %v\n", err)
		os.Exit(1)
	}

	// Write README.md
	if err := writeReadme(destPath); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing README.md: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Successfully vendored packages to %s\n", destPath)
	fmt.Printf("Packages: %v\n", packagesToVendor)
}

// extractCommitFromVersion extracts the commit hash from a pseudo-version.
// Format: v0.0.0-YYYYMMDDHHMMSS-<commit>
func extractCommitFromVersion(version string) string {
	parts := strings.Split(version, "-")
	if len(parts) >= 3 {
		return parts[len(parts)-1]
	}
	return ""
}

// findProjectRoot finds the project root by looking for go.mod
func findProjectRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found")
		}
		dir = parent
	}
}

// extractTarball extracts only the needed packages from the tarball
func extractTarball(r io.Reader, destPath string, keepTests bool) error {
	gzr, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("gzip reader: %w", err)
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)

	// Build a set of package prefixes to extract
	pkgSet := make(map[string]bool)
	for _, pkg := range packagesToVendor {
		pkgSet[pkg] = true
	}

	filesWritten := 0
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("tar read: %w", err)
		}

		// Skip directories (we'll create them as needed)
		if header.Typeflag == tar.TypeDir {
			continue
		}

		// Parse the path: buildtools-<ref>/<package>/...
		parts := strings.SplitN(header.Name, "/", 3)
		if len(parts) < 3 {
			continue
		}
		pkg := parts[1]
		relPath := parts[2]

		// Only extract packages we want
		if !pkgSet[pkg] {
			continue
		}

		// Skip testdata directories
		if strings.Contains(relPath, "testdata/") || strings.HasPrefix(relPath, "testdata") {
			continue
		}

		// Skip test files unless -keep-tests
		if !keepTests && strings.HasSuffix(relPath, "_test.go") {
			continue
		}

		// Only extract .go files
		if !strings.HasSuffix(relPath, ".go") {
			continue
		}

		// Create full destination path
		destFile := filepath.Join(destPath, pkg, relPath)
		destDir := filepath.Dir(destFile)
		if err := os.MkdirAll(destDir, 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w", destDir, err)
		}

		// Read file content
		content, err := io.ReadAll(tr)
		if err != nil {
			return fmt.Errorf("read %s: %w", header.Name, err)
		}

		// Write file
		if err := os.WriteFile(destFile, content, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", destFile, err)
		}

		filesWritten++
		fmt.Printf("  Extracted: %s/%s\n", pkg, relPath)
	}

	fmt.Printf("Extracted %d files\n", filesWritten)
	return nil
}

// rewriteImports rewrites buildtools imports to use the vendored path
func rewriteImports(destPath string) error {
	// Import patterns to rewrite
	oldImports := []string{
		`"github.com/bazelbuild/buildtools/build"`,
		`"github.com/bazelbuild/buildtools/labels"`,
		`"github.com/bazelbuild/buildtools/tables"`,
	}
	newImports := []string{
		fmt.Sprintf(`"%s/build"`, destImportPath),
		fmt.Sprintf(`"%s/labels"`, destImportPath),
		fmt.Sprintf(`"%s/tables"`, destImportPath),
	}

	filesProcessed := 0
	err := filepath.Walk(destPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}

		original := string(content)
		modified := original

		for i, oldImport := range oldImports {
			modified = strings.ReplaceAll(modified, oldImport, newImports[i])
		}

		if modified != original {
			if err := os.WriteFile(path, []byte(modified), 0o644); err != nil {
				return fmt.Errorf("write %s: %w", path, err)
			}
			relPath, _ := filepath.Rel(destPath, path)
			fmt.Printf("  Rewrote imports in: %s\n", relPath)
			filesProcessed++
		}

		return nil
	})

	if err != nil {
		return err
	}

	fmt.Printf("Rewrote imports in %d files\n", filesProcessed)
	return nil
}

// fetchLicense downloads the LICENSE file from the upstream repository
func fetchLicense(destPath string) error {
	url := "https://raw.githubusercontent.com/bazelbuild/buildtools/master/LICENSE"
	fmt.Printf("Fetching LICENSE from: %s\n", url)

	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("fetch license: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fetch license: HTTP %d", resp.StatusCode)
	}

	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read license: %w", err)
	}

	licensePath := filepath.Join(destPath, "LICENSE")
	if err := os.WriteFile(licensePath, content, 0o644); err != nil {
		return fmt.Errorf("write license: %w", err)
	}

	fmt.Println("  Wrote LICENSE file")
	return nil
}

// writeReadme creates a README.md explaining the vendored code
func writeReadme(destPath string) error {
	readme := `# Vendored buildtools Parser

This directory contains a **subset** of [bazelbuild/buildtools](https://github.com/bazelbuild/buildtools).

## Contents

Only the following packages are included:

- ` + "`build/`" + ` - Starlark/BUILD file parser
- ` + "`labels/`" + ` - Bazel label parsing utilities
- ` + "`tables/`" + ` - Parser configuration tables

## License

This code is licensed under the Apache License 2.0, the same license as the original
buildtools repository. See the [LICENSE](LICENSE) file for details.

## Why Vendor?

This library vendors the parser to achieve **zero external runtime dependencies**.
Only the parser packages are needed; the full buildtools repository includes many
other tools (buildifier, buildozer, etc.) that are not required.

## Updating

To update the vendored code, run:

` + "```bash" + `
go run ./tools/vendor-parser -commit <commit-hash>
# or
go run ./tools/vendor-parser -tag <tag>
` + "```" + `

Then update all imports in the project if the destination path has changed.

## Version

See the [VERSION](VERSION) file for information about the vendored version.
`

	readmePath := filepath.Join(destPath, "README.md")
	if err := os.WriteFile(readmePath, []byte(readme), 0o644); err != nil {
		return fmt.Errorf("write readme: %w", err)
	}

	fmt.Println("  Wrote README.md")
	return nil
}
