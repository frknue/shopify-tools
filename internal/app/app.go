// Package app wires the CLI's shared dependencies.
//
// Every tool command receives a *Factory instead of reaching for globals. The
// factory resolves each dependency lazily and memoises it, so a command that
// never touches the network never builds an API client, and tests can swap any
// piece out.
package app

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/frknue/shopify-tools/internal/config"
	"github.com/frknue/shopify-tools/internal/iostreams"
	"github.com/frknue/shopify-tools/internal/output"
	"github.com/frknue/shopify-tools/internal/shopify"
)

// Options holds the values of the CLI's global (persistent) flags.
type Options struct {
	ConfigPath   string
	Profile      string
	OutputFormat string
	Timeout      time.Duration
	Verbose      bool
	Quiet        bool
	NoColor      bool
}

// Factory carries the dependencies every command may need.
//
// The function fields exist so tests can inject fakes:
//
//	f := app.NewFactory(io)
//	f.ClientFunc = func(context.Context) (*shopify.Client, error) { return stub, nil }
type Factory struct {
	IOStreams *iostreams.IOStreams
	Options   *Options

	// ConfigFunc loads the configuration. Override in tests.
	ConfigFunc func() (*config.Config, error)
	// ClientFunc builds an Admin API client. Override in tests.
	ClientFunc func(context.Context) (*shopify.Client, error)

	configOnce sync.Once
	config     *config.Config
	configErr  error

	loggerOnce sync.Once
	logger     *slog.Logger
}

// NewFactory returns a factory backed by the real config file and API.
func NewFactory(io *iostreams.IOStreams) *Factory {
	f := &Factory{
		IOStreams: io,
		Options:   &Options{},
	}
	f.ConfigFunc = f.loadConfig
	f.ClientFunc = f.newClient
	return f
}

// Config returns the resolved configuration.
func (f *Factory) Config() (*config.Config, error) { return f.ConfigFunc() }

// Client returns an Admin API client for the selected profile.
func (f *Factory) Client(ctx context.Context) (*shopify.Client, error) { return f.ClientFunc(ctx) }

// Profile resolves the profile selected by --profile, env or config.
func (f *Factory) Profile() (*config.Profile, error) {
	cfg, err := f.Config()
	if err != nil {
		return nil, err
	}
	return cfg.Profile(f.Options.Profile)
}

// Printer returns a printer for the requested output format.
func (f *Factory) Printer() (output.Printer, error) {
	format := f.Options.OutputFormat
	if format == "" {
		if cfg, err := f.Config(); err == nil && cfg.Defaults.Output != "" {
			format = cfg.Defaults.Output
		} else {
			format = string(output.FormatTable)
		}
	}
	parsed, err := output.ParseFormat(format)
	if err != nil {
		return nil, err
	}
	return output.New(f.IOStreams.Out, parsed), nil
}

// Logger returns the process logger. It writes to stderr so that stdout stays
// machine readable.
func (f *Factory) Logger() *slog.Logger {
	f.loggerOnce.Do(func() {
		level := slog.LevelWarn
		switch {
		case f.Options.Quiet:
			level = slog.LevelError
		case f.Options.Verbose:
			level = slog.LevelDebug
		}
		f.logger = slog.New(slog.NewTextHandler(f.IOStreams.ErrOut, &slog.HandlerOptions{Level: level}))
	})
	return f.logger
}

func (f *Factory) loadConfig() (*config.Config, error) {
	f.configOnce.Do(func() {
		path := f.Options.ConfigPath
		if path == "" {
			p, err := config.DefaultPath()
			if err != nil {
				f.configErr = err
				return
			}
			path = p
		}
		f.config, f.configErr = config.Load(path)
	})
	return f.config, f.configErr
}

func (f *Factory) newClient(_ context.Context) (*shopify.Client, error) {
	profile, err := f.Profile()
	if err != nil {
		return nil, err
	}

	timeout := f.Options.Timeout
	if timeout <= 0 {
		if cfg, cfgErr := f.Config(); cfgErr == nil && cfg.Defaults.TimeoutSeconds > 0 {
			timeout = time.Duration(cfg.Defaults.TimeoutSeconds) * time.Second
		} else {
			timeout = 30 * time.Second
		}
	}

	client, err := shopify.New(profile.Shop, profile.AccessToken, profile.APIVersion,
		shopify.WithTimeout(timeout),
		shopify.WithLogger(f.Logger()),
	)
	if err != nil {
		return nil, fmt.Errorf("build shopify client: %w", err)
	}
	return client, nil
}
