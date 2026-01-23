// Package ast provides AST types for MODULE.bazel files.
// It wraps github.com/bazelbuild/buildtools/build with higher-level types.
package ast

import (
	"github.com/albertocavalcante/go-bzlmod/label"
	"github.com/bazelbuild/buildtools/build"
)

// Position represents a source position for diagnostics.
type Position struct {
	Filename string
	Line     int
	Column   int
}

// ModuleFile represents a parsed MODULE.bazel file.
type ModuleFile struct {
	Path       string
	Statements []Statement
	Comments   []*Comment
	raw        *build.File
}

// Raw returns the underlying buildtools File for advanced use cases.
func (f *ModuleFile) Raw() *build.File {
	return f.raw
}

// Statement is the interface for all MODULE.bazel statements.
type Statement interface {
	Position() Position
	isStatement()
}

// Comment represents a comment in the source.
type Comment struct {
	Pos  Position
	Text string
}

// ModuleDecl represents a module() declaration.
type ModuleDecl struct {
	Pos                Position
	Name               label.Module
	Version            label.Version
	CompatibilityLevel int
	RepoName           label.ApparentRepo
	BazelCompatibility []string
}

func (m *ModuleDecl) Position() Position { return m.Pos }
func (m *ModuleDecl) isStatement()       {}

// BazelDep represents a bazel_dep() declaration.
type BazelDep struct {
	Pos                   Position
	Name                  label.Module
	Version               label.Version
	MaxCompatibilityLevel int
	RepoName              label.ApparentRepo
	DevDependency         bool
}

func (b *BazelDep) Position() Position { return b.Pos }
func (b *BazelDep) isStatement()       {}

// UseExtension represents a use_extension() call.
type UseExtension struct {
	Pos           Position
	ExtensionFile label.ApparentLabel
	ExtensionName label.StarlarkIdentifier
	DevDependency bool
	Isolate       bool
	// Tags contains the tag calls made on this extension proxy
	Tags []ExtensionTag
}

func (u *UseExtension) Position() Position { return u.Pos }
func (u *UseExtension) isStatement()       {}

// ExtensionTag represents a tag call on a module extension proxy.
type ExtensionTag struct {
	Pos        Position
	Name       string
	Attributes map[string]any
}

// UseRepo represents a use_repo() call.
type UseRepo struct {
	Pos           Position
	Extension     *UseExtension
	Repos         []string
	DevDependency bool
}

func (u *UseRepo) Position() Position { return u.Pos }
func (u *UseRepo) isStatement()       {}

// Override is the interface for all override types.
type Override interface {
	Statement
	ModuleName() label.Module
	isOverride()
}

// SingleVersionOverride represents single_version_override().
type SingleVersionOverride struct {
	Pos          Position
	Module       label.Module
	Version      label.Version
	Registry     string
	Patches      []string
	PatchCmds    []string
	PatchStrip   int
}

func (o *SingleVersionOverride) Position() Position     { return o.Pos }
func (o *SingleVersionOverride) ModuleName() label.Module { return o.Module }
func (o *SingleVersionOverride) isStatement()           {}
func (o *SingleVersionOverride) isOverride()            {}

// MultipleVersionOverride represents multiple_version_override().
type MultipleVersionOverride struct {
	Pos      Position
	Module   label.Module
	Versions []label.Version
	Registry string
}

func (o *MultipleVersionOverride) Position() Position     { return o.Pos }
func (o *MultipleVersionOverride) ModuleName() label.Module { return o.Module }
func (o *MultipleVersionOverride) isStatement()           {}
func (o *MultipleVersionOverride) isOverride()            {}

// GitOverride represents git_override().
type GitOverride struct {
	Pos        Position
	Module     label.Module
	Remote     string
	Commit     string
	Tag        string
	Branch     string
	Patches    []string
	PatchCmds  []string
	PatchStrip int
	InitSubmodules bool
	StripPrefix    string
}

func (o *GitOverride) Position() Position     { return o.Pos }
func (o *GitOverride) ModuleName() label.Module { return o.Module }
func (o *GitOverride) isStatement()           {}
func (o *GitOverride) isOverride()            {}

// ArchiveOverride represents archive_override().
type ArchiveOverride struct {
	Pos         Position
	Module      label.Module
	URLs        []string
	Integrity   string
	StripPrefix string
	Patches     []string
	PatchCmds   []string
	PatchStrip  int
}

func (o *ArchiveOverride) Position() Position     { return o.Pos }
func (o *ArchiveOverride) ModuleName() label.Module { return o.Module }
func (o *ArchiveOverride) isStatement()           {}
func (o *ArchiveOverride) isOverride()            {}

// LocalPathOverride represents local_path_override().
type LocalPathOverride struct {
	Pos    Position
	Module label.Module
	Path   string
}

func (o *LocalPathOverride) Position() Position     { return o.Pos }
func (o *LocalPathOverride) ModuleName() label.Module { return o.Module }
func (o *LocalPathOverride) isStatement()           {}
func (o *LocalPathOverride) isOverride()            {}

// RegisterToolchains represents register_toolchains().
type RegisterToolchains struct {
	Pos           Position
	Patterns      []string
	DevDependency bool
}

func (r *RegisterToolchains) Position() Position { return r.Pos }
func (r *RegisterToolchains) isStatement()       {}

// RegisterExecutionPlatforms represents register_execution_platforms().
type RegisterExecutionPlatforms struct {
	Pos           Position
	Patterns      []string
	DevDependency bool
}

func (r *RegisterExecutionPlatforms) Position() Position { return r.Pos }
func (r *RegisterExecutionPlatforms) isStatement()       {}

// Include represents an include() statement (Bazel 7.2+).
// Only root modules and modules with non-registry overrides can use include().
type Include struct {
	Pos   Position
	Label string
}

func (i *Include) Position() Position { return i.Pos }
func (i *Include) isStatement()       {}

// ExtensionTagCall represents a method call on an extension proxy.
// e.g., go_sdk.from_file(name = "...", go_mod = "...")
type ExtensionTagCall struct {
	Pos        Position
	Extension  string            // The extension variable name (e.g., "go_sdk")
	TagName    string            // The method/tag name (e.g., "from_file")
	Attributes map[string]any    // Named attributes
	Raw        build.Expr        // Original expression for advanced parsing
}

func (e *ExtensionTagCall) Position() Position { return e.Pos }
func (e *ExtensionTagCall) isStatement()       {}

// UnknownStatement represents an unrecognized statement for forward compatibility.
type UnknownStatement struct {
	Pos      Position
	FuncName string
	Raw      build.Expr
}

func (u *UnknownStatement) Position() Position { return u.Pos }
func (u *UnknownStatement) isStatement()       {}
