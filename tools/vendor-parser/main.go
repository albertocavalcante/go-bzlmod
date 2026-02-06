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
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"time"
)

const (
	sourceRepo     = "github.com/bazelbuild/buildtools"
	destImportPath = "github.com/albertocavalcante/go-bzlmod/third_party/buildtools"
	destDir        = "third_party/buildtools"
)

var packagesToVendor = []string{"build", "labels", "tables"}

//go:embed templates/README.md.tmpl
var readmeTemplate string

// VersionInfo holds metadata about the vendored code.
type VersionInfo struct {
	Source     string   `json:"source"`
	Ref        string   `json:"ref"`
	VendoredAt string   `json:"vendored_at"`
	Packages   []string `json:"packages"`
	Note       string   `json:"note,omitempty"`
}

// TemplateData contains all data available to the README template.
type TemplateData struct {
	Source         string
	Ref            string
	VendoredAt     string
	Packages       []string
	DestImportPath string
}

func main() {
	version := flag.String("version", "", "Go module version (e.g., v0.0.0-20250602201422-b1e23f1025b8)")
	commit := flag.String("commit", "", "Git commit hash")
	tag := flag.String("tag", "", "Git tag (e.g., v7.1.2)")
	keepTests := flag.Bool("keep-tests", false, "Keep test files")
	flag.Parse()

	// Determine the ref to use
	ref := determineRef(*version, *commit, *tag)
	if ref == "" {
		fmt.Fprintln(os.Stderr, "Error: one of -version, -commit, or -tag is required")
		flag.Usage()
		os.Exit(1)
	}

	fmt.Printf("Vendoring buildtools parser (ref: %s)\n", ref)

	// Find project root (where go.mod is)
	root, err := findProjectRoot()
	if err != nil {
		fatalf("Error finding project root: %v", err)
	}

	destPath := filepath.Join(root, destDir)

	// Download and extract
	if err := downloadAndExtract(ref, destPath, *keepTests); err != nil {
		fatalf("Error downloading/extracting: %v", err)
	}

	// Rewrite imports in all .go files
	if err := rewriteImports(destPath); err != nil {
		fatalf("Error rewriting imports: %v", err)
	}

	// Prepare version info
	versionInfo := VersionInfo{
		Source:     sourceRepo,
		Ref:        ref,
		VendoredAt: time.Now().UTC().Format(time.RFC3339),
		Packages:   packagesToVendor,
	}

	// Write VERSION file
	if err := writeVersionFile(destPath, versionInfo); err != nil {
		fatalf("Error writing VERSION file: %v", err)
	}

	// Fetch LICENSE from upstream
	if err := fetchLicense(destPath); err != nil {
		fatalf("Error fetching LICENSE: %v", err)
	}

	// Write README.md from template
	if err := writeReadme(destPath, versionInfo); err != nil {
		fatalf("Error writing README.md: %v", err)
	}

	fmt.Printf("Successfully vendored packages to %s\n", destPath)
	fmt.Printf("Packages: %v\n", packagesToVendor)
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

// determineRef determines the git ref to use based on flags.
func determineRef(version, commit, tag string) string {
	switch {
	case version != "":
		if ref := extractCommitFromVersion(version); ref != "" {
			return ref
		}
		return version
	case commit != "":
		return commit
	case tag != "":
		return tag
	default:
		return ""
	}
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

// downloadAndExtract downloads the tarball and extracts needed packages.
func downloadAndExtract(ref, destPath string, keepTests bool) error {
	url := fmt.Sprintf("https://github.com/bazelbuild/buildtools/archive/%s.tar.gz", ref)
	fmt.Printf("Downloading from: %s\n", url)

	resp, err := http.Get(url) //nolint:gosec // URL is constructed from user input, intentional
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}

	// Clean and recreate destination directory
	if err := os.RemoveAll(destPath); err != nil {
		return fmt.Errorf("clean destination: %w", err)
	}
	if err := os.MkdirAll(destPath, 0o755); err != nil {
		return fmt.Errorf("create destination: %w", err)
	}

	return extractTarball(resp.Body, destPath, keepTests)
}

// extractTarball extracts only the needed packages from the tarball
func extractTarball(r io.Reader, destPath string, keepTests bool) error {
	gzr, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("gzip reader: %w", err)
	}
	defer func() { _ = gzr.Close() }()

	tr := tar.NewReader(gzr)

	// Build a set of package prefixes to extract
	pkgSet := make(map[string]bool)
	for _, pkg := range packagesToVendor {
		pkgSet[pkg] = true
	}

	// Use os.Root for secure file operations within destPath (Go 1.24+)
	// This prevents path traversal attacks by restricting all operations to destPath
	root, err := os.OpenRoot(destPath)
	if err != nil {
		return fmt.Errorf("open root %s: %w", destPath, err)
	}
	defer root.Close()

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

		// Build relative path within root
		localPath := filepath.Join(pkg, relPath)

		// Validate path is local (no .. or absolute paths) using Go 1.20+ filepath.IsLocal
		if !filepath.IsLocal(localPath) {
			return fmt.Errorf("invalid file path in archive (path traversal attempt): %s", header.Name)
		}

		// Create parent directory using root-relative operations
		destDir := filepath.Dir(localPath)
		if err := root.Mkdir(destDir, 0o755); err != nil && !os.IsExist(err) {
			// MkdirAll equivalent: create parent directories
			if err := mkdirAllInRoot(root, destDir, 0o755); err != nil {
				return fmt.Errorf("mkdir %s: %w", destDir, err)
			}
		}

		// Read file content
		content, err := io.ReadAll(tr)
		if err != nil {
			return fmt.Errorf("read %s: %w", header.Name, err)
		}

		// Write file using root-relative path (secure against path traversal)
		if err := writeFileInRoot(root, localPath, content, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", localPath, err)
		}

		filesWritten++
		fmt.Printf("  Extracted: %s/%s\n", pkg, relPath)
	}

	fmt.Printf("Extracted %d files\n", filesWritten)
	return nil
}

