package webhooks

import (
	"bufio"
	"fmt"
	"strings"

	"github.com/frknue/shopify-tools/internal/app"
)

// confirm asks a yes/no question on stderr. When stdin is not a terminal it
// refuses rather than guessing, so a piped or CI invocation must pass --yes.
func confirm(f *app.Factory, question string) (bool, error) {
	if !f.IOStreams.IsStdinTTY() {
		return false, fmt.Errorf("%s refusing to continue without a terminal: pass --yes to confirm", question)
	}

	fmt.Fprintf(f.IOStreams.ErrOut, "%s [y/N] ", question)
	line, err := bufio.NewReader(f.IOStreams.In).ReadString('\n')
	if err != nil && line == "" {
		return false, fmt.Errorf("read confirmation: %w", err)
	}

	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true, nil
	default:
		return false, nil
	}
}
