package gobzlmod

// Core data types for Bazel module dependency resolution

// ModuleInfo represents the information extracted from a MODULE.bazel file
type ModuleInfo struct {
	Name               string       `json:"name"`
	Version            string       `json:"version"`
	CompatibilityLevel int          `json:"compatibility_level"`
	Dependencies       []Dependency `json:"dependencies"`
	Overrides          []Override   `json:"overrides"`
}

// Dependency represents a bazel_dep declaration in a MODULE.bazel file
type Dependency struct {
	Name          string `json:"name"`
	Version       string `json:"version"`
	RepoName      string `json:"repo_name,omitempty"`
	DevDependency bool   `json:"dev_dependency"`
}

// Override represents various override types (single_version, git, local_path, archive)
type Override struct {
	Type       string `json:"type"`
	ModuleName string `json:"module_name"`
	Version    string `json:"version,omitempty"`
	Registry   string `json:"registry,omitempty"`
}

// ResolutionList represents the final list of resolved modules
type ResolutionList struct {
	Modules []ModuleToResolve `json:"modules"`
	Summary ResolutionSummary `json:"summary"`
}

// ModuleToResolve represents a module that needs to be resolved
type ModuleToResolve struct {
	Name          string   `json:"name"`
	Version       string   `json:"version"`
	Registry      string   `json:"registry"`
	DevDependency bool     `json:"dev_dependency"`
	RequiredBy    []string `json:"required_by"`
}

// ResolutionSummary provides statistics about the resolution list
type ResolutionSummary struct {
	TotalModules      int `json:"total_modules"`
	ProductionModules int `json:"production_modules"`
	DevModules        int `json:"dev_modules"`
}

// DepRequest represents a dependency request during resolution
type DepRequest struct {
	Version       string
	DevDependency bool
	RequiredBy    []string
}
