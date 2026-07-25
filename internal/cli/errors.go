package cli

import (
	"context"
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/frknue/shopify-tools/internal/config"
	"github.com/frknue/shopify-tools/internal/iostreams"
	"github.com/frknue/shopify-tools/internal/shopify"
)

// Exit codes. Keep these stable: scripts depend on them.
const (
	ExitOK           = 0
	ExitError        = 1
	ExitUsage        = 2
	ExitAuth         = 3
	ExitNotFound     = 4
	ExitCancelled    = 130 // 128 + SIGINT
	ExitConfigDefect = 5
)

// ExitCodeError carries an explicit exit code out of a command.
type ExitCodeError struct {
	Code int
	Err  error
}

func (e *ExitCodeError) Error() string { return e.Err.Error() }
func (e *ExitCodeError) Unwrap() error { return e.Err }

// NewExitError wraps err with an explicit exit code.
func NewExitError(code int, err error) error { return &ExitCodeError{Code: code, Err: err} }

// ErrSilent suppresses the error message but still fails the process. Use it
// when the command already told the user what went wrong.
var ErrSilent = errors.New("silent error")

// HandleError renders err and returns the process exit code.
func HandleError(io *iostreams.IOStreams, err error) int {
	if err == nil {
		return ExitOK
	}

	switch {
	case errors.Is(err, ErrSilent):
		return ExitError

	case errors.Is(err, context.Canceled):
		fmt.Fprintln(io.ErrOut, "cancelled")
		return ExitCancelled

	case errors.Is(err, context.DeadlineExceeded):
		fmt.Fprintln(io.ErrOut, "Error: timed out; raise the limit with --timeout")
		return ExitError
	}

	var exitErr *ExitCodeError
	if errors.As(err, &exitErr) {
		fmt.Fprintf(io.ErrOut, "Error: %v\n", exitErr.Err)
		return exitErr.Code
	}

	if errors.Is(err, config.ErrNoProfile) {
		fmt.Fprintf(io.ErrOut, "Error: %v\n", err)
		return ExitConfigDefect
	}

	if apiErr, ok := shopify.AsAPIError(err); ok {
		fmt.Fprintf(io.ErrOut, "Error: %v\n", apiErr)
		switch {
		case apiErr.IsUnauthorized():
			fmt.Fprintln(io.ErrOut, "Hint: the access token may be invalid or lack the required scope.")
			return ExitAuth
		case apiErr.IsNotFound():
			return ExitNotFound
		}
		return ExitError
	}

	fmt.Fprintf(io.ErrOut, "Error: %v\n", err)
	return ExitError
}

// UsageError marks an error as a misuse of the command, so it exits with the
// conventional code 2 and prints usage.
func UsageError(cmd *cobra.Command, format string, args ...any) error {
	_ = cmd.Usage()
	return NewExitError(ExitUsage, fmt.Errorf(format, args...))
}
