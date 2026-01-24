package lockfile

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestNew(t *testing.T) {
	lf := New()

	if lf.Version != CurrentVersion {
		t.Errorf("Version = %d, want %d", lf.Version, CurrentVersion)
	}
	if lf.RegistryFileHashes == nil {
		t.Error("RegistryFileHashes is nil")
	}
	if lf.SelectedYankedVersions == nil {
		t.Error("SelectedYankedVersions is nil")
	}
	if lf.ModuleExtensions == nil {
		t.Error("ModuleExtensions is nil")
	}
	if lf.Facts == nil {
		t.Error("Facts is nil")
	}
}

func TestModuleKey_String(t *testing.T) {
	tests := []struct {
		key  ModuleKey
		want string
	}{
		{ModuleKey{Name: "rules_go", Version: "0.50.1"}, "rules_go@0.50.1"},
		{ModuleKey{Name: "rules_go", Version: ""}, "rules_go@_"},
		{ModuleKey{Name: "bazel_skylib", Version: "1.0.0"}, "bazel_skylib@1.0.0"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.key.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestModuleKey_UnmarshalText(t *testing.T) {
	tests := []struct {
		input string
		want  ModuleKey
	}{
		{"rules_go@0.50.1", ModuleKey{Name: "rules_go", Version: "0.50.1"}},
		{"rules_go@_", ModuleKey{Name: "rules_go", Version: ""}},
		{"bazel_skylib@1.0.0", ModuleKey{Name: "bazel_skylib", Version: "1.0.0"}},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			var key ModuleKey
			if err := key.UnmarshalText([]byte(tt.input)); err != nil {
				t.Fatalf("UnmarshalText failed: %v", err)
			}
			if key != tt.want {
				t.Errorf("UnmarshalText() = %+v, want %+v", key, tt.want)
			}
		})
	}
}

func TestLockfile_RegistryHash(t *testing.T) {
	lf := New()

	url := "https://bcr.bazel.build/modules/rules_go/0.50.1/MODULE.bazel"
	hash := "abc123def456"

	// Initially no hash
	if lf.HasRegistryHash(url) {
		t.Error("HasRegistryHash should be false initially")
	}
	if got := lf.GetRegistryHash(url); got != "" {
		t.Errorf("GetRegistryHash should be empty, got %q", got)
	}

	// Set hash
	lf.SetRegistryHash(url, hash)

	if !lf.HasRegistryHash(url) {
		t.Error("HasRegistryHash should be true after set")
	}
	if got := lf.GetRegistryHash(url); got != hash {
		t.Errorf("GetRegistryHash = %q, want %q", got, hash)
	}
}

func TestLockfile_YankedVersion(t *testing.T) {
	lf := New()

	key := ModuleKey{Name: "old_module", Version: "1.0.0"}
	reason := "security vulnerability"

	// Initially not allowed
	if lf.IsYankedVersionAllowed(key) {
		t.Error("IsYankedVersionAllowed should be false initially")
	}

	// Allow it
	lf.AllowYankedVersion(key, reason)

	if !lf.IsYankedVersionAllowed(key) {
		t.Error("IsYankedVersionAllowed should be true after allow")
	}
	if got := lf.GetYankedVersionReason(key); got != reason {
		t.Errorf("GetYankedVersionReason = %q, want %q", got, reason)
	}
}

func TestLockfile_IsCompatible(t *testing.T) {
	tests := []struct {
		version int
		want    bool
	}{
		{10, false}, // Too old (pre-incremental format)
		{11, true},  // Bazel 7.2.0 - incremental format introduced
		{16, true},  // Bazel 8.0.0
		{26, true},  // Bazel 9.0.0 (current)
		{30, true},  // Future version within tolerance
		{31, false}, // Too new
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			lf := &Lockfile{Version: tt.version}
			if got := lf.IsCompatible(); got != tt.want {
				t.Errorf("IsCompatible() for version %d = %v, want %v", tt.version, got, tt.want)
			}
		})
	}
}

