# shopify-tools

A single Go binary that bundles the day-to-day Shopify utilities. Every utility
is a self-contained "tool" (a top-level command) with its own subcommands:

```
shopify-tools <tool> <command> [flags]

shopify-tools auth login --shop acme.myshopify.com
shopify-tools auth status --profile staging -o json
```

## Install

```sh
make build          # -> ./bin/shopify-tools
make install        # -> $GOBIN/shopify-tools
```

Requires Go 1.26+.

## Getting started

```sh
# Save credentials for a store (token is prompted for, never echoed)
shopify-tools auth login --shop acme.myshopify.com

# Verify the credentials work
shopify-tools auth status

# Work with several stores
shopify-tools auth login --shop staging.myshopify.com --profile-name staging
shopify-tools auth list
shopify-tools auth use staging
```

## Tools

### `account` — Shopify CLI accounts

Switches the Shopify CLI between accounts using memorable profile names. This
is about the `shopify` CLI's own login, not about the Admin API tokens that
`auth` keeps.

```sh
shopify-tools account use work    # switch to a profile, or create it if new
shopify-tools account use         # pick one from the saved profiles
shopify-tools account list        # every profile and the account it points at
shopify-tools account logout      # end every Shopify CLI session
```

Creating a profile opens Shopify's own account picker, and whatever account it
confirms is linked to the name — there is no alias to retype. Every switch is
verified against the account the CLI reports back; a profile whose account
Shopify has forgotten is re-linked through the picker instead of failing.

Authentication is delegated entirely to the Shopify CLI, which owns and
refreshes the credentials. Nothing but the name-to-account mapping is stored
here, under `cli_accounts` in the config file. Requires the Shopify CLI on
`PATH`:

```sh
npm install -g @shopify/cli@latest
```

