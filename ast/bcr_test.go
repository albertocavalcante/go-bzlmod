package ast

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
)

// TestParseBCR tests parsing all MODULE.bazel files from a local BCR checkout.
// Set BCR_PATH environment variable to point to your local bazel-central-registry clone.
// Example: BCR_PATH=/path/to/bazel-central-registry go test -v -run TestParseBCR
func TestParseBCR(t *testing.T) {
	bcrPath := os.Getenv("BCR_PATH")
	if bcrPath == "" {
		t.Skip("BCR_PATH not set. Set it to a local bazel-central-registry clone to run this test.")
	}

	modulesDir := filepath.Join(bcrPath, "modules")
	if _, err := os.Stat(modulesDir); os.IsNotExist(err) {
		t.Fatalf("BCR modules directory not found: %s", modulesDir)
	}

	// Find all MODULE.bazel files
	var moduleFiles []string
	err := filepath.Walk(modulesDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.Name() == "MODULE.bazel" {
			moduleFiles = append(moduleFiles, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Failed to walk BCR modules: %v", err)
	}

	t.Logf("Found %d MODULE.bazel files in BCR", len(moduleFiles))

	// Parse in parallel for speed
	var (
		successCount int64
		failCount    int64
		failures     []string
		failuresMu   sync.Mutex
	)

	numWorkers := runtime.NumCPU()
	jobs := make(chan string, len(moduleFiles))
	var wg sync.WaitGroup

	for range numWorkers {
		wg.Go(func() {
			for path := range jobs {
				content, err := os.ReadFile(path)
				if err != nil {
					failuresMu.Lock()
					failures = append(failures, fmt.Sprintf("%s: read error: %v", path, err))
					failuresMu.Unlock()
					atomic.AddInt64(&failCount, 1)
					continue
				}

				result, err := ParseContent(path, content)
				if err != nil {
					failuresMu.Lock()
					failures = append(failures, fmt.Sprintf("%s: parse error: %v", path, err))
					failuresMu.Unlock()
					atomic.AddInt64(&failCount, 1)
					continue
				}

				// Check for parse errors in result
				if result.HasErrors() {
					failuresMu.Lock()
					for _, e := range result.Errors {
						failures = append(failures, fmt.Sprintf("%s: %v", path, e))
					}
					failuresMu.Unlock()
					atomic.AddInt64(&failCount, 1)
					continue
				}

				atomic.AddInt64(&successCount, 1)
			}
		})
	}

	for _, path := range moduleFiles {
		jobs <- path
	}
	close(jobs)
	wg.Wait()

	t.Logf("Results: %d success, %d failed out of %d total",
		successCount, failCount, len(moduleFiles))

	if len(failures) > 0 {
		t.Logf("First %d failures:", min(10, len(failures)))
		for i, f := range failures {
			if i >= 10 {
				break
			}
			t.Logf("  %s", f)
		}
		if len(failures) > 10 {
			t.Logf("  ... and %d more failures", len(failures)-10)
		}
	}

	// Calculate success rate
	successRate := float64(successCount) / float64(len(moduleFiles)) * 100
	t.Logf("Success rate: %.2f%%", successRate)

	// Fail if success rate is below threshold (allow some failures for edge cases)
	if successRate < 95.0 {
		t.Errorf("Success rate %.2f%% is below 95%% threshold", successRate)
	}
}

// TestParseBCRStrict is like TestParseBCR but requires 100% success rate.
func TestParseBCRStrict(t *testing.T) {
	bcrPath := os.Getenv("BCR_PATH")
	if bcrPath == "" {
		t.Skip("BCR_PATH not set. Set it to a local bazel-central-registry clone to run this test.")
	}

	modulesDir := filepath.Join(bcrPath, "modules")
	if _, err := os.Stat(modulesDir); os.IsNotExist(err) {
		t.Fatalf("BCR modules directory not found: %s", modulesDir)
	}

	// Find all MODULE.bazel files
	var moduleFiles []string
	err := filepath.Walk(modulesDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.Name() == "MODULE.bazel" {
			moduleFiles = append(moduleFiles, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Failed to walk BCR modules: %v", err)
	}

	t.Logf("Found %d MODULE.bazel files in BCR", len(moduleFiles))

	var failures []string

	for _, path := range moduleFiles {
		content, err := os.ReadFile(path)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: read error: %v", path, err))
			continue
		}

		result, err := ParseContent(path, content)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: parse error: %v", path, err))
			continue
		}

		if result.HasErrors() {
			for _, e := range result.Errors {
				failures = append(failures, fmt.Sprintf("%s: %v", path, e))
			}
		}
	}

	if len(failures) > 0 {
		t.Errorf("Found %d failures:", len(failures))
		for _, f := range failures {
			t.Logf("  %s", f)
		}
	} else {
		t.Logf("All %d MODULE.bazel files parsed successfully!", len(moduleFiles))
	}
}
