// Package output renders command results in the format the user asked for.
//
// Commands never format their own output: they build a value and hand it to a
// Printer. That keeps `--output json` working uniformly across every tool and
// makes the CLI scriptable.
package output

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"gopkg.in/yaml.v3"
)

// Format is a supported rendering format.
type Format string

// Supported output formats.
const (
	FormatTable Format = "table"
	FormatJSON  Format = "json"
	FormatYAML  Format = "yaml"
)

// Formats lists every valid value of Format, for flag help and completion.
func Formats() []string {
	return []string{string(FormatTable), string(FormatJSON), string(FormatYAML)}
}

// ParseFormat validates a user supplied format string.
func ParseFormat(s string) (Format, error) {
	switch f := Format(strings.ToLower(strings.TrimSpace(s))); f {
	case FormatTable, FormatJSON, FormatYAML:
		return f, nil
	default:
		return "", fmt.Errorf("invalid output format %q: must be one of %s", s, strings.Join(Formats(), ", "))
	}
}

// Tabler is implemented by results that have a meaningful tabular rendering.
// Values that do not implement it fall back to JSON even in table mode.
type Tabler interface {
	Headers() []string
	Rows() [][]string
}

// Printer renders values to a writer.
type Printer interface {
	// Print renders a command result.
	Print(v any) error
	// Format reports the format this printer renders.
	Format() Format
}

type printer struct {
	w      io.Writer
	format Format
}

// New returns a Printer writing to w in the given format.
func New(w io.Writer, f Format) Printer {
	return &printer{w: w, format: f}
}

func (p *printer) Format() Format { return p.format }

func (p *printer) Print(v any) error {
	switch p.format {
	case FormatJSON:
		return p.printJSON(v)
	case FormatYAML:
		return p.printYAML(v)
	case FormatTable:
		if t, ok := v.(Tabler); ok {
			return p.printTable(t)
		}
		return p.printJSON(v)
	default:
		return fmt.Errorf("unsupported output format %q", p.format)
	}
}

func (p *printer) printJSON(v any) error {
	enc := json.NewEncoder(p.w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

func (p *printer) printYAML(v any) error {
	enc := yaml.NewEncoder(p.w)
	enc.SetIndent(2)
	if err := enc.Encode(v); err != nil {
		return err
	}
	return enc.Close()
}

func (p *printer) printTable(t Tabler) error {
	tw := tabwriter.NewWriter(p.w, 0, 4, 3, ' ', 0)
	if headers := t.Headers(); len(headers) > 0 {
		fmt.Fprintln(tw, strings.Join(headers, "\t"))
	}
	for _, row := range t.Rows() {
		fmt.Fprintln(tw, strings.Join(row, "\t"))
	}
	return tw.Flush()
}
