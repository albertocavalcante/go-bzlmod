package gobzlmod

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

// Option configures resolution behavior.
type Option func(*ResolutionOptions) error

// DefaultOptions enables yank detection, warnings, deprecation checks, and a
// 15-second timeout. Dev dependencies remain excluded; these are library defaults.
func DefaultOptions() []Option {
	return []Option{
		WithYankedCheck(true),
		WithYankedBehavior(YankedVersionWarn),
		WithDeprecatedWarnings(true),
		WithTimeout(15 * time.Second),
	}
}

// WithDevDeps includes dev_dependency modules in resolution.
func WithDevDeps() Option {
	return func(o *ResolutionOptions) error { o.IncludeDevDeps = true; return nil }
}

// WithYankedBehavior sets how yanked versions are handled.
func WithYankedBehavior(b YankedVersionBehavior) Option {
	return func(o *ResolutionOptions) error { o.YankedBehavior = b; return nil }
}

// WithYankedCheck enables or disables yanked version detection.
func WithYankedCheck(check bool) Option {
	return func(o *ResolutionOptions) error { o.CheckYanked = check; return nil }
}

// WithAllowedYankedVersions whitelists specific yanked versions.
func WithAllowedYankedVersions(versions ...string) Option {
	return func(o *ResolutionOptions) error {
		o.AllowYankedVersions = append(o.AllowYankedVersions, versions...)
		return nil
	}
}

// WithDeprecatedWarnings enables warnings for deprecated modules.
func WithDeprecatedWarnings(warn bool) Option {
	return func(o *ResolutionOptions) error { o.WarnDeprecated = warn; return nil }
}

// WithRegistryTrace enables Bazel-style registry tracing.
//
// When enabled, resolution records the canonical registry URLs for MODULE.bazel
// and source.json files accessed during resolution. The resulting trace is exposed
// on ResolutionList.RegistryFileHashes, and ModuleToResolve.Source is populated
// for registry-backed modules.
func WithRegistryTrace() Option {
	return func(o *ResolutionOptions) error { o.TraceRegistryFiles = true; return nil }
}

// WithDirectDepsMode sets how direct dependency versions are validated.
func WithDirectDepsMode(mode DirectDepsCheckMode) Option {
	return func(o *ResolutionOptions) error { o.DirectDepsMode = mode; return nil }
}

// WithSubstituteYanked retains the reserved substitution option. Passing true
// returns errors.ErrUnsupported; resolution never silently changes yanked versions.
func WithSubstituteYanked(substitute bool) Option {
	return func(o *ResolutionOptions) error { o.SubstituteYanked = substitute; return nil }
}

// WithBazelCompatibilityMode sets how Bazel compatibility constraints are validated.
// Requires WithBazelVersion to be set for validation to occur.
func WithBazelCompatibilityMode(mode BazelCompatibilityMode) Option {
	return func(o *ResolutionOptions) error { o.BazelCompatibilityMode = mode; return nil }
}

// WithBazelVersion sets a specific Bazel version to emulate.
func WithBazelVersion(version string) Option {
	return func(o *ResolutionOptions) error { o.BazelVersion = version; return nil }
}

// WithIncludeBuiltinModules exposes Bazel built-in MODULE.tools deps in the
// visible resolution result and graph, mirroring `bazel mod graph --include_builtin`.
func WithIncludeBuiltinModules(include bool) Option {
	return func(o *ResolutionOptions) error { o.IncludeBuiltinModules = include; return nil }
}

// WithIncludeUnusedModules exposes modules from the unpruned post-selection
// graph, mirroring `bazel mod graph --include_unused`.
func WithIncludeUnusedModules(include bool) Option {
	return func(o *ResolutionOptions) error { o.IncludeUnusedModules = include; return nil }
}

// WithBazelToolsLookup sets a custom MODULE.tools lookup for Bazel version emulation.
//
// The supplied lookup replaces the built-in mapping when resolving implicit
// MODULE.tools dependencies. To extend the built-in data instead of replacing it,
// call bazeltools.LookupDeps(version) from inside the callback as a fallback.
func WithBazelToolsLookup(lookup BazelToolsLookup) Option {
	return func(o *ResolutionOptions) error { o.BazelToolsLookup = lookup; return nil }
}

// WithBazelToolsTransformer post-processes implicit MODULE.tools dependencies
// after lookup/default resolution.
func WithBazelToolsTransformer(transformer BazelToolsTransformer) Option {
	return func(o *ResolutionOptions) error { o.BazelToolsTransformer = transformer; return nil }
}

