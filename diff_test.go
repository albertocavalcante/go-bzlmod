package gobzlmod

import (
	"reflect"
	"testing"
)

func TestDiffResolutions_NilInputs(t *testing.T) {
	tests := []struct {
		name string
		old  *ResolutionList
		new  *ResolutionList
	}{
		{"both nil", nil, nil},
		{"old nil", nil, &ResolutionList{}},
		{"new nil", &ResolutionList{}, nil},
		{"both empty", &ResolutionList{}, &ResolutionList{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			diff := DiffResolutions(tt.old, tt.new)
			if diff == nil {
				t.Fatal("DiffResolutions returned nil")
			}
		})
	}
}

func TestDiffResolutions_Identical(t *testing.T) {
	modules := []ModuleToResolve{
		{Name: "module_a", Version: "1.0.0"},
		{Name: "module_b", Version: "2.0.0"},
	}

	old := &ResolutionList{Modules: modules}
	new := &ResolutionList{Modules: modules}

	diff := DiffResolutions(old, new)

	if !diff.IsEmpty() {
		t.Errorf("Expected empty diff for identical lists, got %+v", diff)
	}
	if diff.TotalChanges() != 0 {
		t.Errorf("TotalChanges() = %d, want 0", diff.TotalChanges())
	}
}

func TestDiffResolutions_Added(t *testing.T) {
	old := &ResolutionList{
		Modules: []ModuleToResolve{
			{Name: "module_a", Version: "1.0.0"},
		},
	}
	new := &ResolutionList{
		Modules: []ModuleToResolve{
			{Name: "module_a", Version: "1.0.0"},
			{Name: "module_b", Version: "2.0.0"},
			{Name: "module_c", Version: "3.0.0"},
		},
	}

	diff := DiffResolutions(old, new)

	if len(diff.Added) != 2 {
		t.Fatalf("Expected 2 added modules, got %d", len(diff.Added))
	}
	// Should be sorted by name
	if diff.Added[0].Name != "module_b" || diff.Added[1].Name != "module_c" {
		t.Errorf("Added modules not sorted: %+v", diff.Added)
	}
	if len(diff.Removed) != 0 {
		t.Errorf("Expected 0 removed, got %d", len(diff.Removed))
	}
	if len(diff.Upgraded) != 0 {
		t.Errorf("Expected 0 upgraded, got %d", len(diff.Upgraded))
	}
}

func TestDiffResolutions_Removed(t *testing.T) {
	old := &ResolutionList{
		Modules: []ModuleToResolve{
			{Name: "module_a", Version: "1.0.0"},
			{Name: "module_b", Version: "2.0.0"},
		},
	}
	new := &ResolutionList{
		Modules: []ModuleToResolve{
			{Name: "module_a", Version: "1.0.0"},
		},
	}

	diff := DiffResolutions(old, new)

	if len(diff.Removed) != 1 {
		t.Fatalf("Expected 1 removed module, got %d", len(diff.Removed))
	}
	if diff.Removed[0].Name != "module_b" {
		t.Errorf("Removed module = %q, want module_b", diff.Removed[0].Name)
	}
	if len(diff.Added) != 0 {
		t.Errorf("Expected 0 added, got %d", len(diff.Added))
	}
}

func TestDiffResolutions_Upgraded(t *testing.T) {
	old := &ResolutionList{
		Modules: []ModuleToResolve{
			{Name: "module_a", Version: "1.0.0"},
		},
	}
	new := &ResolutionList{
		Modules: []ModuleToResolve{
			{Name: "module_a", Version: "2.0.0"},
		},
	}

	diff := DiffResolutions(old, new)

	if len(diff.Upgraded) != 1 {
		t.Fatalf("Expected 1 upgraded module, got %d", len(diff.Upgraded))
	}
	upgrade := diff.Upgraded[0]
	if upgrade.Name != "module_a" {
		t.Errorf("Upgraded module = %q, want module_a", upgrade.Name)
	}
	if upgrade.OldVersion != "1.0.0" {
		t.Errorf("OldVersion = %q, want 1.0.0", upgrade.OldVersion)
	}
	if upgrade.NewVersion != "2.0.0" {
		t.Errorf("NewVersion = %q, want 2.0.0", upgrade.NewVersion)
	}
	if len(diff.Downgraded) != 0 {
		t.Errorf("Expected 0 downgraded, got %d", len(diff.Downgraded))
	}
}

