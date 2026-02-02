package gobzlmod

import "testing"

func TestParseBazelCompatConstraint(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantOp    string
		wantVer   string
		wantError bool
	}{
		{
			name:    "greater than or equal",
			input:   ">=7.0.0",
			wantOp:  ">=",
			wantVer: "7.0.0",
		},
		{
			name:    "less than or equal",
			input:   "<=8.0.0",
			wantOp:  "<=",
			wantVer: "8.0.0",
		},
		{
			name:    "greater than",
			input:   ">6.5.0",
			wantOp:  ">",
			wantVer: "6.5.0",
		},
		{
			name:    "less than",
			input:   "<9.0.0",
			wantOp:  "<",
			wantVer: "9.0.0",
		},
		{
			name:    "exclusion",
			input:   "-7.1.0",
			wantOp:  "-",
			wantVer: "7.1.0",
		},
		{
			name:      "invalid no operator",
			input:     "7.0.0",
			wantError: true,
		},
		{
			name:      "invalid two digit version",
			input:     ">=7.0",
			wantError: true,
		},
		{
			name:      "invalid empty",
			input:     "",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			constraint, err := parseBazelCompatConstraint(tt.input)
			if tt.wantError {
				if err == nil {
					t.Errorf("parseBazelCompatConstraint(%q) expected error, got nil", tt.input)
				}
				return
			}
			if err != nil {
				t.Errorf("parseBazelCompatConstraint(%q) unexpected error: %v", tt.input, err)
				return
			}
			if constraint.operator != tt.wantOp {
				t.Errorf("parseBazelCompatConstraint(%q) operator = %q, want %q", tt.input, constraint.operator, tt.wantOp)
			}
			if constraint.version != tt.wantVer {
				t.Errorf("parseBazelCompatConstraint(%q) version = %q, want %q", tt.input, constraint.version, tt.wantVer)
			}
		})
	}
}

func TestBazelCompatConstraintCheck(t *testing.T) {
	tests := []struct {
		name         string
		constraint   string
		bazelVersion string
		want         bool
	}{
		// >= tests
		{">=7.0.0 with 7.0.0", ">=7.0.0", "7.0.0", true},
		{">=7.0.0 with 8.0.0", ">=7.0.0", "8.0.0", true},
		{">=7.0.0 with 6.5.0", ">=7.0.0", "6.5.0", false},
		// <= tests
		{"<=8.0.0 with 8.0.0", "<=8.0.0", "8.0.0", true},
		{"<=8.0.0 with 7.0.0", "<=8.0.0", "7.0.0", true},
		{"<=8.0.0 with 9.0.0", "<=8.0.0", "9.0.0", false},
		// > tests
		{">7.0.0 with 7.0.0", ">7.0.0", "7.0.0", false},
		{">7.0.0 with 7.0.1", ">7.0.0", "7.0.1", true},
		// < tests
		{"<8.0.0 with 8.0.0", "<8.0.0", "8.0.0", false},
		{"<8.0.0 with 7.9.9", "<8.0.0", "7.9.9", true},
		// - (exclusion) tests
		{"-7.1.0 with 7.1.0", "-7.1.0", "7.1.0", false},
		{"-7.1.0 with 7.0.0", "-7.1.0", "7.0.0", true},
		{"-7.1.0 with 7.2.0", "-7.1.0", "7.2.0", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			constraint, err := parseBazelCompatConstraint(tt.constraint)
			if err != nil {
				t.Fatalf("parseBazelCompatConstraint(%q) error: %v", tt.constraint, err)
			}
			got := constraint.check(tt.bazelVersion)
			if got != tt.want {
				t.Errorf("constraint(%q).check(%q) = %v, want %v", tt.constraint, tt.bazelVersion, got, tt.want)
			}
		})
	}
}

