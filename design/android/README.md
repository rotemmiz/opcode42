# Handoff: Forge for Android — Conversation Stream (Direction B · "Terminal-Material")

## Overview
Forge for Android is a **touch-first mobile client** for the same coding-agent backend as the Forge TUI. It presents a streaming agent conversation — reasoning, collapsible tool calls, a syntax-highlighted diff, command output, todos, sub-agents, and a summary — plus a composer with slash/@ autocomplete, command/model/agent/session sheets, a tasks board, and a session-info sheet.

This package covers the **agreed design system** and the **chosen visual direction (B · "Terminal-Material")** for the hero screen: the **conversation stream**. It is the foundation other screens build on.

The product is **phone-primary, tablet-adaptive**, **dark-primary with a planned light-theme toggle**. The aesthetic is **"Material 3 bones + custom Forge character"**: M3 structure and components, but skinned to feel like a sibling of the charcoal terminal client — hairline borders instead of heavy elevation, tighter corners, monospace-forward content, and the TUI's semantic-color system carried over intact.

## About the Design Files
The files in this bundle are **design references built in HTML/CSS/React (via in-browser Babel)** — high-fidelity prototypes that show the intended look, layout, and behavior. **They are not production code to ship directly.**

The task is to **recreate these designs in the target Android environment** using its established patterns:
- **Jetpack Compose + Material 3 (`androidx.compose.material3`)** is the natural target — the design maps 1:1 onto M3 components (`TopAppBar`, `Card`, `BottomSheetScaffold` / `ModalBottomSheet`, `NavigationBar`, `AssistChip`, `TextField`, `FilledIconButton`).
- If a different stack is already in use (Views/XML, Flutter, React Native), translate onto its M3 equivalents.

Treat the HTML's pixel values as **dp** (1px ≈ 1dp at the 412dp design width) and the hex colors as the **M3 color scheme** (mapping table below). Don't import the HTML — rebuild it natively with real theming, real `LazyColumn` scrolling, and real sheet/drag behavior.

## Fidelity
**High-fidelity.** Colors, type, spacing, corner radii, semantic-color usage, and the interaction model are final. Reproduce them faithfully. The single liberty: this is a web mock, so it uses Babel/React and CSS transitions — ignore those mechanics and use Compose's `MaterialTheme`, `animateDpAsState`, `AnchoredDraggable`, etc.

---

## Design Tokens

Defined in `tokens.css` under `:root`. Carried from the Forge TUI's charcoal palette and aliased onto Material 3 color roles.

### Surfaces (dark scheme)
| M3 role | Hex | Use |
|---|---|---|
| `surface` | `#15171a` | App background / scaffold |
| `surfaceContainerLowest` | `#101316` | Inset code/diff body backgrounds |
| `surfaceContainerLow` | `#181b1f` | — |
| `surfaceContainer` | `#1c1f23` | Cards, composer, tool-row group |
| `surfaceContainerHigh` | `#20242a` | Bottom sheets, menus, expanded todo sheet |
| `surfaceContainerHighest` | `#262b31` | Hover / pressed / drag |
| `outlineVariant` | `#2c3137` | Card borders, dividers on emphasized cards |
| `hairline` (outlineVariant dimmed) | `#23272c` | Most dividers, group borders, app-bar underline |
| `outline` | `#3a4047` | Placeholders, disabled, diff gutter sign |

### Text / on-colors
| M3 role | Hex | Use |
|---|---|---|
| `onSurface` | `#d6dade` | Primary text |
| `onSurfaceVariant` | `#8b929a` | Secondary text, tool-row paths, meta |
| `onSurface` (faint) | `#585f67` | Hints, line numbers, counts, metadata |
| `onSurface` (ghost) | `#3a4047` | Placeholder text, empty checkboxes |

### Semantic colors (meanings preserved from the TUI — do not repurpose)
| M3 role | Hex | Meaning |
|---|---|---|
| `primary` | `#6fa8dc` (blue) | Agent mode, prompt accent bar, send button, function names |
| `tertiary` / success | `#8cc265` (green) | Added diff lines, pass/success, file paths, strings |
| `error` | `#e0606e` (red) | Removed diff lines, errors, blocked status |
| `secondary` / active | `#d99a4e` (amber) | **Selection / active state**, in-progress, numbers, thinking line |
| (custom) header | `#b08cd4` (purple) | Section headers, h3, table headers, keywords, hunk markers |
| (custom) link/type | `#5fb3c4` (cyan) | @-mentions, types, in-review status, source pills, links |
| `onPrimary` | `#0a1722` | Text/icon on blue fills |
| `onSecondary` | `#1a1207` | Text on amber fills |

