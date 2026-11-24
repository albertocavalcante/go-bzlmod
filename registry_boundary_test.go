package gobzlmod

import (
	"context"
	"errors"
	"fmt"
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
			_, err := r.GetModuleFile(context.Background(), "alpha", "1.0")
			if err != nil {
				t.Errorf("selected module fetch: %v", err)
			}
			if _, err = r.GetModuleSource(context.Background(), "alpha", "1.0"); err == nil {
				t.Error("source must not switch origin")
			}
			if _, err = moduleMetadataForVersion(context.Background(), r, "alpha", "1.0"); err == nil {
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

func TestRegistryVersionsHaveOrderIndependentOrigins(t *testing.T) {
	one := boundaryServer(t, map[string]string{
		"/modules/alpha/1.0/MODULE.bazel":  discoveryModule("alpha", "1.0"),
		"/modules/alpha/1.0/source.json":   `{"url":"https://one.test/alpha1.tgz"}`,
		"/modules/alpha/2.0/source.json":   `{"url":"https://wrong.test/alpha2.tgz"}`,
		"/modules/alpha/metadata.json":     `{"versions":["1.0","2.0"],"yanked_versions":{"2.0":"wrong_origin"}}`,
		"/modules/parent/1.0/MODULE.bazel": discoveryModule("parent", "1.0") + discoveryDep("alpha", "2.0"),
		"/modules/parent/1.0/source.json":  `{"url":"https://one.test/parent.tgz"}`,
	})
	two := boundaryServer(t, map[string]string{
		"/modules/alpha/1.0/MODULE.bazel": discoveryModule("alpha", "1.0"),
		"/modules/alpha/2.0/MODULE.bazel": discoveryModule("alpha", "2.0"),
		"/modules/alpha/2.0/source.json":  `{"url":"https://two.test/alpha2.tgz"}`,
		"/modules/alpha/metadata.json":    `{"versions":["1.0","2.0"],"yanked_versions":{"2.0":"selected_origin"}}`,
	})
	for _, order := range [][]string{{"1.0", "2.0"}, {"2.0", "1.0"}} {
		r, err := NewRegistry([]string{one.URL, two.URL})
		if err != nil {
			t.Fatal(err)
		}
		for _, version := range order {
			if _, err = r.GetModuleFile(context.Background(), "alpha", version); err != nil {
				t.Fatalf("order%v: %v", order, err)
			}
		}
		source, err := r.GetModuleSource(context.Background(), "alpha", "2.0")
		if err != nil || source.URL != "https://two.test/alpha2.tgz" {
			t.Fatalf("mixed source origin: %+v (%v)", source, err)
		}
	}
	// Source-first lookup must select the same MODULE.bazel origin as resolution.
	r, err := NewRegistry([]string{one.URL, two.URL})
	if err != nil {
		t.Fatal(err)
	}
	source, err := r.GetModuleSource(context.Background(), "alpha", "2.0")
	if err != nil || source.URL != "https://two.test/alpha2.tgz" {
		t.Fatalf("source-first origin: %+v (%v)", source, err)
	}
	root := discoveryModule("root", "1.0") + discoveryDep("alpha", "1.0") + discoveryDep("parent", "1.0")
	result, err := Resolve(context.Background(), ContentSource(root), WithRegistries(one.URL, two.URL), WithYankedCheck(true), WithRegistryTrace())
	if err != nil {
		t.Fatal(err)
	}
	module := result.Module("alpha")
	if module == nil || module.Version != "2.0" || module.Registry != two.URL || module.YankReason != "selected_origin" || module.Source == nil || module.Source.URL != "https://two.test/alpha2.tgz" {
		t.Fatalf("selected coordinate provenance: %+v", module)
	}
}

func TestRegistryOverrideOwnsYankedMetadata(t *testing.T) {
	base := boundaryServer(t, map[string]string{
		"/modules/alpha/1.0/MODULE.bazel": discoveryModule("alpha", "1.0"),
		"/modules/alpha/metadata.json":    `{"versions":["1.0"]}`,
	})
	override := boundaryServer(t, map[string]string{
		"/modules/alpha/1.0/MODULE.bazel": discoveryModule("alpha", "1.0"),
		"/modules/alpha/metadata.json":    `{"versions":["1.0"],"yanked_versions":{"1.0":"override_origin"}}`,
	})
	root := discoveryModule("root", "1.0") + discoveryDep("alpha", "1.0") + fmt.Sprintf("single_version_override(module_name='alpha',registry=%q)", override.URL)
	_, err := Resolve(context.Background(), ContentSource(root), WithRegistries(base.URL), WithYankedCheck(true), WithYankedBehavior(YankedVersionError))
	var yanked *YankedVersionsError
	if !errors.As(err, &yanked) || len(yanked.Modules) != 1 || yanked.Modules[0].YankReason != "override_origin" {
		t.Fatalf("override yank was not enforced: %v", err)
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

func TestRegistryConfigOnlyFetchedForTrace(t *testing.T) {
	for _, trace := range []bool{false, true} {
		t.Run(fmt.Sprint(trace), func(t *testing.T) {
			var probes atomic.Int32
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/bazel_registry.json":
					probes.Add(1)
					_, _ = io.WriteString(w, `{}`)
				case "/modules/alpha/1.0/MODULE.bazel":
					_, _ = io.WriteString(w, discoveryModule("alpha", "1.0"))
				case "/modules/alpha/1.0/source.json":
					_, _ = io.WriteString(w, `{"url":"https://source.test/alpha.tgz"}`)
				default:
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer s.Close()
			opts := []Option{WithRegistries(s.URL)}
			if trace {
				opts = append(opts, WithRegistryTrace())
			}
			result, err := Resolve(context.Background(), ContentSource(discoveryModule("root", "1.0")+discoveryDep("alpha", "1.0")), opts...)
			if err != nil {
				t.Fatal(err)
			}
			want := int32(0)
			if trace {
				want = 1
			}
			if probes.Load() != want {
				t.Fatalf("config requests=%d, want %d", probes.Load(), want)
			}
			if trace && result.RegistryFileHashes[s.URL+"/bazel_registry.json"] == nil {
				t.Fatal("configuration hash missing from trace")
			}
		})
	}
}
