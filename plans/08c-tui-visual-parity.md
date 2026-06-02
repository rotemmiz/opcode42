# Plan 08c — TUI visual parity: themes, markdown, syntax, diff, motion

> **Status: SHIPPED (2026-06-02).** All tiers landed across PRs #83–#93. Tier 0 (M0 white-bg fix +
> light/dark auto-pick), Tier 1 (M1 extended palette, M2 JSON loader + 33 opencode themes = 36 total,
> M3 `tools/tui-shots` screenshot harness), Tier 2 (M4 glamour markdown, M5+M6 chroma syntax + diff
> parity, M7 rich tool/message rendering, M8 chrome pass), Tier 3 (M9 gradient-scanner spinner +
> animTick, M10 block-pixel logo shimmer, M11 toast overlay). **Deferred (intentional):** the
> `bg-pulse` framebuffer field (M10 stretch, out of scope); diff *intra-line* highlight (M6 nice-to-have,
> `// TODO(08c M6)`). **Known residual:** bubbles textarea's internal viewport pads short composer
> lines with an uncolored style we can't reach → a trailing dark bar on the composer row on *light*
> terminals only (dark is pixel-clean; the user-reported white-bg bug is fixed). See
> `internal/tui/`: theme/{theme,loader}.go, markdown.go, syntax.go, diff.go, toolrender.go, chrome.go,
> modal.go, spinner.go, logo.go, toast.go; harness at `tools/tui-shots/`.

> **Scope.** Make the Forge TUI *visually* convincing as an opencode alternative: the theme/token
> system, markdown + syntax rendering, diff fidelity, chrome (logo/footer/sidebar/dialogs), and
> motion (spinner/logo/toasts). Where 08a/08b closed **feature** gaps (endpoints, navigation, panes),
> this plan closes **aesthetic** gaps — the difference between "functionally equivalent" and "looks
> like the same product." It also fixes the **white-background bug**.
>
> **Framing.** The TUI is the masterplan's dogfood/conformance vehicle, not the primary client
> (Android is). But the explicit goal here is a *convincing* alternative, so this plan targets **full
> visual parity in cost tiers** (Tier 0 bug-fix → Tier 1 foundation → Tier 2 content → Tier 3 motion).
> Stop after any tier and the TUI still reads as a coherent, polished product; the expensive motion
> work (Tier 3) is real but isolated so it can be deferred without blocking the rest.
>
> **The honest architecture gap.** opencode's TUI is **opentui** (SolidJS over a frame-buffer
> compositor with per-cell control and live `requestRender` animation loops). Forge's TUI is
> **Bubble Tea** (a string-diff renderer). Everything static — themes, painted backgrounds, markdown,
> syntax, diff — is achievable to pixel parity. The *animations* (gradient-scanner spinner, shimmer
> wordmark, `bg-pulse` field) are achievable but cost more in Bubble Tea (a `tea.Tick` loop + manual
> per-frame string synthesis) and are quarantined into Tier 3.

## Links
- Siblings: `plans/08-client-tui.md`, `plans/08a-tui-parity-now.md`, `plans/08b-tui-parity-planned.md`.
- Forge TUI: `internal/tui/` (Go/Bubble Tea, ~5k LOC); theme in `internal/tui/theme/theme.go`.
- Reference TUI: `/Users/rotemmiz/git/opencode/packages/opencode/src/cli/cmd/tui/` (TS/opentui).
- **Visual oracle:** `/Users/rotemmiz/git/opencode/screenshots-harness/` (VHS; see §V).
- Design handoff already mirrored in Forge: `design/tui/styles.css` (the `:root` tokens `theme.go` lifts).

---

## V. The visual oracle — VHS screenshot harness (use this throughout)

This is the **ground truth** for every decision below; lean on it, don't guess. Verified runnable on
this machine: `vhs`, `ttyd`, `ffmpeg`, `opencode` (1.15.12) all on PATH (only `bun` missing, not
needed for capture). A reference set already exists at
`screenshots-harness/out/opencode-{dark,light}/` (~16 scenes: home, prompt, markdown-reasoning,
tools-diff, summary-table, write-bash-todos, slash/palette/model/theme/session/agent/timeline/status).

