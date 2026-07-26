// Package iostreams bundles the process' standard streams so that every
// command writes through an injectable seam instead of touching os.Stdout
// directly. That is what makes commands testable.
package iostreams

import (
	"bytes"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

// IOStreams holds the input/output streams a command may use.
type IOStreams struct {
	In     io.Reader
	Out    io.Writer
	ErrOut io.Writer

	inTTY  bool
	outTTY bool
	errTTY bool

	colorEnabled bool
}

// System returns the streams backed by the real process handles.
func System() *IOStreams { return FromFiles(os.Stdin, os.Stdout, os.Stderr) }

// FromFiles returns streams backed by the given files, detecting terminals and
// colour support the way System does. Tests use it to drive a command from a
// pseudo terminal, which is the only way to exercise the interactive paths.
func FromFiles(in, out, errOut *os.File) *IOStreams {
	s := &IOStreams{
		In:     in,
		Out:    out,
		ErrOut: errOut,
		inTTY:  isTerminal(in),
		outTTY: isTerminal(out),
		errTTY: isTerminal(errOut),
	}
	s.colorEnabled = envColorEnabled(s.outTTY)
	return s
}

// Test returns streams backed by buffers, for use in tests.
func Test() (streams *IOStreams, stdout, stderr *bytes.Buffer) {
	out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
	return &IOStreams{
		In:     strings.NewReader(""),
		Out:    out,
		ErrOut: errOut,
	}, out, errOut
}

// IsStdinTTY reports whether stdin is attached to a terminal.
func (s *IOStreams) IsStdinTTY() bool { return s.inTTY }

// IsStdoutTTY reports whether stdout is attached to a terminal.
func (s *IOStreams) IsStdoutTTY() bool { return s.outTTY }

// IsStderrTTY reports whether stderr is attached to a terminal.
func (s *IOStreams) IsStderrTTY() bool { return s.errTTY }

// ColorEnabled reports whether ANSI colors should be emitted.
func (s *IOStreams) ColorEnabled() bool { return s.colorEnabled }

// SetColorEnabled overrides color detection (used by --no-color).
func (s *IOStreams) SetColorEnabled(v bool) { s.colorEnabled = v }

// TerminalWidth returns the width of the output terminal, or a sane default.
func (s *IOStreams) TerminalWidth() int {
	if f, ok := s.Out.(*os.File); ok && s.outTTY {
		if w, _, err := term.GetSize(int(f.Fd())); err == nil && w > 0 {
			return w
		}
	}
	return 80
}

func isTerminal(f *os.File) bool {
	return term.IsTerminal(int(f.Fd()))
}

// envColorEnabled applies the informal NO_COLOR / CLICOLOR conventions.
func envColorEnabled(isTTY bool) bool {
	if os.Getenv("CLICOLOR_FORCE") != "" && os.Getenv("CLICOLOR_FORCE") != "0" {
		return true
	}
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		return false
	}
	if os.Getenv("CLICOLOR") == "0" {
		return false
	}
	if os.Getenv("TERM") == "dumb" {
		return false
	}
	return isTTY
}