> `secondaryContainer` = `rgba(217,154,78,0.16)` (amber tint) — used for the active diff-card header.
> `primaryContainer` = `#243648` — used for tonal icon backings.

### Diff tints (translucent over `surface`)
- Added line bg `rgba(140,194,101,0.13)`; inline-change highlight `rgba(140,194,101,0.28)`
- Removed line bg `rgba(224,96,110,0.13)`; inline-change highlight `rgba(224,96,110,0.30)`
- Hunk `@@` line: text purple on `rgba(176,140,212,0.10)`

### Typography
- **UI sans:** Roboto. **Code / diffs / tool output / paths / status / numbers:** Roboto Mono.
- Scale (sp): wordmark 28/700 mono · titleLarge 22 · titleMedium 16/500 · bodyLarge 15 · bodyMedium 14 · label 13/500 · **code 13/1.5 mono** · kicker 11/700 mono, uppercase, +1 letter-spacing.
- No ligatures in code/diffs (alignment).

### Shape (Forge character — tighter than stock M3)
| Token | Radius | Use |
|---|---|---|
| `r-xs` | 4dp | Code/diff blocks, mode chip, status pills, composer send |
| `r-sm` | 8dp | Tool-row group card, diff card, composer field |
| `r-md` | 12dp | System-board cards |
| `r-lg` | 16dp | Bottom sheets (top corners) |
| `r-full` | pill | Chips, FAB, circular icon buttons |

### Spacing & metrics
- 4dp grid. Stream gutter 14dp; block gap 16dp. **All hit targets ≥ 48dp.**
- App bar 52dp tall (+ subtitle). Status strip 32dp. Composer field 48dp. Todo-sheet peek 50dp.
- Device design size: **412 × 892 dp** (compact phone). Status-bar inset ~40dp top, gesture-nav inset ~24dp bottom — respect real insets.

---

## Screen: Conversation Stream

A vertical `Scaffold`: pinned **top app bar** → scrolling **stream** (`LazyColumn`) → **todos bottom sheet** (peek docked above composer) → **status strip** → **composer**. The status strip + composer are a fixed bottom region (~100dp); the bottom sheet anchors directly above it.

### 1. Top app bar (pinned, 52dp + underline)
- Leading: 42dp back icon button (`onSurface`).
- Two-line title block: line 1 `Add retry + backoff to http client` — 15sp/500 `onSurface`, single line, ellipsized. line 2 `~/git/forge · fixture:main` — 11.5sp Roboto Mono, faint (`#585f67`).
- Trailing: `info` (session-info sheet) + `more` (overflow) icon buttons, 20dp icons, `onSurfaceVariant`.
- Bottom: 1px `hairline` divider. Background `surface`.

> This two-line title is the TUI's right-context-sidebar collapsed into an app-bar subtitle. The `info` button opens the **session-info bottom sheet** (tokens/cost/LSP), per the broader spec.

### 2. Stream (scrolls; gutter 14dp, block gap 16dp, bottom pad 64dp so content clears the sheet peek)
Blocks, in order of a typical turn:

- **User turn** — a **2dp `primary` (blue) left accent bar**, 13dp left pad (carried verbatim from the TUI). Body 14.5sp `onSurface`. `@`-mentions rendered `cyan` in Roboto Mono (`@src/http.ts`). No bubble.
- **Thinking line** — Roboto Mono 13sp: `+ Thought:` in `amber`, the duration (`740ms`) in faint.
- **Markdown prose** —
  - Section header: kicker style (11sp/700 mono, uppercase, +1 tracking) in `purple` (`ADDING RETRY WITH BACKOFF`).
  - Body 14.5sp `onSurface`, line-height 1.55. Inline `code` in Roboto Mono 13sp `amber`. Links `cyan` underlined.
  - Ordered list: green bold mono counters (`1.` `2.` `3.`), no native bullets; item text 14sp.
