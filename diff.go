package gobzlmod

import (
	"sort"

	"github.com/albertocavalcante/go-bzlmod/selection/version"
)

// ModuleChange represents an added or removed module in a resolution diff.
type ModuleChange struct {
	// Name is the module name.
	Name string `json:"name"`

	// Version is the module version.
	Version string `json:"version"`
}

// ModuleUpgrade represents a version change for an existing module.
type ModuleUpgrade struct {
	// Name is the module name.
	Name string `json:"name"`

	// OldVersion is the version in the old resolution.
	OldVersion string `json:"old_version"`

	// NewVersion is the version in the new resolution.
	NewVersion string `json:"new_version"`
}

// ResolutionDiff describes the differences between two dependency resolutions.
//
// This is useful for:
//   - Reviewing dependency updates before applying them
//   - Generating changelogs for dependency bumps
//   - Auditing what changed between two MODULE.bazel versions
//   - CI/CD checks to validate dependency changes
//
// Example usage:
//
//	oldResult, _ := Resolve(ctx, oldContent, opts)
//	newResult, _ := Resolve(ctx, newContent, opts)
//	diff := DiffResolutions(oldResult, newResult)
//
//	if !diff.IsEmpty() {
//	    fmt.Printf("Changes: %d added, %d removed, %d upgraded, %d downgraded\n",
//	        len(diff.Added), len(diff.Removed), len(diff.Upgraded), len(diff.Downgraded))
//	}
type ResolutionDiff struct {
	// Added contains modules present in new but not in old.
	Added []ModuleChange `json:"added,omitempty"`

	// Removed contains modules present in old but not in new.
	Removed []ModuleChange `json:"removed,omitempty"`

	// Upgraded contains modules where the new version is higher.
	Upgraded []ModuleUpgrade `json:"upgraded,omitempty"`

	// Downgraded contains modules where the new version is lower.
	Downgraded []ModuleUpgrade `json:"downgraded,omitempty"`
}

// IsEmpty returns true if there are no differences between the resolutions.
func (d *ResolutionDiff) IsEmpty() bool {
	return len(d.Added) == 0 &&
		len(d.Removed) == 0 &&
		len(d.Upgraded) == 0 &&
		len(d.Downgraded) == 0
}

// TotalChanges returns the total number of changes (added + removed + upgraded + downgraded).
func (d *ResolutionDiff) TotalChanges() int {
	return len(d.Added) + len(d.Removed) + len(d.Upgraded) + len(d.Downgraded)
}

// DiffResolutions computes the difference between two resolution results.
//
// The comparison uses Bazel's version comparison semantics, correctly handling
// complex version strings like "1.2.3.bcr.1" and pre-release versions.
//
// Parameters:
//   - old: the baseline resolution (nil is treated as empty)
//   - new: the updated resolution (nil is treated as empty)
//
// Returns a ResolutionDiff describing:
//   - Added: modules in new but not in old
//   - Removed: modules in old but not in new
//   - Upgraded: modules where new version > old version
//   - Downgraded: modules where new version < old version
//
// Results are sorted alphabetically by module name for consistent output.
func DiffResolutions(old, new *ResolutionList) *ResolutionDiff {
	diff := &ResolutionDiff{}

	// Build lookup maps for O(1) comparison
	oldModules := make(map[string]string) // name -> version
	newModules := make(map[string]string)

	if old != nil {
		for _, m := range old.Modules {
			oldModules[m.Name] = m.Version
		}
	}

	if new != nil {
		for _, m := range new.Modules {
			newModules[m.Name] = m.Version
		}
	}

	// Find added and upgraded/downgraded
	for name, newVersion := range newModules {
		oldVersion, existedBefore := oldModules[name]
		if !existedBefore {
			diff.Added = append(diff.Added, ModuleChange{
				Name:    name,
				Version: newVersion,
			})
		} else if oldVersion != newVersion {
			cmp := version.Compare(newVersion, oldVersion)
			if cmp > 0 {
				diff.Upgraded = append(diff.Upgraded, ModuleUpgrade{
					Name:       name,
					OldVersion: oldVersion,
					NewVersion: newVersion,
				})
			} else if cmp < 0 {
				diff.Downgraded = append(diff.Downgraded, ModuleUpgrade{
					Name:       name,
					OldVersion: oldVersion,
					NewVersion: newVersion,
				})
			}
			// cmp == 0 means versions are equal (shouldn't happen if strings differ,
			// but handle it gracefully by not including in diff)
		}
	}

	// Find removed
	for name, oldVersion := range oldModules {
		if _, existsNow := newModules[name]; !existsNow {
			diff.Removed = append(diff.Removed, ModuleChange{
				Name:    name,
				Version: oldVersion,
			})
		}
	}

	// Sort results for consistent output
	sortModuleChanges(diff.Added)
	sortModuleChanges(diff.Removed)
	sortModuleUpgrades(diff.Upgraded)
	sortModuleUpgrades(diff.Downgraded)

	return diff
}

// sortModuleChanges sorts a slice of ModuleChange by name.
func sortModuleChanges(changes []ModuleChange) {
	sort.Slice(changes, func(i, j int) bool {
		return changes[i].Name < changes[j].Name
	})
}

// sortModuleUpgrades sorts a slice of ModuleUpgrade by name.
func sortModuleUpgrades(upgrades []ModuleUpgrade) {
	sort.Slice(upgrades, func(i, j int) bool {
		return upgrades[i].Name < upgrades[j].Name
	})
}
