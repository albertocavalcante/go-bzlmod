package ast

import (
	"fmt"
	"os"

	"github.com/albertocavalcante/go-bzlmod/label"
	"github.com/bazelbuild/buildtools/build"
)

// ParseError represents a parsing error with position information.
type ParseError struct {
	Pos     Position
	Message string
	Wrapped error
}

func (e *ParseError) Error() string {
	if e.Pos.Line > 0 {
		return fmt.Sprintf("%s:%d:%d: %s", e.Pos.Filename, e.Pos.Line, e.Pos.Column, e.Message)
	}
	return e.Message
}

func (e *ParseError) Unwrap() error {
	return e.Wrapped
}

// ParseResult contains the parsed file and any diagnostics.
type ParseResult struct {
	File     *ModuleFile
	Errors   []*ParseError
	Warnings []*ParseError
}

// HasErrors returns true if there were parse errors.
func (r *ParseResult) HasErrors() bool {
	return len(r.Errors) > 0
}

// Parser parses MODULE.bazel files into AST.
type Parser struct {
	filename string
	errors   []*ParseError
	warnings []*ParseError
}

// ParseFile reads and parses a MODULE.bazel file from disk.
func ParseFile(filename string) (*ParseResult, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", filename, err)
	}
	return ParseContent(filename, data)
}

// ParseContent parses MODULE.bazel content from bytes.
func ParseContent(filename string, content []byte) (*ParseResult, error) {
	p := &Parser{filename: filename}
	return p.parse(content)
}

func (p *Parser) parse(content []byte) (*ParseResult, error) {
	raw, err := build.ParseModule(p.filename, content)
	if err != nil {
		return nil, &ParseError{
			Pos:     Position{Filename: p.filename},
			Message: fmt.Sprintf("syntax error: %v", err),
			Wrapped: err,
		}
	}

	file := &ModuleFile{
		Path:       p.filename,
		Statements: make([]Statement, 0, len(raw.Stmt)),
		raw:        raw,
	}

	for _, stmt := range raw.Stmt {
		if s := p.parseStatement(stmt); s != nil {
			file.Statements = append(file.Statements, s)
		}
	}

	return &ParseResult{
		File:     file,
		Errors:   p.errors,
		Warnings: p.warnings,
	}, nil
}

func (p *Parser) parseStatement(expr build.Expr) Statement {
	// Handle assignment expressions like: go = use_extension(...)
	if assign, ok := expr.(*build.AssignExpr); ok {
		if call, ok := assign.RHS.(*build.CallExpr); ok {
			if ident, ok := call.X.(*build.Ident); ok {
				pos := p.position(call)
				switch ident.Name {
				case "use_extension":
					return p.parseUseExtension(call, pos)
				}
			}
		}
		return nil
	}

	call, ok := expr.(*build.CallExpr)
	if !ok {
		return nil
	}

	pos := p.position(call)

	// Handle method calls like go_sdk.from_file(...) - extension tag calls
	if dotExpr, ok := call.X.(*build.DotExpr); ok {
		return p.parseExtensionTagCall(call, dotExpr, pos)
	}

	ident, ok := call.X.(*build.Ident)
	if !ok {
		return nil
	}

	switch ident.Name {
	case "module":
		return p.parseModule(call, pos)
	case "bazel_dep":
		return p.parseBazelDep(call, pos)
	case "use_extension":
		return p.parseUseExtension(call, pos)
	case "use_repo":
		return p.parseUseRepo(call, pos)
	case "single_version_override":
		return p.parseSingleVersionOverride(call, pos)
	case "multiple_version_override":
		return p.parseMultipleVersionOverride(call, pos)
	case "git_override":
		return p.parseGitOverride(call, pos)
	case "archive_override":
		return p.parseArchiveOverride(call, pos)
	case "local_path_override":
		return p.parseLocalPathOverride(call, pos)
	case "register_toolchains":
		return p.parseRegisterToolchains(call, pos)
	case "register_execution_platforms":
		return p.parseRegisterExecutionPlatforms(call, pos)
	case "include":
		return p.parseInclude(call, pos)
	case "use_repo_rule":
		return p.parseUseRepoRule(call, pos)
	case "inject_repo":
		return p.parseInjectRepo(call, pos)
	case "override_repo":
		return p.parseOverrideRepo(call, pos)
	case "flag_alias":
		return p.parseFlagAlias(call, pos)
	default:
		return &UnknownStatement{
			Pos:      pos,
			FuncName: ident.Name,
			Raw:      expr,
		}
	}
}

