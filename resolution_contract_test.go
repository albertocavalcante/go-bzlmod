package gobzlmod

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestUnsupportedResolutionOptionsFailBeforeIO(t *testing.T) {
	for _, configure := range []func(*ResolutionOptions){
		func(o *ResolutionOptions) { o.LockfileMode = LockfileUpdate },
		func(o *ResolutionOptions) { o.LockfileMode = LockfileError },
		func(o *ResolutionOptions) { o.LockfileMode = LockfileRefresh },
		func(o *ResolutionOptions) { o.LockfilePath = "MODULE.bazel.lock" },
		func(o *ResolutionOptions) { o.VendorDir = "vendor" },
		func(o *ResolutionOptions) { o.CheckYanked = true; o.SubstituteYanked = true },
	} {
		var calls atomic.Int64
		o := ResolutionOptions{Registries: []string{"http://registry.test"}, HTTPClient: discoveryHTTPClient(nil, &calls)}
		configure(&o)
		for _, run := range []func() (*ResolutionList, error){
			func() (*ResolutionList, error) {
				return Resolve(context.Background(), RegistrySource{Name: "alpha", Version: "1.0"}, func(dest *ResolutionOptions) error { *dest = o; return nil })
			},
			func() (*ResolutionList, error) { return ResolveContent(context.Background(), "invalid(", o) },
			func() (*ResolutionList, error) { return ResolveFile(context.Background(), "/absent/MODULE.bazel", o) },
			func() (*ResolutionList, error) { return ResolveModule(context.Background(), "alpha", "1.0", o) },
		} {
			result, err := run()
			if result != nil || !errors.Is(err, errors.ErrUnsupported) {
				t.Fatalf("unsupported option accepted: result=%v err=%v", result, err)
			}
		}
		if calls.Load() != 0 {
			t.Fatal("unsupported option performed network IO")
		}
	}
	if _, err := applyOptions(DefaultOptions()...); err != nil {
		t.Fatalf("default options invalid: %v", err)
	}
}

func TestDirectDependencyChecksUseEffectiveEdges(t *testing.T) {
	files := map[string]string{
		"/modules/alpha/1.0/MODULE.bazel":  discoveryModule("alpha", "1.0"),
		"/modules/alpha/2.0/MODULE.bazel":  discoveryModule("alpha", "2.0"),
		"/modules/parent/1.0/MODULE.bazel": discoveryModule("parent", "1.0") + discoveryDep("alpha", "2.0"),
	}
	root := discoveryModule("root", "1.0") + `bazel_dep(name="alpha",version="1.0",repo_name="my_alpha")` + "\n" + discoveryDep("parent", "1.0")
	for _, mode := range []DirectDepsCheckMode{DirectDepsOff, DirectDepsWarn, DirectDepsError} {
		result, err := Resolve(context.Background(), ContentSource(root), WithDirectDepsMode(mode), WithRegistries("http://registry.test"), WithHTTPClient(discoveryHTTPClient(files, nil)))
		if mode == DirectDepsError {
			var mismatch *DirectDepsMismatchError
			if !errors.As(err, &mismatch) || len(mismatch.Mismatches) != 1 || mismatch.Mismatches[0].Name != "alpha" || mismatch.Mismatches[0].ResolvedVersion != "2.0" {
				t.Fatalf("incorrect mismatch: %v", err)
			}
		} else {
			if err != nil {
				t.Fatal(err)
			}
			if (len(result.Warnings) != 0) != (mode == DirectDepsWarn) {
				t.Fatalf("mode %d warnings=%v", mode, result.Warnings)
			}
		}
	}
	for _, suffix := range []string{
		`single_version_override(module_name="alpha",version="2.0")`,
		`multiple_version_override(module_name="alpha",versions=["1.0","2.0"])`,
	} {
		_, err := Resolve(context.Background(), ContentSource(root+suffix), WithDirectDepsMode(DirectDepsError), WithRegistries("http://registry.test"), WithHTTPClient(discoveryHTTPClient(files, nil)))
		if err != nil {
			t.Fatalf("effective override incorrectly mismatched: %v", err)
		}
	}
}

