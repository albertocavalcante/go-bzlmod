package gobzlmod

// ModuleInfo represents the information extracted from a MODULE.bazel file.
// It contains the module's identity, version, and all its direct dependencies
// and overrides.
type ModuleInfo struct {
	// Name is the module name as declared in module(name = "...").
	Name string `json:"name"`

	// Version is the module version as declared in module(version = "...").
	Version string `json:"version"`

	// CompatibilityLevel indicates breaking changes. Modules with different
	// compatibility levels are considered incompatible.
	CompatibilityLevel int `json:"compatibility_level"`

	// Dependencies lists all bazel_dep declarations in the module file.
	Dependencies []Dependency `json:"dependencies"`

	// Overrides lists all override declarations (single_version, git, etc.).
	Overrides []Override `json:"overrides"`
}

// Dependency represents a bazel_dep declaration in a MODULE.bazel file.
// It declares a direct dependency on another Bazel module.
type Dependency struct {
	// Name is the module name being depended upon.
	Name string `json:"name"`

	// Version is the minimum required version.
	Version string `json:"version"`

	// RepoName overrides the default repository name for this dependency.
	// If empty, the module name is used as the repo name.
	RepoName string `json:"repo_name,omitempty"`

	// DevDependency indicates this is only needed for development/testing.
	DevDependency bool `json:"dev_dependency"`
}

// Override represents version or source overrides for a module dependency.
// Override types include: single_version, git, local_path, archive.
type Override struct {
	// Type is the override type: "single_version", "git", "local_path", or "archive".
	Type string `json:"type"`

	// ModuleName is the name of the module being overridden.
	ModuleName string `json:"module_name"`

	// Version is the pinned version (for single_version overrides).
	Version string `json:"version,omitempty"`

	// Registry overrides the registry URL for this module.
	Registry string `json:"registry,omitempty"`
}

// ResolutionList contains the final resolved dependency set after MVS.
type ResolutionList struct {
	// Modules is the list of all resolved modules, sorted by name.
	Modules []ModuleToResolve `json:"modules"`

	// Summary provides aggregate statistics about the resolution.
	Summary ResolutionSummary `json:"summary"`
}

// ModuleToResolve represents a module selected by dependency resolution.
// It includes the selected version and metadata about why it was included.
type ModuleToResolve struct {
	// Name is the module name.
	Name string `json:"name"`

	// Version is the selected version (highest required by any dependent).
	Version string `json:"version"`

	// Registry is the URL to fetch this module from.
	Registry string `json:"registry"`

	// DevDependency indicates if this module is only a dev dependency.
	DevDependency bool `json:"dev_dependency"`

	// RequiredBy lists the modules that depend on this one.
	RequiredBy []string `json:"required_by"`
}

// ResolutionSummary provides statistics about the dependency resolution result.
type ResolutionSummary struct {
	// TotalModules is the total count of resolved modules.
	TotalModules int `json:"total_modules"`

	// ProductionModules is the count of non-dev dependencies.
	ProductionModules int `json:"production_modules"`

	// DevModules is the count of dev-only dependencies.
	DevModules int `json:"dev_modules"`
}

// DepRequest tracks a version request during dependency graph construction.
// Multiple modules may request the same dependency at different versions.
type DepRequest struct {
	// Version is the requested version.
	Version string

	// DevDependency indicates if this request is for a dev dependency.
	DevDependency bool

	// RequiredBy lists the modules that made this request.
	RequiredBy []string
}