func (p *Parser) parseInclude(call *build.CallExpr, pos Position) *Include {
	inc := &Include{Pos: pos}

	// include() takes a single positional string argument (label)
	if len(call.List) > 0 {
		if str, ok := call.List[0].(*build.StringExpr); ok {
			inc.Label = str.Value
		}
	}

	// Also check for named "label" parameter
	if label := p.getString(call, "label"); label != "" {
		inc.Label = label
	}

	if inc.Label == "" {
		p.addError(pos, "include: missing required label argument")
	}

	return inc
}

func (p *Parser) parseExtensionTagCall(call *build.CallExpr, dotExpr *build.DotExpr, pos Position) *ExtensionTagCall {
	tag := &ExtensionTagCall{
		Pos:        pos,
		Attributes: make(map[string]any),
		Raw:        call,
	}

	// Extract extension name (e.g., "go_sdk" from go_sdk.from_file)
	if ident, ok := dotExpr.X.(*build.Ident); ok {
		tag.Extension = ident.Name
	}

	// Extract tag/method name (e.g., "from_file")
	tag.TagName = dotExpr.Name

	// Extract all named attributes
	for _, arg := range call.List {
		if assign, ok := arg.(*build.AssignExpr); ok {
			if lhs, ok := assign.LHS.(*build.Ident); ok {
				tag.Attributes[lhs.Name] = p.extractValue(assign.RHS)
			}
		}
	}

	return tag
}

// extractValue converts a build.Expr to a Go value for storage in attributes
func (p *Parser) extractValue(expr build.Expr) any {
	switch e := expr.(type) {
	case *build.StringExpr:
		return e.Value
	case *build.LiteralExpr:
		// Try to parse as int
		var val int
		if _, err := fmt.Sscanf(e.Token, "%d", &val); err == nil {
			return val
		}
		return e.Token
	case *build.Ident:
		if e.Name == "True" {
			return true
		}
		if e.Name == "False" {
			return false
		}
		if e.Name == "None" {
			return nil
		}
		return e.Name
	case *build.ListExpr:
		result := make([]any, 0, len(e.List))
		for _, item := range e.List {
			result = append(result, p.extractValue(item))
		}
		return result
	case *build.DictExpr:
		result := make(map[string]any)
		for _, kv := range e.List {
			// DictExpr.List is []*KeyValueExpr
			if keyStr, ok := kv.Key.(*build.StringExpr); ok {
				result[keyStr.Value] = p.extractValue(kv.Value)
			}
		}
		return result
	default:
		// For complex expressions, return the raw expression
		return expr
	}
}

func (p *Parser) parseModule(call *build.CallExpr, pos Position) *ModuleDecl {
	decl := &ModuleDecl{Pos: pos}

	if name := p.getString(call, "name"); name != "" {
		if m, err := label.NewModule(name); err != nil {
			p.addError(pos, "invalid module name: %v", err)
		} else {
			decl.Name = m
		}
	}

	if version := p.getString(call, "version"); version != "" {
		if v, err := label.NewVersion(version); err != nil {
			p.addError(pos, "invalid module version: %v", err)
		} else {
			decl.Version = v
		}
	}

	decl.CompatibilityLevel = p.getInt(call, "compatibility_level")

	if repoName := p.getString(call, "repo_name"); repoName != "" {
		if r, err := label.NewApparentRepo(repoName); err != nil {
			p.addError(pos, "invalid repo_name: %v", err)
		} else {
			decl.RepoName = r
		}
	}

	decl.BazelCompatibility = p.getStringList(call, "bazel_compatibility")

	return decl
}