**Confirmed visual facts from the captured references** (`out/opencode-dark/`):
- **03-home-empty:** a **block-pixel "opencode" wordmark** with a left→right light→white **gradient/
  shimmer**; composer with a **blue left accent bar**, ghost placeholder `Ask anything... "Fix a TODO…"`,
  and a mode line **`Build · Big Pickle OpenCode Zen`** (agent · model · provider). Right-aligned hint
  `tab agents   ctrl+p commands`. Footer: `~/path:branch` left, version `1.15.12` right. **Fully
  painted near-black background**, no terminal bleed-through.
- **06/07 (markdown + tools-diff):** reasoning block, **syntax-highlighted** code (TS keywords/types
  colored), **diff with red/green line backgrounds + a line-number gutter**, bash output blocks, and a
  **right sidebar** (title, Context token counts, LSP status).

**Workflow this plan mandates for each visual item:**
1. `cd screenshots-harness && ./capture.sh opencode dark` (and `light`) → refresh `out/opencode-*/`.
2. Build the matching Forge scene; capture Forge the same way (new harness, §V.1).
3. Eyeball side-by-side (and, Tier 1+, run the pixel-diff gate, §T).

### V.1 Build a Forge screenshot harness (Tier 1 deliverable)
Port `screenshots-harness/` to `tools/tui-shots/` for `forge-tui`: a `.tape` per scene driving the
same keystrokes, output to `out/forge-{dark,light}/`, same scene numbering so frames line up 1:1 with
opencode's. Seed a deterministic fixture session (reuse the shape of `fixture-session.json`). This is
the regression harness referenced in §T — build it early; it pays for itself every subsequent item.

---

## Tier 0 — The white-background bug (ship first, ~0.5d)

**Symptom (user-reported):** "white background to all text." **Root cause (found):**
`internal/tui/model.go:1054` —

```go
func (m Model) paintsBackground() bool {
    return m.width > 0 && m.height > 0 && m.themeName != theme.Palettes()[0].Name
}
```

The default `forge-dark` theme is **deliberately excluded** from background painting (`model.go:1045`
returns `body` unpainted "→ terminal-native background"). On a **light/white terminal**, the
forge-dark palette's light-gray foregrounds (`Fg #d6dade`, `FgDim #8b929a`) render on the terminal's
white background → washed-out, near-illegible text on white. opencode never has this: its compositor
**always** fills every cell with the theme background.

**Fix (decision baked in): always paint the theme background.** A themed dark TUI must own its
background; deferring to the terminal is what breaks it. Concretely:
- Delete the default-theme exclusion; `View()` always wraps the frame in
  `lipgloss.NewStyle().Background(p.Bg).Width(m.width).Height(m.height)`.
- **Auto-pick light vs dark by terminal** at startup: `lipgloss.HasDarkBackground()` (already a
  lipgloss capability) → default to `forge-dark` on dark terminals, `forge-light` on light ones, unless
  the user pinned a theme. This mirrors opencode's `theme_mode_lock` (`kv.json`) + `dark/light` token
  resolution (`context/theme.tsx`).
- Ensure **every** child style that draws text also sets `.Background(p.Bg)` (or the appropriate
  surface token `BgPanel`/`BgElev`/`BgSel`) so no cell is left transparent — Lipgloss does **not**
  inherit a parent `Background` into joined sub-strings, which is the subtle trap. Add a `Surface(tok)`
  helper in `theme.Styles` and route all renderers through it.

**Test:** golden render of `viewSplash` + `renderSession` asserts every line is background-filled to
`m.width` (no `\x1b[0m`-terminated cell shorter than width). Add a `forge-light`-on-dark and
`forge-dark`-on-light capture to the harness to prove the auto-pick.