func TestDiffResolutions_Downgraded(t *testing.T) {
	old := &ResolutionList{
		Modules: []ModuleToResolve{
			{Name: "module_a", Version: "2.0.0"},
		},
	}
	new := &ResolutionList{
		Modules: []ModuleToResolve{
			{Name: "module_a", Version: "1.0.0"},
		},
	}

	diff := DiffResolutions(old, new)

	if len(diff.Downgraded) != 1 {
		t.Fatalf("Expected 1 downgraded module, got %d", len(diff.Downgraded))
	}
	downgrade := diff.Downgraded[0]
	if downgrade.Name != "module_a" {
		t.Errorf("Downgraded module = %q, want module_a", downgrade.Name)
	}
	if downgrade.OldVersion != "2.0.0" {
		t.Errorf("OldVersion = %q, want 2.0.0", downgrade.OldVersion)
	}
	if downgrade.NewVersion != "1.0.0" {
		t.Errorf("NewVersion = %q, want 1.0.0", downgrade.NewVersion)
	}
	if len(diff.Upgraded) != 0 {
		t.Errorf("Expected 0 upgraded, got %d", len(diff.Upgraded))
	}
}

func TestDiffResolutions_BCRVersions(t *testing.T) {
	// Test that .bcr.N versions are compared correctly
	old := &ResolutionList{
		Modules: []ModuleToResolve{
			{Name: "protobuf", Version: "21.7.bcr.1"},
		},
	}
	new := &ResolutionList{
		Modules: []ModuleToResolve{
			{Name: "protobuf", Version: "21.7.bcr.10"},
		},
	}

	diff := DiffResolutions(old, new)

	if len(diff.Upgraded) != 1 {
		t.Fatalf("Expected 1 upgraded module, got %d", len(diff.Upgraded))
	}
	if diff.Upgraded[0].OldVersion != "21.7.bcr.1" {
		t.Errorf("OldVersion = %q, want 21.7.bcr.1", diff.Upgraded[0].OldVersion)
	}
	if diff.Upgraded[0].NewVersion != "21.7.bcr.10" {
		t.Errorf("NewVersion = %q, want 21.7.bcr.10", diff.Upgraded[0].NewVersion)
	}
}

func TestDiffResolutions_MixedChanges(t *testing.T) {
	old := &ResolutionList{
		Modules: []ModuleToResolve{
			{Name: "unchanged", Version: "1.0.0"},
			{Name: "upgraded", Version: "1.0.0"},
			{Name: "downgraded", Version: "2.0.0"},
			{Name: "removed", Version: "1.0.0"},
		},
	}
	new := &ResolutionList{
		Modules: []ModuleToResolve{
			{Name: "unchanged", Version: "1.0.0"},
			{Name: "upgraded", Version: "2.0.0"},
			{Name: "downgraded", Version: "1.0.0"},
			{Name: "added", Version: "1.0.0"},
		},
	}

	diff := DiffResolutions(old, new)

	expectedDiff := &ResolutionDiff{
		Added:      []ModuleChange{{Name: "added", Version: "1.0.0"}},
		Removed:    []ModuleChange{{Name: "removed", Version: "1.0.0"}},
		Upgraded:   []ModuleUpgrade{{Name: "upgraded", OldVersion: "1.0.0", NewVersion: "2.0.0"}},
		Downgraded: []ModuleUpgrade{{Name: "downgraded", OldVersion: "2.0.0", NewVersion: "1.0.0"}},
	}

	if !reflect.DeepEqual(diff.Added, expectedDiff.Added) {
		t.Errorf("Added = %+v, want %+v", diff.Added, expectedDiff.Added)
	}
	if !reflect.DeepEqual(diff.Removed, expectedDiff.Removed) {
		t.Errorf("Removed = %+v, want %+v", diff.Removed, expectedDiff.Removed)
	}
	if !reflect.DeepEqual(diff.Upgraded, expectedDiff.Upgraded) {
		t.Errorf("Upgraded = %+v, want %+v", diff.Upgraded, expectedDiff.Upgraded)
	}
	if !reflect.DeepEqual(diff.Downgraded, expectedDiff.Downgraded) {
		t.Errorf("Downgraded = %+v, want %+v", diff.Downgraded, expectedDiff.Downgraded)
	}

	if diff.IsEmpty() {
		t.Error("IsEmpty() should return false")
	}
	if diff.TotalChanges() != 4 {
		t.Errorf("TotalChanges() = %d, want 4", diff.TotalChanges())
	}
}

