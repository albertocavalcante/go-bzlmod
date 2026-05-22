package lockfile

import (
	"os"
	"path/filepath"
	"testing"
)

// Real-corpus tests pin the lockfile parser against actual Bazel-
// emitted MODULE.bazel.lock files from a range of supported schema
// versions. They exist to catch shape mismatches between the typed
// Go model and the JSON Bazel actually writes — a class of bug that
// synthesized fixtures (with shapes assumed to match the model)
// don't catch.
//
// Each fixture pulls from a public Bazel project; see
// testdata/README.md for provenance.

// realCorpusFixtures lists every (version, file) pair we expect to
// parse end-to-end. Adding a new lockfile version → add a fixture
// here + drop the file in testdata/.
var realCorpusFixtures = []struct {
	version int
	file    string
}{
	{13, "v13_rules_go.lock"},
	{14, "v14_rules_scala.lock"},
	{18, "v18_bazel_gazelle.lock"},
	{26, "v26_bazel.lock"},
}

// TestParse_RealCorpus_ExtractsGeneratedRepoSpecs is the bug-driver
// test: for each supported lockfile version, parsing a real Bazel-
// emitted lockfile must surface at least one module extension entry
// whose GeneratedRepoSpecs is non-empty.
//
// Before the ModuleExtensionData shape fix this asserts FALSE for
// every fixture — the parser was unmarshalling into a model that
// expected an extra "general" wrapper Bazel does not emit, so all
// nested fields silently came back as zero values.
func TestParse_RealCorpus_ExtractsGeneratedRepoSpecs(t *testing.T) {
	for _, tc := range realCorpusFixtures {
		t.Run(tc.file, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", tc.file))
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}
			lf, err := Parse(data)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if lf.Version != tc.version {
				t.Errorf("Version = %d, want %d", lf.Version, tc.version)
			}
			if len(lf.ModuleExtensions) == 0 {
				t.Fatalf("ModuleExtensions is empty — fixture should have at least one")
			}

			// Walk every (extension, scope, repo) triple. The contract
			// real lockfiles satisfy:
			//   - At least one extension has a non-empty
			//     GeneratedRepoSpecs map.
			//   - Across all specs, every spec has a non-empty
			//     RepoRuleID (Bazel always emits one).
			//   - Across all specs, at least one has non-empty
			//     Attributes (most do; compatibility-shim repos
			//     legitimately don't, so we don't enforce per-spec).
			scopesSeen := 0
			totalSpecs := 0
			specsWithAttrs := 0
			missingRepoRuleID := 0
			for _, entry := range lf.ModuleExtensions {
				for _, scopeData := range entry {
					scopesSeen++
					for _, spec := range scopeData.GeneratedRepoSpecs {
						totalSpecs++
						if spec.RepoRuleID == "" {
							missingRepoRuleID++
						}
						if len(spec.Attributes) > 0 {
							specsWithAttrs++
						}
					}
				}
			}
			if totalSpecs == 0 {
				t.Errorf("no GeneratedRepoSpecs surfaced across %d scope entries — type model likely mis-matches Bazel's JSON shape",
					scopesSeen)
			}
			if missingRepoRuleID > 0 {
				t.Errorf("%d / %d specs missing RepoRuleID", missingRepoRuleID, totalSpecs)
			}
			if specsWithAttrs == 0 && totalSpecs > 0 {
				t.Errorf("no specs across %d total have non-empty Attributes — parse likely dropped them",
					totalSpecs)
			}
			t.Logf("v%d: %d scopes, %d specs total, %d with attrs",
				lf.Version, scopesSeen, totalSpecs, specsWithAttrs)
		})
	}
}

// Bidirectional RepoRuleID synthesis: v13/v14 lockfiles (BzlFile +
// RuleClassName) and v18+ (RepoRuleID) both end up with all three
// fields populated after Parse, so consumers can rely on whichever
// form they prefer.
func TestParse_RealCorpus_RepoSpecBidirectionalSynthesis(t *testing.T) {
	for _, tc := range realCorpusFixtures {
		t.Run(tc.file, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", tc.file))
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			lf, err := Parse(data)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			missing := 0
			total := 0
			for _, entry := range lf.ModuleExtensions {
				for _, scope := range entry {
					for _, spec := range scope.GeneratedRepoSpecs {
						total++
						if spec.RepoRuleID == "" || spec.BzlFile == "" || spec.RuleClassName == "" {
							missing++
						}
					}
				}
			}
			if total == 0 {
				t.Skip("no repo specs to verify in this fixture")
			}
			if missing > 0 {
				t.Errorf("%d / %d specs missing one of {RepoRuleID, BzlFile, RuleClassName} after parse — bidirectional synthesis incomplete", missing, total)
			}
		})
	}
}

// TestParse_RealCorpus_VersionRoundTrip pins that Parse correctly
// recovers the lockFileVersion across every fixture (independent of
// the moduleExtensions shape).
func TestParse_RealCorpus_VersionRoundTrip(t *testing.T) {
	for _, tc := range realCorpusFixtures {
		t.Run(tc.file, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", tc.file))
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			lf, err := Parse(data)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if lf.Version != tc.version {
				t.Errorf("Version = %d, want %d", lf.Version, tc.version)
			}
		})
	}
}
