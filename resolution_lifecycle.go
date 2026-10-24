package gobzlmod

import (
	"context"
	"fmt"

	"github.com/albertocavalcante/go-bzlmod/selection"
	"github.com/albertocavalcante/go-bzlmod/selection/version"
)

// runResolution owns the public lifecycle, including RegistrySource's target
// checks after dependency selection. Validation happens before any file or IO.
func runResolution(ctx context.Context, opts ResolutionOptions, resolve func() (*ResolutionList, error)) (list *ResolutionList, err error) {
	if err = validateOptions(&opts); err != nil {
		return nil, fmt.Errorf("invalid options: %w", err)
	}
	emitProgress(opts.OnProgress, ProgressEvent{Type: ProgressResolveStart})
	defer func() {
		event := ProgressEvent{Type: ProgressResolveEnd}
		if err != nil {
			event.Message = err.Error()
		}
		emitProgress(opts.OnProgress, event)
	}()
	if contextErr := ctx.Err(); contextErr != nil {
		return nil, contextErr
	}
	list, err = resolve()
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		return nil, err
	}
	return list, nil
}

func emitProgress(callback func(ProgressEvent), event ProgressEvent) {
	if callback != nil {
		callback(event)
	}
}

func fetchModuleWithProgress(ctx context.Context, reg Registry, name, moduleVersion string, callback func(ProgressEvent)) (*ModuleInfo, error) {
	event := ProgressEvent{Type: ProgressModuleFetchStart, Module: name, Version: moduleVersion}
	emitProgress(callback, event)
	info, err := reg.GetModuleFile(ctx, name, moduleVersion)
	event.Type = ProgressModuleFetchEnd
	if err != nil {
		event.Message = err.Error()
	}
	emitProgress(callback, event)
	return info, err
}

// Compare effective root edges by position, preserving aliases and multiple
// versions. Discovery already applies overrides, dev filtering and root refs.
func checkDirectDeps(mode DirectDepsCheckMode, declared, selected []selection.DepSpec) ([]string, error) {
	if mode == DirectDepsOff {
		return nil, nil
	}
	if len(declared) != len(selected) {
		return nil, fmt.Errorf("direct dependency edge count changed during selection")
	}
	var mismatches []DirectDepMismatch
	for i, dep := range declared {
		if dep.Version == "" || version.Compare(dep.Version, selected[i].Version) == 0 {
			continue
		}
		mismatches = append(mismatches, DirectDepMismatch{Name: dep.Name, DeclaredVersion: dep.Version, ResolvedVersion: selected[i].Version})
	}
	if len(mismatches) == 0 {
		return nil, nil
	}
	err := &DirectDepsMismatchError{Mismatches: mismatches}
	if mode == DirectDepsError {
		return nil, err
	}
	return []string{err.Error()}, nil
}
