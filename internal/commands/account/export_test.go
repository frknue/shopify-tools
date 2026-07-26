package account

import "github.com/frknue/shopify-tools/internal/iostreams"

// CurrentAccountFromOutput exposes the parser for the Shopify CLI's
// "Current account: …" line to the package's tests.
var CurrentAccountFromOutput = currentAccountFromOutput

// NewTerminalSelector exposes the real picker, so that the tests can drive it
// from a pseudo terminal instead of always replacing it with a fake.
func NewTerminalSelector(streams *iostreams.IOStreams) Selector {
	return newTerminalSelector(streams)
}

// NewExecRunner exposes the runner that shells out to the Shopify CLI. Tests
// using it MUST put a fake `shopify` first on PATH.
func NewExecRunner(streams *iostreams.IOStreams) Runner { return newExecRunner(streams) }

// The validators guard what reaches the config file, including whatever was
// scraped out of the Shopify CLI's terminal output.
var (
	ValidateProfileName  = validateProfileName
	ValidateShopifyAlias = validateShopifyAlias
)
