package gobzlmod

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestYankedVersionDetection(t *testing.T) {
	// Create mock server that returns yanked version info
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/modules/rules_go/0.41.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "rules_go", version = "0.41.0")
			bazel_dep(name = "bazel_skylib", version = "1.4.0")`)
		case "/modules/bazel_skylib/1.4.0/MODULE.bazel":
			fmt.Fprint(w, `module(name = "bazel_skylib", version = "1.4.0")`)
		case "/modules/rules_go/metadata.json":
			metadata := map[string]any{
				"versions":        []string{"0.40.0", "0.41.0"},
				"yanked_versions": map[string]string{},
			}
			json.NewEncoder(w).Encode(metadata)
		case "/modules/bazel_skylib/metadata.json":
			metadata := map[string]any{
				"versions": []string{"1.3.0", "1.4.0", "1.5.0"},
				"yanked_versions": map[string]string{
					"1.4.0": "Critical bug in skylib 1.4.0, upgrade to 1.5.0",
				},
			}
			json.NewEncoder(w).Encode(metadata)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	moduleContent := `module(name = "test", version = "1.0.0")
bazel_dep(name = "rules_go", version = "0.41.0")`

	t.Run("CheckYanked=false does not populate yanked info", func(t *testing.T) {
		opts := ResolutionOptions{
			Registries:     []string{server.URL},
			IncludeDevDeps: false,
			CheckYanked:    false,
		}

		list, err := ResolveContent(context.Background(), moduleContent, opts)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		for _, m := range list.Modules {
			if m.Yanked {
				t.Errorf("module %s@%s should not be marked as yanked when CheckYanked=false", m.Name, m.Version)
			}
		}
	})

	t.Run("CheckYanked=true with YankedVersionAllow", func(t *testing.T) {
		opts := ResolutionOptions{
			Registries:     []string{server.URL},
			IncludeDevDeps: false,
			CheckYanked:    true,
			YankedBehavior: YankedVersionAllow,
		}

		list, err := ResolveContent(context.Background(), moduleContent, opts)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		foundYanked := false
		for _, m := range list.Modules {
			if m.Name == "bazel_skylib" && m.Version == "1.4.0" {
				if !m.Yanked {
					t.Error("bazel_skylib@1.4.0 should be marked as yanked")
				}
				if m.YankReason == "" {
					t.Error("yanked module should have a YankReason")
				}
				foundYanked = true
			}
		}
		if !foundYanked {
			t.Error("bazel_skylib@1.4.0 should be in resolved modules")
		}

		if list.Summary.YankedModules != 1 {
			t.Errorf("Summary.YankedModules = %d, want 1", list.Summary.YankedModules)
		}

		// Should have no warnings with YankedVersionAllow
		if len(list.Warnings) != 0 {
			t.Errorf("expected no warnings with YankedVersionAllow, got %d", len(list.Warnings))
		}
	})

	t.Run("CheckYanked=true with YankedVersionWarn", func(t *testing.T) {
		opts := ResolutionOptions{
			Registries:     []string{server.URL},
			IncludeDevDeps: false,
			CheckYanked:    true,
			YankedBehavior: YankedVersionWarn,
		}

		list, err := ResolveContent(context.Background(), moduleContent, opts)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(list.Warnings) == 0 {
			t.Error("expected warning for yanked module")
		}

		hasYankWarning := false
		for _, w := range list.Warnings {
			if strings.Contains(w, "bazel_skylib@1.4.0") && strings.Contains(w, "yanked") {
				hasYankWarning = true
				break
			}
		}
		if !hasYankWarning {
			t.Error("expected warning to mention bazel_skylib@1.4.0 as yanked")
		}
	})

	t.Run("CheckYanked=true with YankedVersionError", func(t *testing.T) {
		opts := ResolutionOptions{
			Registries:     []string{server.URL},
			IncludeDevDeps: false,
			CheckYanked:    true,
			YankedBehavior: YankedVersionError,
		}

		_, err := ResolveContent(context.Background(), moduleContent, opts)
		if err == nil {
			t.Fatal("expected error for yanked version, got nil")
		}

		var yankedErr *YankedVersionsError
		if !isYankedError(err, &yankedErr) {
			t.Fatalf("expected YankedVersionsError, got %T: %v", err, err)
		}

		if len(yankedErr.Modules) != 1 {
			t.Errorf("expected 1 yanked module in error, got %d", len(yankedErr.Modules))
		}

		if yankedErr.Modules[0].Name != "bazel_skylib" {
			t.Errorf("expected bazel_skylib in error, got %s", yankedErr.Modules[0].Name)
		}
	})
}

func TestYankedVersionsError_Message(t *testing.T) {
	t.Run("single module", func(t *testing.T) {
		err := &YankedVersionsError{
			Modules: []ModuleToResolve{
				{Name: "foo", Version: "1.0.0", YankReason: "security issue"},
			},
		}
		msg := err.Error()
		if !strings.Contains(msg, "foo@1.0.0") {
			t.Error("error message should contain module name and version")
		}
		if !strings.Contains(msg, "security issue") {
			t.Error("error message should contain yank reason")
		}
	})

	t.Run("multiple modules", func(t *testing.T) {
		err := &YankedVersionsError{
			Modules: []ModuleToResolve{
				{Name: "foo", Version: "1.0.0", YankReason: "bug"},
				{Name: "bar", Version: "2.0.0", YankReason: "deprecated"},
			},
		}
		msg := err.Error()
		if !strings.Contains(msg, "2 yanked") {
			t.Error("error message should mention count of yanked versions")
		}
		if !strings.Contains(msg, "foo@1.0.0") || !strings.Contains(msg, "bar@2.0.0") {
			t.Error("error message should list all yanked modules")
		}
	})
}

func TestRegistryClient_GetModuleMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/modules/rules_go/metadata.json" {
			metadata := map[string]any{
				"homepage":    "https://github.com/bazelbuild/rules_go",
				"versions":    []string{"0.40.0", "0.41.0", "0.42.0"},
				"maintainers": []map[string]any{{"github": "maintainer1"}},
				"yanked_versions": map[string]string{
					"0.40.0": "Critical bug",
				},
			}
			json.NewEncoder(w).Encode(metadata)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := newRegistryClient(server.URL)

	t.Run("fetch metadata successfully", func(t *testing.T) {
		metadata, err := client.GetModuleMetadata(context.Background(), "rules_go")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if metadata.Homepage != "https://github.com/bazelbuild/rules_go" {
			t.Errorf("homepage = %s, want https://github.com/bazelbuild/rules_go", metadata.Homepage)
		}

		if len(metadata.Versions) != 3 {
			t.Errorf("expected 3 versions, got %d", len(metadata.Versions))
		}

		if !metadata.IsYanked("0.40.0") {
			t.Error("0.40.0 should be yanked")
		}

		if metadata.IsYanked("0.41.0") {
			t.Error("0.41.0 should not be yanked")
		}

		if metadata.YankReason("0.40.0") != "Critical bug" {
			t.Errorf("yank reason = %s, want 'Critical bug'", metadata.YankReason("0.40.0"))
		}
	})

	t.Run("caching works", func(t *testing.T) {
		// First call
		_, err := client.GetModuleMetadata(context.Background(), "rules_go")
		if err != nil {
			t.Fatalf("first call error: %v", err)
		}

		// Second call should use cache (server won't be hit)
		_, err = client.GetModuleMetadata(context.Background(), "rules_go")
		if err != nil {
			t.Fatalf("second call error: %v", err)
		}
	})

	t.Run("module not found", func(t *testing.T) {
		_, err := client.GetModuleMetadata(context.Background(), "nonexistent")
		if err == nil {
			t.Error("expected error for nonexistent module")
		}
	})
}

// isYankedError checks if err is or wraps a YankedVersionsError
func isYankedError(err error, target **YankedVersionsError) bool {
	if ye, ok := err.(*YankedVersionsError); ok {
		*target = ye
		return true
	}
	return false
}
