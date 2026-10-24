package registry

import (
	"fmt"
	"regexp"

	bzlversion "github.com/albertocavalcante/go-bzlmod/selection/version"
)

var moduleNamePattern = regexp.MustCompile(`^[a-z]([a-z0-9._-]*[a-z0-9])?$`)

// ValidateModuleName checks a Bazel registry module name before URL or path use.
func ValidateModuleName(name string) error {
	if !moduleNamePattern.MatchString(name) {
		return fmt.Errorf("invalid registry module name %q", name)
	}
	return nil
}

// ValidateModuleCoordinate checks a registry module name and nonempty version.
func ValidateModuleCoordinate(name, version string) error {
	if err := ValidateModuleName(name); err != nil {
		return err
	}
	if version == "" {
		return fmt.Errorf("registry version for %s must not be empty", name)
	}
	if _, err := bzlversion.Parse(version); err != nil {
		return fmt.Errorf("invalid registry version for %s: %w", name, err)
	}
	return nil
}
