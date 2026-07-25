// Command shopify-tools is the CLI entry point.
//
// It stays deliberately thin: build the streams, build the factory, run the
// command tree, map the error onto an exit code. All logic lives in internal/.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/frknue/shopify-tools/internal/app"
	"github.com/frknue/shopify-tools/internal/cli"
	"github.com/frknue/shopify-tools/internal/iostreams"
)

func main() {
	os.Exit(run())
}

func run() int {
	// Ctrl-C cancels in-flight API calls instead of killing the process
	// mid-write.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	io := iostreams.System()
	factory := app.NewFactory(io)
	root := cli.NewRootCommand(factory)

	return cli.HandleError(io, root.ExecuteContext(ctx))
}