---

## Tier 1 — Foundation: the token & theme system (the unlock for everything else)

Forge has **3 hand-coded palettes** (`theme.go`: `Default/Light/Mono`) with a **narrow token set**.
opencode has **33 JSON themes** and a **much richer token schema** (`context/theme/*.json`,
`$schema: opencode.ai/theme.json`). Parity on themes is the foundation: markdown, syntax, and diff
rendering below all consume tokens that Forge's palette doesn't even have yet (`diffAddedBg`,
`markdownHeading`, `syntaxKeyword`, …).

### 1a. Extend `theme.Palette` to opencode's token surface
opencode's per-theme schema (verified in `context/theme/opencode.json`) carries:
`primary, secondary, accent, error, warning, success, info, text, textMuted,
background, backgroundPanel, backgroundElement, border, borderActive, borderSubtle`,
a **diff group** (`diffAdded, diffRemoved, diffContext, diffHunkHeader, diffHighlightAdded/Removed,
diffAddedBg, diffRemovedBg, diffContextBg, diffLineNumber, diffAddedLineNumberBg,
diffRemovedLineNumberBg`), and a **markdown/syntax group** (`markdownText, markdownHeading,
markdownLink, …, syntaxKeyword, syntaxString, syntaxFunction, syntaxType, syntaxComment, …`).

**Forge mapping** (extend the struct in `theme.go`; existing names mostly survive):

| opencode token | Forge `Palette` field |
|---|---|
| `background` / `backgroundPanel` / `backgroundElement` | `Bg` / `BgPanel` / `BgElev` |
| `border` / `borderActive` / `borderSubtle` | `Border` / *(new)* `BorderActive` / `BorderSoft` |
| `text` / `textMuted` | `Fg` / `FgDim` |
| `primary` | `Accent()` (currently aliased to `Blue`) |
| `secondary` / `accent` | `Blue` / `Purple` |
| `error`/`warning`/`success`/`info` | `Red`/`Amber`/`Green`/`Cyan` |
| `diff*` group | **new** `Diff` sub-struct (added/removed/context fg+bg, hunk, line-number bg) |
| `markdown*` / `syntax*` | **new** `Markdown` + `Syntax` sub-structs |

Keep the existing flat fields for back-compat; add the three sub-structs. The current 3 palettes get
sensible diff/markdown/syntax defaults derived from their existing semantic colors.

### 1b. JSON theme loader (import opencode's 33 themes verbatim)
Add `theme/loader.go`: parse the `opencode.ai/theme.json` shape — a `defs` map of color literals + a
`theme` map whose leaves are `{dark, light}` references-or-literals — into a `Palette` for the active
mode. **Copy all 33 JSON files** into `internal/tui/theme/themes/` and `//go:embed` them. The registry
(`Palettes()`) becomes: the 3 native Forge themes **plus** the embedded opencode set, so the theme
picker (`/themes`, `dialog-theme-list`) lists ~36 and **a Forge user can pick `gruvbox`, `tokyonight`,
`catppuccin`, etc., exactly as in opencode** — instant credibility, and it makes the screenshot-diff
gate (§T) trivially apply across themes.

- Honor the user's `config.theme` (opencode reads it from config; Forge should read the same key so a
  shared `opencode.json`/`AGENTS.md` config "just works" — wire-/ecosystem-compat per CLAUDE.md).
- Resolve `dark`/`light` per the Tier 0 terminal detection + `theme_mode_lock` KV (08a §H).

**Test:** load every embedded theme, assert no missing token resolves to zero-value; golden the
`opencode` theme's resolved palette against the JSON. **Conformance value:** ecosystem-compat (theme
files are a shared resource format).

---

## Tier 2 — Content rendering: markdown, syntax, diff, tools, chrome

This is where the TUI currently looks the most unfinished: `render.go` emits **plain text**. `prose()`
(`render.go:118`) is `Width(...).Render(Base.Render(text))` — no markdown. There is **no syntax
highlighting anywhere** (the diff is marker-colored lines only). The reference 06/07 screenshots show
opencode does full markdown + tree-sitter syntax + background-colored diffs.