func (p *Parser) parseBazelDep(call *build.CallExpr, pos Position) *BazelDep {
	dep := &BazelDep{Pos: pos}

	name := p.getString(call, "name")
	if name == "" {
		p.addError(pos, "bazel_dep: missing required 'name' attribute")
		return nil
	}

	if m, err := label.NewModule(name); err != nil {
		p.addError(pos, "bazel_dep: invalid name: %v", err)
		return nil
	} else {
		dep.Name = m
	}

	version := p.getString(call, "version")
	if version == "" {
		// Missing version is valid when using local_path_override or other overrides
		p.addWarning(pos, "bazel_dep: missing 'version' attribute for %s (valid if using override)", name)
		// Keep an empty version
	} else {
		if v, err := label.NewVersion(version); err != nil {
			p.addError(pos, "bazel_dep: invalid version for %s: %v", name, err)
			return nil
		} else {
			dep.Version = v
		}
	}

	dep.MaxCompatibilityLevel = p.getInt(call, "max_compatibility_level")
	dep.DevDependency = p.getBool(call, "dev_dependency")

	if repoName := p.getString(call, "repo_name"); repoName != "" {
		if r, err := label.NewApparentRepo(repoName); err != nil {
			p.addError(pos, "bazel_dep: invalid repo_name for %s: %v", name, err)
		} else {
			dep.RepoName = r
		}
	}

	return dep
}

func (p *Parser) parseUseExtension(call *build.CallExpr, pos Position) *UseExtension {
	ext := &UseExtension{Pos: pos}

	// First positional arg is the .bzl file
	if len(call.List) > 0 {
		if str, ok := call.List[0].(*build.StringExpr); ok {
			if lbl, err := label.ParseApparentLabel(str.Value); err != nil {
				p.addError(pos, "use_extension: invalid extension file: %v", err)
			} else {
				ext.ExtensionFile = lbl
			}
		}
	}

	// Second positional arg is the extension name
	if len(call.List) > 1 {
		if str, ok := call.List[1].(*build.StringExpr); ok {
			if id, err := label.NewStarlarkIdentifier(str.Value); err != nil {
				p.addError(pos, "use_extension: invalid extension name: %v", err)
			} else {
				ext.ExtensionName = id
			}
		}
	}

	ext.DevDependency = p.getBool(call, "dev_dependency")
	ext.Isolate = p.getBool(call, "isolate")

	return ext
}

func (p *Parser) parseUseRepo(call *build.CallExpr, pos Position) *UseRepo {
	repo := &UseRepo{Pos: pos}

	// First positional arg is the extension proxy (we just capture repos for now)
	repo.Repos = make([]string, 0)

	// Collect all string positional args after the first
	for i := 1; i < len(call.List); i++ {
		if str, ok := call.List[i].(*build.StringExpr); ok {
			repo.Repos = append(repo.Repos, str.Value)
		}
	}

	return repo
}

func (p *Parser) parseSingleVersionOverride(call *build.CallExpr, pos Position) *SingleVersionOverride {
	override := &SingleVersionOverride{Pos: pos}

	moduleName := p.getString(call, "module_name")
	if moduleName == "" {
		p.addError(pos, "single_version_override: missing required 'module_name'")
		return nil
	}

	if m, err := label.NewModule(moduleName); err != nil {
		p.addError(pos, "single_version_override: invalid module_name: %v", err)
		return nil
	} else {
		override.Module = m
	}

	if version := p.getString(call, "version"); version != "" {
		if v, err := label.NewVersion(version); err != nil {
			p.addError(pos, "single_version_override: invalid version: %v", err)
		} else {
			override.Version = v
		}
	}

	override.Registry = p.getString(call, "registry")
	override.Patches = p.getStringList(call, "patches")
	override.PatchCmds = p.getStringList(call, "patch_cmds")
	override.PatchStrip = p.getInt(call, "patch_strip")

	return override
}

func (p *Parser) parseMultipleVersionOverride(call *build.CallExpr, pos Position) *MultipleVersionOverride {
	override := &MultipleVersionOverride{Pos: pos}

	moduleName := p.getString(call, "module_name")
	if moduleName == "" {
		p.addError(pos, "multiple_version_override: missing required 'module_name'")
		return nil
	}

	if m, err := label.NewModule(moduleName); err != nil {
		p.addError(pos, "multiple_version_override: invalid module_name: %v", err)
		return nil
	} else {
		override.Module = m
	}

	versionStrings := p.getStringList(call, "versions")
	for _, vs := range versionStrings {
		if v, err := label.NewVersion(vs); err != nil {
			p.addError(pos, "multiple_version_override: invalid version %q: %v", vs, err)
		} else {
			override.Versions = append(override.Versions, v)
		}
	}

	override.Registry = p.getString(call, "registry")

	return override
}

