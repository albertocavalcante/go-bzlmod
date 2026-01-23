package gobzlmod

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// RegistryClient provides fast access to Bazel module registry with caching and connection pooling.
type RegistryClient struct {
	baseURL string
	client  *http.Client
	cache   sync.Map
}

// BaseURL returns the registry base URL.
func (r *RegistryClient) BaseURL() string {
	return r.baseURL
}

// NewRegistryClient creates a new registry client with optimized HTTP settings
func NewRegistryClient(baseURL string) *RegistryClient {
	transport := &http.Transport{
		MaxIdleConns:        50,
		MaxIdleConnsPerHost: 20,
		IdleConnTimeout:     90 * time.Second,
		DisableCompression:  false,
	}

	return &RegistryClient{
		baseURL: strings.TrimSuffix(baseURL, "/"),
		client: &http.Client{
			Timeout:   15 * time.Second,
			Transport: transport,
		},
	}
}

// GetModuleFile fetches and parses a MODULE.bazel file from the registry
func (r *RegistryClient) GetModuleFile(ctx context.Context, moduleName, version string) (*ModuleInfo, error) {
	cacheKey := moduleName + "@" + version
	if cached, ok := r.cache.Load(cacheKey); ok {
		return cached.(*ModuleInfo), nil
	}

	url := fmt.Sprintf("%s/modules/%s/%s/MODULE.bazel", r.baseURL, moduleName, version)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("registry returned status %d for module %s@%s", resp.StatusCode, moduleName, version)
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
