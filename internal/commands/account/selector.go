package account

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/muesli/cancelreader"
	"golang.org/x/term"

	"github.com/frknue/shopify-tools/internal/iostreams"
)

// errCancelled is returned when the user aborts a prompt. It wraps
// context.Canceled so the CLI maps it onto exit code 130 like any other
// interruption.
var errCancelled = fmt.Errorf("cancelled: %w", context.Canceled)

// escapeSequenceTimeout bounds the wait for the rest of an escape sequence, so
// that a lone Escape key is not mistaken for the start of an arrow key.
const escapeSequenceTimeout = 100 * time.Millisecond

// Keys the picker understands beyond the arrow keys: emacs and vi style
// movement, and Ctrl-C to abort.
const (
	keyCtrlC = 3
	keyCtrlN = 14
	keyCtrlP = 16
	keyEsc   = 27
)

// terminalSelector renders a small picker on stderr. On a real terminal it is
// driven with the arrow keys; when either end is redirected it degrades to a
// numbered list, which keeps the tool usable from a script or a test.
type terminalSelector struct {
	streams *iostreams.IOStreams
	// out is where the picker draws: stderr, because it is a prompt.
	out io.Writer
}

func newTerminalSelector(streams *iostreams.IOStreams) *terminalSelector {
	return &terminalSelector{streams: streams, out: streams.ErrOut}
}

// fdOf returns the file descriptor behind a stream, if it has one.
func fdOf(stream any) (int, bool) {
	f, ok := stream.(interface{ Fd() uintptr })
	if !ok {
		return 0, false
	}
	return int(f.Fd()), true
}

// Select asks the user to choose one of options and returns its index.
func (s *terminalSelector) Select(ctx context.Context, title string, options []string) (int, error) {
	if len(options) == 0 {
		return 0, errors.New("no options available")
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}

	inputFD, ok := fdOf(s.streams.In)
	if !ok || !s.streams.IsStdinTTY() || !s.streams.IsStderrTTY() {
		return s.selectNumbered(ctx, title, options)
	}
	return s.selectWithArrows(ctx, title, options, inputFD)
}

func (s *terminalSelector) selectWithArrows(
	ctx context.Context, title string, options []string, inputFD int,
) (selection int, err error) {
	previousState, err := term.MakeRaw(inputFD)
	if err != nil {
		return 0, fmt.Errorf("prepare terminal input: %w", err)
	}
	defer func() {
		if restoreErr := term.Restore(inputFD, previousState); restoreErr != nil {
			err = errors.Join(err, fmt.Errorf("restore terminal input: %w", restoreErr))
		}
	}()

	reader, err := cancelreader.NewReader(s.streams.In)
	if err != nil {
		return 0, fmt.Errorf("prepare selector input: %w", err)
	}

	type readResult struct {
		bytes []byte
		err   error
	}
	reads := make(chan readResult, 1)
	stopReads := make(chan struct{})
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		send := func(result readResult) bool {
			select {
			case reads <- result:
				return true
			case <-stopReads:
				return false
			}
		}
		buffer := make([]byte, 32)
		for {
			count, readErr := reader.Read(buffer)
			if count > 0 {
				chunk := append([]byte(nil), buffer[:count]...)
				if !send(readResult{bytes: chunk}) {
					return
				}
			}
			if readErr != nil {
				send(readResult{err: readErr})
				return
			}
		}
	}()
	defer func() {
		close(stopReads)
		reader.Cancel()
		<-readDone
		if closeErr := reader.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close selector input: %w", closeErr))
		}
	}()

	selected := 0
	fmt.Fprintf(s.out, "? %s\r\n", title)
	s.renderOptions(options, selected, false)

	var pending []byte
	nextByte := func(timeout <-chan time.Time) (byte, error) {
		for len(pending) == 0 {
			select {
			case <-ctx.Done():
				reader.Cancel()
				return 0, ctx.Err()
			case <-timeout:
				return 0, errCancelled
			case result := <-reads:
				if result.err != nil {
					if errors.Is(result.err, cancelreader.ErrCanceled) {
						return 0, errCancelled
					}
					return 0, result.err
				}
				pending = append(pending, result.bytes...)
			}
		}
		value := pending[0]
		pending = pending[1:]
		return value, nil
	}

	for {
		key, readErr := nextByte(nil)
		if readErr != nil {
			s.cancel(len(options))
			return 0, readErr
		}

		switch key {
		case '\r', '\n':
			s.finish(title, options[selected], len(options))
			return selected, nil
		case keyCtrlC:
			s.cancel(len(options))
			return 0, errCancelled
		case 'k', keyCtrlP:
			selected = (selected - 1 + len(options)) % len(options)
			s.renderOptions(options, selected, true)
		case 'j', keyCtrlN:
			selected = (selected + 1) % len(options)
			s.renderOptions(options, selected, true)
		case keyEsc:
			moved, escapeErr := s.readArrow(nextByte, len(options), &selected)
			if escapeErr != nil {
				s.cancel(len(options))
				return 0, escapeErr
			}
			if moved {
				s.renderOptions(options, selected, true)
			}
		}
	}
}

