# shopify-tools

A Go CLI (Go 1.26+, cobra) that bundles Shopify utilities as independent "tools":
`shopify-tools <tool> <command> [flags]`. Ships as a single static binary; no
server, no runtime dependencies. `auth` is the reference tool — copy its shape.

## Commands

```sh
make check                  # fmt + vet + lint + test — run before every commit
make build                  # -> ./bin/shopify-tools (injects version via ldflags)
make test                   # go test -race -shuffle=on ./...
make lint                   # go tool golangci-lint run ./...
make run ARGS="auth list"
make cover                  # coverage report in the browser
```

Single test / package:

```sh
go test -run TestNormalizeShop ./internal/config/
go test -race ./internal/shopify/
```

## Architecture

Data flows one way: `main` → `cli` (root cmd + registry) → `commands/<tool>` →
`shopify` (transport) → `output` (render) → `iostreams`.

| Path | Role |
| --- | --- |
| `cmd/shopify-tools/` | Entry point only: build streams, run, map error → exit code. Keep it thin. |
| `internal/app/` | `*Factory` — the dependency container passed to every command. Resolves config, API client, printer, logger lazily and memoises them. |
| `internal/cli/` | Root command, global flags, `registry.go` (tool list), `errors.go` (error → exit code). |
| `internal/commands/<tool>/` | One package per tool. Tools never import each other. Current: `account`, `auth`, `webhooks`. |
| `internal/exitcode/` | Exit-code constants and the error type carrying one. A leaf package: tool packages need the codes, and `internal/cli` maps errors onto them, so neither can own it without an import cycle. **Tools import `exitcode`, never `cli`.** |
| `internal/config/` | Layered config: defaults < file < `SHOPIFY_TOOLS_*` env < flags. Named store profiles, plus the `account` tool's Shopify CLI mappings under `cli_accounts`. |
| `internal/shopify/` | Admin API **transport only** — auth headers, retries, error mapping. No domain logic. |
| `internal/output/` | table / json / yaml renderers. |
| `internal/iostreams/` | Injectable stdin/stdout/stderr; `iostreams.Test()` returns buffers. |

### Adding a tool

Two steps, documented in full in `docs/adding-a-tool.md`:

1. Create `internal/commands/<tool>/` exposing `NewCommand(*app.Factory) *cobra.Command`.
2. Add it to the slice in `internal/cli/registry.go`.

Nothing else. Global flags, config, output formats, exit codes and completion
come from the root command for free.

## Conventions

These are enforced by review, not by the compiler — getting them wrong compiles fine:

- **No globals.** Everything a command needs comes from `*app.Factory`. Never add
  package-level state to a tool.
- **Never write to `os.Stdout`/`os.Stderr` directly.** Results go to
  `f.IOStreams.Out`, progress/status/logs to `f.IOStreams.ErrOut`, so that
  `-o json | jq` keeps working.
- **Never format output by hand.** Build a result type implementing
  `output.Tabler`, hand it to `f.Printer()`. That is what makes `--output
  json|yaml` work on every tool without per-command code.
- **Return errors, never call `os.Exit`.** `internal/cli/errors.go` maps them.
  For a specific code use `exitcode.New(exitcode.Error, err)` from a tool
  package (importing `cli` there is an import cycle), or `cli.NewExitError`
  from inside `internal/cli`. Wrap with `%w`.
- **Thread `cmd.Context()`** into every call that does I/O, so Ctrl-C cancels
  in-flight requests.
- **GraphQL documents live with the tool that uses them**, not in
  `internal/shopify`. That package must not grow when tools are added.
- Subcommand shape: options struct → `newXCommand(f)` binding flags → `runX(ctx,
  f, opts)` → result type. See `internal/commands/auth/status.go`.
- Import grouping: stdlib, third-party, then `github.com/frknue/shopify-tools/...`
  (goimports `local-prefixes` enforces this).

### Exit codes

Stable, scripts depend on them: `0` ok, `1` generic, `2` usage, `3` auth,
`4` not found, `5` config, `130` cancelled. Defined in `internal/cli/errors.go`.

## Testing

Test commands through the factory with fake streams — never shell out to the binary:

```go
io, stdout, _ := iostreams.Test()
f := app.NewFactory(io)
f.Options.OutputFormat = "json"
f.ConfigFunc = func() (*config.Config, error) { return cfg, nil }
f.ClientFunc = func(context.Context) (*shopify.Client, error) { /* httptest-backed */ }
```

`ConfigFunc` and `ClientFunc` exist for exactly this. Fake the API with
`httptest` + `shopify.WithBaseURL(srv.URL)`; never hit a real store in tests.
Examples: `internal/commands/auth/auth_test.go` (single tool),
`internal/cli/root_test.go` (end-to-end through the root command).

Interactive paths need a real terminal: `iostreams.Test()` reports no TTY, so
a picker under it silently takes its non-interactive fallback and the code that
matters never runs. Use `iostreams.FromFiles` over a `pty.Open()` pair and
drive it with keystrokes — see `internal/commands/account/terminal_test.go`,
which runs `NewCommand` with nothing faked but the `shopify` binary itself.
Always drain the master side, or a full terminal buffer deadlocks the command.

## Gotchas

- **golangci-lint is a pinned Go tool dependency.** Use `go tool golangci-lint
  run ./...` (or `make lint`) — do not install or invoke a global binary; the
  version would differ from CI. This is why `go.sum` is large.
- **Plain `go build` loses the version.** Version/commit/date are injected via
  `-ldflags` into `internal/buildinfo`; use `make build`. Unset values fall back
  to `dev` plus the VCS stamps the toolchain embeds.
- **`config.yaml` is gitignored and must stay that way** — it holds Admin API
  access tokens and is written at `0600`. Never write a token into a test
  fixture path outside `t.TempDir()`.
- **Config tests must use `t.Setenv`/`t.TempDir`**, never the real config path,
  or they will clobber the developer's credentials.
- **The environment never edits the config file.** `applyEnv` records what it
  overlaid and `Save` reverts it, so a `SHOPIFY_TOOLS_ACCESS_TOKEN` from CI is
  not persisted and cannot overwrite a stored one. Change config through
  `SetProfile`/`SetCurrentProfile` rather than assigning the fields: those mark
  the change as deliberate, and one that merely happens to match the
  environment's value would otherwise be reverted on save.
- **`account` shells out to the real `shopify` CLI**; its tests must always
  inject a fake through `account.NewCommandWithDeps`, or they would change the
  developer's Shopify login. They must also point `SHOPIFY_AUTH_CONFIG` at a
  temp path, because the tool imports the standalone shopify-auth profiles once.
- `go mod tidy` must leave no diff — CI fails on it.
- Tests run with `-shuffle=on`: no ordering dependencies between tests.
- **Validate new GraphQL against the schema** before building code around it —
  the Shopify admin skill's `validate.mjs` does this. `webhookSubscriptions`
  returns only subscriptions created via the API *by the same access token*,
  never app-scoped ones from a `shopify.app.toml`; `WebhookSubscriptionInput`
  uses `uri` (`callbackUrl` is deprecated).
- **`config.DefaultAPIVersion` needs bumping roughly quarterly.** Shopify
  supports each version for about a year.