func TestProgressLifecycleForEverySource(t *testing.T) {
	dir := t.TempDir()
	modulePath := filepath.Join(dir, "MODULE.bazel")
	if err := os.WriteFile(modulePath, []byte(discoveryModule("root", "1.0")+discoveryDep("alpha", "1.0")), 0600); err != nil {
		t.Fatal(err)
	}
	for _, source := range []ModuleSource{ContentSource(discoveryModule("root", "1.0") + discoveryDep("alpha", "1.0")), FileSource(modulePath), RegistrySource{Name: "alpha", Version: "1.0"}} {
		for _, fail := range []bool{false, true} {
			files := map[string]string{"/modules/alpha/1.0/MODULE.bazel": discoveryModule("alpha", "1.0")}
			if fail {
				files = nil
			}
			var mu sync.Mutex
			var events []ProgressEvent
			callback := func(e ProgressEvent) { mu.Lock(); defer mu.Unlock(); events = append(events, e) }
			_, err := Resolve(context.Background(), source, WithRegistries("http://registry.test"), WithHTTPClient(discoveryHTTPClient(files, nil)), WithProgress(callback))
			if (err != nil) != fail {
				t.Fatalf("unexpected resolution outcome: %v", err)
			}
			if len(events) != 4 || events[0].Type != ProgressResolveStart || events[1].Type != ProgressModuleFetchStart || events[2].Type != ProgressModuleFetchEnd || events[3].Type != ProgressResolveEnd {
				t.Fatalf("unpaired lifecycle: %v", events)
			}
			if fail && (events[2].Message == "" || events[3].Message == "") {
				t.Fatalf("failure not reported: %v", events)
			}
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var events []ProgressEvent
	_, err := Resolve(ctx, ContentSource(discoveryModule("root", "1.0")), WithProgress(func(e ProgressEvent) { events = append(events, e) }))
	if !errors.Is(err, context.Canceled) || len(events) != 2 || !strings.Contains(events[1].Message, "canceled") {
		t.Fatalf("cancellation lifecycle: events=%v err=%v", events, err)
	}
}

func TestProgressEndIncludesTargetValidationFailure(t *testing.T) {
	files := map[string]string{
		"/modules/alpha/1.0/MODULE.bazel": discoveryModule("alpha", "1.0"),
		"/modules/alpha/metadata.json":    `{"versions":["1.0"],"yanked_versions":{"1.0":"broken"}}`,
	}
	var last ProgressEvent
	_, err := Resolve(context.Background(), RegistrySource{Name: "alpha", Version: "1.0"}, WithRegistries("http://registry.test"), WithHTTPClient(discoveryHTTPClient(files, nil)), WithYankedCheck(true), WithYankedBehavior(YankedVersionError), WithProgress(func(e ProgressEvent) { last = e }))
	var yanked *YankedVersionsError
	if !errors.As(err, &yanked) || last.Type != ProgressResolveEnd || last.Message == "" {
		t.Fatalf("final target validation not inside lifecycle: last=%+v err=%v", last, err)
	}
}

func TestRegistrySourceTraceIncludesTarget(t *testing.T) {
	files := map[string]string{
		"/modules/alpha/1.0/MODULE.bazel": discoveryModule("alpha", "1.0"),
		"/modules/alpha/1.0/source.json":  `{"url":"https://sources.test/alpha.tar.gz"}`,
	}
	result, err := Resolve(context.Background(), RegistrySource{Name: "alpha", Version: "1.0"}, WithRegistries("http://registry.test"), WithHTTPClient(discoveryHTTPClient(files, nil)), WithRegistryTrace())
	if err != nil {
		t.Fatal(err)
	}
	module := result.Module("alpha")
	if module.Source == nil || module.Source.URL != "https://sources.test/alpha.tar.gz" || result.RegistryFileHashes["http://registry.test/modules/alpha/1.0/source.json"] == nil {
		t.Fatalf("target source/trace omitted: %+v", result)
	}
}
