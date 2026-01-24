package gobzlmod

import (
	"context"
	"errors"
	"fmt"
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
// Resilience improvements over Bazel:
//
// This implementation falls back to the next registry on ANY error, including:
//   - HTTP 404 (module not found)
//   - HTTP 5xx (server errors)
//   - TLS/certificate errors (Dec 2025 BCR outage)
//   - Network timeouts and connection failures
//
// Known Bazel bugs we work around:
//   - Bazel fails on source.json errors instead of trying next registry
//   - Bazel has issues with TLS error recovery across multiple URLs
//
// References:
//   - https://github.com/bazelbuild/bazel/issues/28101 (BCR TLS outage)
//   - https://github.com/bazelbuild/bazel/issues/28158 (TLS recovery bug)
//   - https://github.com/bazelbuild/bazel/issues/26442 (source.json fallback bug)
//
// By always falling back, we provide better resilience than Bazel itself.
type registryChain struct {
	clients []registryInterface

	// moduleRegistry tracks which registry provides each module (by module name)
	// Once a module is found in a registry, all versions come from that registry
	moduleRegistry   map[string]int // module name -> registry index
	moduleRegistryMu sync.RWMutex
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
	if len(registryURLs) == 0 {
		return nil, errors.New("no registry URLs provided")
	}

	clients := make([]registryInterface, 0, len(registryURLs))
	for _, url := range registryURLs {
		client, err := createRegistryClientWithTimeout(url, timeout)
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
		moduleRegistry: make(map[string]int),
	}, nil
}

// GetModuleFile fetches a MODULE.bazel file using the registry chain.
// It tries registries in order for the first request for a module name,
// then caches which registry provides that module.
func (rc *registryChain) GetModuleFile(ctx context.Context, moduleName, version string) (*ModuleInfo, error) {
	// Check if we've already determined which registry provides this module
	rc.moduleRegistryMu.RLock()
	registryIdx, found := rc.moduleRegistry[moduleName]
	rc.moduleRegistryMu.RUnlock()

	if found {
		// We know which registry has this module, use it directly
		return rc.clients[registryIdx].GetModuleFile(ctx, moduleName, version)
	}

	// Try each registry in order to find the module
	var notFoundErrors []string
	for i, client := range rc.clients {
		moduleInfo, err := client.GetModuleFile(ctx, moduleName, version)
		if err == nil {
			// Success! Remember this registry for future requests for this module
			rc.moduleRegistryMu.Lock()
			if _, exists := rc.moduleRegistry[moduleName]; !exists {
				rc.moduleRegistry[moduleName] = i
			}
			rc.moduleRegistryMu.Unlock()
			return moduleInfo, nil
		}

		// Check if it's a 404 (module not found in this registry)
		if isNotFound(err) {
			notFoundErrors = append(notFoundErrors, fmt.Sprintf("%s: %v", client.BaseURL(), err))
			continue
		}

		// For other errors (TLS, network, server errors, etc.), continue to next registry.
		// This provides resilience against infrastructure issues like:
		//   - TLS certificate expiration
		//     https://github.com/bazelbuild/bazel/issues/28101
		//     https://github.com/bazelbuild/bazel/issues/28158
		//   - Server errors when source.json is missing
		//     https://github.com/bazelbuild/bazel/issues/26442
		//   - Network timeouts and connection failures
		notFoundErrors = append(notFoundErrors, fmt.Sprintf("%s: %v", client.BaseURL(), err))
		continue
	}

	// Module not found in any registry
	if len(notFoundErrors) == 1 {
		return nil, fmt.Errorf("module %s@%s not found: %s", moduleName, version, notFoundErrors[0])
	}
	return nil, fmt.Errorf("module %s@%s not found in any registry:\n  %v",
		moduleName, version, notFoundErrors)
}

// GetModuleMetadata fetches metadata using the registry that provides this module.
func (rc *registryChain) GetModuleMetadata(ctx context.Context, moduleName string) (*registry.Metadata, error) {
	// Check if we've already determined which registry provides this module
	rc.moduleRegistryMu.RLock()
	registryIdx, found := rc.moduleRegistry[moduleName]
	rc.moduleRegistryMu.RUnlock()

	if found {
		// We know which registry has this module, use it directly
		return rc.clients[registryIdx].GetModuleMetadata(ctx, moduleName)
	}

	// Try each registry in order to find the metadata
	var lastErr error
	for i, client := range rc.clients {
		metadata, err := client.GetModuleMetadata(ctx, moduleName)
		if err == nil {
			// Success! Remember this registry for future requests for this module
			rc.moduleRegistryMu.Lock()
			if _, exists := rc.moduleRegistry[moduleName]; !exists {
				rc.moduleRegistry[moduleName] = i
			}
			rc.moduleRegistryMu.Unlock()
			return metadata, nil
		}

		// Check if it's a 404 (module not found in this registry)
		if isNotFound(err) {
			lastErr = err
			continue
		}

		// For other errors, continue to next registry
		lastErr = err
		continue
	}

	// Module metadata not found in any registry
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("metadata for module %s not found in any registry", moduleName)
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

// registryInterface defines the minimal interface needed by dependencyResolver.
// Both registryClient and registryChain implement this interface.
type registryInterface interface {
	GetModuleFile(ctx context.Context, moduleName, version string) (*ModuleInfo, error)
	GetModuleMetadata(ctx context.Context, moduleName string) (*registry.Metadata, error)
	BaseURL() string
}

// Verify that both types implement the interface
var _ registryInterface = (*registryClient)(nil)
var _ registryInterface = (*registryChain)(nil)
