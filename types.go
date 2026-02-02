package gobzlmod

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/albertocavalcante/go-bzlmod/graph"
)

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

	// MaxCompatibilityLevel specifies the maximum compatibility level allowed
	// for this dependency. If set (> 0), the resolved version must have a
	// compatibility_level <= this value. This allows depending on modules
	// that may upgrade within the same compatibility level range.
	// A value of 0 means no constraint (default).
	MaxCompatibilityLevel int `json:"max_compatibility_level,omitempty"`

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

	// Warnings contains non-fatal issues encountered during resolution.
	// For example, yanked version warnings when YankedVersionWarn is used.
	Warnings []string `json:"warnings,omitempty"`

	// Graph is the dependency graph for advanced queries.
	// Use this for bazel mod graph/explain equivalent functionality.
	// Supports: Explain(), Path(), AllPaths(), ToJSON(), ToDOT(), ToText()
	Graph *graph.Graph `json:"-"`
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

	// Depth is the shortest path length from root to this module.
	// 0 = root (not in Modules list), 1 = direct dependency, 2+ = transitive.
	Depth int `json:"depth"`

	// DevDependency indicates if this module is only a dev dependency.
	DevDependency bool `json:"dev_dependency"`

	// Dependencies lists the modules this one depends on (by name).
	// These are the resolved dependency names, not versions.
	Dependencies []string `json:"dependencies,omitempty"`

	// RequiredBy lists the modules that depend on this one.
	RequiredBy []string `json:"required_by"`

	// Yanked indicates if this version has been yanked from the registry.
	// Check YankReason for details on why.
	Yanked bool `json:"yanked,omitempty"`

	// YankReason explains why the version was yanked.
	// Empty if not yanked.
	YankReason string `json:"yank_reason,omitempty"`

	// IsDeprecated indicates the module is deprecated.
	// Check DeprecationReason for details.
	IsDeprecated bool `json:"deprecated,omitempty"`

	// DeprecationReason explains why the module is deprecated.
	DeprecationReason string `json:"deprecation_reason,omitempty"`
}

// Key returns a unique identifier for this module in "name@version" format.
func (m ModuleToResolve) Key() string {
	return m.Name + "@" + m.Version
}

// ResolutionSummary provides statistics about the dependency resolution result.
type ResolutionSummary struct {
	// TotalModules is the total count of resolved modules.
	TotalModules int `json:"total_modules"`

	// ProductionModules is the count of non-dev dependencies.
	ProductionModules int `json:"production_modules"`

	// DevModules is the count of dev-only dependencies.
	DevModules int `json:"dev_modules"`

	// YankedModules is the count of modules with yanked versions.
	YankedModules int `json:"yanked_modules,omitempty"`

	// DeprecatedModules is the count of deprecated modules.
	DeprecatedModules int `json:"deprecated_modules,omitempty"`
}

// YankedVersionBehavior controls how yanked versions are handled during resolution.
type YankedVersionBehavior int

const (
	// YankedVersionAllow allows yanked versions without error (default for backwards compatibility).
	// Yanked info is still populated in the result.
	YankedVersionAllow YankedVersionBehavior = iota

	// YankedVersionWarn allows yanked versions but includes warnings in the result.
	YankedVersionWarn

	// YankedVersionError rejects resolution if any yanked version is selected.
	YankedVersionError
)

// DirectDepsCheckMode controls how direct dependency version mismatches are handled.
type DirectDepsCheckMode int

const (
	// DirectDepsOff disables direct dependency checking (default).
	DirectDepsOff DirectDepsCheckMode = iota

	// DirectDepsWarn includes warnings when direct deps don't match resolved versions.
	DirectDepsWarn

	// DirectDepsError fails resolution if direct deps don't match resolved versions.
	DirectDepsError
)

// NetworkMode controls network access during resolution.
// This enables airgap and restricted network environments.
type NetworkMode int

const (
	// NetworkOnline allows unrestricted network access (default).
	// All configured registries can be accessed.
	NetworkOnline NetworkMode = iota

	// NetworkOffline disables all network access.
	// Only cached data and file:// registries can be used.
	// Useful for fully airgapped environments.
	NetworkOffline

	// NetworkAllowlist restricts network access to allowed domains only.
	// Use with AllowedDomains to specify permitted hosts.
	// Useful for environments with network egress restrictions.
	NetworkAllowlist
)

