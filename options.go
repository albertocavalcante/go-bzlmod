package gobzlmod

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"
)

// Option configures resolution behavior.
type Option func(*resolverConfig) error

// resolverConfig holds all resolution configuration.
type resolverConfig struct {
	includeDevDeps      bool
	yankedBehavior      YankedVersionBehavior
	checkYanked         bool
	allowYankedVersions []string
	warnDeprecated      bool
	directDepsMode      DirectDepsCheckMode
	substituteYanked    bool
	bazelVersion        string
	registries          []string
	vendorDir           string
	timeout             time.Duration
	onProgress          func(ProgressEvent)
	httpClient          *http.Client
	cache               ModuleCache

	// logger is the structured logger for debug/info output.
	// If nil, logging is disabled (silent mode).
	//
	// Design decision: We use *slog.Logger (Go 1.21+ stdlib) rather than a custom
	// interface because slog provides frontend/backend separation by design.
	// Users can plug in any backend (zap, zerolog, etc.) via slog handlers.
	// See: https://go.dev/blog/slog
	logger *slog.Logger
}

// DefaultOptions returns options with safe defaults that match Bazel's behavior.
// These enable yanked version checking and deprecated warnings by default.
func DefaultOptions() []Option {
	return []Option{
		WithYankedCheck(true),
		WithYankedBehavior(YankedVersionWarn),
		WithDeprecatedWarnings(true),
		WithSubstituteYanked(true),
		WithTimeout(15 * time.Second),
	}
}

// WithDevDeps includes dev_dependency modules in resolution.
func WithDevDeps() Option {
	return func(c *resolverConfig) error {
		c.includeDevDeps = true
		return nil
	}
}

// WithYankedBehavior sets how yanked versions are handled.
func WithYankedBehavior(b YankedVersionBehavior) Option {
	return func(c *resolverConfig) error {
		c.yankedBehavior = b
		return nil
	}
}

// WithYankedCheck enables or disables yanked version detection.
func WithYankedCheck(check bool) Option {
	return func(c *resolverConfig) error {
		c.checkYanked = check
		return nil
	}
}

// WithAllowedYankedVersions whitelists specific yanked versions.
func WithAllowedYankedVersions(versions ...string) Option {
	return func(c *resolverConfig) error {
		c.allowYankedVersions = append(c.allowYankedVersions, versions...)
		return nil
	}
}

// WithDeprecatedWarnings enables warnings for deprecated modules.
func WithDeprecatedWarnings(warn bool) Option {
	return func(c *resolverConfig) error {
		c.warnDeprecated = warn
		return nil
	}
}

// WithDirectDepsMode sets how direct dependency versions are validated.
func WithDirectDepsMode(mode DirectDepsCheckMode) Option {
	return func(c *resolverConfig) error {
		c.directDepsMode = mode
		return nil
	}
}

// WithSubstituteYanked enables automatic substitution of yanked versions.
func WithSubstituteYanked(substitute bool) Option {
	return func(c *resolverConfig) error {
		c.substituteYanked = substitute
		return nil
	}
}

// WithBazelVersion sets a specific Bazel version to emulate.
func WithBazelVersion(version string) Option {
	return func(c *resolverConfig) error {
		c.bazelVersion = version
		return nil
	}
}

// WithRegistries sets the registry URLs to use (in priority order).
func WithRegistries(urls ...string) Option {
	return func(c *resolverConfig) error {
		c.registries = append(c.registries, urls...)
		return nil
	}
}

// WithVendorDir sets the local vendor directory for modules.
func WithVendorDir(dir string) Option {
	return func(c *resolverConfig) error {
		c.vendorDir = dir
		return nil
	}
}

// WithTimeout sets the HTTP request timeout.
func WithTimeout(d time.Duration) Option {
	return func(c *resolverConfig) error {
		c.timeout = d
		return nil
	}
}

