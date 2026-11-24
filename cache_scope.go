package gobzlmod

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// ScopeModuleCache namespaces MODULE.bazel cache keys by registry URL.
// Registry clients apply this automatically. Use the same wrapper when seeding
// a shared cache explicitly. Content and version strings remain unchanged.
func ScopeModuleCache(cache ModuleCache, registryURL string) ModuleCache {
	if cache == nil {
		return nil
	}
	if scoped, ok := cache.(scopedModuleCache); ok {
		cache = scoped.cache
	}
	// Match registryClient's single trailing-slash normalization exactly.
	digest := sha256.Sum256([]byte(strings.TrimSuffix(registryURL, "/")))
	return scopedModuleCache{cache: cache, prefix: "registry_" + hex.EncodeToString(digest[:]) + "_"}
}

type scopedModuleCache struct {
	cache  ModuleCache
	prefix string
}

func (c scopedModuleCache) Get(ctx context.Context, name, version string) (content []byte, found bool, err error) {
	return c.cache.Get(ctx, c.prefix+name, version)
}

func (c scopedModuleCache) Put(ctx context.Context, name, version string, content []byte) error {
	return c.cache.Put(ctx, c.prefix+name, version, content)
}
