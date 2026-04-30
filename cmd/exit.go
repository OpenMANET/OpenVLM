package cmd

import "errors"

// Exit codes documented in the plan's Usage section.
const (
	exitOK            = 0
	exitFailure       = 1
	exitUsage         = 2
	exitNotIdentified = 3
)

// usageError flags a failure that should map to exit code 2 (bad command-line
// input). It wraps the underlying error so `errors.Is` continues to work.
type usageError struct{ err error }

func (e *usageError) Error() string { return e.err.Error() }
func (e *usageError) Unwrap() error { return e.err }

// notIdentifiedError flags a failure where the device is present but the
// GPIO1 strap is low and the user did not pass --force. Maps to exit code 3.
type notIdentifiedError struct{ err error }

func (e *notIdentifiedError) Error() string { return e.err.Error() }
func (e *notIdentifiedError) Unwrap() error { return e.err }

func exitCodeFor(err error) int {
	var ue *usageError
	if errors.As(err, &ue) {
		return exitUsage
	}

	var ne *notIdentifiedError
	if errors.As(err, &ne) {
		return exitNotIdentified
	}

	return exitFailure
}
