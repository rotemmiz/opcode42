# Plan 08e — TUI finish line: graphics, parity, mDNS, subagents

> **Scope.** The single consolidated plan to take the Opcode42 Go TUI from "functional dogfood
> vehicle" (where plans 08/08a/08b/08c landed it) to a **finished, polished terminal client** with
> graphics fidelity, mobile parity, mDNS discovery, and first-class subagent support — all on the
> Bubble Tea v2 / Lip Gloss v2 compositor that [PR #171](https://github.com/rotemmiz/opcode42/pull/171)
> ports us onto.
>
> **Status of the foundation (verified 2026-07-21).** PR #171 (`track-p08d-tui-v2-spike`) rebases
> cleanly onto `main` at `d76e3d4` (no conflicts), `go build ./...` is green, `go vet` clean, `gofmt -l`
> empty, `go test ./internal/tui/...` green. All seven CI checks were green at PR time. The PR is
> **M1 only** (the mechanical, behavior-preserving v1→v2 port); M2–M5 (canvas, layers, scroll,
> harness re-baseline) are **the first workstream of this plan**. The `cellsel/` selection work
> the PR body mentions is uncommitted and not part of the PR — ignore it; the canvas adoption here
> subsumes it.
>
> **Decision: continue #171, do not rebuild.** The v2 port is clean, the rebase is trivial, and the
> remaining v2 value (the compositor) is the unlock for every visual item below. Rebuilding from
> scratch would throw away a working, gated, CI-green migration and redo the mechanical port for
> no gain. The plan opens from the rebased #171 branch.

## Links

- **Parent:** `plans/08-client-tui.md` (the TUI spec; Phases 0–3 done, U12/U13 closed).
- **Migration:** `plans/08d-tui-bubbletea-v2-migration.md` (M0 spike + M1 port done; M2–M5 are
  the §A workstream here).
- **Visual parity:** `plans/08c-tui-visual-parity.md` (SHIPPED — themes, markdown, syntax, diff,
  spinner, logo shimmer, toasts. This plan *re-baselines* it on the v2 canvas; it does not redo
  it. The 08c items marked "costs more on v1" become "idiomatic on v2" per the 08d parity table).
- **Feature parity:** `plans/08a-tui-parity-now.md` (SHIPPED — session ops, shell, nav, toggles,
  help, editor, mcp/skills, KV/history), `plans/08b-tui-parity-planned.md` (SHIPPED — diff, PTY,
  subagent nav, stash, variant; the daemon-gated items stay parked).
- **Android parity target:** `plans/07-client-mobile.md` + `plans/15-android-ux-overhaul.md`
  (the mobile feature surface to match or exceed).
- **Reference TUI:** `/Users/rotemmiz/git/opencode/packages/opencode/src/cli/cmd/tui/` (TS/opentui).
- **Frozen wire contract:** `/Users/rotemmiz/git/opencode/packages/sdk/openapi.json`.
- **Design handoff:** `design/tui/` (high-fidelity; `design/tui/README.md` is the source of truth).
- **Daemon mDNS:** `internal/mdns/mdns.go` (already publishes `_http._tcp` + `_opencode._tcp`).

## Ground rules (from CLAUDE.md)

- **Wire-compat by default.** Match opencode's endpoints, SSE `{id,type,properties}` shape, PTY
  framing, auth, `x-opencode-directory` routing. Log intentional divergences in plan 12's registry.
- **No fabricated numbers.** Any "feels faster / smoother" claim is unmeasured until captured
  head-to-head in `tools/tui-shots/` vs the opencode reference (`screenshots-harness/`).
- **Go runtime, single binary.** Libs vetted in the plans. New deps in this plan: `grandcat/zeroconf`
  (already vendored by the daemon for mDNS — reuse it for the client side, no new dep).
- **Git workflow per feature.** Each workstream is its own branch → PR → review subagent → CI green
  → squash-merge. The agent owns the PR end-to-end until merged.

---

## Workstream A — Finish the v2 migration (the graphics unlock)

**Source:** plan 08d M2–M5. **Why first:** every visual item below (logo, overlays, diff bg tints,
subagent cards, mDNS picker) is cheaper and structurally correct on the canvas. Doing them on the
v1 string-diff renderer first would be throwaway work.

### A1. Canvas adoption (08d M2)
Replace `internal/tui/paint.go` + `Model.paintsBackground` + manual frame string-compositing with
`lipgloss.NewCanvas(w,h)` as the owned cell buffer. Render the stream, sidebar, footer, composer,
and tasks dock as `Layer`s over the base. Delete the v1 bg-bleed workarounds (the "always paint
background" hacks from 08c Tier 0 become structural: the canvas owns every cell).

- `renderView()` becomes: build a base canvas filled with `P.Bg`, compose each pane as a layer at
  its `(x,y,z)`, return `canvas.Render()`.
- `viewSplash()` / `viewSession()` return layer *content* (strings), not full-frame composites.
- The `overlayToasts` string-splice (08c M11) becomes a `Layer.Z(10)` over the base.
- **Kills the known residual:** the "trailing dark bar on the composer" (08c known-residual, the
  bubbles-internal style we can't reach) — the composer renders as a layer over a filled canvas,
  so the unreachable bubbles style is masked by the canvas fill behind it.

### A2. Layered overlays (08d M3)
Migrate modals (`modal.go`), autocomplete (`slash.go`), toasts (`toast.go`), permission/question
overlays → `Layer.Z` popovers over the base canvas. Drop the width-surgery string splicing and the
`lipgloss.Place(m.width, m.height, Center, Center, ...)` full-frame-center hacks (those fight the
canvas). A modal is now a layer at `(centerX, centerY, Z=20)` with its own border/padding; the base
stream renders underneath undistorted.

- This is also the foundation for the **subagent expandable card** (§C2) and the **mDNS picker**
  (§D2): both are popovers that must not corrupt the base frame.

### A3. Scroll reconciliation (08d M4)
Re-seat the `scrollregion` package (from PR #96, framework-agnostic) on the canvas viewport. Keep
the DECSET-1007 native-copy behavior (the genuinely-good UX decision from #96). Move the clamp/window
math off `model.go` string operations onto the canvas viewport (`Canvas.CellAt`, `Canvas.SetCell`).
Re-validate the keyboard-scroll routing tests.

### A4. Gate + harness re-baseline (08d M5)
Full review gate: `go build/vet`, `gofmt -l`, `golangci-lint run`, `go test ./...`, `make gen` +
`git diff --exit-code internal/api/gen/`, `scripts/run-conformance.sh self`. Re-baseline
`tools/tui-shots/` captures on v2; screenshot-diff vs opencode refs at
`/Users/rotemmiz/git/opencode/screenshots-harness/out/opencode-{dark,light}/`. Add canvas-specific
goldens: full-fill (no transparent cell), layer z-order (modal over stream), composer-no-dark-bar
regression.

**Acceptance:** the TUI renders on v2 with the canvas as the render root; every cell is owned
(no terminal bleed); overlays are z-ordered layers; scroll is native-copy-safe on the canvas. All
existing `internal/tui/*_test.go` stay green. PRs: one squashed PR for A1+A2 (the flip is
non-incremental — LG1↔LG2 `Color` incompatibility forces the canvas+layers together), then A3, A4.

---

## Workstream B — Logo & graphics (the opcode42 identity)

**Source:** design/tui/README.md "Typography → Wordmark", 08c M10 (shipped on v1), 08d parity table
(M10 "bg-pulse becomes feasible" on v2). **Goal:** the splash is the first impression; it must read
as a polished product, not a dogfood tool.

### B1. Re-baseline the block-pixel logo on the canvas
`internal/tui/logo.go` already implements the block-pixel "opcode42" wordmark + left→right shimmer
sweep (ported from opencode `logo.tsx` ShimmerConfig: period 4600ms, rings 2, core/soft/tail/halo
Gaussians). On v1 it renders as per-column `lipgloss.NewStyle().Foreground(col).Render(rune)` strings
spliced into full-width Bg-filled rows — fragile. On the v2 canvas it becomes **per-cell
`SetCell(x, y, cell)` with the shimmer color directly** — the native opentui idiom. The shimmer math
(`shimmerBrightness`, `columnColor`) is unchanged; only the render path moves.

### B2. The bg-pulse framebuffer field (08d "now feasible")
opencode's `bg-pulse-render.ts` paints a subtle background pulse behind the logo — the single most
opentui-specific effect, explicitly marked "optional / least worth the cost" in 08c. On v2 it is a
per-frame `SetCell` color ramp over the canvas region behind the wordmark: a slow ambient breath
synchronized to the shimmer period. Port the `breathBase`/`ambientAmp`/`ambientCenter` numerics
already in `logo.go` (they're computed but only feed the column brightness today) into a background
tint on the logo rows. Gate behind `viewState.bgPulse` (default on for splash, off in session).

### B3. Static logo asset for non-animated contexts
Add a static (frame-0) render of the wordmark for: the `--no-anim` flag, the `tools/tui-shots/`
captures (deterministic), and the "about" / status modal. One function, `logoStatic(p) string`,
returning the brightest-frame wordmark. The animated path is `logoFrame(frame, p)` unchanged.

### B4. Logo in the footer/version chip
The design's footer shows `• Opcode42 0.4.2` with the name bold. Promote this to a small inline
block-pixel mark (3-row micro-logo, derived from `opcode42Glyph` rows 2–4) in the footer version chip.
Subtle, not gimmicky — matches the design's "name bold, version faint" treatment.

**Acceptance:** splash shows the animated opcode42 wordmark with a bg-pulse field, pixel-clean on
light and dark terminals; a static logo renders for captures and the status modal; the footer carries
a micro-mark. Screenshots: capture `01-splash` and diff against opencode's `03-home-empty` (the
wordmark differs by design — opcode42 not opencode — but the *quality* must match).

---

## Workstream C — Subagent support: first-class, in-stream, navigable

**Source:** 08b §9 (shipped — parent/child/sibling nav), plan 15 WS D1 (Android's subagent overhaul).
**Gap:** the TUI has *navigation* (ctrl+x ↓/↑/[ /]) but the Android client has a richer in-stream
card: tappable, expandable transcript, live status, rail subtree. The TUI's `subagentFooterView`
is a one-line strip; the Android `SubAgentBlock` is a full card. **Goal: match Android, exceed it
with terminal-native affordances.**

### C1. In-stream subagent card
Render the `task` tool part as a **card** (not a one-liner row), mirroring Android's `SubAgentBlock`:
- **Header:** spinner (braille `⠋⠙⠹…` from `spinner.go`, already built) or `│` when idle, then
  `<Kind>` in amber-while-running / purple-when-done + `— <task description>`. Kind parsed from the
  tool input's `subagent_type` / `description` (Android reads the same field).
- **Meta line:** `N toolcalls · running…/1.2s` in `FgFaint` — the toolcall count comes from the
  child session's message stream (`GET /session/{id}/children` → child ID → message count).
- **Inline expandable transcript:** `enter`/`space` on the card toggles an indented view of the
  child session's message stream, loaded on first expand via `GET /session/{childId}/message`
  (the SDK `GetJSON` wrapper already exists; add `loadChildMessagesCmd`). This is the TUI's
  advantage over Android: the terminal's scrollback makes a nested transcript natural.
- **"Open as session" affordance:** `o` on the card calls `openSession(childId)` (the existing
  `subagent.go` function) — descend into the child as a full chat view, with a subtitle in the
  status bar showing `subagent of <parent title>`.

### C2. Surface the child session id
The `task` tool's input JSON carries the spawned session id (Android parses it in
`SubAgentBlock.kt:57-58`; verify the field name against opencode's `task` tool input shape in
`packages/opencode/src/server/...`). The TUI's `toolrender.go` currently renders only the
description. Add:
- Parse `childSessionId` from the tool input when the tool is `task`.
- Store it on the rendered block so C1's expand/open actions can target it.
- Fallback: if the id arrives via a `session.updated` SSE with `parentID == <current>`, capture it in
  `store.go`'s reducer (the `childrenLoadedMsg` path already loads children — cross-reference).

### C3. Live subagent status in the sidebar
The design's right sidebar has a **Tasks** block that appears "only while a sub-agent runs": a `●`
dot (`--blue`) + `general · auditing`. Add this to `chrome.go`'s `sidebarView`:
- When `m.childrenOf(m.cfg.SessionID)` is non-empty, render a Tasks section with each child's
  `subagentLabel` + `session.status` (the store tracks `sessionStatus` by id already).
- A running child shows the spinner; a completed child shows `✓`.
- This is the sidebar parity item from 08c M8 that was deferred — the canvas makes it a layer.

### C4. Subagent rail subtree (match Android WS D1)
In the session list modal (`modalSessions`), render children **indented under their parent** instead
of flat. Android does this in the rail (`SessionBrowser.kt`); the TUI's session list is a modal, so
the subtree is: parent row → indented child rows with a `└─` prefix. Keep the `parentID == nil`
filter OFF for this view (the main list filters them; the subtree view shows them). Toggle between
flat and subtree with `t` in the sessions modal.

**Acceptance:** a `task` tool call renders as an expandable card; expanding shows the child
transcript inline; `o` opens it as a session; the sidebar shows live subagent status; the sessions
modal shows a subtree. Tests: `subagent_test.go` extended for the card render + child-id parsing;
`store_test.go` for the subtree projection.

---

## Workstream D — mDNS: discover daemons on the LAN

**Source:** plan 15 WS F3 (Android mDNS), `internal/mdns/mdns.go` (daemon-side publish, already
shipping). **Gap:** the daemon advertises; no Go client discovers. The TUI currently requires
`--url` — a first-run user with a daemon on the LAN has no way to find it from the TUI. **Goal: the
TUI discovers nearby daemons and lists them in a connect overlay.**

### D1. mDNS browser (client-side, reuse zeroconf)
`internal/mdns/mdns.go` uses `github.com/grandcat/zeroconf` to *publish*. The same lib *browses*.
Add `internal/mdns/discover.go`:
- `Browse(ctx, serviceType string) (<-chan Service, error)` — wraps `zeroconf.Browse`, resolves
  each service, emits `Service{Name, Host, Port, TXT}` on the channel.
- Browse **two** service types in parallel (matches Android F3 + the daemon's dual-publish):
  1. `_http._tcp.` — catches `opencode serve --mdns` and the daemon's `_http._tcp` alias.
  2. `_opencode._tcp.` — catches the daemon's brand service type.
- Filter by name prefix `opencode-` or `opcode42-` (the daemon names instances `opcode42-<port>`,
  opencode names `opencode-<port>`). Dedupe by `(host, port)`.
- **No new dependency** — `zeroconf` is already in `go.mod` (the daemon uses it). This is a
  client-side reuse, vetted by the existing `internal/mdns/mdns_test.go`.

### D2. Connect overlay (first-run + `/connect`)
Add a `modalConnect` modal (or a first-run screen `ScreenConnect`) that:
- Shows a "Nearby servers" list populated by D1's browser (started when the modal opens, stopped on
  close — battery/CPU is not a TUI concern but clean lifecycle still matters).
- Each row: `opencode-4096` / `opcode42-4096` + `host:port` + a green/amber dot for reachability
  (a quick `GET /global/health` probe per discovered service, best-effort).
- `enter` on a row fills the URL (`http://<host>:<port>`) and connects (reuses the existing
  `healthCmd` + `openSSECmd` path; just swaps the client's URL).
- A manual URL field at the top for the `--url`-equivalent entry (for daemons not on mDNS, or
  remote/tunnel URLs). This subsumes the `--url` flag; the flag still works for scripts.
- `/connect` slash command + `ctrl+x c` leader key open it.

### D3. First-run flow
Mirror Android's F1: if no `--url` is passed AND no KV-pinned server exists, open the connect
overlay on startup instead of the splash. Once a connection succeeds, pin the URL to KV
(`server_url` key) so subsequent runs skip the overlay. Add `--no-discover` flag to disable mDNS
browsing entirely (airgapped / CI / scripted runs).

### D4. Daemon-side mDNS is already correct
Verified: `internal/mdns/mdns.go:66` publishes `_http._tcp` with TXT `{path:"/"}` (matches opencode
`mdns.ts:21`), and `:79` publishes `_opencode._tcp` (the Opcode42 brand type). `cmd/opcoded/main.go:73`
has the `--mdns` flag. **No daemon changes needed.** The TUI browses what the daemon already
advertises. The one divergence — `opcode42.local` vs `opencode.local` host — is handled by browsing
on service *type* + name *prefix*, not host (matches Android F3's filter logic).

**Acceptance:** run `opcoded --mdns --hostname 0.0.0.0` on the LAN; the TUI's `/connect` lists
`opcode42-<port>` within seconds; `enter` connects; a remote URL pasted in the manual field
connects. First-run with no `--url` opens the connect overlay. Tests: `discover_test.go` with a
loopback mDNS publisher (reuse `mdns_test.go`'s `freePort` helper).

---

## Workstream E — Mobile parity: the items that make sense for a terminal

**Source:** `plans/15-android-ux-overhaul.md` (the mobile feature surface). **Not all mobile
features map to a TUI** — voice input, push notifications, FCM, image thumbnails (Coil), and the
foldable/tablet adaptive layout are mobile-native and *out of scope* here. The items below are the
parity gaps that a terminal client can and should close.

### E1. VCS working-tree diff source
Android has `GET /vcs/diff?directory=&mode=git` (the heavier working-tree changes with patches) and
`GET /vcs/status` (`Opcode42Client.kt:116-130`). The TUI's diff reviewer (`diff.go`) is
**session-patch only** — it calls `GET /session/{id}/diff` and notes "VCS working-tree source is
deferred" (`diff.go:21`). Close it:
- Add `SDK.VCSDiff(ctx, dir)` and `SDK.VCSStatus(ctx, dir)` to the Go SDK wrapper (`conn.go`).
- Add a source switch in `diffState`: `sourceSession` | `sourceWorkingTree`. `ctrl+x s` in the diff
  reviewer toggles (matches opencode's `diff_switch_source`).
- The session-patch path is unchanged; the working-tree path renders the same unified/split viewer
  on the new data. This is the last 08b §1 follow-up.

### E2. Image parts (sixel / iTerm inline)
Android renders image `FilePart`s as Coil thumbnails + a full-screen zoomable viewer (WS D2). A
terminal can't decode JPEGs natively, but it *can* emit **Sixel** graphics (VT340+, widely
supported: xterm -ti vt340, mlterm, wezterm, kitty) or **iTerm2 inline image** escape sequences.
- In `render.go`'s file-part renderer, branch on `part.mime.startsWith("image/")`:
  - Detect Sixel support (`$TERM` or a `--sixel` flag) → emit the sixel-encoded image (use a Go
    sixel encoder; `github.com/mrmpp/sixel` or a small in-package encoder). Fallback: a placeholder
    glyph `🖼 <filename> (N×M, <mime>)` in `FgDim`.
  - Detect iTerm2 (`$TERM_PROGRAM == iTerm.app`) → emit the iTerm2 inline-image base64 escape.
- This is **stretch / opt-in** — gate behind `viewState.images` (default off; enable via
  `ctrl+x i`). Most terminals won't support it, but wezterm/kitty/iTerm users get a real visual.
  Non-image files keep the existing chip render.

### E3. Reconcile-on-reconnect for pending permissions/questions
Android WS I / plan 16 Bug 3: when the daemon cancels a question/permission without an SSE event
(agent finalizer), the client's store has a stale entry. Android's fix: `reconcilePending()` on
reconnect + on `session.status → idle`, fetching `GET /permission` and `GET /question` and replacing
the store. The TUI's `store.go` reducer handles `permission.replied`/`question.replied`/`rejected`
but has **no reconcile-on-reconnect**. Port it:
- On `streamOpenedMsg` (reconnect success), fire `reconcilePendingCmd` → `GET /permission` +
  `GET /question` (parallel) → `permissionsReconciledMsg` / `questionsReconciledMsg` that *replace*
  the store's maps (matches Android `StoreReducer.kt:114`).
- On a `session.updated` with `status == "idle"` for the open session, fire the same reconcile.
- Swallow HTTP 404 on the reply endpoints silently (plan 16 Bug 1 — the question is already gone;
  the optimistic clear already removed it from the UI; the 404 must not surface as a status error).

### E4. Question card in the stream (match Android D3's in-stream block)
Android renders a synthetic question block in the chat stream when `pendingQuestion != null`
(plan 15 D3 part 4), so the question is visible in history, not just a transient modal. The TUI's
`questionView()` is a blocking centered overlay only. Add a **collapsible question card** inline in
`renderSession()`:
- When `pendingQuestion != nil`, render a card after the last assistant message: header + question
  text + the selected/answered state. `enter` opens the full overlay (the existing `questionView`).
- After `questionRepliedMsg`/`rejected`, the card collapses to the answered/skipped state (showing
  the selected labels or "Skipped") and stays in the scrollback.
- This is the 08c M7 "foldable blocks" pattern applied to questions — the canvas makes it a layer.

### E5. Context gauge (match Android D4)
Android's context bar flake (WS D4) — on session switch, the context gauge blanks because tokens
need a completed assistant turn. The TUI's sidebar (`chrome.go`) has the same issue. Fix:
- On session switch, populate the context gauge from the **session's last message's tokens**
  (fetched in `loadMessagesCmd`, already runs on switch) — not waiting for a *new* turn.
- Cache the providers catalog across session switches (load once per connection; the limit resolves
  synchronously from the cache).
- Draft (no messages): show `0 / <default limit>` instead of blank.

**Acceptance:** the TUI matches Android on VCS diff, reconcile-on-reconnect, in-stream question
card, and context gauge. Image rendering is opt-in and works on Sixel/iTerm2 terminals. Voice,
push, and FCM are explicitly out of scope (mobile-native). Tests: `conn_test.go` for the VCS SDK
wrappers, `store_test.go` for the reconcile reducers, `render_test.go` for the question card.

---

## Workstream F — Polish, parity audit, and the finish line

### F1. Visual parity audit (the 08c gate, re-run on v2)
Re-run the `tools/tui-shots/` harness (08c §V) on the v2 canvas. Capture all scenes against the
opencode reference set at `/Users/rotemmiz/git/opencode/screenshots-harness/out/opencode-{dark,light}/`.
The scene set (from 08c): `01-splash`, `03-home-empty`, `06-markdown-reasoning`, `07-tools-diff`,
`08-summary-table`, `09-write-bash-todos`, plus the modals (`palette/model/agent/theme/session/
timeline/status`). The opcode42 scenes differ by brand but the *quality* (background fill, color
treatment, spacing, diff fidelity, syntax) must match. Record per-scene pixel-diff % as a guidance
signal. This is the gate for §B (graphics) and §A (canvas).

### F2. Keybind discoverability (08a §E, shipped but re-check on v2)
The `ctrl+x` leader shows a one-line cheat-sheet (model.go:381). On the v2 canvas, promote this to a
**which-key overlay** (a `Layer.Z(15)` strip showing the next-key options after `ctrl+x` is pressed,
not a status-line string) — matches opencode's `feature-plugins/system/which-key.tsx`. Pure UX polish
but it's the difference between "dogfood" and "product" for new users.

### F3. Help overlay (08a §E)
`modalHelp` listing all keybinds, generated from the keybind table. `/help` + `ctrl+x h` + `F1`.
Static content; the value is discoverability for the ~40 keybinds the TUI now has.

### F4. Conformance: TUI↔Opcode42 dual-run (U13, re-pointed)
The U13 parity gate (`internal/tui/opcode42_e2e_test.go`) boots the real `internal/server` + a
deterministic mock provider and asserts the core flows. Re-validate on the v2 canvas: health + SSE,
session create, prompt → streamed parts, permission round-trip, question round-trip, abort. This is
the wire-compat proof and the CLAUDE.md non-negotiable. No new endpoints in this plan (all consumed
endpoints already exist on the daemon), so the conformance `self` gate stays green by construction.

### F5. Known-divergence registry update
Update plan 12's `known-divergence` registry for any intentional divergence this plan introduces:
- mDNS client browsing (additive — no divergence; the daemon already publishes compatibly).
- Image rendering via Sixel/iTerm2 (additive client-only; no wire divergence).
- Subagent in-stream card (client rendering; no wire divergence — same `GET /children` + `GET /message`).
- VCS working-tree diff source (uses existing daemon endpoints; no divergence).
The `GET /command` non-deterministic-ordering exclusion (08 §U12) is unchanged.

**Acceptance:** the TUI passes the conformance `self` gate; the visual oracle shows parity with
opencode references; all known divergences are recorded; the keybind help makes the TUI
discoverable. This is the finish line.

---

## Sequencing & dependencies

```
A1+A2 (canvas + layers) ──┬──> A3 (scroll) ──> A4 (gate)
                          ├──> B1-B4 (logo/graphics)   [needs canvas]
                          ├──> C1-C4 (subagents)       [layers for cards]
                          ├──> D2 (connect overlay)    [layers for modal]
                          └──> E1-E5 (parity)          [canvas + layers]
D1 (mDNS browser) ─── independent, no dep on A; ship in parallel with A.
F1-F5 (polish/audit) ── after A-E land.
```

**Critical path:** A1+A2 → (B, C, D2, E in parallel) → F. A1+A2 is the irreversible flip (canvas +
layers in one PR, like 08d M1+M2); everything else layers onto it on the same branch flow.

## Effort & sizing

| Workstream | Est | PRs |
|---|---|---|
| A — v2 canvas + layers + scroll + gate | 5d | 2 (A1+A2 squashed, A3, A4) |
| B — logo & graphics | 2d | 1 |
| C — subagent support | 3d | 1 |
| D — mDNS | 1.5d | 1 (D1 + D2-D4) |
| E — mobile parity | 3d | 1-2 |
| F — polish & audit | 1.5d | 1 |
| **Total** | **~16d** | **~7 PRs** |

Sizes assume the v2 port (PR #171) is merged first as the foundation. D1 (mDNS browse) is the only
truly independent piece — it can ship before A if we want an early win.

## Out of scope (explicitly deferred)

- **Voice input** (Android `VoiceInputController`) — mobile-native; a TUI has no mic affordance.
- **Push notifications / FCM** (Android `POST /push/register`) — mobile-native; a TUI runs in a
  terminal, not a notification-bearing context. The terminal bell (08a §I, shipped) is the TUI's
  "notification."
- **Foldable/tablet adaptive layout** (Android `AdaptiveChatScreen` triptych) — the TUI is a
  single-column terminal; width adaptation (sidebar toggle) is already handled.
- **TUI plugin host** (08b §5) — architectural, daemon-gated, explicitly "probably never for a
  dogfood TUI." Unchanged.
- **Workspace management** (08b §3) — daemon-gated (`/experimental/workspace*`). Unchanged.
- **Provider OAuth / org auth** (08b §4) — daemon-gated, belongs to daemon + Android. Unchanged.
- **Tag** (08b §8) — daemon-gated (session metadata). Unchanged.

## Risks / decisions to flag

1. **PR #171 must merge first.** This plan's §A is the direct continuation of 08d M2–M5. If #171
   is rejected or stalls, this plan is blocked at A1. Mitigation: #171 is clean (rebased, builds,
   tests green, CI green) — merge it as the first action.
2. **Canvas API surface** — 08d M0 verified `NewCanvas(w,h)` + `Compose(Layer)` + `SetCell`. If
   the v2 canvas API has drifted since M0 (the PR is from 2026-06), re-verify with `go doc
   charm.land/lipgloss/v2` before A1. The M1 port already imports `charm.land/lipgloss/v2 v2.0.3`.
3. **Sixel support is fragmentary** (E2) — gate hard behind `viewState.images` and a capability
   probe; never emit sixel escapes to a terminal that didn't advertise support (garbage on screen).
4. **mDNS on non-LAN** — browsing when the network blocks multicast (VPNs, strict firewalls) just
   yields an empty list; the manual URL field (D2) is the fallback. No crash, no hang.
5. **Subagent child-id parsing** (C2) — depends on the exact `task` tool input JSON shape. Verify
   against opencode's tool definition before coding; the fallback (session.updated with parentID)
   covers the case where the id isn't in the tool input.
6. **No fabricated smoothness claims** — per CLAUDE.md, the v2 canvas "feeling smoother" is
   unmeasured until `tools/tui-shots/` captures it head-to-head. F1's pixel-diff is the measurement.

## Testing posture (unchanged gate, extended)

- Every `internal/tui/*_test.go` stays green through A (translation must not change behavior).
- A adds: canvas full-fill golden, layer z-order golden, composer-no-dark-bar regression.
- B adds: logo frame goldens (static + a few animated frames), bg-pulse golden.
- C adds: subagent card render golden, child-id parsing, subtree projection.
- D adds: `discover_test.go` (loopback mDNS publish + browse round-trip), connect overlay render.
- E adds: VCS SDK wrappers, reconcile reducers, question card render, context gauge projection.
- F adds: the re-baselined `tools/tui-shots/` screenshot-diff gate.
- Full review gate each round (`go build/vet`, `gofmt -l`, `golangci-lint`, `go test ./...`,
  `make gen` diff-clean, conformance `self`) + independent review subagent until clean, per
  CLAUDE.md git workflow.

## Decisions baked in (flag if reality contradicts)

1. **Continue PR #171, do not rebuild.** The v2 port is clean and CI-green; rebuilding throws away
   gated work. Merge #171 as the foundation; A1 is its direct continuation.
2. **Reuse `grandcat/zeroconf` for client-side mDNS** (D1). No new dependency — the daemon already
   vets it. Client browsing is the mirror of the daemon's publishing.
3. **Sixel/iTerm2 images are opt-in** (E2). Default off; capability-probed; never emit to an
   unsupported terminal. The placeholder render is always available.
4. **Subagent in-stream card is the TUI's mobile-parity advantage** (C1). The terminal's scrollback
   makes a nested transcript natural — the TUI can exceed Android here, not just match.
5. **No mobile-native features in the TUI** (out of scope). Voice, push, FCM, foldable layout are
   mobile-native; chasing them in a terminal is miscalibrated. The terminal bell (08a §I) is the
   TUI's notification surface; `ctrl+x` leader is the TUI's "gesture."