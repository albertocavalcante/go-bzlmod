package gobzlmod

import (
	"testing"

	lockpkg "github.com/albertocavalcante/go-bzlmod/lockfile"
)

func TestResolutionList_ToLockfile(t *testing.T) {
	foundHash := "abc123"

	result := &ResolutionList{
		RegistryFileHashes: map[string]*string{
			"https://registry.example/modules/foo/1.0.0/MODULE.bazel": &foundHash,
			"https://registry.example/modules/bar/1.0.0/MODULE.bazel": nil,
		},
		Modules: []ModuleToResolve{
			{
				Name:       "foo",
				Version:    "1.0.0",
				Yanked:     true,
				YankReason: "security issue",
			},
		},
	}

	lf := result.ToLockfile()

	hash, ok := lf.GetRegistryHashValue("https://registry.example/modules/foo/1.0.0/MODULE.bazel")
	if !ok || hash == nil || *hash != foundHash {
		t.Fatalf("found registry hash = %v, %v, want %q", ok, hash, foundHash)
	}
	if !lf.IsRegistryHashMissing("https://registry.example/modules/bar/1.0.0/MODULE.bazel") {
		t.Fatal("missing registry entry should be preserved")
	}
	if !lf.IsYankedVersionAllowed(lockpkg.ModuleKey{Name: "foo", Version: "1.0.0"}) {
		t.Fatal("yanked module should be recorded in lockfile")
	}
}
