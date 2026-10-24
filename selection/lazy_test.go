package selection

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func ambiguousGraph(count int) *DepGraph {
	root := ModuleKey{Name: "root", Version: "1.0"}
	g := &DepGraph{RootKey: root, Modules: map[ModuleKey]*Module{root: {Key: root}}}
	for i := range count {
		name := fmt.Sprintf("dep%03d", i)
		one := ModuleKey{Name: name, Version: "1.0"}
		two := ModuleKey{Name: name, Version: "2.0"}
		g.Modules[one] = &Module{Key: one, CompatLevel: 0}
		g.Modules[two] = &Module{Key: two, CompatLevel: 1}
		g.Modules[root].Deps = append(g.Modules[root].Deps, DepSpec{Name: name, Version: "1.0", MaxCompatibilityLevel: 1})
	}
	return g
}

func TestLargeChoiceSpaceStopsAfterFirstSuccess(t *testing.T) {
	// 2^64 possible strategies cannot be materialized. The first valid result
	// must be returned using state proportional to the input graph.
	g := ambiguousGraph(64)
	result, err := Run(g, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ResolvedGraph) != 65 {
		t.Fatalf("resolved %d modules", len(result.ResolvedGraph))
	}
	for _, dep := range result.ResolvedGraph[g.RootKey].Deps {
		if dep.Version != "1.0" {
			t.Fatalf("first-success ordering changed: %+v", dep)
		}
	}
}

func TestRunContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if result, err := RunContext(ctx, ambiguousGraph(64), nil); result != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled result=%v err=%v", result, err)
	}
}

func TestRunRejectsMalformedGraph(t *testing.T) {
	for _, graph := range []*DepGraph{nil, {Modules: map[ModuleKey]*Module{{Name: "root"}: nil}}} {
		if result, err := Run(graph, nil); result != nil || err == nil {
			t.Fatalf("malformed graph accepted: %v %v", result, err)
		}
	}
}

func BenchmarkSelectionFirstSuccess(b *testing.B) {
	for _, count := range []int{8, 14, 64} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			g := ambiguousGraph(count)
			b.ReportAllocs()
			for b.Loop() {
				if _, err := Run(g, nil); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
