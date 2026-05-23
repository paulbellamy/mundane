package mundane

import "fmt"

// Errors do not include a "mundane:" prefix; the CLI prepends one in die().
// Go callers can format as they like.

type LockedError struct {
	Path string
}

func (e *LockedError) Error() string {
	return fmt.Sprintf("%s: locked by another process", e.Path)
}

type SchemaError struct {
	Path    string
	Version string
}

func (e *SchemaError) Error() string {
	return fmt.Sprintf("%s: schema_version is %q, expected %q", e.Path, e.Version, SchemaVersion)
}

type DuplicateStepError struct {
	Name string
}

func (e *DuplicateStepError) Error() string {
	return fmt.Sprintf("duplicate step name: %s", e.Name)
}

type SerializationError struct {
	Detail string
}

func (e *SerializationError) Error() string {
	return "value does not JSON round-trip: " + e.Detail
}

type StepFailedError struct {
	Name string
	Err  error
}

func (e *StepFailedError) Error() string {
	return fmt.Sprintf("step %q failed: %v", e.Name, e.Err)
}

func (e *StepFailedError) Unwrap() error { return e.Err }

type InvalidNameError struct {
	Name string
}

func (e *InvalidNameError) Error() string {
	return fmt.Sprintf("invalid step name %q (must match %s)", e.Name, NamePattern)
}
