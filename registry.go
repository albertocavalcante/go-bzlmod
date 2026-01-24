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

// RegistryClient fetches Bazel module metadata from a registry (typically BCR).
//
// The client is optimized for high-throughput concurrent access with:
//   - Connection pooling (up to 50 idle connections, 20 per host)
//   - Thread-safe in-memory caching (module files are cached by name@version)
//   - Configurable timeouts (15 second default)
//
// The cache is unbounded and lives for the lifetime of the client. For long-running
// processes, consider creating a new client periodically to clear the cache.
type RegistryClient struct {
	baseURL       string
	client        *http.Client
	cache         sync.Map // map[string]*ModuleInfo keyed by "name@version"
	metadataCache sync.Map // map[string]*registry.Metadata keyed by module name
}

// BaseURL returns the base URL of the registry (e.g., "https://bcr.bazel.build").
func (r *RegistryClient) BaseURL() string {
	return r.baseURL
}

// NewRegistryClient creates a client for the given registry URL.
//
// The URL should be the base of a Bazel registry implementing the standard
// layout where module files are at /modules/{name}/{version}/MODULE.bazel.
//
// Example:
//
//	client := NewRegistryClient("https://bcr.bazel.build")
func NewRegistryClient(baseURL string) *RegistryClient {
	transport := &http.Transport{
		MaxIdleConns:        defaultMaxIdleConns,
		MaxIdleConnsPerHost: defaultMaxIdleConnsPerHost,
		IdleConnTimeout:     defaultIdleConnTimeout,
		DisableCompression:  false,
	}

	return &RegistryClient{
		baseURL: strings.TrimSuffix(baseURL, "/"),
		client: &http.Client{
			Timeout:   defaultRequestTimeout,
			Transport: transport,
		},
	}
}

// GetModuleFile fetches and parses a MODULE.bazel file from the registry.
// Results are cached, so repeated calls for the same module@version are fast.
//
// Returns an error if the module doesn't exist, the network fails, or parsing fails.
func (r *RegistryClient) GetModuleFile(ctx context.Context, moduleName, version string) (*ModuleInfo, error) {
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
func (r *RegistryClient) GetModuleMetadata(ctx context.Context, moduleName string) (*registry.Metadata, error) {
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
