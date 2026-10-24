package gobzlmod

import (
	"context"
	"sync"
)

// checkModuleMetadata fetches metadata for all modules and marks yanked/deprecated status.
// Follows Bazel's fail-open pattern: if metadata.json fetch fails, resolution continues.
//
// This function concurrently fetches metadata for all modules in the resolution list and
// updates their Yanked, YankReason, IsDeprecated, and DeprecationReason fields based on
// the metadata retrieved from the registry.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control
//   - registry: The registry interface to fetch metadata from
//   - opts: Resolution options containing AllowYankedVersions configuration
//   - list: The resolution list to update with metadata information
//
// The function respects the AllowYankedVersions option:
//   - If "all" is in the list, no modules are marked as yanked
//   - If "module@version" is in the list, that specific version is not marked as yanked
//
// Error handling follows Bazel's fail-open pattern: if metadata cannot be fetched for a
// module, that module is silently skipped and resolution continues. This matches
// YankedVersionsFunction.java behavior.
func checkModuleMetadata(ctx context.Context, registry Registry, opts ResolutionOptions, list *ResolutionList) {
	// Build allowed yanked versions set for quick lookup
	allowedYanked := buildAllowedYankedSet(opts.AllowYankedVersions)

	type result struct {
		idx               int
		yanked            bool
		yankReason        string
		deprecated        bool
		deprecationReason string
	}

	results := make(chan result, len(list.Modules))
	var wg sync.WaitGroup

	for i := range list.Modules {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			module := &list.Modules[idx]
			if module.Version == "" {
				return
			}
			origin := metadataRegistryForModule(registry, *module, opts)
			metadata, err := moduleMetadataForVersion(ctx, origin, module.Name, module.Version)
			if err != nil {
				// Bazel's fail-open pattern: metadata fetch failures don't block resolution.
				// This matches YankedVersionsFunction.java behavior (lines 47-62).
				return
			}

			res := result{idx: idx}

			// Check yanked status
			if metadata.IsYanked(module.Version) {
				res.yanked = true
				res.yankReason = metadata.YankReason(module.Version)
			}

			// Check deprecated status
			if metadata.IsDeprecated() {
				res.deprecated = true
				res.deprecationReason = metadata.Deprecated
			}

			if res.yanked || res.deprecated {
				results <- res
			}
		}(i)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	for res := range results {
		if res.yanked {
			// Check if this specific module@version is allowed
			moduleKey := list.Modules[res.idx].Name + "@" + list.Modules[res.idx].Version
			if !allowedYanked["all"] && !allowedYanked[moduleKey] {
				list.Modules[res.idx].Yanked = true
				list.Modules[res.idx].YankReason = res.yankReason
			}
		}
		if res.deprecated {
			list.Modules[res.idx].IsDeprecated = true
			list.Modules[res.idx].DeprecationReason = res.deprecationReason
		}
	}
}

// metadataRegistryForModule preserves explicit registry overrides as well as
// the default chain's selected coordinate origin.
func metadataRegistryForModule(base Registry, module ModuleToResolve, opts ResolutionOptions) Registry {
	if module.Registry == "" {
		return base
	}
	if provider, ok := base.(interface{ GetRegistryForVersion(string, string) string }); ok {
		if provider.GetRegistryForVersion(module.Name, module.Version) == module.Registry {
			return base
		}
	}
	if base.BaseURL() == module.Registry {
		return base
	}
	return registryWithAllOptionsAndTrace(opts.HTTPClient, opts.Cache, opts.Timeout, opts.Logger, sharedRegistryFileTrace(base), module.Registry)
}

// buildAllowedYankedSet creates a set from AllowYankedVersions for O(1) lookup.
// Returns nil if the input list is empty to avoid unnecessary allocations.
func buildAllowedYankedSet(allowed []string) map[string]bool {
	if len(allowed) == 0 {
		return nil
	}
	set := make(map[string]bool, len(allowed))
	for _, v := range allowed {
		set[v] = true
	}
	return set
}