func (p *Parser) parseGitOverride(call *build.CallExpr, pos Position) *GitOverride {
	override := &GitOverride{Pos: pos}

	moduleName := p.getString(call, "module_name")
	if moduleName == "" {
		p.addError(pos, "git_override: missing required 'module_name'")
		return nil
	}

	if m, err := label.NewModule(moduleName); err != nil {
		p.addError(pos, "git_override: invalid module_name: %v", err)
		return nil
	} else {
		override.Module = m
	}

	override.Remote = p.getString(call, "remote")
	override.Commit = p.getString(call, "commit")
	override.Tag = p.getString(call, "tag")
	override.Branch = p.getString(call, "branch")
	override.Patches = p.getStringList(call, "patches")
	override.PatchCmds = p.getStringList(call, "patch_cmds")
	override.PatchStrip = p.getInt(call, "patch_strip")
	override.InitSubmodules = p.getBool(call, "init_submodules")
	override.StripPrefix = p.getString(call, "strip_prefix")

	return override
}

func (p *Parser) parseArchiveOverride(call *build.CallExpr, pos Position) *ArchiveOverride {
	override := &ArchiveOverride{Pos: pos}

	moduleName := p.getString(call, "module_name")
	if moduleName == "" {
		p.addError(pos, "archive_override: missing required 'module_name'")
		return nil
	}

	if m, err := label.NewModule(moduleName); err != nil {
		p.addError(pos, "archive_override: invalid module_name: %v", err)
		return nil
	} else {
		override.Module = m
	}

	override.URLs = p.getStringList(call, "urls")
	override.Integrity = p.getString(call, "integrity")
	override.StripPrefix = p.getString(call, "strip_prefix")
	override.Patches = p.getStringList(call, "patches")
	override.PatchCmds = p.getStringList(call, "patch_cmds")
	override.PatchStrip = p.getInt(call, "patch_strip")

	return override
}

func (p *Parser) parseLocalPathOverride(call *build.CallExpr, pos Position) *LocalPathOverride {
	override := &LocalPathOverride{Pos: pos}

	moduleName := p.getString(call, "module_name")
	if moduleName == "" {
		p.addError(pos, "local_path_override: missing required 'module_name'")
		return nil
	}

	if m, err := label.NewModule(moduleName); err != nil {
		p.addError(pos, "local_path_override: invalid module_name: %v", err)
		return nil
	} else {
		override.Module = m
	}

	override.Path = p.getString(call, "path")
	if override.Path == "" {
		p.addError(pos, "local_path_override: missing required 'path'")
	}

	return override
}

func (p *Parser) parseRegisterToolchains(call *build.CallExpr, pos Position) *RegisterToolchains {
	reg := &RegisterToolchains{Pos: pos}

	// Positional args are the toolchain patterns
	for _, arg := range call.List {
		if str, ok := arg.(*build.StringExpr); ok {
			reg.Patterns = append(reg.Patterns, str.Value)
		}
	}

	reg.DevDependency = p.getBool(call, "dev_dependency")
	return reg
}

func (p *Parser) parseRegisterExecutionPlatforms(call *build.CallExpr, pos Position) *RegisterExecutionPlatforms {
	reg := &RegisterExecutionPlatforms{Pos: pos}

	// Positional args are the platform patterns
	for _, arg := range call.List {
		if str, ok := arg.(*build.StringExpr); ok {
			reg.Patterns = append(reg.Patterns, str.Value)
		}
	}

	reg.DevDependency = p.getBool(call, "dev_dependency")
	return reg
}

func (p *Parser) parseUseRepoRule(call *build.CallExpr, pos Position) *UseRepoRule {
	rule := &UseRepoRule{Pos: pos}

	// use_repo_rule takes two positional args: bzl_file and rule_name
	if len(call.List) >= 1 {
		if str, ok := call.List[0].(*build.StringExpr); ok {
			rule.RuleFile = str.Value
		}
	}
	if len(call.List) >= 2 {
		if str, ok := call.List[1].(*build.StringExpr); ok {
			rule.RuleName = str.Value
		}
	}

	// Also check named parameters
	if file := p.getString(call, "repo_rule_bzl_file"); file != "" {
		rule.RuleFile = file
	}
	if name := p.getString(call, "repo_rule_name"); name != "" {
		rule.RuleName = name
	}

	return rule
}

