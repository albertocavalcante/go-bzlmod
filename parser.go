package gobzlmod

import (
	"fmt"
	"os"

	"github.com/albertocavalcante/go-bzlmod/internal/buildutil"
	"github.com/bazelbuild/buildtools/build"
)

// ParseModuleFile reads and parses a MODULE.bazel file from disk.
// This is a convenience wrapper around ParseModuleContent.
//
// For more advanced parsing with error diagnostics and position information,
// use the ast package instead.
func ParseModuleFile(filename string) (*ModuleInfo, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("read module file: %w", err)
	}
	return parseModule(filename, data)
}

// ParseModuleContent parses the content of a MODULE.bazel file.
func ParseModuleContent(content string) (*ModuleInfo, error) {
	return parseModule("MODULE.bazel", []byte(content))
}

func parseModule(filename string, content []byte) (*ModuleInfo, error) {
	f, err := build.ParseModule(filename, content)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", filename, err)
	}
	return extractModuleInfo(f), nil
}

// extractModuleInfo extracts module information from parsed BUILD file
func extractModuleInfo(f *build.File) *ModuleInfo {
	info := &ModuleInfo{
		Dependencies: []Dependency{},
		Overrides:    []Override{},
	}

	for _, stmt := range f.Stmt {
		call, ok := stmt.(*build.CallExpr)
		if !ok {
			continue
		}

		switch buildutil.FuncName(call) {
		case "module":
			info.Name = buildutil.String(call, "name")
			info.Version = buildutil.String(call, "version")
			info.CompatibilityLevel = buildutil.Int(call, "compatibility_level")

		case "bazel_dep":
			dep := Dependency{
				Name:          buildutil.String(call, "name"),
				Version:       buildutil.String(call, "version"),
				RepoName:      buildutil.String(call, "repo_name"),
				DevDependency: buildutil.Bool(call, "dev_dependency"),
			}
			if dep.Name != "" && dep.Version != "" {
				info.Dependencies = append(info.Dependencies, dep)
			}

		case "single_version_override":
			override := Override{
				Type:       "single_version",
				ModuleName: buildutil.String(call, "module_name"),
				Version:    buildutil.String(call, "version"),
				Registry:   buildutil.String(call, "registry"),
			}
			if override.ModuleName != "" {
				info.Overrides = append(info.Overrides, override)
			}

		case "git_override":
			override := Override{
				Type:       "git",
				ModuleName: buildutil.String(call, "module_name"),
			}
			if override.ModuleName != "" {
				info.Overrides = append(info.Overrides, override)
			}

		case "local_path_override":
			override := Override{
				Type:       "local_path",
				ModuleName: buildutil.String(call, "module_name"),
			}
			if override.ModuleName != "" {
				info.Overrides = append(info.Overrides, override)
			}

		case "archive_override":
			override := Override{
				Type:       "archive",
				ModuleName: buildutil.String(call, "module_name"),
			}
			if override.ModuleName != "" {
				info.Overrides = append(info.Overrides, override)
			}
		}
	}

	return info
}