// WithProgress sets a callback for resolution progress events.
func WithProgress(fn func(ProgressEvent)) Option {
	return func(c *resolverConfig) error {
		c.onProgress = fn
		return nil
	}
}

// WithHTTPClient sets a custom HTTP client for registry requests.
func WithHTTPClient(client *http.Client) Option {
	return func(c *resolverConfig) error {
		c.httpClient = client
		return nil
	}
}

// WithCache sets an external cache for MODULE.bazel files.
func WithCache(cache ModuleCache) Option {
	return func(c *resolverConfig) error {
		c.cache = cache
		return nil
	}
}

// WithLogger sets a structured logger for resolution diagnostics.
// If not set, logging is disabled (silent mode).
//
// The library uses log/slog (Go 1.21+) which supports any backend via handlers.
// For example, zap users can use: slog.New(zapslog.NewHandler(zapCore, nil))
//
// Example:
//
//	// Use default logger
//	Resolve(ctx, src, WithLogger(slog.Default()))
//
//	// Use custom logger with attributes
//	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil)).With("component", "bzlmod")
//	Resolve(ctx, src, WithLogger(logger))
func WithLogger(l *slog.Logger) Option {
	return func(c *resolverConfig) error {
		c.logger = l
		return nil
	}
}

// validate checks the configuration for logical consistency.
func (c *resolverConfig) validate() error {
	// If substituteYanked is true, checkYanked must also be true
	if c.substituteYanked && !c.checkYanked {
		return errors.New("substituteYanked requires checkYanked to be enabled")
	}

	// timeout must be positive if set
	if c.timeout < 0 {
		return errors.New("timeout must be positive")
	}

	return nil
}

// log returns the configured logger, or a no-op logger if none was set.
// This allows internal code to call logging methods without nil checks.
//
// Design: Libraries should be silent by default. Users opt-in to logging
// via WithLogger(). This avoids surprising output and respects the principle
// that libraries shouldn't write to stdout/stderr without explicit consent.
func (c *resolverConfig) log() *slog.Logger {
	if c.logger != nil {
		return c.logger
	}
	// Return a logger that discards all output
	return slog.New(discardHandler{})
}

// discardHandler is a slog.Handler that discards all log records.
// This is used when no logger is configured to avoid nil checks throughout the code.
type discardHandler struct{}

func (discardHandler) Enabled(context.Context, slog.Level) bool  { return false }
func (discardHandler) Handle(context.Context, slog.Record) error { return nil }
func (d discardHandler) WithAttrs([]slog.Attr) slog.Handler      { return d }
func (d discardHandler) WithGroup(string) slog.Handler           { return d }

// newResolverConfig creates a new resolver configuration by applying
// the given options and validating the result.
func newResolverConfig(opts ...Option) (*resolverConfig, error) {
	// Create config with zero values
	c := &resolverConfig{}

	// Apply all options
	for _, opt := range opts {
		if err := opt(c); err != nil {
			return nil, err
		}
	}

	// Validate the configuration
	if err := c.validate(); err != nil {
		return nil, err
	}

	return c, nil
}

// toResolutionOptions converts the resolver config to the existing
// ResolutionOptions struct for backward compatibility.
func (c *resolverConfig) toResolutionOptions() ResolutionOptions {
	return ResolutionOptions{
		IncludeDevDeps:      c.includeDevDeps,
		YankedBehavior:      c.yankedBehavior,
		CheckYanked:         c.checkYanked,
		AllowYankedVersions: c.allowYankedVersions,
		WarnDeprecated:      c.warnDeprecated,
		DirectDepsMode:      c.directDepsMode,
		SubstituteYanked:    c.substituteYanked,
		BazelVersion:        c.bazelVersion,
		Registries:          c.registries,
		VendorDir:           c.vendorDir,
		Timeout:             c.timeout,
		OnProgress:          c.onProgress,
		HTTPClient:          c.httpClient,
		Cache:               c.cache,
		Logger:              c.logger,
	}
}
