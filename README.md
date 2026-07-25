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
    api_version: "2025-07"
  staging:
    shop: acme-staging.myshopify.com
    access_token: shpat_yyy
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