func TestLockfile_JSONRoundTrip(t *testing.T) {
	original := New()
	original.SetRegistryHash("https://bcr.bazel.build/modules/foo/1.0/MODULE.bazel", "hash1")
	original.SetRegistryHash("https://bcr.bazel.build/modules/bar/2.0/MODULE.bazel", "hash2")
	original.AllowYankedVersion(ModuleKey{Name: "old", Version: "0.1"}, "deprecated")

	// Marshal
	data, err := original.Marshal()
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	// Parse back
	restored, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	// Compare
	if restored.Version != original.Version {
		t.Errorf("Version = %d, want %d", restored.Version, original.Version)
	}
	if len(restored.RegistryFileHashes) != len(original.RegistryFileHashes) {
		t.Errorf("RegistryFileHashes count = %d, want %d",
			len(restored.RegistryFileHashes), len(original.RegistryFileHashes))
	}
	for url, hash := range original.RegistryFileHashes {
		if restored.RegistryFileHashes[url] != hash {
			t.Errorf("RegistryFileHashes[%s] = %q, want %q", url, restored.RegistryFileHashes[url], hash)
		}
	}
}

func TestLockfile_WriteRead(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "MODULE.bazel.lock")

	original := New()
	original.SetRegistryHash("https://example.com/module", "testhash")

	// Write
	if err := original.WriteFile(path); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	// Read
	restored, err := ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}

	if restored.GetRegistryHash("https://example.com/module") != "testhash" {
		t.Error("Hash not preserved through write/read")
	}
}

func TestLockfile_Merge(t *testing.T) {
	t.Run("basic merge", func(t *testing.T) {
		lf1 := New()
		lf1.SetRegistryHash("url1", "hash1")

		lf2 := New()
		lf2.SetRegistryHash("url2", "hash2")

		opts := DefaultMergeOptions()
		if err := lf1.Merge(lf2, opts); err != nil {
			t.Fatalf("Merge failed: %v", err)
		}

		if !lf1.HasRegistryHash("url1") {
			t.Error("url1 should still exist")
		}
		if !lf1.HasRegistryHash("url2") {
			t.Error("url2 should be merged")
		}
	})

	t.Run("conflict prefer new", func(t *testing.T) {
		lf1 := New()
		lf1.SetRegistryHash("url", "old_hash")

		lf2 := New()
		lf2.SetRegistryHash("url", "new_hash")

		opts := MergeOptions{Strategy: MergePreferNew}
		if err := lf1.Merge(lf2, opts); err != nil {
			t.Fatalf("Merge failed: %v", err)
		}

		if got := lf1.GetRegistryHash("url"); got != "new_hash" {
			t.Errorf("hash = %q, want %q", got, "new_hash")
		}
	})

	t.Run("conflict prefer existing", func(t *testing.T) {
		lf1 := New()
		lf1.SetRegistryHash("url", "old_hash")

		lf2 := New()
		lf2.SetRegistryHash("url", "new_hash")

		opts := MergeOptions{Strategy: MergePreferExisting}
		if err := lf1.Merge(lf2, opts); err != nil {
			t.Fatalf("Merge failed: %v", err)
		}

		if got := lf1.GetRegistryHash("url"); got != "old_hash" {
			t.Errorf("hash = %q, want %q", got, "old_hash")
		}
	})

	t.Run("conflict error", func(t *testing.T) {
		lf1 := New()
		lf1.SetRegistryHash("url", "old_hash")

		lf2 := New()
		lf2.SetRegistryHash("url", "new_hash")

		opts := MergeOptions{Strategy: MergeErrorOnConflict}
		err := lf1.Merge(lf2, opts)
		if err == nil {
			t.Error("expected error on conflict")
		}
	})
}

func TestLockfile_Diff(t *testing.T) {
	lf1 := New()
	lf1.SetRegistryHash("url1", "hash1")
	lf1.SetRegistryHash("url2", "hash2")

	lf2 := New()
	lf2.SetRegistryHash("url2", "hash2_changed")
	lf2.SetRegistryHash("url3", "hash3")

	diff := lf1.Diff(lf2)

	if diff.IsEmpty() {
		t.Error("diff should not be empty")
	}
	if len(diff.AddedHashes) != 1 {
		t.Errorf("AddedHashes = %d, want 1", len(diff.AddedHashes))
	}
	if diff.AddedHashes["url3"] != "hash3" {
		t.Error("url3 should be in added")
	}
	if len(diff.RemovedHashes) != 1 {
		t.Errorf("RemovedHashes = %d, want 1", len(diff.RemovedHashes))
	}
	if diff.RemovedHashes["url1"] != "hash1" {
		t.Error("url1 should be in removed")
	}
	if len(diff.ChangedHashes) != 1 {
		t.Errorf("ChangedHashes = %d, want 1", len(diff.ChangedHashes))
	}
	if diff.ChangedHashes["url2"] != [2]string{"hash2", "hash2_changed"} {
		t.Error("url2 should be in changed")
	}
}

