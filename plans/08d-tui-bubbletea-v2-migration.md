# Plan 08d — TUI infra migration to Bubble Tea v2 / Lip Gloss v2 (+ the compositor unlock)

> **Status: M0 SPIKE PASSED (2026-06-04).** Research complete; the de-risking spike is green (see
> "M0 results" below). Awaiting go/no-go on M1 (the irreversible flip). This plan supersedes the
> *approach* of incremental v1 polish (PR #96 and the 08c "Bubble Tea is a string-diff renderer,
> animations cost more" framing). It does **not** discard 08c's content goals — it makes most of them
> *cheaper* by moving onto a real compositor.
>
> ### M0 results (spike branch `track-p08d-tui-v2-spike`, `cmd/v2spike/` — throwaway)
> - **Vanity paths resolve** on go1.26.3: `charm.land/{bubbletea v2.0.7, bubbles v2.1.0, glamour v2.0.0,
>   lipgloss v2.0.3}`. *Hard gate (risk #1) cleared.*
> - **v1 + v2 coexist**: `go build ./...` is green with both present (different major-version module
>   paths). This means M1 could, if needed, stage file-by-file rather than big-bang — though a single
>   focused PR is still recommended (risk #2 softened, not removed).
> - **Compositor verified**: `lipgloss.NewCanvas(w,h)` + `.Compose(NewLayer(s).X().Y().Z())` renders a
>   z-ordered, true-color, per-cell composite (a modal layer painted over a base stream layer). This is
>   the opentui model, working in Go.
> - **BT2 API verified**: `Model.View() tea.View` + `tea.KeyPressMsg` compile and `go vet` clean.
> - **Two web claims corrected by ground truth** (`go doc`): `NewCanvas` is `(w,h int)` then chained
>   `.Compose(layer)` — **not** the variadic `NewCanvas(...layers)` the beta blog showed. And `Model.Init()
>   Cmd` / `Update(Msg)(Model,Cmd)` are **unchanged** — only `View()` changed (a migration guide summary
>   wrongly implied `Init` returns a Model). The reference table below is corrected accordingly.

## Why this plan exists

PR #96 ("graphics-fineness pass + `scrollregion`") is green and mergeable, but the user's read is correct:
graphics + usability are still subpar, and the PR is **fighting the framework**. Every hard part of it —
full-screen background paint (`internal/tui/paint.go`, `model.go` `paintsBackground`), native-selection-safe
scrollback via raw DECSET 1007 (`scrollregion/`), the "trailing dark bar on the composer" residual logged in
08c, manual modal/toast string-splicing — is a symptom of **Bubble Tea v1's string-diff renderer**: there is
no cell buffer, no z-ordered overlay, no owned background. 08c says this out loud (§"The honest architecture
gap"): opencode's TUI is **opentui** — SolidJS over a **frame-buffer compositor** with per-cell control and a
live render loop; Forge's is Bubble Tea, "a string-diff renderer," so "everything static is achievable… the
animations cost more."

**That gap closed in 2026.** Lip Gloss **v2** ships a real **canvas + layers compositor**, and Bubble Tea
**v2** ships higher-fidelity input and a declarative cell-based render path. Migrating the infra is now the
highest-leverage usability move: it deletes a class of v1 hacks and makes opentui parity a styling exercise
rather than a fight.

## Reference facts (grounded — do not re-derive from memory)

| Lib | Forge today (`go.mod`) | v2 (resolved in M0) | New module path |
|---|---|---|---|
| Bubble Tea | `v1.3.10` | `v2.0.7` | `charm.land/bubbletea/v2` |
| Lip Gloss | `v1.1.1-0.2025…` | `v2.0.3` (v2.0.0 pub. 2026-04-13) | `charm.land/lipgloss/v2` |
| Bubbles | `v1.0.0` | `v2.1.0` (requires BT2 + LG2) | `charm.land/bubbles/v2` |
| Glamour | `v1.0.0` | `v2.0.0` (pub. 2026-03-09, on LG2) | `charm.land/glamour/v2` |

Sources: charmbracelet `UPGRADE_GUIDE_V2.md` (bubbletea / lipgloss / bubbles / glamour), Lip Gloss v2
release notes, `pkg.go.dev/charm.land/lipgloss/v2`. **chroma** (08c M5) is charm-independent — unaffected.

> ⚠️ **Module-path change is real**: v2 imports are vanity paths under `charm.land/...`, not
> `github.com/charmbracelet/...`. Verify `go get charm.land/lipgloss/v2` resolves on this toolchain in M0
> before committing the rest. (They redirect to the same GitHub repos; this is a `go.mod` directive, not a
> source move.)

### M1 results (mechanical port, behavior-preserving)
The port surfaced four v2 semantic changes worth recording (they will recur in M2+):
- **`theme.Color`**: introduced a local `type Color string` implementing `image/color.Color` (RGBA
  delegates to `lipgloss.Color`) so the TUI's pervasive hex-string color math survives while satisfying
  v2's `color.Color`-typed `Foreground/Background`. Replaced 100+ `lipgloss.Color` *type* usages; the
  `lipgloss.Color(...)` *call* sites were already v2-shaped.
- **`Width`/`Height` now include the border.** v1 added borders *outside* `Width`; v2 counts them inside.
  Every bordered box with an explicit `Width(target − borderCols)` compensation had to drop the
  compensation (sidebar) or add the border cols back (`toast +4`, modal/permission/question `+2`). The
  top-only border on the tasks dock needed no change.
- **Space bar stringifies as `"space"`** (was `" "`); handlers that matched `case " ":` now match
  `" ", "space"`. This is real-input correctness, not just tests.
- **`Strikethrough` renders per-character SGR** in v2, so raw-substring test assertions over struck
  text must strip ANSI first.
- **bubbles v2 composer**: `FocusedStyle`/`BlurredStyle` fields → `Styles()`/`SetStyles()` (a copy);
  `HasDarkBackground()` → `HasDarkBackground(os.Stdin, os.Stdout)`; `Model.View() string` → `tea.View`
  with `AltScreen` set on the struct (replacing `tea.WithAltScreen`).

---

## The compositor unlock (the reason to do this)

Lip Gloss v2 adds **`Canvas` + `Layer`** (verified on `pkg.go.dev/charm.land/lipgloss/v2`):

```go
canvas := lipgloss.NewCanvas(width, height)          // an owned cell buffer
canvas.Compose(lipgloss.NewLayer(stream).X(0).Y(0).Z(0))          // base
canvas.Compose(lipgloss.NewLayer(box.Render("modal")).X(5).Y(10).Z(2)) // overlay, on top
out := canvas.Render()
// also: canvas.SetCell(x,y,*uv.Cell) / CellAt / Compose(uv.Drawable) / Resize / Clear
// (verified via go doc in M0; the beta blog's variadic NewCanvas(...layers) is stale)
```

This is **opentui's model, in Go**: per-cell control (`SetCell`/`CellAt`), z-ordered overlays (`Layer.Z`),
nested layers (`Layer.AddLayers`), absolute positioning (`X/Y`). Concretely it dissolves the PR #96 pain
points:

| PR #96 / 08c hack (v1) | v2 replacement |
|---|---|
| `paint.go` full-screen bg fill + `paintsBackground` exclusion + "uncolored cell bleed-through" | `NewCanvas(w,h)` is an **owned buffer**; fill once, no transparent cells, no terminal bleed |
| composer "trailing dark bar" residual (08c known-residual) | composer renders into a layer over a filled canvas — no unreachable bubbles-internal style |
| `scrollregion` raw DECSET 1007 + wheel-as-arrows (to keep native copy) | **keep** `scrollregion` (it's framework-agnostic stdlib) *or* fold into BT2's higher-fidelity input; either way the **clamp/window math** moves off `model.go` strings onto canvas viewport |
| manual modal/toast/autocomplete string splicing (`modal.go`, `toast.go`, `slash.go`) | **`Layer` overlays with `Z`** — true z-ordered popovers, no width-math surgery on the base frame |
| 08c Tier-3 "animations cost more in Bubble Tea" (spinner scanner, logo shimmer, bg-pulse) | per-frame `SetCell` over the canvas = the opentui `requestRender` pattern; **bg-pulse becomes feasible**, not "explicitly optional" |

The migration is therefore not just an upgrade — it's the thing that makes 08c Tier-2/Tier-3 parity tractable.

---

## Migration surface (grounded in the current tree — `grep` counts)

`internal/tui/` is ~13.8k LOC across ~50 files (~27 non-test). The surface is **mechanical and bounded**:

**Bubble Tea (input/program/view):**
- `42×` `tea.KeyMsg` → `tea.KeyPressMsg` (now an interface; press vs release). Highest-churn item.
- `~30×` `tea.KeyXxx` constants + field reads: `msg.Type`→`msg.Code` (rune), `msg.Runes`→`msg.Text` (string),
  `msg.Alt`→`msg.Mod.Contains(tea.ModAlt)`, `" "`→`"space"`. Most forge key handling already switches on
  `msg.String()` — those lines are **untouched**; only `.Type`/`.Runes`/constant comparisons change.
- `20×` `tea.WindowSizeMsg` — unchanged shape; still delivered.
- `Model.View() string` → `tea.View` struct (only the **top-level** `Model.View` in `view.go`/`model.go`;
  the ~6 `xxxView() string` helpers stay `string` and become **layer content**).
- `cmd/forge-tui/main.go:38` `tea.NewProgram(model, tea.WithAltScreen())` → drop the option; set
  `view.AltScreen = true` (and mouse mode) on the returned `tea.View`.
- `tea.Sequence` already correct (v2 name). `tea.Tick` (5×) unchanged. `tea.ExecProcess` (PTY) unchanged.

**Lip Gloss (color/style):**
- `156×` `lipgloss.Color("…")` — **already the v2 call shape** (a function call), so call sites largely
  survive. The break is **typing**: any field/var typed `lipgloss.Color` / `lipgloss.TerminalColor` becomes
  `color.Color` (+`import "image/color"`). Audit `theme/theme.go` `Palette` struct fields specifically.
- `3×` `lipgloss.HasDarkBackground()` → `HasDarkBackground(os.Stdin, os.Stdout)`; pairs with `lipgloss.LightDark(hasDark)`
  for the 08c Tier-0 light/dark auto-pick (cleaner than today).
- `2×` `lipgloss.WithWhitespaceBackground` → `WithWhitespaceStyle(NewStyle().Background(…))`.
- `87× NewStyle`, `49× Width`, `Join*`, `Place`, borders — **API-stable**; recompile-only.
- Output downsampling moved to write-time: any direct `fmt.Print(style)` → `lipgloss.Print/Sprint`. (BT2
  owns program I/O, so in-program rendering is unaffected; check `tools/tui-shots` / any stderr prints.)

**Bubbles (composer only — `2× textinput`, `1× textarea`):**
- `DefaultKeyMap` var → `DefaultKeyMap()` func; `ti.Width = n` → `ti.SetWidth(n)`.
- Style fields → `Styles{Focused,Blurred}` (`StyleState`); `DefaultStyles(isDark)`.
- Cursor: `ta.Cursor` field → `ta.Cursor()` (`*tea.Cursor`); `SetCursor`→`SetCursorColumn`; `VirtualCursor`
  toggle. **This likely fixes the composer dark-bar residual outright.**

**Glamour (markdown, 08c M4):** `charm.land/glamour/v2`; its `ansi.StyleConfig` is now LG2-colored — the
theme→glamour style mapping rebuilds against `color.Color`. **chroma** (08c M5) unaffected.

> **Non-incremental within the package:** LG1 and LG2 `Color` types are incompatible, so `internal/tui`
> (and `theme/`) must flip **in one PR** — you cannot half-migrate a package. Scope it as one focused branch.

---

## Migration plan (milestones)

| # | Deliverable | Est | Notes |
|---|---|---|---|
| **M0** ✅ | **Spike** — DONE (see "M0 results"). | 0.5d | Gate cleared. |
| **M1** ✅ | **Mechanical port** — DONE. Full repo builds; `go vet`/`gofmt`/`golangci-lint` clean; **all `internal/tui` + repo tests green**; `go mod tidy` dropped the v1 charm deps (4 `charm.land/.../v2` direct). 58 files, +429/−378. See "M1 results" below. | 2d | Lean on `go build` errors as the checklist. |
| **M2** | **Canvas adoption**: replace `paint.go` + `paintsBackground` + manual frame compositing with a `NewCanvas(w,h)` base layer; render stream/sidebar/footer/composer as layers. Delete the v1 bg-bleed workarounds | 1.5d | First *real* win: kills the white-bg/dark-bar class of bugs structurally. |
| **M3** | **Layered overlays**: modals (`modal.go`), autocomplete (`slash.go`), toasts (`toast.go`), permission/question prompts → `Layer.Z` popovers over the base canvas; drop width-surgery splicing | 1.5d | Unlocks correct overlapping + no base-frame corruption. |
| **M4** | **Scroll reconciliation**: re-seat `scrollregion` on the canvas viewport (keep DECSET-1007 native-copy behavior from PR #96; move clamp/window onto canvas). Re-validate keyboard-scroll routing tests | 1d | Preserves PR #96's genuinely-good UX decision; sheds its string math. |
| **M5** | **Gate + harness**: full review gate (`build/vet/gofmt/golangci/test`, `make gen` diff-clean, conformance `self`); re-baseline `tools/tui-shots/` captures (08c §V) on v2; screenshot-diff vs opencode refs | 1d | Proves no regressions; refreshes the visual oracle. |

**Critical path:** M0 → M1 → M2 (after M2 the usability/graphics complaint is materially addressed). M3–M4
are the overlay/scroll polish; M5 is the gate. ~7.5d total, single squashed PR (M1 is the irreversible flip;
M2–M5 layer onto it on the same branch).

---

## Parity-with-opencode roadmap (post-migration — 08c, made cheaper on v2)

The 08c milestones now re-cost. Keep 08c's content goals; re-base them on the compositor:

| 08c item | On v1 (08c framing) | On v2 |
|---|---|---|
| M0 always-paint bg + light/dark auto-pick | the central hack | **free** — canvas owns the buffer; `LightDark()` is first-class |
| M1/M2 token system + 33 JSON themes | unaffected | unaffected (theme JSON is data; colors flip to `color.Color`) |
| M4 markdown (glamour) | v1 glamour | glamour **v2** (already LG2) — drop-in once M1 lands |
| M5 syntax (chroma) | independent | independent — unchanged |
| M6 diff bg tints + line-number gutter + intra-line | per-cell bg via string padding | **`SetCell` per-cell bg** — exactly the opentui approach |
| M7 tool/message richness, foldable blocks | manual | layered, collapsible regions |
| M8 chrome (footer/sidebar/dialogs) | string joins | layers + `Place` |
| **M9 spinner gradient-scanner** | "costs more"; `tea.Tick` + string synth | per-frame `SetCell` color ramp — the opentui `getScannerState` math ports directly |
| **M10 logo shimmer + bg-pulse** | bg-pulse "explicitly optional / least worth the cost" | **bg-pulse becomes feasible** — a per-frame canvas field is now the native idiom |
| M11 toasts / connecting states | overlay string-splice | `Layer.Z` overlay (done as part of M3) |

**Net:** the migration converts 08c's two hardest, most-deferred pillars (per-cell diff bg, and Tier-3
motion incl. bg-pulse) from "expensive / optional" to "idiomatic." That is the parity argument.

---

## What happens to PR #96

PR #96 mixes (a) **durable** wins and (b) **soon-throwaway** v1 hacks. Recommended split:
- **Keep / cherry-pick forward:** the `scrollregion` package + its native-copy DECSET-1007 decision
  (framework-agnostic, survives — M4 reuses it); the sidebar-width / token-dedup / gutter inset *values*
  (re-applied as canvas layout); the tests' *intent*.
- **Expect to delete in M2/M3:** `paint.go` full-screen fill, `paintsBackground`, manual frame string
  compositing — superseded by the canvas.

Two viable orderings (decision for the user, below): **(A)** merge #96 as-is to bank the polish, then open
the v2 PR on top; or **(B)** park/close #96, carry only `scrollregion` + the layout constants into the v2 PR
and skip landing throwaway hacks. (A) keeps momentum + a clean bisect history; (B) avoids merging code that
M2 immediately deletes.

---

## Risks / decisions to flag (per CLAUDE.md "flag if reality contradicts")

1. **Vanity module path** (`charm.land/...`) — M0 must prove it resolves before anything else. *Hard gate.*
2. **No partial migration** — the LG1↔LG2 `Color` incompatibility forces a single-PR flip of `internal/tui`
   + `theme/`. Plan for one big, well-gated branch, not a trickle.
3. **bubbles surface is tiny** (composer only) — low risk; the cursor/Styles changes likely *fix* a known
   08c residual rather than add work.
4. **glamour v2 style rebuild** — the theme→`ansi.StyleConfig` mapping is re-authored against `color.Color`;
   budget it inside M1, re-validate markdown goldens in M5.
5. **Conformance is unaffected** — this is client-side rendering only; no wire/endpoint surface. The
   `scripts/run-conformance.sh self` gate stays green by construction. (TUI is the dogfood vehicle, not the
   contract — masterplan framing holds.)
6. **Don't fabricate "smoothness" wins** — per CLAUDE.md no-fabricated-numbers: any "feels faster / smoother"
   claim is unmeasured until captured head-to-head in `tools/tui-shots`.

## Testing posture (unchanged gate, re-baselined)
- Every `internal/tui/*_test.go` must stay green through M1 (translation must not change behavior).
- M2+ adds: canvas full-fill golden (no transparent cell), layer z-order golden (modal over stream),
  composer-no-dark-bar regression (the 08c residual), scroll-routing re-validation.
- M5: `tools/tui-shots/` re-capture on v2 + screenshot-diff vs `screenshots-harness/out/opencode-{dark,light}/`.
- Full review gate each round (`go build/vet`, `gofmt -l`, `golangci-lint`, `go test ./...`, `make gen`
  diff-clean, conformance `self`) + independent review subagent until clean, per CLAUDE.md git workflow.

## Out of scope
- New endpoints/features (08a/08b own those). This is rendering-infra only.
- Rewriting the theme **data** (33 JSON themes from 08c stay; only their color *typing* flips).
- Replacing chroma or the markdown content model — only their host libs' versions move.