func TestDiffResolutions_SortedOutput(t *testing.T) {
	// Input modules in unsorted order
	old := &ResolutionList{
		Modules: []ModuleToResolve{
			{Name: "z_module", Version: "1.0.0"},
			{Name: "a_module", Version: "1.0.0"},
			{Name: "m_module", Version: "1.0.0"},
		},
	}
	new := &ResolutionList{
		Modules: []ModuleToResolve{
			{Name: "z_module", Version: "2.0.0"},
			{Name: "a_module", Version: "2.0.0"},
			{Name: "m_module", Version: "2.0.0"},
		},
	}

	diff := DiffResolutions(old, new)

	// Verify sorted output
	expectedOrder := []string{"a_module", "m_module", "z_module"}
	for i, upgrade := range diff.Upgraded {
		if upgrade.Name != expectedOrder[i] {
			t.Errorf("Upgraded[%d].Name = %q, want %q", i, upgrade.Name, expectedOrder[i])
		}
	}
}

func TestResolutionDiff_IsEmpty(t *testing.T) {
	tests := []struct {
		name     string
		diff     ResolutionDiff
		expected bool
	}{
		{
			name:     "completely empty",
			diff:     ResolutionDiff{},
			expected: true,
		},
		{
			name: "has added",
			diff: ResolutionDiff{
				Added: []ModuleChange{{Name: "a", Version: "1.0.0"}},
			},
			expected: false,
		},
		{
			name: "has removed",
			diff: ResolutionDiff{
				Removed: []ModuleChange{{Name: "a", Version: "1.0.0"}},
			},
			expected: false,
		},
		{
			name: "has upgraded",
			diff: ResolutionDiff{
				Upgraded: []ModuleUpgrade{{Name: "a", OldVersion: "1.0.0", NewVersion: "2.0.0"}},
			},
			expected: false,
		},
		{
			name: "has downgraded",
			diff: ResolutionDiff{
				Downgraded: []ModuleUpgrade{{Name: "a", OldVersion: "2.0.0", NewVersion: "1.0.0"}},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.diff.IsEmpty(); got != tt.expected {
				t.Errorf("IsEmpty() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestResolutionDiff_TotalChanges(t *testing.T) {
	diff := ResolutionDiff{
		Added:      []ModuleChange{{Name: "a"}, {Name: "b"}},
		Removed:    []ModuleChange{{Name: "c"}},
		Upgraded:   []ModuleUpgrade{{Name: "d"}, {Name: "e"}, {Name: "f"}},
		Downgraded: []ModuleUpgrade{{Name: "g"}},
	}

	if got := diff.TotalChanges(); got != 7 {
		t.Errorf("TotalChanges() = %d, want 7", got)
	}
}

// BenchmarkDiffResolutions benchmarks diffing large resolution lists.
func BenchmarkDiffResolutions(b *testing.B) {
	// Create lists with 100 modules each, 50% overlap
	oldModules := make([]ModuleToResolve, 100)
	newModules := make([]ModuleToResolve, 100)

	for i := 0; i < 100; i++ {
		oldModules[i] = ModuleToResolve{
			Name:    "module_" + string(rune('a'+i%26)) + string(rune('0'+i/26)),
			Version: "1.0.0",
		}
	}
	for i := 0; i < 100; i++ {
		name := "module_" + string(rune('a'+(i+50)%26)) + string(rune('0'+(i+50)/26))
		newModules[i] = ModuleToResolve{
			Name:    name,
			Version: "2.0.0",
		}
	}

	old := &ResolutionList{Modules: oldModules}
	new := &ResolutionList{Modules: newModules}

	b.ResetTimer()
	for b.Loop() {
		_ = DiffResolutions(old, new)
	}
}