- **Tool-row group** — a single card: `surfaceContainer`, 1px `hairline`, 8dp radius. Each row 44dp min, divided by `hairline`, Roboto Mono 13sp:
  - glyph (faint, 1.1em col) + label (`onSurface`) + path (`onSurfaceVariant`, ellipsized) + right-aligned meta (faint).
  - Glyph grammar: `→ Read src/http.ts` · `↳ Loaded src/http.ts · 64 lines` · `* Grep "fetch(" · 2` · `* Glob "src/**/*.ts" · 5`.
- **Diff card (collapsible)** — `surfaceContainer`, 1px `outlineVariant`, 8dp radius.
  - **Header** (46dp): the **active state** — background `secondaryContainer` (amber tint) with a **2dp amber inset-start bar** (`box-shadow: inset 2px 0 0 amber`). Caret (`chevdown` when open) + `Edit ` + filename in `green` (mono 13sp) + right `+14 −1` (green / red).
  - **Body**: 1px `hairline` top divider; inset, Roboto Mono 12sp / 1.65, horizontal scroll. Real unified diff (see `forge-bits.jsx → DIFF_LINES`): `---`/`+++` headers (`red`/`cyan`), `@@` hunks (`purple` on tint), context lines `onSurfaceVariant`, added lines `+` on green tint (sign `green`), removed `-` on red tint (sign `red`), with brighter **inline-change spans** for the changed token (e.g. `sleep`, `withRetry(...)`). Gutter sign is one char wide.

  The full diff content is the `src/http.ts` retry edit — porting it verbatim:
  ```diff
  --- src/http.ts
  +++ src/http.ts
  @@ -1,6 +1,7 @@
   // Minimal HTTP client over fetch.
   import { Logger } from "./log"
  +import { sleep } from "./util"
  
   export interface ReqOpts {
     readonly url: string
  @@ -18,9 +19,24 @@
   export async function request(o: ReqOpts) {
  -  return fetch(o.url, { method: o.method })
  +  return withRetry(() => fetch(o.url, { method: o.method }))
   }
  +
  +const RETRIABLE = new Set([502, 503, 504])
  +
  +async function withRetry(fn, max = 3) {
  +  for (let attempt = 1; ; attempt++) {
  +    const res = await fn()
  +    if (res.ok || !RETRIABLE.has(res.status)) return res
  +    if (attempt >= max) return res
  +    const backoff = 2 ** attempt * 50 + Math.random() * 50
  +    await sleep(backoff)
  +  }
  +}
  ```

> Other block kinds from the TUI (Write/code listing, Bash output, sub-agent, summary table) follow the same card/idiom rules and should be implemented alongside; their detailed specs live in the TUI handoff (`data.js` event kinds). They are out of scope for this B-direction reference, which focuses on the stream skeleton + diff.

### 3. Todos bottom sheet (slide-up; `TodoSheet` in `dir-b.jsx`)
The TUI's collapsible tasks/todos dock, translated to a **standard M3 bottom sheet** that anchors above the composer.
- **Peek (50dp):** `surfaceContainerHigh`, 16dp top corners, 1px `hairline` top, shadow `0 -10dp 34dp rgba(0,0,0,.45)`. Contents: a 32×4dp drag-handle (centered, faint), then a row: `tasks` icon (purple) + `Todos` (14sp/500) + `tasks.md` source pill (cyan text on `rgba(95,179,196,0.12)`, 4dp radius, mono 11.5sp) + right summary `**1** active · 2 done` (amber count) + caret.
- **Expanded (≈308dp):** a **scrim** `rgba(8,9,10,0.5)` covers the stream (from below the app bar to the sheet top); the sheet grows upward. List of todo rows (46dp min, `hairline` between):
  - **done** — 20dp `green` filled circle with a white check; text `onSurfaceVariant`.
  - **in progress** — 20dp circle with 2dp amber inset ring + a 7dp amber center dot; text `amber` 600 + a small braille **spinner** at the row end.
  - **pending** — 20dp 5dp-radius square, 2dp `outline` border; text `onSurface`.
  - Footer: `Open tasks board ›` link in `cyan` (navigates to the Tasks tab).
- **Gesture:** drag the handle to resize (snaps to peek/expanded at the midpoint); **tap** the handle toggles peek↔expanded; tap the scrim collapses. (In Compose: `ModalBottomSheet` with two anchors, or `BottomSheetScaffold` with `sheetPeekHeight = 50.dp`.)

