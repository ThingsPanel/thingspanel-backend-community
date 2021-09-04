package errors

import (
	"errors"
	"fmt"
	"runtime"
)

// New creates a new instance of the base error.
func New(msg string) error {
	return fmt.Errorf("%s %s", msg, filePath())
}

// Wrap creates a new error by wrapping an existing error.
func Wrap(err error, msg string) error {
	return fmt.Errorf("%s %s \ncaused by: %w", msg, filePath(), err)
}

// Unwrap unwraps the error
func Unwrap(err error) error {
	return errors.Unwrap(err)
}

// Is reports whether any error in err's chain matches target.
func Is(err, target error) bool {
	return errors.Is(err, target)
}

// Errorf returns an error with the specified format
func Errorf(format string, args ...interface{}) error {
	args = append(args, filePath())
	return fmt.Errorf(format+" %s", args...)
}

// filePath returns the location in which the error occurred.
func filePath() string {
	pc, f, l, ok := runtime.Caller(2) // nolint
	fn := `unknown`
	if ok {
		fn = runtime.FuncForPC(pc).Name()
	}

	return fmt.Sprintf("at %s\n\t%s:%d", fn, f, l)
}