> Profiles from the standalone [`shopify-auth`](https://github.com/frknue/shopify-auth)
> tool are imported once, the first time an `account` command runs. Set
> `SHOPIFY_AUTH_CONFIG` to import them from a non-standard location.

### `auth` — credentials and store profiles

```sh
shopify-tools auth login --shop acme.myshopify.com   # token prompted, never echoed
shopify-tools auth status                            # verify against the live API
shopify-tools auth list                              # every configured profile
shopify-tools auth use staging                       # switch the active profile
shopify-tools auth logout staging
```

### `webhooks` — webhook subscriptions, imperatively or declaratively

One-off changes:

```sh
shopify-tools webhooks list
shopify-tools webhooks list --topic orders/create        # REST-style topics work too
shopify-tools webhooks topics --search order             # read from the live schema
shopify-tools webhooks create --topic ORDERS_CREATE --uri https://api.acme.dev/hooks/orders
shopify-tools webhooks update 1234567890 --uri https://api.acme.dev/hooks/orders-v2
shopify-tools webhooks delete 1234567890 --yes
```

Or keep the desired state in a file and let the CLI converge the store to it:

```yaml
# webhooks.yaml
webhooks:
  - topic: ORDERS_CREATE
    uri: https://api.acme.dev/hooks/orders
    include_fields: [id, total_price]
  - topic: PRODUCTS_UPDATE
    uri: https://api.acme.dev/hooks/products
    filter: "status:active"
```

```sh
shopify-tools webhooks diff --file webhooks.yaml    # what would change
shopify-tools webhooks sync --file webhooks.yaml    # apply it, after confirming
shopify-tools webhooks sync --file webhooks.yaml --prune --yes   # also delete extras
```

`sync` is idempotent: run it twice and the second run reports no changes.
Deletions only happen with `--prune`, so webhooks managed elsewhere are left
alone by default. In CI, `diff --exit-code` fails the build when the store has
drifted from the file. A full annotated manifest lives in
[docs/webhooks.example.yaml](docs/webhooks.example.yaml).

> **Scope:** the Admin API only returns webhooks created through the API by the
> *same* access token, and never the app-scoped ones declared in a
> `shopify.app.toml`. Every `webhooks` command operates on the subscriptions
> belonging to the active profile's token.

## Configuration

Settings are resolved in this order, later sources win:

1. Built-in defaults
2. Config file — `$XDG_CONFIG_HOME/shopify-tools/config.yaml`
   (macOS: `~/Library/Application Support/shopify-tools/config.yaml`)
3. Environment variables — `SHOPIFY_TOOLS_*`
4. Command-line flags

```yaml
current_profile: production
defaults:
  output: table
  timeout_seconds: 30
profiles:
  production:
    shop: acme.myshopify.com
    access_token: shpat_xxx
    api_version: "2026-04"
  staging:
    shop: acme-staging.myshopify.com
    access_token: shpat_yyy
cli_accounts:                     # `account` tool; no credentials, see above
  current: work
  accounts:
    - name: work
      shopify_alias: dev@example.com
```

The file is written with `0600` permissions because it holds access tokens.

### Environment variables

| Variable                     | Purpose                                        |
| ---------------------------- | ---------------------------------------------- |
| `SHOPIFY_TOOLS_CONFIG`       | Path to the config file                        |
| `SHOPIFY_TOOLS_PROFILE`      | Profile to use                                 |
| `SHOPIFY_TOOLS_SHOP`         | Store domain (defines an implicit profile)     |
| `SHOPIFY_TOOLS_ACCESS_TOKEN` | Admin API access token                         |
| `SHOPIFY_TOOLS_API_VERSION`  | Admin API version                              |
| `SHOPIFY_TOOLS_OUTPUT`       | Default output format                          |
| `SHOPIFY_AUTH_CONFIG`        | Where `account` imports shopify-auth profiles from |
| `NO_COLOR`                   | Disable colored output                         |

In CI you can skip the config file entirely:

```sh
export SHOPIFY_TOOLS_SHOP=acme.myshopify.com
export SHOPIFY_TOOLS_ACCESS_TOKEN=shpat_xxx
shopify-tools auth status -o json
```

## Global flags

| Flag              | Description                            |
| ----------------- | -------------------------------------- |
| `--config`        | Config file path                       |
| `-p, --profile`   | Store profile to use                   |
| `-o, --output`    | `table` (default), `json`, `yaml`      |
| `--timeout`       | Timeout for API calls (default `30s`)  |
| `-v, --verbose`   | Debug logging on stderr                |
| `-q, --quiet`     | Suppress non-essential output          |
| `--no-color`      | Disable colored output                 |

Results go to **stdout**, logs and status messages to **stderr**, so
`shopify-tools ... -o json | jq` always works.

## Exit codes

| Code | Meaning                       |
| ---- | ----------------------------- |
| 0    | Success                       |
| 1    | Generic error                 |
| 2    | Usage error                   |
| 3    | Authentication / authorization |
| 4    | Not found                     |
| 5    | Configuration problem         |
| 130  | Cancelled (Ctrl-C)            |

## Shell completion

```sh
shopify-tools completion zsh  > "${fpath[1]}/_shopify-tools"
shopify-tools completion bash > /etc/bash_completion.d/shopify-tools
```

## Project layout

```
cmd/shopify-tools/        entry point: build streams, run, map error -> exit code
internal/
  app/                    dependency container (*Factory) handed to every command
  cli/                    root command, global flags, tool registry, error mapping
  commands/<tool>/        one package per tool (auth, ...)
  config/                 layered configuration and store profiles
  shopify/                Admin API transport: auth, retries, rate limits, errors
  output/                 table / json / yaml renderers
  iostreams/              injectable stdin / stdout / stderr
  buildinfo/              version metadata injected at build time
docs/adding-a-tool.md     how to add a new tool
```

### Design rules

- **Tools are independent.** A tool package imports `internal/app` and the
  shared helpers, never another tool. Adding one touches its own package plus a
  single line in `internal/cli/registry.go`.
- **Dependencies are injected, never global.** Commands receive `*app.Factory`,
  which resolves config, API client, printer and logger lazily and memoises
  them — a command that never calls the API never builds a client, and tests
  swap any of them out.
- **The API package knows transport, not domain.** `internal/shopify` handles
  authentication, retries with `Retry-After`, and error mapping; GraphQL
  documents live with the tool that uses them.
- **Output is data, not text.** Commands build a result value and hand it to a
  printer, so `--output json` works uniformly and without per-command effort.

## Development

```sh
make check      # fmt + vet + lint + test
make test       # go test -race -shuffle=on ./...
make cover      # coverage report in the browser
make run ARGS="auth list"
```

`golangci-lint` is pinned as a Go tool dependency, so `make lint` uses the same
version as CI with no separate install.

## Adding a tool

See [docs/adding-a-tool.md](docs/adding-a-tool.md).