### 2a. Markdown rendering (assistant prose)  — **highest visual ROI**
Adopt **`github.com/charmbracelet/glamour`** (the Charm markdown renderer; same family as Bubble
Tea/Lipgloss, so it composes). Replace `prose()` with a glamour render:
- Build a glamour `ansi.StyleConfig` **from the active theme's `Markdown` tokens** (headings →
  `markdownHeading`, links → `markdownLink`, code → `markdownCode`, etc.) so markdown re-themes with
  the palette — opencode's markdown colors are theme tokens, not hardcoded.
- Cover the tokens the harness scene `06-markdown-reasoning` exercises: headings, bold/italic, inline
  code, fenced code blocks, lists, **tables** (scene `08-summary-table`), block quotes, links, hr.
- Width = `contentWidth()`; cache the rendered string per (part-text, width, theme) — glamour is not
  free and the stream re-renders each frame.

### 2b. Syntax highlighting (code blocks + diff bodies)
Adopt **`github.com/alecthomas/chroma/v2`** for language-aware highlighting. opencode uses tree-sitter;
chroma is the pragmatic Go equivalent (lexers for all the common languages, ANSI formatter). Two
consumers:
- **Fenced code blocks** inside markdown — glamour can delegate to chroma, or post-process code spans.
- **Diff viewer** (`diff.go`) — highlight each hunk line's *code* under the +/- marker, then overlay
  the diff add/remove **background** (`diffAddedBg`/`diffRemovedBg`). Build a chroma style from the
  theme `Syntax` tokens so highlighting matches the active theme.
- Map the chroma token classes (`Keyword`, `String`, `Function`, `Type`, `Comment`, …) → the new
  `Syntax` palette sub-struct.

### 2c. Diff viewer visual parity (`diff.go`)
The diff *reviewer* shipped (08b §1), but it's marker-colored, not opencode-faithful. From scene
`07-tools-diff`, match: **full-row add/remove background tints** (`diffAddedBg`/`diffRemovedBg`, not
just colored signs), a **line-number gutter** with its own bg (`diffAddedLineNumberBg`/
`diffRemovedLineNumberBg`/`diffLineNumber`), **hunk headers** in `diffHunkHeader`, **intra-line
highlight** of the changed span (`diffHighlightAdded/Removed`), and syntax-highlighted code bodies
(2b). Keep the existing unified + split modes; this is a re-style of `diffLineStyle` (`diff.go:321`)
to consume the new `Diff` tokens + per-cell background fill.

### 2d. Tool & message rendering richness (`render.go`)
`toolRow()` (`render.go:130`) is a terse one-liner. opencode renders tools richly (per-tool formatting,
collapsible output, todo lists, bash output blocks — scene `09-write-bash-todos`). Targets, ordered:
- **Per-tool headers** with the tool's key arg (e.g. `Read src/x.ts`, `Bash npm test`) — opencode
  formats by tool name, not a generic `status` string.
- **Collapsible tool output** with the theme's panel background (`BgPanel`) and a fold affordance
  (ties to 08a §D `hideTools`/tool-details toggle — promote it to per-tool collapse).
- **Todo lists** (`todo-item.tsx`) — checkbox glyphs + status colors.
- **Reasoning** (`thinking()`, `render.go:123`) is truncated to one line today; opencode shows a
  foldable reasoning block. Make it expandable (collapsed one-liner ↔ full, theme `Amber`/muted).
- **User turns** already match (blue left accent bar, `render.go:109`) — keep.

### 2e. Chrome: footer, sidebar, dialogs (static styling pass)
Re-style to the references:
- **Footer / status bar** (`chrome.go:57`): the home scene shows `agent · model · provider` chips on
  the prompt mode line and `cwd:branch` + version in the footer. Match the chip grammar (mode chip
  exists as `ModeChip` in `theme.go:136`) and the dim/accent split.
