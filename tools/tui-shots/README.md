# Forge TUI screenshot harness

Deterministic, repeatable screenshots of `forge-tui` for design-system
reference and visual-diff regression against the opencode TUI. No LLM calls:
a fixed session (reusing opencode's `fixture-session.json`) is imported into
a sandboxed opencode daemon, and `forge-tui` attaches to it as a pure HTTP+SSE
client. Scenes are numbered identically to opencode's reference harness so
frames line up 1:1 for side-by-side comparison.

## Architecture

```
fixture-session.json   (opencode's hand-built session: reasoning, md, tools, diff, todos)
       │
       │   opencode import  (into isolated state/, XDG-sandboxed)
       ▼
  opencode serve --port 4199   (headless API server, no LLM calls)
       │
       │   HTTP + SSE
       ▼
  forge-tui --url http://127.0.0.1:4199 --session ses_00… --theme forge-dark/light
       │
       │   VHS tapes
       ▼
  out/forge-{dark,light}/NN-scene.png
```

**Why opencode as the daemon?** `forge-tui` is a pure HTTP+SSE client (plan 08
architecture). It owns no agent state — the daemon is the source of truth.
opencode is wire-compatible, so pointing `forge-tui` at an opencode daemon
seeded with the *same* fixture that opencode's reference harness uses gives
identical content in both front-ends. This is the most honest path to 1:1
visual parity.

**Theme pinning:** `forge-tui` now accepts `--theme <name>` (added in plan 08c
M3), which overrides the KV-pinned or auto-detected theme for deterministic
capture. The harness also writes a sandboxed `tui-kv.json` for belt-and-
suspenders determinism.

## Quick start

```bash
cd tools/tui-shots
./capture.sh               # dark + light, all scenes
./capture.sh dark          # dark only
./capture.sh light         # light only
```

Output lands in `out/forge-{dark,light}/`.

## Side-by-side compare

```bash
./compare.sh dark    # montage forge vs opencode reference for dark mode
./compare.sh light
```

Requires ImageMagick (`montage`) or `ffmpeg`. Without either, it lists path
pairs you can open manually.

## Scenes (matching opencode's numbering)

| # | file | surface |
|---|------|---------|
| 03 | home-empty | splash: wordmark + empty composer + chrome |
| 04 | prompt-text | composer with typed text |
| 06 | markdown-reasoning | top of transcript: reasoning block + markdown |
| 07 | tools-diff | edit diff (read/grep/glob/edit tools) |
| 08 | summary-table | bottom of transcript: markdown table + summary |
| 09 | write-bash-todos | write/code, bash output, todo list |
| 15 | slash-commands | `/` command menu |
| 16 | command-palette | `Ctrl+P` palette |
| 17 | model-list | `Ctrl+X m` |
| 18 | theme-list | `/themes` slash command |
| 19 | session-list | `Ctrl+X l` |
| 20 | agent-list | `Ctrl+X a` |
| 22 | timeline | `Ctrl+X g` |
| 23 | status | `Ctrl+X s` |

## forge-tui keybindings (vs opencode's `Ctrl+X`)

forge-tui also uses `Ctrl+X` as leader key:

| Action | forge-tui | opencode |
|--------|-----------|---------|
| Sessions | `Ctrl+X l` | `Ctrl+X l` |
| New session | `Ctrl+X n` | `Ctrl+X n` |
| Models | `Ctrl+X m` | `Ctrl+X m` |
| Agents | `Ctrl+X a` | `Ctrl+X a` |
| Timeline | `Ctrl+X g` | `Ctrl+X g` |
| Status | `Ctrl+X s` | `Ctrl+X s` |
| Command palette | `Ctrl+P` | `Ctrl+P` |
| Theme list | `/themes` slash cmd | `Ctrl+X t` |

## Adding scenes

- New scene: add keystrokes + `Screenshot out/NN-name.png` to a tape in `tapes/`.
- New content: edit `fixture-session.json` directly (it's a hand-built JSON);
  delete `state/data/` and re-run `capture.sh` to force a re-import.

## Requirements

- `vhs` (+ `ttyd`, `ffmpeg`) — install with `brew install vhs`
- `opencode` ≥ 1.15.12 — `npm install -g opencode-ai`
- `go` ≥ 1.22 — to build `forge-tui`
- `curl` — for daemon health-check polling

## Reference

- opencode's reference harness: `../../screenshots-harness/`
- plan 08c §V.1 — the mandate for this harness
- Theme --flag: `cmd/forge-tui/main.go`, `internal/tui/model.go` `Config.Theme`
