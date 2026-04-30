package gobzlmod

import (
	"testing"
)

func TestComputeSummary(t *testing.T) {
	list := &ResolutionList{
		Modules: []ModuleToResolve{
			{Name: "a", Version: "1.0.0"},
			{Name: "b", Version: "2.0.0", DevDependency: true},
			{Name: "c", Version: "1.0.0", Yanked: true},
			{Name: "d", Version: "1.0.0", IsDeprecated: true},
			{Name: "e", Version: "1.0.0", IsBazelIncompatible: true},
		},
	}
	computeSummary(list)

	if list.Summary.TotalModules != 5 {
		t.Errorf("TotalModules = %d, want 5", list.Summary.TotalModules)
	}
	if list.Summary.ProductionModules != 4 {
		t.Errorf("ProductionModules = %d, want 4", list.Summary.ProductionModules)
	}
	if list.Summary.DevModules != 1 {
		t.Errorf("DevModules = %d, want 1", list.Summary.DevModules)
	}
	if list.Summary.YankedModules != 1 {
		t.Errorf("YankedModules = %d, want 1", list.Summary.YankedModules)
	}
	if list.Summary.DeprecatedModules != 1 {
		t.Errorf("DeprecatedModules = %d, want 1", list.Summary.DeprecatedModules)
	}
	if list.Summary.IncompatibleModules != 1 {
		t.Errorf("IncompatibleModules = %d, want 1", list.Summary.IncompatibleModules)
	}
}

func TestApplyYankedBehavior_Allow(t *testing.T) {
	list := &ResolutionList{
		Modules: []ModuleToResolve{
			{Name: "a", Version: "1.0.0", Yanked: true, YankReason: "bad"},
		},
	}
	computeSummary(list)
	err := applyYankedBehavior(list, ResolutionOptions{YankedBehavior: YankedVersionAllow})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(list.Warnings) != 0 {
		t.Errorf("expected no warnings, got %v", list.Warnings)
	}
}

func TestApplyYankedBehavior_Warn(t *testing.T) {
	list := &ResolutionList{
		Modules: []ModuleToResolve{
			{Name: "a", Version: "1.0.0", Yanked: true, YankReason: "bad"},
			{Name: "b", Version: "2.0.0"},
		},
	}
	computeSummary(list)
	err := applyYankedBehavior(list, ResolutionOptions{YankedBehavior: YankedVersionWarn})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(list.Warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d", len(list.Warnings))
	}
}

func TestApplyYankedBehavior_Error(t *testing.T) {
	list := &ResolutionList{
		Modules: []ModuleToResolve{
			{Name: "a", Version: "1.0.0", Yanked: true, YankReason: "bad"},
		},
	}
	computeSummary(list)
	err := applyYankedBehavior(list, ResolutionOptions{YankedBehavior: YankedVersionError})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	yankedErr, ok := err.(*YankedVersionsError)
	if !ok {
		t.Fatalf("expected *YankedVersionsError, got %T", err)
	}
	if len(yankedErr.Modules) != 1 {
		t.Errorf("expected 1 yanked module, got %d", len(yankedErr.Modules))
	}
}

func TestApplyYankedBehavior_NoYanked(t *testing.T) {
	list := &ResolutionList{
		Modules: []ModuleToResolve{{Name: "a", Version: "1.0.0"}},
	}
	computeSummary(list)
	err := applyYankedBehavior(list, ResolutionOptions{YankedBehavior: YankedVersionError})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestApplyDeprecatedWarnings(t *testing.T) {
	list := &ResolutionList{
		Modules: []ModuleToResolve{
			{Name: "a", Version: "1.0.0", IsDeprecated: true, DeprecationReason: "use b"},
			{Name: "b", Version: "2.0.0"},
		},
	}
	computeSummary(list)

	// Disabled: no warnings added
	applyDeprecatedWarnings(list, ResolutionOptions{WarnDeprecated: false})
	if len(list.Warnings) != 0 {
		t.Errorf("expected no warnings when disabled, got %v", list.Warnings)
	}

	// Enabled: warning added
	applyDeprecatedWarnings(list, ResolutionOptions{WarnDeprecated: true})
	if len(list.Warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d", len(list.Warnings))
	}
}

func TestApplyBazelCompatBehavior_Warn(t *testing.T) {
	list := &ResolutionList{
		Modules: []ModuleToResolve{
			{Name: "a", Version: "1.0.0", IsBazelIncompatible: true, BazelIncompatibilityReason: "needs >=8"},
		},
	}
	computeSummary(list)
	err := applyBazelCompatBehavior(list, ResolutionOptions{
		BazelCompatibilityMode: BazelCompatibilityWarn,
		BazelVersion:           "7.0.0",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(list.Warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d", len(list.Warnings))
	}
}

func TestApplyBazelCompatBehavior_Error(t *testing.T) {
	list := &ResolutionList{
		Modules: []ModuleToResolve{
			{Name: "a", Version: "1.0.0", IsBazelIncompatible: true, BazelIncompatibilityReason: "needs >=8"},
		},
	}
	computeSummary(list)
	err := applyBazelCompatBehavior(list, ResolutionOptions{
		BazelCompatibilityMode: BazelCompatibilityError,
		BazelVersion:           "7.0.0",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	compatErr, ok := err.(*BazelIncompatibilityError)
	if !ok {
		t.Fatalf("expected *BazelIncompatibilityError, got %T", err)
	}
	if len(compatErr.Modules) != 1 {
		t.Errorf("expected 1 incompatible module, got %d", len(compatErr.Modules))
	}
}

func TestApplyBazelCompatBehavior_Off(t *testing.T) {
	list := &ResolutionList{
		Modules: []ModuleToResolve{
			{Name: "a", Version: "1.0.0", IsBazelIncompatible: true},
		},
	}
	computeSummary(list)
	err := applyBazelCompatBehavior(list, ResolutionOptions{
		BazelCompatibilityMode: BazelCompatibilityOff,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(list.Warnings) != 0 {
		t.Errorf("expected no warnings, got %v", list.Warnings)
	}
}
