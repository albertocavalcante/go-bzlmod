package gobzlmod

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/albertocavalcante/go-bzlmod/registry"
)

// DefaultRegistry is the Bazel Central Registry (BCR) URL.
// This matches Bazel's DEFAULT_REGISTRIES in BazelRepositoryModule.java.
const DefaultRegistry = "https://bcr.bazel.build"

// DefaultRegistryMirror is the GitHub-hosted mirror of BCR.
// This serves as a fallback when bcr.bazel.build is unavailable.
// The BCR has experienced outages due to certificate expiration.
// Using raw.githubusercontent.com provides resilience against such incidents.
//
// Reference: https://github.com/bazelbuild/bazel/issues/28101
const DefaultRegistryMirror = "https://raw.githubusercontent.com/bazelbuild/bazel-central-registry/main"

// DefaultRegistries is the default list of registries with fallback.
// BCR is tried first, with the GitHub mirror as backup.
var DefaultRegistries = []string{
	DefaultRegistry,
	DefaultRegistryMirror,
}

// HTTP client configuration constants.
const (
	defaultMaxIdleConns        = 50
	defaultMaxIdleConnsPerHost = 20
	defaultIdleConnTimeout     = 90 * time.Second
	defaultRequestTimeout      = 15 * time.Second
)

// RegistryError provides status details for registry HTTP failures.
type RegistryError struct {
	StatusCode int
	ModuleName string
	Version    string
	URL        string
}

func (e *RegistryError) Error() string {
	if e.ModuleName != "" && e.Version != "" {
		return fmt.Sprintf("registry returned status %d for module %s@%s", e.StatusCode, e.ModuleName, e.Version)
	}
	if e.URL != "" {
		return fmt.Sprintf("registry returned status %d for %s", e.StatusCode, e.URL)
	}
	return fmt.Sprintf("registry returned status %d", e.StatusCode)
}

// registryClient fetches Bazel module metadata from a registry (typically BCR).
//
// The client is optimized for high-throughput concurrent access with:
//   - Connection pooling (up to 50 idle connections, 20 per host)
//   - Thread-safe in-memory caching (module files are cached by name@version)
//   - Configurable timeouts (15 second default)
//
// The cache is unbounded and lives for the lifetime of the client. For long-running
// processes, consider creating a new client periodically to clear the cache.
type registryClient struct {
	baseURL       string
	client        *http.Client
	cache         sync.Map // map[string]*ModuleInfo keyed by "name@version"
	metadataCache sync.Map // map[string]*registry.Metadata keyed by module name
}

// BaseURL returns the base URL of the registry (e.g., "https://bcr.bazel.build").
func (r *registryClient) BaseURL() string {
	return r.baseURL
}

// Registry creates a registry client for dependency resolution.
//
// With no arguments, uses BCR with GitHub mirror fallback for resilience.
// With one URL, creates a single registry client (no fallback).
// With multiple URLs, creates a registry chain that tries each in order.
//
// Supports both remote and local registries:
//   - https:// or http:// - Remote registry
//   - file:// - Local filesystem registry (for airgap/offline)
//
// When using multiple registries, modules are looked up in order and the first
// registry where a module is found is used for ALL versions of that module.
// This matches Bazel's --registry flag behavior.
//
// The default configuration provides resilience against BCR outages.
// See: https://github.com/bazelbuild/bazel/issues/28101
//
// Panics if no valid registries could be created from the provided URLs.
// This should only happen with invalid URLs; the default registries are
// guaranteed to be valid.
//
// Examples:
//
//	// Use BCR with GitHub mirror fallback (recommended)
//	reg := Registry()
//
//	// Use a private registry only (no fallback)
//	reg := Registry("https://registry.example.com")
//
//	// Private registry with BCR fallback
//	reg := Registry("https://registry.example.com", DefaultRegistry)
//
//	// Local/airgap registry
//	reg := Registry("file:///opt/bazel-registry")
//
//	// Mixed: local first, then remote fallback
//	reg := Registry("file:///opt/local-modules", DefaultRegistry)
func Registry(urls ...string) registryInterface {
	return registryWithTimeout(0, urls...)
}

// registryWithTimeout creates a registry with a custom timeout.
// If timeout is zero or negative, uses the default timeout.
func registryWithTimeout(timeout time.Duration, urls ...string) registryInterface {
	return registryWithHTTPClient(nil, timeout, urls...)
}

