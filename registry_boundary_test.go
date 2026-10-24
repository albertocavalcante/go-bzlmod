package gobzlmod

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
)

func boundaryServer(t *testing.T, files map[string]string) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if content, ok := files[r.URL.Path]; ok {
			_, _ = io.WriteString(w, content)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(s.Close)
	return s
}

func TestRegistryCacheSeparatesOrigins(t *testing.T) {
	one := boundaryServer(t, map[string]string{"/modules/alpha/1.0/MODULE.bazel": discoveryModule("alpha", "1.0") + discoveryDep("private_dep", "1.0")})
	two := boundaryServer(t, map[string]string{"/modules/alpha/1.0/MODULE.bazel": discoveryModule("alpha", "1.0")})
	cache := NewMemoryCache()
	for i, url := range []string{one.URL, two.URL} {
		r, err := NewRegistry([]string{url}, WithRegistryCache(cache))
		if err != nil {
			t.Fatal(err)
		}
		module, err := r.GetModuleFile(context.Background(), "alpha", "1.0")
		if err != nil {
			t.Fatal(err)
		}
		if got, want := len(module.Dependencies), 1-i; got != want {
			t.Fatalf("registry%d dependencies=%d, want%d", i, got, want)
		}
	}
	if cache.Len() != 2 {
		t.Fatalf("cache has %d entries, want two origins", cache.Len())
	}
}

func TestScopeModuleCachePreservesDistinctPaths(t *testing.T) {
	cache := NewMemoryCache()
	one := ScopeModuleCache(cache, "https://registry.test/base/")
	if err := one.Put(context.Background(), "alpha", "1.0", []byte("one")); err != nil {
		t.Fatal(err)
	}
	for _, url := range []string{"https://registry.test/base", "https://registry.test/base/"} {
		if _, found, err := ScopeModuleCache(cache, url).Get(context.Background(), "alpha", "1.0"); err != nil || !found {
			t.Fatalf("equivalent base URL cache miss: %s (%v)", url, err)
		}
	}
	if _, found, err := ScopeModuleCache(cache, "https://registry.test/base//").Get(context.Background(), "alpha", "1.0"); err != nil || found {
		t.Fatalf("distinct base path shared entry: found=%v err=%v", found, err)
	}
	if _, found, err := ScopeModuleCache(one, "https://registry.test/base/").Get(context.Background(), "alpha", "1.0"); err != nil || !found {
		t.Fatalf("scoping must be idempotent: found=%v err=%v", found, err)
	}
}

func TestRegistrySelectedOriginIsStickyUnderConcurrency(t *testing.T) {
	one := boundaryServer(t, map[string]string{"/modules/alpha/1.0/MODULE.bazel": discoveryModule("alpha", "1.0")})
	var fallbackCalls atomic.Int64
	two := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fallbackCalls.Add(1)
		_, _ = io.WriteString(w, discoveryModule("alpha", "2.0"))
	}))
	defer two.Close()
	r, err := NewRegistry([]string{one.URL, two.URL})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.GetModuleFile(context.Background(), "alpha", "1.0"); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() {
			_, err := r.GetModuleFile(context.Background(), "alpha", "2.0")
			if !errors.Is(err, ErrVersionNotFound) {
				t.Errorf("want original version404, got %v", err)
			}
			if _, err = r.GetModuleSource(context.Background(), "alpha", "2.0"); err == nil {
				t.Error("source must not switch origin")
			}
			if _, err = r.GetModuleMetadata(context.Background(), "alpha"); err == nil {
				t.Error("metadata must not switch origin")
			}
		})
	}
	wg.Wait()
	if got := fallbackCalls.Load(); got != 0 {
		t.Fatalf("fallback called %d times after origin was selected", got)
	}
}

func TestRegistrySourcePathsDoNotRelocateMetadata(t *testing.T) {
	s := boundaryServer(t, map[string]string{
		"/bazel_registry.json":            `{"module_base_path":"/local/sources"}`,
		"/modules/alpha/1.0/MODULE.bazel": discoveryModule("alpha", "1.0"),
		"/modules/alpha/2.0/MODULE.bazel": discoveryModule("alpha", "2.0"),
	})
	r, err := NewRegistry([]string{s.URL})
	if err != nil {
		t.Fatal(err)
	}
	for _, version := range []string{"1.0", "2.0"} {
		if _, err = r.GetModuleFile(context.Background(), "alpha", version); err != nil {
			t.Fatal(err)
		}
	}
}

func TestLocalRegistryRejectsTraversalAndEscapingSymlinks(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "registry")
	if err := os.MkdirAll(filepath.Join(root, "modules"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(parent, "MODULE.bazel"), []byte(discoveryModule("outside", "1.0")), 0600); err != nil {
		t.Fatal(err)
	}
	r, err := NewRegistry([]string{"file://" + root})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"../..", ".", "foo/bar", "foo\\bar", "foo?query", "foo#fragment", "%2e%2e"} {
		if _, err = r.GetModuleFile(context.Background(), name, "."); err == nil {
			t.Errorf("accepted coordinate %q", name)
		}
		if _, err = r.GetModuleMetadata(context.Background(), name); err == nil {
			t.Errorf("accepted metadata coordinate %q", name)
		}
	}
	outside := filepath.Join(parent, "outside", "1.0")
	if err := os.MkdirAll(outside, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "MODULE.bazel"), []byte(discoveryModule("alpha", "1.0")), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Dir(outside), filepath.Join(root, "modules", "alpha")); err != nil {
		t.Skipf("symlink creation unavailable: %v", err)
	}
	if _, err = r.GetModuleFile(context.Background(), "alpha", "1.0"); err == nil {
		t.Fatal("followed a symlink outside registry root")
	}
}
