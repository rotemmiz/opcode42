# Vetted dependencies

The libraries below were vetted in `plans/01-daemon-core.md`. Go's module system
prunes unused `require`s on `go mod tidy`, so runtime libraries are added to
`go.mod` **on first import** by the milestone that needs them — they are not
declared ahead of use. This file is the source of truth for *which* library and
*why*, so later milestones don't re-litigate the choice or pick a different one.

The one exception is the build tool, which is pinned now (needed in S3):

| Tool | Module | Pinned | Purpose |
|------|--------|--------|---------|
| oapi-codegen | `github.com/oapi-codegen/oapi-codegen/v2` | v2.7.0 (`tool` directive) | Generate server interfaces + types from the OpenAPI reference (S3). Run via `go tool oapi-codegen`. |

## Runtime libraries (added on first import, per plan 01)

| Concern | Module | Plan ref | Rationale |
|---------|--------|----------|-----------|
| HTTP router | `github.com/go-chi/chi/v5` | §1, M1 | Thin router over net/http; standard `http.Handler` middleware. |
| WebSocket (PTY) | `github.com/coder/websocket` | §3, M5 | Pure Go, no CGo; upgrades `http.ResponseWriter` directly; actively maintained. |
| SQLite | `modernc.org/sqlite` | §5, M2 | Pure-Go, CGo-free — trivial cross-compile to Android/ARM64. |
| SQL scanning | `github.com/jmoiron/sqlx` | §5, M2 | Lightweight struct scanning over database/sql; keeps SQL visible. |
| JSONC config | `github.com/tailscale/hujson` | §4, M1 | Strips JSONC comments/trailing commas; then encoding/json. |
| ULID ids | `github.com/oklog/ulid/v2` | §8, M2/M4 | Prefixed, monotonic, lexicographically-sortable ids. |
| Migrations | `github.com/golang-migrate/migrate/v4` | §5, M2 | Embedded SQL migrations (embed.FS), run on startup. |
| mDNS | `github.com/grandcat/zeroconf` | §9, M6 | Pure Go; richer TXT support than hashicorp/mdns. |
| PTY spawn | `github.com/creack/pty` | §3, M5 | Standard Go PTY allocation. |
| Glob matching | `github.com/bmatcuk/doublestar/v4` | plan 02 M5 | Pure-Go `**` glob support for the `glob`/`grep` built-in tools; `filepath.Glob` lacks `**`. |

## Notes

- A Go-native OpenAPI differ for the spec-drift gate (C0) may reuse
  `github.com/getkin/kin-openapi` / `oasdiff`, which are already in the indirect
  set via oapi-codegen — avoids the `npx openapi-diff` Node dependency.

## TUI client (added plan 08, on first import)

| Concern | Module | Plan ref | Rationale |
|---------|--------|----------|-----------|
| TUI framework | `github.com/charmbracelet/bubbletea` | plan 08 | Elm-style model/update/view; the stack opencode's TUI uses. |
| TUI styling | `github.com/charmbracelet/lipgloss` | plan 08 U1 | Truecolor styles with graceful 256/16-color degrade; renders the design tokens. |