func TestCheckBazelCompatibility(t *testing.T) {
	tests := []struct {
		name         string
		bazelVersion string
		constraints  []string
		wantCompat   bool
		wantReason   string
	}{
		{
			name:         "empty constraints",
			bazelVersion: "7.0.0",
			constraints:  nil,
			wantCompat:   true,
		},
		{
			name:         "empty bazel version",
			bazelVersion: "",
			constraints:  []string{">=7.0.0"},
			wantCompat:   true,
		},
		{
			name:         "single constraint satisfied",
			bazelVersion: "7.0.0",
			constraints:  []string{">=7.0.0"},
			wantCompat:   true,
		},
		{
			name:         "single constraint not satisfied",
			bazelVersion: "6.5.0",
			constraints:  []string{">=7.0.0"},
			wantCompat:   false,
			wantReason:   "requires >=7.0.0",
		},
		{
			name:         "range constraints all satisfied",
			bazelVersion: "7.5.0",
			constraints:  []string{">=7.0.0", "<8.0.0"},
			wantCompat:   true,
		},
		{
			name:         "range constraints lower bound not satisfied",
			bazelVersion: "6.5.0",
			constraints:  []string{">=7.0.0", "<8.0.0"},
			wantCompat:   false,
			wantReason:   "requires >=7.0.0",
		},
		{
			name:         "range constraints upper bound not satisfied",
			bazelVersion: "8.5.0",
			constraints:  []string{">=7.0.0", "<8.0.0"},
			wantCompat:   false,
			wantReason:   "requires <8.0.0",
		},
		{
			name:         "exclusion constraint satisfied",
			bazelVersion: "7.0.0",
			constraints:  []string{">=7.0.0", "-7.1.0"},
			wantCompat:   true,
		},
		{
			name:         "exclusion constraint not satisfied",
			bazelVersion: "7.1.0",
			constraints:  []string{">=7.0.0", "-7.1.0"},
			wantCompat:   false,
			wantReason:   "requires -7.1.0",
		},
		{
			name:         "multiple failed constraints",
			bazelVersion: "6.0.0",
			constraints:  []string{">=7.0.0", "<8.0.0", ">6.0.0"},
			wantCompat:   false,
			wantReason:   "requires >=7.0.0 and >6.0.0",
		},
		{
			name:         "prerelease bazel version normalized",
			bazelVersion: "7.0.0-pre.20231115.1",
			constraints:  []string{">=7.0.0"},
			wantCompat:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotCompat, gotReason := checkBazelCompatibility(tt.bazelVersion, tt.constraints)
			if gotCompat != tt.wantCompat {
				t.Errorf("checkBazelCompatibility(%q, %v) compatible = %v, want %v",
					tt.bazelVersion, tt.constraints, gotCompat, tt.wantCompat)
			}
			if gotReason != tt.wantReason {
				t.Errorf("checkBazelCompatibility(%q, %v) reason = %q, want %q",
					tt.bazelVersion, tt.constraints, gotReason, tt.wantReason)
			}
		})
	}
}

func TestNormalizeBazelVersion(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"7.0.0", "7.0.0"},
		{"7.0.0-pre.20231115.1", "7.0.0"},
		{"8.0.0+build123", "8.0.0"},
		{"7.1.2-rc1", "7.1.2"},
		{"7.0", "7.0"}, // Not enough parts, return as-is
		{"abc", "abc"}, // Not a version, return as-is
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := normalizeBazelVersion(tt.input)
			if got != tt.want {
				t.Errorf("normalizeBazelVersion(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestCheckModuleBazelCompatibility(t *testing.T) {
	modules := []ModuleToResolve{
		{Name: "module_a", Version: "1.0.0"},
		{Name: "module_b", Version: "2.0.0"},
		{Name: "module_c", Version: "3.0.0"},
	}

	moduleInfoCache := map[string]*ModuleInfo{
		"module_a@1.0.0": {
			Name:               "module_a",
			Version:            "1.0.0",
			BazelCompatibility: []string{">=7.0.0"},
		},
		"module_b@2.0.0": {
			Name:               "module_b",
			Version:            "2.0.0",
			BazelCompatibility: []string{">=8.0.0"},
		},
		// module_c has no bazel_compatibility constraints
	}

	// Test with Bazel 7.5.0 - module_b should be incompatible
	bazelVersion := "7.5.0"
	checkModuleBazelCompatibility(modules, moduleInfoCache, bazelVersion)

	// Check module_a - should be compatible
	if modules[0].IsBazelIncompatible {
		t.Errorf("module_a should be compatible with Bazel %s", bazelVersion)
	}
	if len(modules[0].BazelCompatibility) != 1 || modules[0].BazelCompatibility[0] != ">=7.0.0" {
		t.Errorf("module_a BazelCompatibility not populated correctly")
	}

	// Check module_b - should be incompatible
	if !modules[1].IsBazelIncompatible {
		t.Errorf("module_b should be incompatible with Bazel %s", bazelVersion)
	}
	if modules[1].BazelIncompatibilityReason != "requires >=8.0.0" {
		t.Errorf("module_b incompatibility reason = %q, want %q",
			modules[1].BazelIncompatibilityReason, "requires >=8.0.0")
	}

	// Check module_c - should have no constraints
	if modules[2].IsBazelIncompatible {
		t.Errorf("module_c should be compatible (no constraints)")
	}
	if len(modules[2].BazelCompatibility) != 0 {
		t.Errorf("module_c should have no BazelCompatibility constraints")
	}
}
