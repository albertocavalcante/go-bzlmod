package gobzlmod

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/albertocavalcante/go-bzlmod/registry"
)

// registryChain implements multi-registry lookup with fallback behavior.
// It tries registries in order and remembers which registry provides each module.
//
// Supports both remote (https://) and local (file://) registries,
// enabling airgap and offline workflows.
//
// Key behaviors matching Bazel's implementation (ModuleFileFunction.java:745-810):
//  1. Modules are looked up in registry order (first to last)
//  2. The first registry where a module is found is used for ALL versions of that module
//  3. If a module is not found in any registry, an error is returned
//
// A registry owns all versions and metadata of a module once selected. Only
// not-found responses permit initial fallback; authentication, transport, and
// parsing failures never redirect a module to another origin.
type registryChain struct {
	clients []Registry
	trace   *registryFileTrace

	// moduleRegistry tracks which registry provides each module (by module name)
	// Once a module is found in a registry, all versions come from that registry
	moduleRegistry   map[string]int // module name -> registry index
	moduleRegistryMu sync.RWMutex
	moduleLocks      sync.Map // module name -> chan struct{}; context-aware per-module serialization
}

// newRegistryChain creates a chain of registries from URLs.
//
// Supports both remote and local registries:
//   - https:// or http:// - Remote registry
//   - file:// - Local filesystem registry (for airgap/offline)
//
// Returns an error if:
//   - No registry URLs are provided (nil or empty slice)
//   - All provided URLs are invalid or cannot be parsed
//
// If some URLs are invalid but at least one is valid, the invalid URLs
// are silently skipped and a chain is created with the valid ones.
func newRegistryChain(registryURLs []string) (*registryChain, error) {
	return newRegistryChainWithTimeout(registryURLs, 0)
}

// newRegistryChainWithTimeout creates a chain of registries with a custom timeout.
// If timeout is zero or negative, uses the default timeout.
func newRegistryChainWithTimeout(registryURLs []string, timeout time.Duration) (*registryChain, error) {
	return newRegistryChainWithHTTPClient(registryURLs, nil, timeout)
}

// newRegistryChainWithHTTPClient creates a chain of registries with an optional custom HTTP client.
// If httpClient is nil, creates default clients with connection pooling.
// If timeout is positive, it overrides the httpClient's timeout.
func newRegistryChainWithHTTPClient(registryURLs []string, httpClient *http.Client, timeout time.Duration) (*registryChain, error) {
	return newRegistryChainWithOptions(registryURLs, httpClient, nil, timeout)
}

// newRegistryChainWithOptions creates a chain of registries with all optional parameters.
// If httpClient is nil, creates default clients with connection pooling.
// If cache is nil, no external caching is used.
// If timeout is positive, it overrides the httpClient's timeout.
func newRegistryChainWithOptions(registryURLs []string, httpClient *http.Client, cache ModuleCache, timeout time.Duration) (*registryChain, error) {
	return newRegistryChainWithAllOptions(registryURLs, httpClient, cache, timeout, nil)
}

// newRegistryChainWithAllOptions creates a chain of registries with all optional parameters including logger.
// If httpClient is nil, creates default clients with connection pooling.
// If cache is nil, no external caching is used.
// If timeout is positive, it overrides the httpClient's timeout.
// If logger is nil, logging is disabled.
func newRegistryChainWithAllOptions(registryURLs []string, httpClient *http.Client, cache ModuleCache, timeout time.Duration, logger *slog.Logger) (*registryChain, error) {
	return newRegistryChainWithAllOptionsAndTrace(registryURLs, httpClient, cache, timeout, logger, nil)
}

func newRegistryChainWithAllOptionsAndTrace(registryURLs []string, httpClient *http.Client, cache ModuleCache, timeout time.Duration, logger *slog.Logger, trace *registryFileTrace) (*registryChain, error) {
	if len(registryURLs) == 0 {
		return nil, errors.New("no registry URLs provided")
	}

	clients := make([]Registry, 0, len(registryURLs))
	for _, url := range registryURLs {
		client, err := createRegistryClientWithAllOptionsAndTrace(url, httpClient, cache, timeout, logger, trace)
		if err != nil {
			// Log error but continue with other registries
			// In production, consider adding a warning mechanism
			continue
		}
		clients = append(clients, client)
	}

	if len(clients) == 0 {
		return nil, fmt.Errorf("no valid registries could be created from %d URLs", len(registryURLs))
	}

	return &registryChain{
		clients:        clients,
		trace:          trace,
		moduleRegistry: make(map[string]int),
	}, nil
}

