# AGENTS.md

Project overview, plans, git workflow, and code style live in `CLAUDE.md`,
`README.md`, and `CONTRIBUTING.md`. Read those first. This file only records
durable, non-obvious operating notes for agents.

## Cursor Cloud specific instructions

The startup update script already refreshes Go modules and installs
`golangci-lint` and `bun`; you do not need to reinstall them. `go`, `golangci-lint`,
and `bun` are on `PATH` (via `~/.bashrc`).

### Services / components
- `opcoded` (Go daemon) — the core product, `cmd/opcoded`. This is what you build,
  run, and test end-to-end.
- `opcode-tui` (Go TUI) — a dogfood client, `cmd/opcode-tui`. Pure HTTP+SSE client;
  the daemon owns all state.
- Android app (`android/`) and Kotlin/Swift SDKs (`sdk/`) — NOT set up here; they
  need the Android SDK / Swift toolchain (see `.github/workflows/ci.yml`, `android/BUILD.md`).

### Build / lint / test / run
Standard commands are in the `Makefile`, `README.md`, and `CONTRIBUTING.md`
(`make build`, `make test`, `make gen`, `make lint`). Non-obvious caveats:

- **Toolchain mismatch for `golangci-lint`.** `go.mod` targets `go 1.26.3` but the
  base `go` is older with auto-toolchain. A `golangci-lint` built with an older Go
  refuses to run ("language version used to build ... is lower than the targeted
  Go version"). It must be built with go1.26.3 — the update script does this via
  `GOTOOLCHAIN=go1.26.3 go install ...@v2.12.2`. Then `golangci-lint run` is clean.
- **`bun` is required for a green `go test ./...`.** `internal/pluginbridge`
  integration tests spawn the plugin host (`packages/opcode42-plugin-host/src/index.ts`).
  They pick the first available JS runtime, and plain `node` cannot execute the
  `.ts` entry, so without `bun` those tests FAIL (they only skip when NO js runtime
  exists at all). `bun` runs the `.ts` entry natively and is preferred.
- **Running the daemon:** `./bin/opcoded` takes flags directly — there is **no
  `serve` subcommand** (the `opcoded serve` shown in `README.md`/`CONTRIBUTING.md`
  is inaccurate). It binds `127.0.0.1:4096` by default and needs no password on
  loopback. Binding non-loopback (`--host 0.0.0.0` or `--mdns`) requires
  `OPENCODE_SERVER_PASSWORD` or it refuses to start.
- **Client + directory routing:** all API calls route per-directory via the
  `x-opencode-directory` header (e.g. `-H "x-opencode-directory: /workspace"`).
  The TUI passes this via `--dir`. Storage is embedded SQLite (pure Go, no DB
  server); sessions persist across daemon restarts.
- **LLM / conformance flows are opt-in.** Live agent runs need a provider key
  (see `.env.example`; nothing auto-loads `.env`). The conformance self-diff
  (`make selfdiff` / `scripts/run-conformance.sh`) needs a real `opencode` binary
  on `PATH`, which is not installed here; those modes skip/require external setup.
