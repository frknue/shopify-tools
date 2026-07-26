// Package exitcode defines the process exit codes and the error type that
// carries one.
//
// It is a leaf package on purpose: internal/cli maps errors onto these codes,
// and tool packages need to produce them, so neither can own the definition
// without creating an import cycle.
package exitcode

// Exit codes. Keep these stable: scripts depend on them.
const (
	OK           = 0
	Error        = 1
	Usage        = 2
	Auth         = 3
	NotFound     = 4
	ConfigDefect = 5
	Cancelled    = 130 // 128 + SIGINT
)

// CodeError carries an explicit exit code out of a command.
type CodeError struct {
	Code int
	Err  error
}

func (e *CodeError) Error() string { return e.Err.Error() }
func (e *CodeError) Unwrap() error { return e.Err }

// New wraps err with an explicit exit code.
func New(code int, err error) error { return &CodeError{Code: code, Err: err} }