- **Right sidebar** (`chrome.go:93`): scene 06 shows title + **Context token counts** + **LSP status**.
  Forge has a sidebar; align its sections/labels/spacing to opencode's.
- **Dialogs** (`modal.go`): opencode's `dialog-select.tsx` (18KB) is the workhorse — bordered,
  title, filter input, selected-row bar, scroll. Forge's `modalView` (`modal.go:442`) should match the
  border (`border.tsx`), the selection bar (already `Selection` style), and the filter affordance.
  Audit each Forge modal against its opencode dialog screenshot (15–23 scenes).

---

## Tier 3 — Motion & flourish (isolated; defer-able without blocking Tiers 0–2)

opentui animates via a live render loop; Bubble Tea needs an explicit `tea.Tick` → re-`View()` loop.
The pattern for **all** of these: a single low-frequency `animTick` (≈30–60ms) gated to only fire when
something is actually animating (avoid waking the render loop at idle — battery/CPU), each item a pure
`frame(int) string`.

### 3a. Spinner (start here — most visible, lowest cost)
opencode's `ui/spinner.ts` (12KB) is a **gradient-scanner**: a bright head sweeps a string with a
fading trail (`getScannerState`, `AdvancedGradientOptions`). Port the scanner math to Go as
`theme/spinner.go`: given the active-stream label + frame index, color each rune by distance from the
sweep head using the theme `primary`/`accent` ramp. Use it for the "thinking/streaming" indicator.

### 3b. Logo shimmer (home screen)
The home wordmark (scene 03) is a **block-pixel "opencode"** with a left→right light→white shimmer
(`component/logo.tsx` `ShimmerConfig`: `period 4600ms`, rings, sweep, halo). Forge's splash renders a
flat bold `"forge"` (`model.go:1061`). Build `theme/logo.go`: a block-pixel "forge" glyph matrix +
a per-frame brightness sweep over the active theme's text→primary ramp. The shimmer config can be a
straight numeric port. (Stretch within 3b: the `bg-pulse` framebuffer field behind the logo —
`bg-pulse-render.ts` — is the single most opentui-specific effect; treat as optional.)

### 3c. Toasts & transient notices
`ui/toast.tsx` — transient bottom-corner notices (share-URL copied, errors, "interrupted"). Forge uses
inline status lines today. Add a `toast` overlay model: a queue of `{text, kind, ttl}` rendered
bottom-right with the panel/elevated background, fading via the animTick. Ties to 08a §A (share toast)
and §I (notifications).

### 3d. Misc motion
`command-palette` open/close already feels fine static; opencode has subtle enter transitions — low
priority. Connection/loading states (`startup-loading.tsx`, `use-connected.tsx`) → a small animated
"connecting…" using 3a's spinner.

---

## Code styling, formatting & conventions (the "code styling" ask)

Two readings of "test and code styling"; cover both:

**(a) Match opencode's *rendered* styling of code/tests in the transcript** — covered by Tier 2
(markdown 2a, syntax 2b, diff 2c). A test file in a code block or a diff should highlight like
opencode's.

**(b) Keep Forge's own Go code/test style consistent** as this lands — non-negotiable per CLAUDE.md's
review gate:
- `gofmt -l` clean, `go vet`, `golangci-lint run` clean each PR.
- New theme/render packages follow the existing `internal/tui/` idiom: small files per concern
  (`theme/loader.go`, `theme/spinner.go`, `theme/logo.go`, `render/markdown.go`), table-driven
  `*_test.go` next to each (mirroring `theme/theme_test.go`, `diff_test.go`).
- Comment density matches the surrounding files (the existing `theme.go`/`render.go` are heavily
  commented with *why*; keep that).
- No new top-level deps beyond glamour + chroma (both Charm-adjacent / vetted Go libs) without noting
  it; record them in the plan if the set changes.

---

## T. Testing posture