// ProgressEventType identifies the type of progress event.
type ProgressEventType string

const (
	// ProgressResolveStart is emitted when resolution begins.
	ProgressResolveStart ProgressEventType = "resolve_start"

	// ProgressResolveEnd is emitted when resolution completes.
	ProgressResolveEnd ProgressEventType = "resolve_end"

	// ProgressModuleFetchStart is emitted when fetching a module begins.
	ProgressModuleFetchStart ProgressEventType = "module_fetch_start"

	// ProgressModuleFetchEnd is emitted when fetching a module completes.
	ProgressModuleFetchEnd ProgressEventType = "module_fetch_end"
)

// ProgressEvent contains information about resolution progress.
type ProgressEvent struct {
	// Type identifies the event type.
	Type ProgressEventType `json:"type"`

	// Module is the module name (for module_fetch_* events).
	Module string `json:"module,omitempty"`

	// Version is the module version (for module_fetch_* events).
	Version string `json:"version,omitempty"`

	// Message provides additional context about the event.
	Message string `json:"message,omitempty"`
}

// ResolutionOptions configures the dependency resolution behavior.
type ResolutionOptions struct {
	// IncludeDevDeps includes dev_dependency=True modules in resolution.
	IncludeDevDeps bool

	// YankedBehavior controls how yanked versions are handled.
	// Default is YankedVersionAllow.
	YankedBehavior YankedVersionBehavior

	// CheckYanked enables fetching metadata to detect yanked versions.
	// When false, Yanked/YankReason fields will not be populated.
	// Default is false for backwards compatibility.
	CheckYanked bool

	// AllowYankedVersions lists specific module@version pairs that are allowed
	// even if yanked. Use "all" as special keyword to allow all yanked versions.
	// Format: []string{"module@version", "other@1.0.0"} or []string{"all"}
	// Mirrors Bazel's --allow_yanked_versions flag.
	AllowYankedVersions []string

	// WarnDeprecated enables warnings when deprecated modules are used.
	// Default is false.
	WarnDeprecated bool

	// DirectDepsMode controls validation of direct dependency versions.
	// When enabled, checks if declared versions match resolved versions.
	// Default is DirectDepsOff for backwards compatibility.
	DirectDepsMode DirectDepsCheckMode

	// SubstituteYanked enables automatic substitution of yanked versions
	// with the next non-yanked version in the same compatibility level.
	// This matches Bazel's default behavior.
	// Default is false for backwards compatibility.
	SubstituteYanked bool

	// BazelVersion specifies which Bazel version's behavior to emulate.
	// When set, includes that version's MODULE.tools dependencies in resolution.
	// Format: "7.0.0", "8.0.0", etc.
	// Default is empty (no MODULE.tools deps included).
	BazelVersion string

	// Registries is an ordered list of registry URLs to search for modules.
	// When multiple registries are specified, modules are looked up in order.
	// The first registry where a module is found is used for ALL versions of that module.
	// This matches Bazel's --registry flag behavior.
	// If empty or nil, uses DefaultRegistries (BCR + GitHub mirror).
	//
	// Supported URL schemes:
	//   - https:// - Remote registry (e.g., "https://bcr.bazel.build")
	//   - http://  - Remote registry (not recommended for production)
	//   - file://  - Local registry (e.g., "file:///path/to/registry")
	//
	// Example: []string{"https://registry.example.com", "https://bcr.bazel.build"}
	// Airgap:  []string{"file:///opt/bazel-registry"}
	Registries []string

	// Network controls network access mode for resolution.
	// Default is NetworkOnline (unrestricted access).
	//
	// For airgapped environments, use NetworkOffline with file:// registries.
	// For restricted networks, use NetworkAllowlist with AllowedDomains.
	Network NetworkMode

	// AllowedDomains restricts network access to these domains only.
	// Only used when Network is NetworkAllowlist.
	// Example: []string{"bcr.bazel.build", "registry.example.com"}
	AllowedDomains []string

	// VendorDir specifies a directory containing vendored module files.
	// When set, modules are first looked up in this directory before
	// checking registries. This enables offline/airgap workflows.
	//
	// The vendor directory should have the same structure as a registry:
	//   vendor/modules/{name}/{version}/MODULE.bazel
	//
	// This mirrors Bazel's --vendor_dir flag behavior.
	VendorDir string

	// Timeout specifies the HTTP request timeout for registry requests.
	// When set to a positive value, overrides the default 15 second timeout.
	// Zero or negative values use the default timeout.
	// This is useful for slow networks or testing scenarios.
	//
	// Example: 30 * time.Second for slower networks
	Timeout time.Duration

	// OnProgress is called with progress updates during resolution.
	// This can be used for logging, progress bars, or debugging.
	//
	// The callback must be thread-safe as it may be called concurrently
	// from multiple goroutines during parallel module fetching.
	// The callback should return quickly to avoid blocking resolution.
	//
	// If nil, no progress events are emitted.
	OnProgress func(event ProgressEvent)

	// HTTPClient allows providing a custom HTTP client for registry requests.
	// Use this to configure authentication, custom TLS, proxies, or middleware.
	// If nil, a default client with connection pooling is used.
	//
	// Example with bearer token:
	//
	//	type bearerTransport struct {
	//	    token string
	//	    base  http.RoundTripper
	//	}
	//
	//	func (t *bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	//	    req = req.Clone(req.Context())
	//	    req.Header.Set("Authorization", "Bearer "+t.token)
	//	    return t.base.RoundTrip(req)
	//	}
	//
	//	client := &http.Client{
	//	    Transport: &bearerTransport{token: os.Getenv("TOKEN"), base: http.DefaultTransport},
	//	}
	//	result, _ := Resolve(ctx, content, ResolutionOptions{HTTPClient: client})
	//
	HTTPClient *http.Client

	// Cache provides external caching for MODULE.bazel file contents.
	//
	// When set, the resolver checks the cache before fetching from registries
	// and stores fetched content in the cache for future use. This enables:
	//   - Faster repeated resolutions (avoid redundant network requests)
	//   - Offline resolution after initial cache population
	//   - Shared caching across multiple processes or machines
	//
	// Cache errors are handled gracefully: if Get fails, the resolver fetches
	// from the registry; if Put fails, resolution continues without caching.
	// This ensures caching issues never break dependency resolution.
	//
	// If nil, no external caching is used. Note that in-memory caching within
	// a single resolution is always active regardless of this setting.
	//
	// Implementations must be safe for concurrent use from multiple goroutines.
	Cache ModuleCache
}