// registryWithHTTPClient creates a registry with an optional custom HTTP client.
// If httpClient is nil, creates default clients with connection pooling.
// If timeout is positive, it overrides the httpClient's timeout.
func registryWithHTTPClient(httpClient *http.Client, timeout time.Duration, urls ...string) registryInterface {
	if len(urls) == 0 {
		// Use BCR + GitHub mirror for resilience
		chain, err := newRegistryChainWithHTTPClient(DefaultRegistries, httpClient, timeout)
		if err != nil {
			// This should never happen with DefaultRegistries
			panic(fmt.Sprintf("failed to create default registry chain: %v", err))
		}
		return chain
	}
	if len(urls) == 1 {
		reg, err := createRegistryClientWithHTTPClient(urls[0], httpClient, timeout)
		if err != nil {
			// Fall back to treating it as a remote URL if parsing fails
			return newRegistryClientWithHTTPClient(urls[0], httpClient, timeout)
		}
		return reg
	}
	chain, err := newRegistryChainWithHTTPClient(urls, httpClient, timeout)
	if err != nil {
		// Panic on invalid URLs - caller should validate URLs before calling
		panic(fmt.Sprintf("failed to create registry chain: %v", err))
	}
	return chain
}

// newRegistryClient creates a registryClient for the given registry URL.
func newRegistryClient(baseURL string) *registryClient {
	return newRegistryClientWithTimeout(baseURL, 0)
}

// newRegistryClientWithTimeout creates a registryClient with a custom timeout.
// If timeout is zero or negative, uses defaultRequestTimeout (15 seconds).
func newRegistryClientWithTimeout(baseURL string, timeout time.Duration) *registryClient {
	return newRegistryClientWithHTTPClient(baseURL, nil, timeout)
}

// newRegistryClientWithHTTPClient creates a registryClient with an optional custom HTTP client.
// If client is nil, creates a default client with connection pooling.
// If timeout is positive, it overrides the client's timeout.
func newRegistryClientWithHTTPClient(baseURL string, client *http.Client, timeout time.Duration) *registryClient {
	if client == nil {
		// Create default pooled client
		transport := &http.Transport{
			MaxIdleConns:        defaultMaxIdleConns,
			MaxIdleConnsPerHost: defaultMaxIdleConnsPerHost,
			IdleConnTimeout:     defaultIdleConnTimeout,
			DisableCompression:  false,
		}

		actualTimeout := defaultRequestTimeout
		if timeout > 0 {
			actualTimeout = timeout
		}

		client = &http.Client{
			Timeout:   actualTimeout,
			Transport: transport,
		}
	} else if timeout > 0 {
		// Clone client with new timeout to avoid modifying the user's client
		client = &http.Client{
			Transport:     client.Transport,
			CheckRedirect: client.CheckRedirect,
			Jar:           client.Jar,
			Timeout:       timeout,
		}
	}

	return &registryClient{
		baseURL: strings.TrimSuffix(baseURL, "/"),
		client:  client,
	}
}

// GetModuleFile fetches and parses a MODULE.bazel file from the registry.
// Results are cached, so repeated calls for the same module@version are fast.
//
// Returns an error if the module doesn't exist, the network fails, or parsing fails.
func (r *registryClient) GetModuleFile(ctx context.Context, moduleName, version string) (*ModuleInfo, error) {
	cacheKey := moduleName + "@" + version
	if cached, ok := r.cache.Load(cacheKey); ok {
		return cached.(*ModuleInfo), nil
	}

	url := fmt.Sprintf("%s/modules/%s/%s/MODULE.bazel", r.baseURL, moduleName, version)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, err
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, &RegistryError{
			StatusCode: resp.StatusCode,
			ModuleName: moduleName,
			Version:    version,
			URL:        url,
		}
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	moduleInfo, err := ParseModuleContent(string(data))
	if err != nil {
		return nil, err
	}

	r.cache.Store(cacheKey, moduleInfo)
	return moduleInfo, nil
}

// GetModuleMetadata fetches the metadata.json file for a module.
// This includes version list, yanked versions, maintainers, and deprecation info.
// Results are cached, so repeated calls for the same module are fast.
func (r *registryClient) GetModuleMetadata(ctx context.Context, moduleName string) (*registry.Metadata, error) {
	if cached, ok := r.metadataCache.Load(moduleName); ok {
		return cached.(*registry.Metadata), nil
	}

	url := fmt.Sprintf("%s/modules/%s/metadata.json", r.baseURL, moduleName)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, err
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, &RegistryError{
			StatusCode: resp.StatusCode,
			ModuleName: moduleName,
			URL:        url,
		}
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var metadata registry.Metadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return nil, fmt.Errorf("parse metadata for %s: %w", moduleName, err)
	}

	r.metadataCache.Store(moduleName, &metadata)
	return &metadata, nil
}
