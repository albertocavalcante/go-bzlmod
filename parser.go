package gobzlmod

import (
	"fmt"
	"os"
	"strconv"

	"github.com/bazelbuild/buildtools/build"
)

// ParseModuleFile reads and parses a MODULE.bazel file from disk
func ParseModuleFile(filename string) (*ModuleInfo, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read module file: %v", err)
	}

	return ParseModuleContent(string(data))
}

// ParseModuleContent parses the content of a MODULE.bazel file
func ParseModuleContent(content string) (*ModuleInfo, error) {
	f, err := build.ParseModule("MODULE.bazel", []byte(content))
	if err != nil {
		return nil, fmt.Errorf("failed to parse MODULE.bazel: %v", err)
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

		ident, ok := call.X.(*build.Ident)
		if !ok {
			continue
		}

		switch ident.Name {
		case "module":
			info.Name = getStringAttr(call, "name")
			info.Version = getStringAttr(call, "version")
			info.CompatibilityLevel = getIntAttr(call, "compatibility_level")

		case "bazel_dep":
			dep := Dependency{
				Name:          getStringAttr(call, "name"),
				Version:       getStringAttr(call, "version"),
				RepoName:      getStringAttr(call, "repo_name"),
				DevDependency: getBoolAttr(call, "dev_dependency"),
			}
			if dep.Name != "" && dep.Version != "" {
				info.Dependencies = append(info.Dependencies, dep)
			}

		case "single_version_override":
			override := Override{
				Type:       "single_version",
				ModuleName: getStringAttr(call, "module_name"),
				Version:    getStringAttr(call, "version"),
				Registry:   getStringAttr(call, "registry"),
			}
			if override.ModuleName != "" {
				info.Overrides = append(info.Overrides, override)
			}

		case "git_override":
			override := Override{
				Type:       "git",
				ModuleName: getStringAttr(call, "module_name"),
			}
			if override.ModuleName != "" {
				info.Overrides = append(info.Overrides, override)
			}

		case "local_path_override":
			override := Override{
				Type:       "local_path",
				ModuleName: getStringAttr(call, "module_name"),
			}
			if override.ModuleName != "" {
				info.Overrides = append(info.Overrides, override)
			}

		case "archive_override":
			override := Override{
				Type:       "archive",
				ModuleName: getStringAttr(call, "module_name"),
			}
			if override.ModuleName != "" {
				info.Overrides = append(info.Overrides, override)
			}
		}
	}

	return info
}

// getStringAttr extracts a string attribute from a function call
func getStringAttr(call *build.CallExpr, name string) string {
	if name == "" && len(call.List) > 0 {
		if str, ok := call.List[0].(*build.StringExpr); ok {
			return str.Value
		}
		return ""
	}

	for _, arg := range call.List {
		if assign, ok := arg.(*build.AssignExpr); ok {
			if lhs, ok := assign.LHS.(*build.Ident); ok && lhs.Name == name {
				if str, ok := assign.RHS.(*build.StringExpr); ok {
					return str.Value
				}
			}
		}
	}
	return ""
}

// getIntAttr extracts an integer attribute from a function call
func getIntAttr(call *build.CallExpr, name string) int {
	for _, arg := range call.List {
		if assign, ok := arg.(*build.AssignExpr); ok {
			if lhs, ok := assign.LHS.(*build.Ident); ok && lhs.Name == name {
				if num, ok := assign.RHS.(*build.LiteralExpr); ok {
					if val, err := strconv.Atoi(num.Token); err == nil {
						return val
					}
				}
			}
		}
	}
	return 0
}

// getBoolAttr extracts a boolean attribute from a function call
func getBoolAttr(call *build.CallExpr, name string) bool {
	for _, arg := range call.List {
		if assign, ok := arg.(*build.AssignExpr); ok {
			if lhs, ok := assign.LHS.(*build.Ident); ok && lhs.Name == name {
				if ident, ok := assign.RHS.(*build.Ident); ok {
					return ident.Name == "True"
				}
			}
		}
	}
	return false
}