// WithRegistries sets the registry URLs to use (in priority order).
func WithRegistries(urls ...string) Option {
	return func(o *ResolutionOptions) error {
		o.Registries = append(o.Registries, urls...)
		return nil
	}
}

// WithVendorDir is reserved. A nonempty value returns errors.ErrUnsupported.
// To resolve a local registry layout, use WithRegistries("file:///path").
func WithVendorDir(dir string) Option {
	return func(o *ResolutionOptions) error { o.VendorDir = dir; return nil }
}

// WithLockfileMode is reserved. Modes other than LockfileOff return
// errors.ErrUnsupported. Use the lockfile package for explicit parsing/writing.
func WithLockfileMode(mode LockfileMode) Option {
	return func(o *ResolutionOptions) error { o.LockfileMode = mode; return nil }
}

// WithLockfilePath is reserved. A nonempty path returns errors.ErrUnsupported.
func WithLockfilePath(path string) Option {
	return func(o *ResolutionOptions) error { o.LockfilePath = path; return nil }
}

// WithTimeout sets the HTTP request timeout.
func WithTimeout(d time.Duration) Option {
	return func(o *ResolutionOptions) error { o.Timeout = d; return nil }
}

// WithProgress sets a callback for resolution progress events.
func WithProgress(fn func(ProgressEvent)) Option {
	return func(o *ResolutionOptions) error { o.OnProgress = fn; return nil }
}

// WithHTTPClient sets a custom HTTP client for registry requests.
func WithHTTPClient(client *http.Client) Option {
	return func(o *ResolutionOptions) error { o.HTTPClient = client; return nil }
}

// WithCache sets an external cache for MODULE.bazel files.
func WithCache(cache ModuleCache) Option {
	return func(o *ResolutionOptions) error { o.Cache = cache; return nil }
}

// WithLogger sets a structured logger for resolution diagnostics.
// If not set, logging is disabled (silent mode).
//
// Uses log/slog (Go 1.21+) which supports any backend via handlers.
func WithLogger(l *slog.Logger) Option {
	return func(o *ResolutionOptions) error { o.Logger = l; return nil }
}

// validateOptions checks configuration for logical consistency.
func validateOptions(opts *ResolutionOptions) error {
	if opts.SubstituteYanked && !opts.CheckYanked {
		return fmt.Errorf("substituteYanked requires checkYanked to be enabled; substitution: %w", errors.ErrUnsupported)
	}
	for _, setting := range []struct {
		name   string
		active bool
	}{
		{"SubstituteYanked", opts.SubstituteYanked},
		{"VendorDir", opts.VendorDir != ""},
		{"LockfileMode", opts.LockfileMode != LockfileOff},
		{"LockfilePath", opts.LockfilePath != ""},
	} {
		if setting.active {
			return fmt.Errorf("resolution option %s: %w", setting.name, errors.ErrUnsupported)
		}
	}
	if opts.DirectDepsMode < DirectDepsOff || opts.DirectDepsMode > DirectDepsError {
		return fmt.Errorf("invalid direct dependency mode: %d", opts.DirectDepsMode)
	}
	if opts.YankedBehavior < YankedVersionAllow || opts.YankedBehavior > YankedVersionError {
		return fmt.Errorf("invalid yanked version behavior: %d", opts.YankedBehavior)
	}
	if opts.BazelCompatibilityMode < BazelCompatibilityOff || opts.BazelCompatibilityMode > BazelCompatibilityError {
		return fmt.Errorf("invalid Bazel compatibility mode: %d", opts.BazelCompatibilityMode)
	}
	if opts.Timeout < 0 {
		return errors.New("timeout must be positive")
	}
	return nil
}

// applyOptions applies functional options to a ResolutionOptions and validates.
func applyOptions(opts ...Option) (ResolutionOptions, error) {
	var o ResolutionOptions
	for _, opt := range opts {
		if err := opt(&o); err != nil {
			return o, err
		}
	}
	if err := validateOptions(&o); err != nil {
		return o, err
	}
	return o, nil
}

// discardHandler is a slog.Handler that discards all log records.
type discardHandler struct{}

func (discardHandler) Enabled(context.Context, slog.Level) bool  { return false }
func (discardHandler) Handle(context.Context, slog.Record) error { return nil }
func (d discardHandler) WithAttrs([]slog.Attr) slog.Handler      { return d }
func (d discardHandler) WithGroup(string) slog.Handler           { return d }
