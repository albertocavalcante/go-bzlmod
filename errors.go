package gobzlmod

import "errors"

// Sentinel errors for common registry failures.
var (
	// ErrModuleNotFound indicates the requested module does not exist in any registry.
	ErrModuleNotFound = errors.New("module not found")

	// ErrVersionNotFound indicates the requested version does not exist.
	ErrVersionNotFound = errors.New("version not found")

	// ErrRateLimited indicates the registry is rate limiting requests.
	ErrRateLimited = errors.New("rate limited")

	// ErrUnauthorized indicates authentication is required or failed.
	ErrUnauthorized = errors.New("unauthorized")
)

// UnsupportedInputError reports syntax that static resolution cannot evaluate.
// Callers can use errors.Is(err, errors.ErrUnsupported) or errors.As.
type UnsupportedInputError struct {
	Directive string
	Attribute string
	Reason    string
}

func (e *UnsupportedInputError) Error() string {
	if e.Reason != "" {
		return "unsupported MODULE.bazel input: " + e.Directive + ": " + e.Reason
	}
	if e.Attribute != "" {
		return "unsupported MODULE.bazel input: " + e.Directive + "." + e.Attribute + " requires a literal value"
	}
	return "unsupported MODULE.bazel directive: " + e.Directive
}

// Unwrap identifies unsupported static resolution inputs.
func (e *UnsupportedInputError) Unwrap() error { return errors.ErrUnsupported }