// ModuleCache provides external caching for MODULE.bazel file contents.
//
// This interface enables persistent caching across resolutions. Common
// implementations include file-based caches, Redis, memcached, or any
// key-value store. The library does not provide a built-in implementation;
// users should implement this interface based on their infrastructure.
//
// # Thread Safety
//
// Implementations MUST be safe for concurrent use. Multiple goroutines
// will call Get and Put simultaneously during parallel module fetching.
//
// # Error Handling
//
// Cache operations should not fail resolution. The resolver treats cache
// errors as cache misses and continues with registry fetches. Implementations
// may log errors internally but should avoid returning errors for transient
// issues like network timeouts to cache backends.
//
// # Content Format
//
// The content parameter in Get/Put is the raw MODULE.bazel file content
// as fetched from the registry (UTF-8 text). Implementations should store
// this content verbatim without modification.
//
// # Example Implementation
//
// A minimal file-based cache:
//
//	type FileCache struct {
//	    dir string
//	}
//
//	func (c *FileCache) Get(ctx context.Context, name, version string) ([]byte, bool, error) {
//	    path := filepath.Join(c.dir, name, version, "MODULE.bazel")
//	    data, err := os.ReadFile(path)
//	    if os.IsNotExist(err) {
//	        return nil, false, nil // Cache miss, not an error
//	    }
//	    if err != nil {
//	        return nil, false, err
//	    }
//	    return data, true, nil
//	}
//
//	func (c *FileCache) Put(ctx context.Context, name, version string, content []byte) error {
//	    path := filepath.Join(c.dir, name, version, "MODULE.bazel")
//	    if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
//	        return err
//	    }
//	    return os.WriteFile(path, content, 0644)
//	}
type ModuleCache interface {
	// Get retrieves cached MODULE.bazel content for a module version.
	//
	// Returns:
	//   - (content, true, nil)  - Cache hit: content found
	//   - (nil, false, nil)     - Cache miss: content not in cache (normal condition)
	//   - (nil, false, err)     - Cache error: failed to access cache
	//
	// A cache miss (found=false) is a normal condition, not an error.
	// Implementations should only return errors for actual failures like
	// I/O errors or connection failures to cache backends.
	Get(ctx context.Context, name, version string) (content []byte, found bool, err error)

	// Put stores MODULE.bazel content for a module version.
	//
	// The operation should be idempotent: storing the same content multiple
	// times for the same module version should succeed without error.
	//
	// Implementations should handle the content as opaque bytes and store
	// it verbatim. The content is the raw MODULE.bazel file as fetched
	// from the registry.
	Put(ctx context.Context, name, version string, content []byte) error
}

