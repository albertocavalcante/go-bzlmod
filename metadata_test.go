package gobzlmod

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCheckModuleMetadata(t *testing.T) {
	// Create mock server that returns metadata
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/modules/yanked_module/metadata.json":
			metadata := map[string]interface{}{
				"versions": []string{"1.0.0", "1.1.0"},
				"yanked_versions": map[string]string{
					"1.0.0": "Critical security issue",
				},
			}
			json.NewEncoder(w).Encode(metadata)
		case "/modules/deprecated_module/metadata.json":
			metadata := map[string]interface{}{
				"versions":   []string{"2.0.0"},
				"deprecated": "Use new_module instead",
			}
			json.NewEncoder(w).Encode(metadata)
		case "/modules/both_yanked_deprecated/metadata.json":
			metadata := map[string]interface{}{
				"versions": []string{"3.0.0"},
				"yanked_versions": map[string]string{
					"3.0.0": "Broken build",
				},
				"deprecated": "Package is no longer maintained",
			}
			json.NewEncoder(w).Encode(metadata)
		case "/modules/normal_module/metadata.json":
			metadata := map[string]interface{}{
				"versions": []string{"4.0.0"},
			}
			json.NewEncoder(w).Encode(metadata)
		case "/modules/missing_metadata/metadata.json":
			// Simulate missing metadata (fail-open pattern)
			w.WriteHeader(http.StatusNotFound)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	registry := newRegistryClient(server.URL)

	t.Run("marks yanked module correctly", func(t *testing.T) {
		list := &ResolutionList{
			Modules: []ModuleToResolve{
				{Name: "yanked_module", Version: "1.0.0"},
				{Name: "yanked_module", Version: "1.1.0"},
			},
		}

		opts := ResolutionOptions{
			CheckYanked:         true,
			AllowYankedVersions: nil,
		}

		checkModuleMetadata(context.Background(), registry, opts, list)

		if !list.Modules[0].Yanked {
			t.Error("yanked_module@1.0.0 should be marked as yanked")
		}
		if list.Modules[0].YankReason != "Critical security issue" {
			t.Errorf("unexpected yank reason: %s", list.Modules[0].YankReason)
		}
		if list.Modules[1].Yanked {
			t.Error("yanked_module@1.1.0 should not be marked as yanked")
		}
	})

	t.Run("marks deprecated module correctly", func(t *testing.T) {
		list := &ResolutionList{
			Modules: []ModuleToResolve{
				{Name: "deprecated_module", Version: "2.0.0"},
			},
		}

		opts := ResolutionOptions{
			WarnDeprecated: true,
		}

		checkModuleMetadata(context.Background(), registry, opts, list)

		if !list.Modules[0].IsDeprecated {
			t.Error("deprecated_module should be marked as deprecated")
		}
		if list.Modules[0].DeprecationReason != "Use new_module instead" {
			t.Errorf("unexpected deprecation reason: %s", list.Modules[0].DeprecationReason)
		}
	})

	t.Run("marks module with both yanked and deprecated", func(t *testing.T) {
		list := &ResolutionList{
			Modules: []ModuleToResolve{
				{Name: "both_yanked_deprecated", Version: "3.0.0"},
			},
		}

		opts := ResolutionOptions{
			CheckYanked:    true,
			WarnDeprecated: true,
		}

		checkModuleMetadata(context.Background(), registry, opts, list)

		if !list.Modules[0].Yanked {
			t.Error("both_yanked_deprecated should be marked as yanked")
		}
		if !list.Modules[0].IsDeprecated {
			t.Error("both_yanked_deprecated should be marked as deprecated")
		}
		if list.Modules[0].YankReason != "Broken build" {
			t.Errorf("unexpected yank reason: %s", list.Modules[0].YankReason)
		}
		if list.Modules[0].DeprecationReason != "Package is no longer maintained" {
			t.Errorf("unexpected deprecation reason: %s", list.Modules[0].DeprecationReason)
		}
	})

	t.Run("does not mark normal module", func(t *testing.T) {
		list := &ResolutionList{
			Modules: []ModuleToResolve{
				{Name: "normal_module", Version: "4.0.0"},
			},
		}

		opts := ResolutionOptions{
			CheckYanked:    true,
			WarnDeprecated: true,
		}

		checkModuleMetadata(context.Background(), registry, opts, list)

		if list.Modules[0].Yanked {
			t.Error("normal_module should not be marked as yanked")
		}
		if list.Modules[0].IsDeprecated {
			t.Error("normal_module should not be marked as deprecated")
		}
	})

	t.Run("respects AllowYankedVersions with specific module", func(t *testing.T) {
		list := &ResolutionList{
			Modules: []ModuleToResolve{
				{Name: "yanked_module", Version: "1.0.0"},
			},
		}

		opts := ResolutionOptions{
			CheckYanked:         true,
			AllowYankedVersions: []string{"yanked_module@1.0.0"},
		}

		checkModuleMetadata(context.Background(), registry, opts, list)

		if list.Modules[0].Yanked {
			t.Error("yanked_module@1.0.0 should not be marked as yanked when in AllowYankedVersions")
		}
	})

	t.Run("respects AllowYankedVersions with 'all'", func(t *testing.T) {
		list := &ResolutionList{
			Modules: []ModuleToResolve{
				{Name: "yanked_module", Version: "1.0.0"},
			},
		}

		opts := ResolutionOptions{
			CheckYanked:         true,
			AllowYankedVersions: []string{"all"},
		}

		checkModuleMetadata(context.Background(), registry, opts, list)

		if list.Modules[0].Yanked {
			t.Error("yanked_module@1.0.0 should not be marked as yanked when 'all' is in AllowYankedVersions")
		}
	})

	t.Run("fail-open pattern: missing metadata does not block resolution", func(t *testing.T) {
		list := &ResolutionList{
			Modules: []ModuleToResolve{
				{Name: "missing_metadata", Version: "1.0.0"},
			},
		}

		opts := ResolutionOptions{
			CheckYanked:    true,
			WarnDeprecated: true,
		}

		// Should not panic or error
		checkModuleMetadata(context.Background(), registry, opts, list)

		// Module should not be marked as yanked or deprecated
		if list.Modules[0].Yanked {
			t.Error("module with missing metadata should not be marked as yanked")
		}
		if list.Modules[0].IsDeprecated {
			t.Error("module with missing metadata should not be marked as deprecated")
		}
	})

	t.Run("concurrent metadata fetching for multiple modules", func(t *testing.T) {
		list := &ResolutionList{
			Modules: []ModuleToResolve{
				{Name: "yanked_module", Version: "1.0.0"},
				{Name: "deprecated_module", Version: "2.0.0"},
				{Name: "normal_module", Version: "4.0.0"},
				{Name: "both_yanked_deprecated", Version: "3.0.0"},
			},
		}

		opts := ResolutionOptions{
			CheckYanked:    true,
			WarnDeprecated: true,
		}

		checkModuleMetadata(context.Background(), registry, opts, list)

		// Verify all modules were processed correctly
		if !list.Modules[0].Yanked {
			t.Error("yanked_module should be marked as yanked")
		}
		if !list.Modules[1].IsDeprecated {
			t.Error("deprecated_module should be marked as deprecated")
		}
		if list.Modules[2].Yanked || list.Modules[2].IsDeprecated {
			t.Error("normal_module should not be marked")
		}
		if !list.Modules[3].Yanked || !list.Modules[3].IsDeprecated {
			t.Error("both_yanked_deprecated should be marked as both")
		}
	})
}

func TestBuildAllowedYankedSet(t *testing.T) {
	t.Run("empty list returns nil", func(t *testing.T) {
		result := buildAllowedYankedSet(nil)
		if result != nil {
			t.Error("expected nil for empty list")
		}

		result = buildAllowedYankedSet([]string{})
		if result != nil {
			t.Error("expected nil for empty slice")
		}
	})

	t.Run("creates set from list", func(t *testing.T) {
		result := buildAllowedYankedSet([]string{"foo@1.0.0", "bar@2.0.0", "all"})
		if result == nil {
			t.Fatal("expected non-nil map")
		}
		if len(result) != 3 {
			t.Errorf("expected 3 entries, got %d", len(result))
		}
		if !result["foo@1.0.0"] {
			t.Error("expected foo@1.0.0 in set")
		}
		if !result["bar@2.0.0"] {
			t.Error("expected bar@2.0.0 in set")
		}
		if !result["all"] {
			t.Error("expected all in set")
		}
	})
}
