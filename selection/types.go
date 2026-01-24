// Package selection implements Bazel's module version selection algorithm.
//
// This is a Go port of Bazel's Selection.java, implementing the Minimal Version Selection
// algorithm with Bazel-specific extensions for compatibility levels and overrides.
//
// Reference: https://github.com/bazelbuild/bazel/blob/master/src/main/java/com/google/devtools/build/lib/bazel/bzlmod/Selection.java
package selection

// ModuleKey uniquely identifies a module in the dependency graph.
// Corresponds to Bazel's ModuleKey.java.
type ModuleKey struct {
	Name    string
	Version string
}

// String returns the module key as "name@version" or "name@_" if version is empty.
func (k ModuleKey) String() string {
	if k.Version == "" {
		return k.Name + "@_"
	}
	return k.Name + "@" + k.Version
}

// DepSpec represents a dependency specification.
// Corresponds to Bazel's InterimModule.DepSpec.
//
// Reference: InterimModule.java lines 59-81
type DepSpec struct {
	Name    string
	Version string
	// MaxCompatibilityLevel allows depending on modules with compatibility levels
	// up to this value. -1 means no max (use the dep's own compat level).
	// Reference: InterimModule.java line 61
	MaxCompatibilityLevel int
}

// ToModuleKey converts a DepSpec to a ModuleKey.
func (d DepSpec) ToModuleKey() ModuleKey {
	return ModuleKey{Name: d.Name, Version: d.Version}
}

// Module represents a node in the dependency graph during resolution.
// Corresponds to Bazel's InterimModule.java.
type Module struct {
	Key         ModuleKey
	Deps        []DepSpec
	CompatLevel int // The compatibility_level from module(), default 0

	// NodepDeps are dependencies that participate in version selection but don't
	// create transitive dependency edges during graph pruning. Modules reachable
	// only via NodepDeps are not included in the final resolved graph.
	// Introduced in Bazel 7.6+.
	// Reference: Selection.java lines 397-403
	NodepDeps []DepSpec
}

// DepGraph represents the complete dependency graph before selection.
type DepGraph struct {
	Modules map[ModuleKey]*Module
	RootKey ModuleKey
}

// Override is the interface for all override types.
type Override interface {
	isOverride()
}

// SingleVersionOverride forces a specific version for a module.
// Corresponds to Bazel's SingleVersionOverride.java.
type SingleVersionOverride struct {
	Version  string
	Registry string
	Patches  []string
}

func (o *SingleVersionOverride) isOverride() {}

// MultipleVersionOverride allows multiple versions of the same module.
// Corresponds to Bazel's MultipleVersionOverride.java.
//
// Reference: Selection.java lines 64-73
// "If module foo has a multiple-version override which allows versions [1.3, 1.5, 2.0],
// then we further split the selection groups by the target allowed version."
type MultipleVersionOverride struct {
	Versions []string
	Registry string
}

func (o *MultipleVersionOverride) isOverride() {}

// NonRegistryOverride represents git_override, local_path_override, or archive_override.
// These override the module source entirely, so version becomes empty.
type NonRegistryOverride struct {
	// Type is "git", "local_path", or "archive"
	Type string
}

func (o *NonRegistryOverride) isOverride() {}

// Result contains the output of the selection algorithm.
//
// Reference: Selection.java lines 88-100
type Result struct {
	// ResolvedGraph is the final dep graph with unused modules removed.
	// Sorted in BFS iteration order.
	ResolvedGraph map[ModuleKey]*Module

	// UnprunedGraph contains all modules including unused ones.
	// Useful for inspection/debugging.
	UnprunedGraph map[ModuleKey]*Module

	// BFSOrder maintains the breadth-first traversal order of modules.
	BFSOrder []ModuleKey
}

// SelectionGroup identifies a group of module versions that compete for selection.
// One version is selected per SelectionGroup.
//
// Reference: Selection.java lines 102-107
// "During selection, a version is selected for each distinct 'selection group'."
type SelectionGroup struct {
	ModuleName  string
	CompatLevel int
	// TargetAllowedVersion is only used for modules with multiple-version overrides.
	// Empty string means no multiple-version override.
	TargetAllowedVersion string
}

// SelectionError represents an error during version selection.
type SelectionError struct {
	Code    string
	Message string
}

func (e *SelectionError) Error() string {
	return e.Message
}