// YankedVersionsError is returned when resolution selects yanked versions
// and YankedVersionError behavior is configured.
type YankedVersionsError struct {
	// Modules contains the yanked modules that were selected.
	Modules []ModuleToResolve
}

func (e *YankedVersionsError) Error() string {
	if len(e.Modules) == 1 {
		m := e.Modules[0]
		return "selected yanked version " + m.Name + "@" + m.Version + ": " + m.YankReason
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("selected %d yanked versions:", len(e.Modules)))
	for _, m := range e.Modules {
		sb.WriteString("\n  - ")
		sb.WriteString(m.Name)
		sb.WriteByte('@')
		sb.WriteString(m.Version)
		sb.WriteString(": ")
		sb.WriteString(m.YankReason)
	}
	return sb.String()
}

// DirectDepMismatch represents a mismatch between declared and resolved versions.
type DirectDepMismatch struct {
	// Name is the module name.
	Name string
	// DeclaredVersion is the version declared in the root MODULE.bazel.
	DeclaredVersion string
	// ResolvedVersion is the version selected by resolution.
	ResolvedVersion string
}

// DirectDepsMismatchError is returned when direct dependencies don't match resolved versions
// and DirectDepsError mode is configured.
type DirectDepsMismatchError struct {
	// Mismatches contains the direct dependencies that don't match.
	Mismatches []DirectDepMismatch
}

func (e *DirectDepsMismatchError) Error() string {
	if len(e.Mismatches) == 1 {
		m := e.Mismatches[0]
		return fmt.Sprintf("direct dependency %s declared as %s but resolved to %s",
			m.Name, m.DeclaredVersion, m.ResolvedVersion)
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%d direct dependencies don't match resolved versions:", len(e.Mismatches)))
	for _, m := range e.Mismatches {
		sb.WriteString("\n  - ")
		sb.WriteString(m.Name)
		sb.WriteString(": declared ")
		sb.WriteString(m.DeclaredVersion)
		sb.WriteString(", resolved ")
		sb.WriteString(m.ResolvedVersion)
	}
	return sb.String()
}

// depRequest tracks a version request during dependency graph construction.
// Multiple modules may request the same dependency at different versions.
type depRequest struct {
	// Version is the requested version.
	Version string

	// DevDependency indicates if this request is for a dev dependency.
	DevDependency bool

	// RequiredBy lists the modules that made this request.
	RequiredBy []string
}

// formatDepPath formats a dependency path for display.
// Example: ["<root>", "A@1.0", "B@1.0"] -> "<root> -> A@1.0 -> B@1.0"
func formatDepPath(path []string) string {
	if len(path) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(path[0])
	for i := 1; i < len(path); i++ {
		b.WriteString(" -> ")
		b.WriteString(path[i])
	}
	return b.String()
}

// MaxDepthExceededError is returned when dependency depth exceeds the maximum allowed.
type MaxDepthExceededError struct {
	// Depth is the depth at which the error occurred.
	Depth int
	// MaxDepth is the maximum allowed depth.
	MaxDepth int
	// Path is the dependency path that exceeded the depth.
	Path []string
}

func (e *MaxDepthExceededError) Error() string {
	return fmt.Sprintf("maximum dependency depth exceeded: depth %d > max %d (path: %s)",
		e.Depth, e.MaxDepth, formatDepPath(e.Path))
}
