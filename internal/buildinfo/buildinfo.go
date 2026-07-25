// Package buildinfo exposes the version metadata of the binary.
//
// Values are injected at build time via -ldflags (see the Makefile) and fall
// back to the information the Go toolchain embeds in the binary, so that
// `go install` builds still report something useful.
package buildinfo

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"strings"
)

// Injected via -ldflags "-X github.com/frknue/shopify-tools/internal/buildinfo.version=...".
var (
	version = ""
	commit  = ""
	date    = ""
)

// Info describes the running binary.
type Info struct {
	Version   string `json:"version" yaml:"version"`
	Commit    string `json:"commit" yaml:"commit"`
	Date      string `json:"date" yaml:"date"`
	GoVersion string `json:"go_version" yaml:"go_version"`
	Platform  string `json:"platform" yaml:"platform"`
}

// Current returns the build information of the running binary.
func Current() Info {
	i := Info{
		Version:   version,
		Commit:    commit,
		Date:      date,
		GoVersion: runtime.Version(),
		Platform:  runtime.GOOS + "/" + runtime.GOARCH,
	}

	if bi, ok := debug.ReadBuildInfo(); ok {
		if i.Version == "" && bi.Main.Version != "" {
			i.Version = bi.Main.Version
		}
		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs.revision":
				if i.Commit == "" {
					i.Commit = s.Value
				}
			case "vcs.time":
				if i.Date == "" {
					i.Date = s.Value
				}
			}
		}
	}

	if i.Version == "" {
		i.Version = "dev"
	}
	return i
}

// Version returns just the semantic version string.
func Version() string { return Current().Version }

// String renders a single-line human readable summary.
func (i Info) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "shopify-tools %s", i.Version)
	if i.Commit != "" && i.Commit != "unknown" {
		short := i.Commit
		if len(short) > 7 {
			short = short[:7]
		}
		fmt.Fprintf(&b, " (%s)", short)
	}
	fmt.Fprintf(&b, " %s %s", i.GoVersion, i.Platform)
	return b.String()
}

// UserAgent is sent with every outbound HTTP request.
func UserAgent() string {
	return "shopify-tools/" + Current().Version + " (+https://github.com/frknue/shopify-tools)"
}