// mkdirAllInRoot creates a directory and all parent directories within an os.Root
func mkdirAllInRoot(root *os.Root, path string, perm os.FileMode) error {
	parts := strings.Split(filepath.Clean(path), string(filepath.Separator))
	current := ""
	for _, part := range parts {
		if part == "" {
			continue
		}
		if current == "" {
			current = part
		} else {
			current = filepath.Join(current, part)
		}
		if err := root.Mkdir(current, perm); err != nil && !os.IsExist(err) {
			return err
		}
	}
	return nil
}

// writeFileInRoot writes a file within an os.Root
func writeFileInRoot(root *os.Root, path string, data []byte, perm os.FileMode) error {
	f, err := root.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	_, writeErr := f.Write(data)
	closeErr := f.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
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

// writeVersionFile writes the VERSION JSON file.
func writeVersionFile(destPath string, info VersionInfo) error {
	versionFile := filepath.Join(destPath, "VERSION")
	versionData, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	if err := os.WriteFile(versionFile, append(versionData, '\n'), 0o644); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	fmt.Println("  Wrote VERSION file")
	return nil
}

// fetchLicense downloads the LICENSE file from the upstream repository
func fetchLicense(destPath string) error {
	url := "https://raw.githubusercontent.com/bazelbuild/buildtools/master/LICENSE"
	fmt.Printf("Fetching LICENSE from: %s\n", url)

	resp, err := http.Get(url) //nolint:gosec // URL is constant
	if err != nil {
		return fmt.Errorf("fetch: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read: %w", err)
	}

	licensePath := filepath.Join(destPath, "LICENSE")
	if err := os.WriteFile(licensePath, content, 0o644); err != nil {
		return fmt.Errorf("write: %w", err)
	}

	fmt.Println("  Wrote LICENSE file")
	return nil
}

// writeReadme renders the README template and writes it to the destination.
func writeReadme(destPath string, info VersionInfo) error {
	tmpl, err := template.New("readme").Parse(readmeTemplate)
	if err != nil {
		return fmt.Errorf("parse template: %w", err)
	}

	data := TemplateData{
		Source:         info.Source,
		Ref:            info.Ref,
		VendoredAt:     info.VendoredAt,
		Packages:       info.Packages,
		DestImportPath: destImportPath,
	}

	readmePath := filepath.Join(destPath, "README.md")
	f, err := os.Create(readmePath)
	if err != nil {
		return fmt.Errorf("create: %w", err)
	}
	defer func() { _ = f.Close() }()

	if err := tmpl.Execute(f, data); err != nil {
		return fmt.Errorf("execute template: %w", err)
	}

	fmt.Println("  Wrote README.md")
	return nil
}