### 4. Status strip (32dp, above composer)
The TUI status bar, compacted. `surface`, 1px `hairline` top. Roboto Mono 12sp, left→right:
- **mode chip** `Build` — `primary` (blue) fill, `onPrimary` text, 700, 4dp radius.
- model `Opus 4.8` (`onSurface`) · `·` (ghost) · provider `Anthropic` (`onSurfaceVariant`) · right-aligned token count `34.9K` (faint).

### 5. Composer (fixed bottom)
- A row: a field + a send button. Field: `surfaceContainer`, 1px `hairline`, **2dp `primary` left border** (the TUI prompt accent), 6dp radius, 48dp min. Placeholder `Ask anything…  /  @` in ghost, Roboto Mono 13.5sp. A 42dp `add` (attach) icon button inside on the right.
- Send: 40dp blue (`primary`) square, 6dp radius, paper-plane icon in `onPrimary`.

---

## Interactions & Behavior
- **Streaming:** blocks reveal sequentially (per-kind delay; see TUI spec — thought ~620ms, prose ~520ms, each tool row ~240ms, diff ~760ms, todos ~460ms). New blocks slide in `translateY(4dp)→0` over 0.2s (**transform only, no opacity** — opacity fades can stick if rendering pauses). Auto-scroll to newest. A blinking block cursor sits at the end while typing.
- **Diff/tool cards:** tap header to expand/collapse (caret rotates). Diff body scrolls horizontally.
- **Todos sheet:** drag handle (snap) + tap-to-toggle + tap-scrim-to-collapse, as above.
- **Composer:** typing `/` opens the **inline command panel** above the field (keyboard stays up while filtering) — built-in client actions merged ahead of daemon/MCP/skill commands; typing `@` opens the **@-mention file sheet** (inserts a cyan mention). Send streams the turn.
- **App bar:** `info` → session-info sheet (tokens / % used / cost / LSP); `more` → overflow (rename, fork, compact, share, theme, …).
- **Light theme:** planned toggle — invert the surface ramp and on-colors to a light M3 scheme while keeping the six semantic hues (tune them for AA contrast on light surfaces).
- **Tablet:** widen the stream to a max content width (~720dp) centered; the todos/tasks board and session-info can become a persistent side pane instead of sheets.

## State Management
Per the TUI app model (`app.jsx`), reusable here:
- `blocks[]` — ordered conversation blocks (`user | thought | md | rule | tool | diff | write | bash | todos | subagent | summary`). Each maps to one renderer.
- `streaming: boolean` + a cancel handle. `input: string`. `autocomplete: { type: 'slash' | 'mention', items, selected } | null`.
- `sheet: null | 'commands' | 'mention' | 'models' | 'agents' | 'sessions' | 'sessionInfo'`.
- `mode` (agent), `model`, `provider`, `theme` (`dark | light`).
- `todosSheet: 'peek' | 'expanded'` (or a draggable anchor value).
- `ctx: { tokens, pct, cost }`, `subRunning: boolean` (drives spinners).
- **Data:** replace the scripted events with a live event stream from the agent backend (each event → one block). Tasks/todos read from `tasks.md` / the issue source.

## Assets
No raster assets. All glyphs are Unicode/vector: tool glyphs `→ ↳ *`, checkbox/diff signs `✓ + −`, braille spinner `⠋⠙⠹…`, and the line icons in `icons.jsx` (back, info, more, send, add, search, chevron, file, diff, tasks, check, spark). Fonts: **Roboto** + **Roboto Mono** (Google Fonts) — both ship with Android; use the system families. Use real Material Symbols in the app where an equivalent exists.

## Files
- `Forge Android (B) — reference.html` — open this to see the **System board** + the **Direction-B phone** rendered together. (Type is interactive: drag/tap the todo sheet, scroll the diff.)
- `tokens.css` — the design tokens (palette + M3 role aliases + shape scale). Source of truth for color/type/shape.
- `system-board.jsx` — the system proposal board (mapping, type scale, shape/density, idiom translation, component inventory).
- `dir-b.jsx` — the Direction-B conversation stream + `TodoSheet`. The screen to rebuild.
- `forge-bits.jsx` — shared primitives: syntax/diff colors, `DiffRow`, `Badge`, `Spinner`, and `DIFF_LINES` (the real diff content).
- `icons.jsx` — the line-icon set.
- `android-frame.jsx` — the device bezel used for the mock (not part of the app — ignore when rebuilding).
