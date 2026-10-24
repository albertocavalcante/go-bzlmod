package gobzlmod

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

func discoveryModule(name, version string) string {
	return fmt.Sprintf("module(name=%q, version=%q)\n", name, version)
}

func discoveryDep(name, version string) string {
	return fmt.Sprintf("bazel_dep(name=%q, version=%q)\n", name, version)
}

func discoveryHTTPClient(files map[string]string, requests *atomic.Int64) *http.Client {
	return &http.Client{Transport: &roundTripperFunc{fn: func(r *http.Request) (*http.Response, error) {
		if requests != nil {
			requests.Add(1)
		}
		data, ok := files[r.URL.Path]
		status := http.StatusOK
		if !ok {
			status = http.StatusNotFound
		}
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(data)), Request: r}, nil
	}}}
}

func resolveDiscovery(t *testing.T, root string, files map[string]string) *ResolutionList {
	t.Helper()
	result, err := Resolve(context.Background(), ContentSource(root), WithRegistries("http://registry.test"), WithHTTPClient(discoveryHTTPClient(files, nil)))
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestDiscoveryMissingTransitiveFails(t *testing.T) {
	files := map[string]string{"/modules/alpha/1.0/MODULE.bazel": discoveryModule("alpha", "1.0") + discoveryDep("missing", "1.0")}
	result, err := Resolve(context.Background(), ContentSource(discoveryModule("root", "1.0")+discoveryDep("alpha", "1.0")), WithRegistries("http://registry.test"), WithHTTPClient(discoveryHTTPClient(files, nil)))
	if err == nil || result != nil || !strings.Contains(err.Error(), "missing@1.0") {
		t.Fatalf("got result=%v, err=%v; want missing dependency error", result, err)
	}
}

func TestDiscoveryRootNodepActivatesAfterDelayedOrdinaryEdge(t *testing.T) {
	files := map[string]string{
		"/modules/alpha/1.0/MODULE.bazel": discoveryModule("alpha", "1.0") + discoveryDep("beta", "1.0"),
		"/modules/beta/1.0/MODULE.bazel":  discoveryModule("beta", "1.0") + discoveryDep("foo", "1.0"),
		"/modules/foo/1.0/MODULE.bazel":   discoveryModule("foo", "1.0"),
		"/modules/foo/2.0/MODULE.bazel":   discoveryModule("foo", "2.0") + discoveryDep("leaf", "1.0"),
		"/modules/leaf/1.0/MODULE.bazel":  discoveryModule("leaf", "1.0"),
	}
	root := discoveryModule("root", "1.0") + discoveryDep("alpha", "1.0") + "bazel_dep(name='foo',version='2.0',repo_name=None)\n"
	result := resolveDiscovery(t, root, files)
	if m := result.Module("foo"); m == nil || m.Version != "2.0" {
		t.Fatalf("want foo@2.0, got %v", m)
	}
	if !result.HasModule("leaf") {
		t.Fatal("selected nodep version's ordinary dependencies were not discovered")
	}
}

func TestDiscoveryAbsentNodepDoesNotIntroduceVersions(t *testing.T) {
	files := map[string]string{
		"/modules/alpha/1.0/MODULE.bazel": discoveryModule("alpha", "1.0") + "bazel_dep(name='ghost',version='1.0',repo_name=None)\n",
		"/modules/ghost/1.0/MODULE.bazel": discoveryModule("ghost", "1.0") + discoveryDep("foo", "9.0"),
		"/modules/foo/1.0/MODULE.bazel":   discoveryModule("foo", "1.0"),
		"/modules/foo/9.0/MODULE.bazel":   discoveryModule("foo", "9.0"),
	}
	result := resolveDiscovery(t, discoveryModule("root", "1.0")+discoveryDep("alpha", "1.0")+discoveryDep("foo", "1.0"), files)
	if m := result.Module("foo"); m == nil || m.Version != "1.0" {
		t.Fatalf("absent nodep upgraded foo: %v", m)
	}
	if result.HasModule("ghost") {
		t.Fatal("absent nodep module should not be included")
	}
}

func TestDiscoveryEligibleMissingNodepFails(t *testing.T) {
	files := map[string]string{"/modules/foo/1.0/MODULE.bazel": discoveryModule("foo", "1.0")}
	root := discoveryModule("root", "1.0") + discoveryDep("foo", "1.0") + "bazel_dep(name='foo',version='2.0',repo_name=None)\n"
	_, err := Resolve(context.Background(), ContentSource(root), WithRegistries("http://registry.test"), WithHTTPClient(discoveryHTTPClient(files, nil)))
	if err == nil || !strings.Contains(err.Error(), "foo@2.0") {
		t.Fatalf("want missing eligible nodep version error, got %v", err)
	}
}

func TestDiscoveryWideConcurrentGraph(t *testing.T) {
	const count = 2000
	files := make(map[string]string, count)
	var root strings.Builder
	root.WriteString(discoveryModule("root", "1.0"))
	for i := range count {
		name := fmt.Sprintf("module%04d", i)
		root.WriteString(discoveryDep(name, "1.0"))
		files["/modules/"+name+"/1.0/MODULE.bazel"] = discoveryModule(name, "1.0")
	}
	result := resolveDiscovery(t, root.String(), files)
	if len(result.Modules) != count {
		t.Fatalf("resolved %d modules, want %d", len(result.Modules), count)
	}
}

func TestDiscoveryCancelledContextDoesNotFetch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var requests atomic.Int64
	_, err := Resolve(ctx, ContentSource(discoveryModule("root", "1.0")+discoveryDep("alpha", "1.0")), WithRegistries("http://registry.test"), WithHTTPClient(discoveryHTTPClient(nil, &requests)))
	if err == nil {
		t.Fatal("cancelled resolution succeeded")
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("cancelled resolution made %d HTTP requests", got)
	}
}

func TestDiscoveryRootBackreferences(t *testing.T) {
	for _, nodep := range []bool{false, true} {
		t.Run(fmt.Sprintf("nodep_%t", nodep), func(t *testing.T) {
			backreference := discoveryDep("root", "2.0")
			if nodep {
				backreference = "bazel_dep(name='root',version='2.0',repo_name=None)\n"
			}
			files := map[string]string{
				"/modules/alpha/1.0/MODULE.bazel": discoveryModule("alpha", "1.0") + backreference,
			}
			result := resolveDiscovery(t, discoveryModule("root", "1.0")+discoveryDep("alpha", "1.0"), files)
			if !result.HasModule("alpha") || result.HasModule("root") {
				t.Fatalf("unexpected root backreference result: %+v", result.Modules)
			}
		})
	}
}