func (p *Parser) parseInjectRepo(call *build.CallExpr, pos Position) *InjectRepo {
	inject := &InjectRepo{
		Pos:   pos,
		Repos: make(map[string]string),
	}

	// First arg is the extension proxy
	if len(call.List) >= 1 {
		if ident, ok := call.List[0].(*build.Ident); ok {
			inject.Extension = ident.Name
		}
	}

	// Named kwargs are the repo mappings
	for _, arg := range call.List {
		if assign, ok := arg.(*build.AssignExpr); ok {
			if lhs, ok := assign.LHS.(*build.Ident); ok {
				if str, ok := assign.RHS.(*build.StringExpr); ok {
					inject.Repos[lhs.Name] = str.Value
				}
			}
		}
	}

	return inject
}

func (p *Parser) parseOverrideRepo(call *build.CallExpr, pos Position) *OverrideRepo {
	override := &OverrideRepo{
		Pos:   pos,
		Repos: make(map[string]string),
	}

	// First arg is the extension proxy
	if len(call.List) >= 1 {
		if ident, ok := call.List[0].(*build.Ident); ok {
			override.Extension = ident.Name
		}
	}

	// Named kwargs are the repo mappings
	for _, arg := range call.List {
		if assign, ok := arg.(*build.AssignExpr); ok {
			if lhs, ok := assign.LHS.(*build.Ident); ok {
				if str, ok := assign.RHS.(*build.StringExpr); ok {
					override.Repos[lhs.Name] = str.Value
				}
			}
		}
	}

	return override
}

func (p *Parser) parseFlagAlias(call *build.CallExpr, pos Position) *FlagAlias {
	alias := &FlagAlias{Pos: pos}

	alias.Name = p.getString(call, "name")
	alias.StarlarkFlag = p.getString(call, "starlark_flag")

	if alias.Name == "" {
		p.addError(pos, "flag_alias: missing required 'name' attribute")
	}
	if alias.StarlarkFlag == "" {
		p.addError(pos, "flag_alias: missing required 'starlark_flag' attribute")
	}

	return alias
}

// Helper methods for extracting attributes

func (p *Parser) position(expr build.Expr) Position {
	start, _ := expr.Span()
	return Position{
		Filename: p.filename,
		Line:     start.Line,
		Column:   start.LineRune,
	}
}

func (p *Parser) addError(pos Position, format string, args ...any) {
	p.errors = append(p.errors, &ParseError{
		Pos:     pos,
		Message: fmt.Sprintf(format, args...),
	})
}

func (p *Parser) addWarning(pos Position, format string, args ...any) {
	p.warnings = append(p.warnings, &ParseError{
		Pos:     pos,
		Message: fmt.Sprintf(format, args...),
	})
}

func (p *Parser) getString(call *build.CallExpr, name string) string {
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

func (p *Parser) getInt(call *build.CallExpr, name string) int {
	for _, arg := range call.List {
		if assign, ok := arg.(*build.AssignExpr); ok {
			if lhs, ok := assign.LHS.(*build.Ident); ok && lhs.Name == name {
				if lit, ok := assign.RHS.(*build.LiteralExpr); ok {
					var val int
					fmt.Sscanf(lit.Token, "%d", &val)
					return val
				}
			}
		}
	}
	return 0
}

func (p *Parser) getBool(call *build.CallExpr, name string) bool {
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

func (p *Parser) getStringList(call *build.CallExpr, name string) []string {
	for _, arg := range call.List {
		if assign, ok := arg.(*build.AssignExpr); ok {
			if lhs, ok := assign.LHS.(*build.Ident); ok && lhs.Name == name {
				if list, ok := assign.RHS.(*build.ListExpr); ok {
					result := make([]string, 0, len(list.List))
					for _, elem := range list.List {
						if str, ok := elem.(*build.StringExpr); ok {
							result = append(result, str.Value)
						}
					}
					return result
				}
			}
		}
	}
	return nil
}