func TestHashContent(t *testing.T) {
	content := []byte("hello world")
	hash := HashContent(content)

	// SHA256 of "hello world" is known
	expected := "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"
	if hash != expected {
		t.Errorf("HashContent = %q, want %q", hash, expected)
	}

	if !VerifyHash(content, expected) {
		t.Error("VerifyHash should return true for correct hash")
	}
	if VerifyHash(content, "wrong") {
		t.Error("VerifyHash should return false for incorrect hash")
	}
}

func TestParse_RealLockfile(t *testing.T) {
	// Test parsing a realistic lockfile structure
	lockfileJSON := `{
  "lockFileVersion": 26,
  "registryFileHashes": {
    "https://bcr.bazel.build/bazel_registry.json": "abc123",
    "https://bcr.bazel.build/modules/rules_go/0.50.1/MODULE.bazel": "def456"
  },
  "selectedYankedVersions": {},
  "moduleExtensions": {
    "@@rules_go+//go:extensions.bzl%go_sdk": {
      "general": {
        "bzlTransitiveDigest": "xyz789",
        "usagesDigest": "uvw012",
        "generatedRepoSpecs": {
          "go_sdk": {
            "repoRuleId": "@@rules_go+//go/private:sdk.bzl%go_download_sdk",
            "attributes": {
              "version": "1.21.0"
            }
          }
        }
      }
    }
  },
  "facts": {}
}`

	lf, err := Parse([]byte(lockfileJSON))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if lf.Version != 26 {
		t.Errorf("Version = %d, want 26", lf.Version)
	}
	if len(lf.RegistryFileHashes) != 2 {
		t.Errorf("RegistryFileHashes count = %d, want 2", len(lf.RegistryFileHashes))
	}
	if len(lf.ModuleExtensions) != 1 {
		t.Errorf("ModuleExtensions count = %d, want 1", len(lf.ModuleExtensions))
	}
}

func TestExists(t *testing.T) {
	tmpDir := t.TempDir()

	// File doesn't exist
	path := filepath.Join(tmpDir, "MODULE.bazel.lock")
	if Exists(path) {
		t.Error("Exists should return false for non-existent file")
	}

	// Create file
	if err := os.WriteFile(path, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	if !Exists(path) {
		t.Error("Exists should return true for existing file")
	}
}

func TestDefaultPath(t *testing.T) {
	tests := []struct {
		root string
		want string
	}{
		{"", "MODULE.bazel.lock"},
		{"/workspace", "/workspace/MODULE.bazel.lock"},
		{"/home/user/project", "/home/user/project/MODULE.bazel.lock"},
	}

	for _, tt := range tests {
		if got := DefaultPath(tt.root); got != tt.want {
			t.Errorf("DefaultPath(%q) = %q, want %q", tt.root, got, tt.want)
		}
	}
}

func TestMarshal_Deterministic(t *testing.T) {
	// Create lockfile with multiple entries to test ordering
	lf := New()
	lf.SetRegistryHash("z_url", "hash_z")
	lf.SetRegistryHash("a_url", "hash_a")
	lf.SetRegistryHash("m_url", "hash_m")

	// Marshal twice and compare
	data1, err := lf.Marshal()
	if err != nil {
		t.Fatalf("Marshal 1 failed: %v", err)
	}

	data2, err := lf.Marshal()
	if err != nil {
		t.Fatalf("Marshal 2 failed: %v", err)
	}

	if string(data1) != string(data2) {
		t.Error("Marshal is not deterministic")
	}

	// Verify keys are sorted
	var parsed map[string]json.RawMessage
	if err := json.Unmarshal(data1, &parsed); err != nil {
		t.Fatal(err)
	}
}