- **Golden render tests (Go, table-driven):** each renderer (`prose`/markdown, `toolRow`, diff line,
  splash, footer) golden-tested per theme (at least `forge-dark`, `forge-light`, `opencode`,
  `gruvbox`). Assert full-width background fill (Tier 0 regression guard).
- **Theme loader tests:** every embedded JSON theme resolves all tokens (dark + light), no zero-values;
  golden one resolved palette against its JSON.
- **Spinner/logo frame tests (Tier 3):** `frame(i)` is pure and deterministic — golden a few frames.
- **Screenshot-diff gate (the visual oracle, §V):** the Forge harness (V.1) captures the same scene
  set as opencode; a CI-local step renders both and reports per-scene pixel-diff %. This is a
  *guidance* signal (layouts won't be byte-identical), but it catches regressions and quantifies
  "how close." Run it each Tier 1+ PR for the scenes that PR touches.
- **Per CLAUDE.md review gate:** `go build/vet`, `gofmt -l`, `golangci-lint run`, `go test ./...`,
  `make gen` diff-clean, conformance `self` — each round until the local review subagent is clean.

---

## Milestones & sequencing

| # | Deliverable | Tier | Est | Blocks |
|---|---|---|---|---|
| M0 | White-bg fix: always-paint + terminal light/dark auto-pick + `Surface()` helper | 0 | 0.5d | — |
| M1 | `Palette` extended to opencode token surface (diff/markdown/syntax sub-structs) | 1 | 1d | M2,M3,M4 |
| M2 | JSON theme loader + embed all 33 opencode themes + registry/picker wiring | 1 | 1.5d | — |
| M3 | Forge screenshot harness (`tools/tui-shots/`) + fixture, scene parity w/ opencode | 1 | 1d | screenshot-diff gate |
| M4 | Markdown rendering via glamour, theme-driven styles | 2 | 1.5d | — |
| M5 | Syntax highlighting via chroma (code blocks + diff bodies) | 2 | 1.5d | M4 |
| M6 | Diff viewer re-style: bg tints, line-number gutter, hunk/intra-line, syntax | 2 | 1.5d | M5 |
| M7 | Tool/message richness: per-tool headers, collapsible output, todos, foldable reasoning | 2 | 2d | — |
| M8 | Chrome pass: footer chips, sidebar (context/LSP), dialog styling audit | 2 | 1.5d | — |
| M9 | Spinner (gradient scanner) + animTick infra | 3 | 1d | — |
| M10 | Logo shimmer (block-pixel "forge" + sweep); bg-pulse optional | 3 | 1.5d | M9 |
| M11 | Toasts + connecting/loading states | 3 | 1d | M9 |

**Critical path to "convincing":** M0 → M1 → M2 → M4 → M5 → M6 (theme system + markdown + syntax +
diff). After M6 + M8 the TUI reads as opencode-class **static**. M3 should land early (it's the
regression net for everything after). Tier 3 (M9–M11) is the "slick" layer — high delight, isolatable,
shippable last or independently.

## Out of scope (here)
- New *features*/endpoints (covered by 08a/08b: shell, nav, sub-agents, PTY, stash, variant).
- Daemon-gated dialogs' *behavior* (workspaces/provider-auth/tags — 08b); their *styling* is in M8
  only if/when they exist.
- The `bg-pulse` framebuffer field is explicitly optional (M10 stretch) — it is the most opentui-
  specific effect and the least worth the Bubble Tea cost.

## Decisions baked in (flag if reality contradicts)
1. **Always paint the theme background** + auto-pick light/dark by terminal (Tier 0). The current
   "defer to terminal for the default theme" policy is the bug, not a feature.
2. **glamour + chroma** as the markdown + syntax stack (Charm-family, single-binary friendly; vs.
   tree-sitter cgo, which would break the pure-Go single-binary posture).
3. **Embed opencode's 33 theme JSONs verbatim** and read `config.theme` — ecosystem-compat, and it
   makes Forge instantly recognizable to opencode users.