// readArrow completes an escape sequence and applies the arrow key it encodes.
// It reports whether the selection moved.
func (s *terminalSelector) readArrow(
	nextByte func(<-chan time.Time) (byte, error), optionCount int, selected *int,
) (bool, error) {
	next := func() (byte, error) {
		timer := time.NewTimer(escapeSequenceTimeout)
		defer timer.Stop()
		return nextByte(timer.C)
	}

	second, err := next()
	if err != nil {
		return false, err
	}
	if second != '[' {
		return false, nil
	}
	third, err := next()
	if err != nil {
		return false, err
	}
	switch third {
	case 'A':
		*selected = (*selected - 1 + optionCount) % optionCount
	case 'B':
		*selected = (*selected + 1) % optionCount
	default:
		return false, nil
	}
	return true, nil
}

func (s *terminalSelector) renderOptions(options []string, selected int, redraw bool) {
	if redraw {
		fmt.Fprintf(s.out, "\x1b[%dA", len(options))
	}
	for index, option := range options {
		prefix := "  "
		if index == selected {
			prefix = "> "
		}
		fmt.Fprintf(s.out, "\r\x1b[2K%s%s\r\n", prefix, option)
	}
}

// finish replaces the picker with the single line the user chose.
func (s *terminalSelector) finish(title, selected string, optionCount int) {
	s.clear(optionCount)
	fmt.Fprintf(s.out, "\x1b[%dA\r✔ %s\r\n  %s\r\n", optionCount, title, selected)
}

// cancel erases the picker, leaving the terminal as it was found.
func (s *terminalSelector) cancel(optionCount int) {
	s.clear(optionCount)
	fmt.Fprintf(s.out, "\x1b[%dA\r", optionCount)
}

func (s *terminalSelector) clear(optionCount int) {
	fmt.Fprintf(s.out, "\x1b[%dA", optionCount+1)
	for index := 0; index <= optionCount; index++ {
		fmt.Fprint(s.out, "\r\x1b[2K")
		if index < optionCount {
			fmt.Fprint(s.out, "\x1b[1B")
		}
	}
}

// selectNumbered is the fallback for redirected streams: a plain numbered list.
func (s *terminalSelector) selectNumbered(
	ctx context.Context, title string, options []string,
) (selection int, err error) {
	fmt.Fprintf(s.out, "%s:\n", title)
	for index, option := range options {
		fmt.Fprintf(s.out, "  %d) %s\n", index+1, option)
	}

	reader, err := cancelreader.NewReader(s.streams.In)
	if err != nil {
		return 0, fmt.Errorf("prepare selector input: %w", err)
	}
	defer func() {
		if closeErr := reader.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close selector input: %w", closeErr))
		}
	}()

	type scanResult struct {
		line string
		err  error
	}
	for {
		fmt.Fprint(s.out, "> ")
		scanned := make(chan scanResult, 1)
		go func() {
			var line string
			_, scanErr := fmt.Fscanln(reader, &line)
			scanned <- scanResult{line: line, err: scanErr}
		}()

		select {
		case <-ctx.Done():
			reader.Cancel()
			return 0, ctx.Err()
		case result := <-scanned:
			if result.err != nil {
				if errors.Is(result.err, cancelreader.ErrCanceled) {
					return 0, errCancelled
				}
				return 0, fmt.Errorf("read selection: %w", result.err)
			}
			choice, convertErr := strconv.Atoi(strings.TrimSpace(result.line))
			if convertErr == nil && choice >= 1 && choice <= len(options) {
				return choice - 1, nil
			}
			fmt.Fprintf(s.out, "Enter a number between 1 and %d.\n", len(options))
		}
	}
}
