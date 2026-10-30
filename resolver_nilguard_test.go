package gobzlmod

import "testing"

// TestCalculateModuleDepthsSelectionNilGraph pins the nil guard in
// calculateModuleDepthsSelection.
//
// The guard existed but sat one line BELOW the dereference it was meant to
// protect: `make(map[...], len(g.Modules))` ran first, so a nil graph panicked
// before the `if g == nil` check was ever reached. staticcheck's SA5011 found it.
func TestCalculateModuleDepthsSelectionNilGraph(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("calculateModuleDepthsSelection(nil) panicked: %v", r)
		}
	}()

	got := calculateModuleDepthsSelection(nil)
	if got == nil {
		t.Error("want a non-nil empty map so callers can range over it safely")
	}
	if len(got) != 0 {
		t.Errorf("len = %d, want 0", len(got))
	}
}
