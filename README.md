# Forge

A ground-up, interop-first alternative to opencode: a Go daemon that is wire-compatible with opencode's HTTP+SSE+WebSocket API.

opencode is amazing. This is a personal exercise in reimplementing it from scratch in Go — to see how far the rewrite gets, and as a bonus ship the mobile client that opencode is missing.

Wire compatibility is kept deliberately until interop becomes a wall worth breaking.

## Architecture

```
                ┌─────────────────────────────────────────────┐
   Mobile  ─────┤   Forge Daemon (Go, single static binary)   │── SQLite (sessions/msgs/parts)
   (primary) ───┤   - HTTP/REST + SSE bus + WS PTY            │── repo + built-in tools
   TUI (Go) ────┤   - Auth + directory/instance routing       │── MCP clients (stdio/http/sse)
   opencode's   │   - Agent engine (LLM stream + tool loop)   │── LSP servers (jsonrpc)
   web/desktop  │   - Ecosystem loaders                       │
   (unmodified) │   - Plugin host sidecar (Node/Bun) ◄────────┼── opencode-format TS plugins
                └─────────────────────────────────────────────┘
        all clients speak the SAME opencode wire protocol
```

All clients — mobile, Go TUI, and unmodified opencode web/desktop — speak the same wire protocol (~113 REST+SSE+WebSocket endpoints).

## Quick Start

**Prerequisites:** Go 1.22+

```sh
git clone https://github.com/rotemmiz/forge
cd forge
make build          # outputs bin/forged
./bin/forged        # flags: --host, --port, --mdns, --version, ...
```

The daemon listens on `localhost:4096` by default. Clients authenticate via HTTP Basic or `?auth_token=base64(user:pass)` and route to per-directory instances using the `x-opencode-directory` header.

By default the daemon binds loopback only. Binding a non-loopback interface (`--host 0.0.0.0`, or `--mdns`, which implies it) **requires** a password — set `OPENCODE_SERVER_PASSWORD` or the daemon refuses to start.

## Deployment & Packaging

Releases ship a single static, CGO-free binary per platform plus multi-arch container images, built with [goreleaser](https://goreleaser.com) (`.goreleaser.yaml`). Tagging `vX.Y.Z` runs the `release` workflow; PR CI validates the same config via a snapshot dry-run.

```sh
# Container (binds 0.0.0.0, so a password is required):
docker run -d -p 4096:4096 -e OPENCODE_SERVER_PASSWORD=secret \
  ghcr.io/rotemmiz/forge:latest --host 0.0.0.0 --port 4096
```

Service unit templates live under [`packaging/`](packaging/): a hardened systemd unit (`packaging/systemd/forge.service`) and a macOS launchd agent (`packaging/launchd/dev.forge.daemon.plist`). For remote access, prefer Tailscale, an SSH tunnel, or a TLS-terminating reverse proxy (SSE needs unbuffered proxying) over an open port — see [`plans/13-remote-ops.md`](plans/13-remote-ops.md).

## Testing

```sh
make test                 # unit tests
make conformance          # conformance suite against TARGET= (default: localhost:4096)
make selfdiff             # opencode-vs-opencode self-diff gate
```

Or directly:

```sh
go test ./...
scripts/run-conformance.sh self
```

## Project Status

Early development. The wire protocol conformance harness is the correctness gate. Passing `make selfdiff` clean is required before merging any change that touches an API endpoint.

## License

MIT — see [LICENSE](LICENSE).

## Further Reading

- [`plans/00-masterplan.md`](plans/00-masterplan.md) — vision, frozen wire contract, build sequence
- [`CONTRIBUTING.md`](CONTRIBUTING.md) — git workflow, local CI gate, code style
- [`DEPENDENCIES.md`](DEPENDENCIES.md) — vetted library choices and rationale
- [`CLAUDE.md`](CLAUDE.md) — instructions for agents