// GetModuleFile fetches a MODULE.bazel file using the registry chain.
// It tries registries in order for the first request for a module name,
// then caches which registry provides that module.
func (rc *registryChain) GetModuleFile(ctx context.Context, moduleName, version string) (*ModuleInfo, error) {
	return lookupRegistry(ctx, rc, moduleName, func(client Registry) (*ModuleInfo, error) {
		return client.GetModuleFile(ctx, moduleName, version)
	})
}

// GetModuleMetadata fetches metadata from the module's selected registry.
func (rc *registryChain) GetModuleMetadata(ctx context.Context, moduleName string) (*registry.Metadata, error) {
	return lookupRegistry(ctx, rc, moduleName, func(client Registry) (*registry.Metadata, error) {
		return client.GetModuleMetadata(ctx, moduleName)
	})
}

// lookupRegistry binds a module atomically, including concurrent requests for
// different versions. Requests for unrelated modules remain concurrent.
func lookupRegistry[T any](ctx context.Context, rc *registryChain, name string, fetch func(Registry) (T, error)) (T, error) {
	var zero T
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	value, _ := rc.moduleLocks.LoadOrStore(name, make(chan struct{}, 1))
	lock := value.(chan struct{})
	select {
	case lock <- struct{}{}:
		defer func() { <-lock }()
	case <-ctx.Done():
		return zero, ctx.Err()
	}
	rc.moduleRegistryMu.RLock()
	index, found := rc.moduleRegistry[name]
	rc.moduleRegistryMu.RUnlock()
	if found {
		return fetch(rc.clients[index])
	}
	var failures []error
	for i, client := range rc.clients {
		result, err := fetch(client)
		if err == nil {
			rc.moduleRegistryMu.Lock()
			rc.moduleRegistry[name] = i
			rc.moduleRegistryMu.Unlock()
			return result, nil
		}
		if !isNotFound(err) {
			return zero, err
		}
		failures = append(failures, fmt.Errorf("%s: %w", client.BaseURL(), err))
	}
	return zero, fmt.Errorf("module %s not found in any registry: %w", name, errors.Join(failures...))
}

// BaseURL returns the URL of the first registry in the chain.
// This is used for display purposes and backwards compatibility.
func (rc *registryChain) BaseURL() string {
	if len(rc.clients) == 0 {
		return ""
	}
	return rc.clients[0].BaseURL()
}

// GetRegistryForModule returns the registry URL that provides the given module.
// Returns empty string if the module hasn't been looked up yet.
func (rc *registryChain) GetRegistryForModule(moduleName string) string {
	rc.moduleRegistryMu.RLock()
	defer rc.moduleRegistryMu.RUnlock()

	if idx, found := rc.moduleRegistry[moduleName]; found {
		return rc.clients[idx].BaseURL()
	}
	return ""
}

func (rc *registryChain) registryFileHashesSnapshot() map[string]*string {
	if rc.trace != nil {
		return rc.trace.snapshot()
	}

	var hashes map[string]*string
	for _, client := range rc.clients {
		provider, ok := client.(registryFileTraceProvider)
		if !ok {
			continue
		}
		hashes = mergeRegistryFileHashes(hashes, provider.registryFileHashesSnapshot())
	}
	return hashes
}

func (rc *registryChain) registryFileTrace() *registryFileTrace {
	return rc.trace
}

// GetModuleSource fetches source.json using the registry that provides this module.
func (rc *registryChain) GetModuleSource(ctx context.Context, moduleName, version string) (*registry.Source, error) {
	return lookupRegistry(ctx, rc, moduleName, func(client Registry) (*registry.Source, error) {
		return client.GetModuleSource(ctx, moduleName, version)
	})
}

// Registry provides access to Bazel module registries.
// Implementations fetch MODULE.bazel files and module metadata from registries.
//
// This interface is exported to allow users to implement custom registries
// or create mocks for testing.
type Registry interface {
	// GetModuleFile fetches and parses a MODULE.bazel file for the given module version.
	GetModuleFile(ctx context.Context, moduleName, version string) (*ModuleInfo, error)

	// GetModuleMetadata fetches metadata for a module (available versions, yanked info, etc).
	GetModuleMetadata(ctx context.Context, moduleName string) (*registry.Metadata, error)

	// GetModuleSource fetches the source.json file for a module version.
	// This describes how to fetch the module's source code (archive, git, or local_path).
	GetModuleSource(ctx context.Context, moduleName, version string) (*registry.Source, error)

	// BaseURL returns the base URL of this registry.
	BaseURL() string
}

// Verify that both types implement the interface
var _ Registry = (*registryClient)(nil)
var _ Registry = (*registryChain)(nil)
