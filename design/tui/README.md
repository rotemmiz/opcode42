# Handoff: Opcode42 — Agent TUI Client

## Overview
Opcode42 is a terminal-user-interface (TUI) client for a coding agent — think opencode/Claude Code-in-a-terminal. It presents a streaming agent conversation (reasoning, tool calls, diffs, command output, todos, sub-agents), a right context sidebar, a bottom status bar, a persistent **tasks board**, slash-command autocomplete, `@`-mention file picking, vim-style leader keys, and a set of centered command modals (palette, model/agent/theme pickers, session list, timeline, status).

This package documents the design precisely enough to rebuild it in a real terminal renderer.

## About the Design Files
The files in this bundle are **design references built in HTML/CSS/React** — high-fidelity prototypes that show the intended look, layout, and behavior. **They are not the production codebase.** The task is to **recreate these designs in your actual TUI environment**, using its established patterns and primitives.

For a real TUI this most likely means a terminal-renderer stack such as:
- **Go + [Bubble Tea](https://github.com/charmbracelet/bubbletea) / Lipgloss / Bubbles** (what opencode uses)
- **Rust + [ratatui](https://github.com/ratatui/ratatui)**
- **TypeScript + [Ink](https://github.com/vadimdemedes/ink)** (React for CLIs)
- **Python + [Textual](https://github.com/Textualize/textual)**

Everything here maps onto real terminal concepts: monospace cells, ANSI/256/truecolor, box-drawing characters, and full-screen "alternate screen" layout. Where the HTML uses pixels, translate to **character cells** (1 cell ≈ the chosen monospace glyph). Where it uses hex colors, those are truecolor values — degrade gracefully to a 256-color palette if the target terminal can't do truecolor.

## Fidelity
**High-fidelity.** Colors, type treatment, spacing rhythm, semantic color usage, and interaction model are all final. Reproduce them faithfully. The one liberty: this is a *web* mock of a terminal, so it has smooth scrolling, sub-pixel text, and a draggable Tweaks panel that a real terminal won't have — ignore those affordances and keep the cell-grid discipline.

---

## Design Tokens

All values live in `styles.css` under `:root`. Truecolor hex below.

### Surfaces (neutral charcoal)
| Token | Hex | Use |
|---|---|---|
| `--bg` | `#15171a` | Terminal background |
| `--bg-panel` | `#1c1f23` | Collapsible tool panels, composer, autocomplete |
| `--bg-elev` | `#20242a` | Modals / popovers |
| `--bg-sel` | `#262b31` | Row hover |
| `--border` | `#2c3137` | Table/panel borders, modal outline |
| `--border-soft` | `#23272c` | Hairline dividers, sidebar edge |

### Text
| Token | Hex | Use |
|---|---|---|
| `--fg` | `#d6dade` | Primary text |
| `--fg-dim` | `#8b929a` | Secondary text, tool-call lines |
| `--fg-faint` | `#585f67` | Hints, line numbers, metadata |
| `--fg-ghost` | `#3a4047` | Placeholders, disabled, diff gutters |

### Semantic colors (the heart of the system)
| Token | Hex | Meaning |
|---|---|---|
| `--blue` | `#6fa8dc` | Agent mode chip, prompt accent bar, diff `+++` header, function names |
| `--green` | `#8cc265` | Added diff lines, success/pass, file paths, strings, `$` prompt, list numbers |
| `--red` | `#e0606e` | Removed diff lines, errors, blocked status, object props |
| `--amber` | `#d99a4e` | **Selection highlight bar**, in-progress todo `[•]`, numbers, "in progress" status, thinking line |
| `--purple` | `#b08cd4` | Section headers, markdown h3, table headers, keywords (`const`/`export`), modal section labels |
| `--cyan` | `#5fb3c4` | Types, diff `@@` hunk markers, `@`-mentions, links, "in review" status, source pills |
| `--yellow` | `#d6c370` | (reserve / rarely used) |

### Selection bar (modal & table highlight)
- Background `--sel-bg` `#d99a4e` (amber), text `--sel-fg` `#1a1207` (near-black). A full-width solid bar with dark text — this is the canonical "cursor row" treatment in every list/modal.

### Diff backgrounds (translucent over `--bg`)
- Added line bg: `rgba(140,194,101,0.13)`; inline-change highlight: `rgba(140,194,101,0.28)`
- Removed line bg: `rgba(224,96,110,0.13)`; inline-change highlight: `rgba(224,96,110,0.30)`
- Hunk `@@` line: text `--purple` on `rgba(176,140,212,0.08)`

### Typography
- **Font**: `Consolas` with fallback stack `"Cascadia Mono", "DejaVu Sans Mono", "Menlo", ui-monospace, monospace`. In a real terminal this is whatever monospace the user has; don't force a font.
- **Base size**: 14px / **line-height 1.55** (the "cell"). Compact = 13px/1.45, Cozy = 15px/1.62.
- **No ligatures** (`font-variant-ligatures: none`) — important for diff/code alignment.
- **Wordmark** (`opcode42` on splash): blocky pixel font *Silkscreen* 76px, grey→white vertical gradient (`#6b7178 → #d6dade → #ffffff`). In a terminal, render as ASCII/figlet block-letters with a grey gradient if supported, else bold.

### Spacing
- Stream gutter (`--pad`): 22px (≈ 2 cells). Compact 16 / Cozy 28.
- Block vertical gap: 18px. Panel padding: 9px 14px. Modal padding: 14px 18px.
- Status bar height: 30px (≈ 2 rows). Tasks header / collapsed dock: 33px. Sidebar width: 300px (≈ 38 cols).

---

## Screens / Views

> Layout shell (all session screens): a vertical app filling the viewport.
> `┌─ main column ────────────┬─ sidebar (300px) ─┐`
> `│  stream (scrolls)        │  context info     │`
> `│  composer (input)        │                   │`
> `│  tasks dock (collapsible)│                   │`
> `├──────────────────────────┴───────────────────┤`
> `│  status bar (full width, 30px)                │`
> Sidebar is hidden on the splash screen and toggleable (`ctrl+x b`).

### 1. Splash (first run)
- **Purpose**: empty entry state; type the first prompt.
- **Layout**: centered column, top-aligned ~7vh: wordmark → composer (max 720px) → hint row → **tasks board card** that flex-fills the remaining height and ends one *textbox-height* above the status bar.
- **Components**:
  - Wordmark `opcode42` (see Typography).
  - Composer card: `--bg-panel`, **left accent bar 2px `--blue`**, placeholder `Ask anything…  "Fix a TODO in the codebase"` in `--fg-ghost`; mode line below: `Build` (`--blue`, bold) · `Claude Opus 4.8` (`--fg`) ` Anthropic` (`--fg-faint`).
  - Hint row (right-aligned): `tab agents`  `ctrl+p commands` — keys bold `--fg-dim`, labels `--fg-faint`.
  - Tasks board (see Tasks Board below), boxed in a `--border-soft` card on `--bg-panel`.

### 2. Conversation stream (a full coding turn)
Rendered as a vertical list of **blocks**, each separated by 18px. Block types in order of a typical turn:
- **User turn**: left accent bar 2px `--blue`, 16px left pad. `@mentions` colored `--cyan`. Pasted-file pills: `--blue` bg, dark text.
- **Thinking line**: `+ Thought: 740ms` — `+ Thought:` in `--amber`, the ms in `--fg-faint`.
- **Markdown prose**: body `--fg`; h3 in `--purple` bold; inline `code` in `--amber`; links `--cyan` underlined; blockquote with 2px `--border` left rule, italic `--fg-dim`; ordered lists use green `1.`/`2.` counters (no native bullets). Inline code tokens may color keywords `--purple`, fns `--blue`, idents `--green`.
- **Divider rule**: 1px `--border-soft`, 16px margin.
- **Tool rows** (terse one-liners): a glyph in `--fg-faint` + label + path in `--fg-dim` + optional meta in `--fg-faint`. Glyph grammar:
  - `→ Read <file>` · `↳ Loaded <file> · N lines` · `* Grep "<q>" (N matches)` · `* Glob "<pat>" (N matches)`
- **Edit panel (diff)**: collapsible. Header `← Edit <file>` (caret rotates 90° when open). Optional error line in `--red`. Body is a monospace diff: `---`/`+++` headers (`--red`/`--cyan`), `@@` hunk (`--purple` on tint), context lines `--fg-dim`, added lines `+` on green tint (sign `--green`), removed `-` on red tint (sign `--red`), with optional brighter inline-change spans.
- **Write panel**: collapsible. Header `# Wrote <file>`. Body = code listing with right-aligned line numbers (`--fg-ghost`, ~2.4em col) + syntax highlighting: keywords `--purple`, fns `--blue`, types `--cyan`, strings `--green`, numbers `--amber`, comments italic `--fg-faint`, object props `--red`, punctuation `--fg-dim`.
- **Bash panel**: collapsible. Header `# <title>`. Body: command line `$ bun test` (`$` in `--green`), then output — pass lines `✓ …` in `--green`, neutral lines `--fg-faint`, failures `--red`.
- **Todos**: header `# Todos`, then rows: `[✓]` done (green check, dim text), `[•]` in-progress (amber box + amber bold text), `[ ]` pending (ghost box, faint text).
- **Sub-agent block**: left rule 2px (amber while running, else `--border`). Head: spinner (braille `⠋⠙⠹…` cycling ~220ms) or `│`, then `<Kind>` in amber/purple + `— <task>`. Meta line `N toolcalls · running…/1.2s` in `--fg-faint`. Then indented tool output lines.
- **Summary**: h3 `Done` in `--purple`; prose lines starting `–` with green idents/amber numbers; a **2-column bordered table** (`File` / `Change`, headers `--purple`, file cells `--green`, change cells `--fg-dim`); closing line `All green — ` + green `3 pass, 0 fail.`
- **Streaming cursor**: while the agent is "typing", a blinking block cursor (`--fg`, 1s steps) sits at the end of the stream.

### 3. Right sidebar
Width 300px, left border `--border-soft`. Top→bottom:
- Session title (bold `--fg`, wraps).
- **Context** block: heading bold; `<n> tokens`, `<p>% used`, `$<cost> spent` (numbers `--fg`, labels `--fg-dim`); a 4px progress bar (`--accent` fill on `--border-soft`).
- **LSP** block: `typescript · ready`.
- **Tasks** block (only while a sub-agent runs): a `●` dot (`--blue`) + `general · auditing`.
- Spacer, then footer: cwd path + `fixture:main` (`--fg-dim`) and `• Opcode42 0.4.2` (version, `--fg-faint`, name bold).

### 4. Status bar (bottom, full width, 30px)
Left segment: **mode chip** (`Build`, `--accent` bg + dark text, bold) · `·` · model name (`--fg` bold) · provider (`--fg-dim`). Right segment: token count (e.g. `34.9K`) · `·` · `ctrl+p` (bold) `commands` (faint). Separators `·` in `--fg-ghost`.

### 5. Tasks board (persistent dock)
- **Purpose**: live view of open work (from `tasks.md` / an issue board).
- **Placement**: on splash, a card directly below the text box that fills to a textbox-height above the status bar; in-session, a dock between composer and status bar. Collapsible.
- **Header (33px)**: caret (rotates when open) + `Tasks` (bold) + source pill `tasks.md` (`--cyan` on cyan tint) + `<n> open` (count in `--amber`) + right hint `ctrl+x t toggle`. Clicking the header **collapses the entire card to just this header.**
- **Body**: a scrollable table with sticky header row. Columns: `#` (`--fg-faint`, `#` glyph in ghost), `Status` (badge), `Task` (`--fg`, fills width), `Labels` (purple chips), `Owner` (`--fg-dim`). Header cells `--purple`, bottom border `--border`. Rows: hover → `--bg-sel`; selected → amber-tinted bg with a 2px amber inset bar on the first cell.
- **Status badges** (each prefixed `●`): `in progress` `--amber`, `blocked` `--red`, `in review` `--cyan`, `todo` `--fg-dim`, `done` `--green`. Rows are sorted doing → blocked → review → todo → done.

### 6–12. Command modals (centered overlays)
Shared chrome: scrim `rgba(8,9,10,0.55)`; panel `--bg-elev`, 1px `--border`, big soft shadow, ~560px wide, anchored ~16vh from top, max-height 64vh. Header row: title (bold `--fg`) + `esc` (`--fg-faint`). Optional search input row (placeholder `Search`, `--fg-ghost`). List below with **purple section headers** and the amber **selection bar** on the active row. `↑/↓` (also `j/k`, `ctrl+n/p`) move, `Enter` selects, `Esc`/scrim-click closes. Mouse hover also moves selection.

- **Command palette** (`ctrl+p`): sections `Suggested` / `Session`; each row = label + right-aligned shortcut. Actions: Switch session/model/agent, New session, themes, timeline, status, toggle sidebar/tasks/scanlines, etc.
- **Model switcher** (`ctrl+x m` or `/models`): grouped by provider (`Opcode42 Cloud`, `Anthropic`, `OpenAI`, `Google`); current model marked with leading `●`; free models tagged `Free` (right). Footer: `Connect provider ctrl+a   Favorite ctrl+f`.
- **Agent switcher** (`tab` or `ctrl+x a`): rows `build native` / `plan native` / `review subagent`; name bold + mode in `--fg-dim`; current marked `●`.
- **Theme picker** (`/themes`): flat list of theme names; current marked `●`. (Cosmetic in the mock.)
- **Session list** (`ctrl+x l` or `/sessions`): grouped by day (`Today`, `Yesterday`); row = `●`(current) + title + right-aligned time. Footer: `pin/unpin ctrl+f   delete ctrl+d   rename ctrl+r`.
- **Timeline** (`ctrl+x g`): search + list of message anchors with timestamps (jump-to-message).
- **Status**: non-list panel; lines like `typescript LSP · ready` (green), `prettier · formatting on save` (green), `No MCP servers` (dim), `2 plugins loaded` (dim).

---

## Interactions & Behavior
- **Submit prompt** (`Enter`, `Shift+Enter` = newline): on splash, switches to session view and streams the scripted turn; in-session, runs a short follow-up. Empty submit on splash runs the seeded example.
- **Streaming**: blocks reveal sequentially with a small per-kind delay (thought ~620ms, prose ~520ms, each tool row ~240ms, diff ~760ms, write ~680ms, bash ~760ms, todos ~460ms, sub-agent ~460ms, summary ~620ms). New blocks get a 0.2s `translateY(4px)→0` slide (transform only, **no opacity** — opacity fades can get stuck if rendering is paused). The stream auto-scrolls to the newest block. `ctrl+c` interrupts streaming.
- **Slash autocomplete**: typing `/` opens a menu above the composer filtered by prefix; `↑/↓` move, `Enter`/`Tab` accept. Commands mapping to a modal (e.g. `/models`) open it directly.
- **`@`-mention**: typing `@<query>` opens a file picker filtered by substring; accept inserts the path (colored `--cyan` in the turn).
- **Leader keys**: `ctrl+x` enters a leader mode (shows a one-line cheat-sheet toast), then a second key dispatches: `l` sessions, `n` new, `m` model, `a` agent, `g` timeline, `b` sidebar, `t` tasks, `h` scanlines.
- **Tasks dock**: header click toggles collapse (whole card ↔ header only); row click surfaces the issue (toast in the mock). `ctrl+x t` hides/shows the dock entirely.
- **Ephemeral toast**: bottom-center notice (`--bg-elev` + border) for confirmations; auto-dismisses ~1.9s.

## State Management
Single top-level app state (in `app.jsx`):
- `screen`: `"splash" | "session"`.
- `blocks[]`: ordered conversation blocks (user/thought/md/rule/tool/diff/write/bash/todos/subagent/summary).
- `streaming`: bool; `cancelRef` to abort.
- `input`: composer text; `ac`: `{type:'slash'|'mention', items, sel}` autocomplete state.
- `modal`: `null | 'palette'|'models'|'themes'|'sessions'|'agents'|'timeline'|'status'`.
- `mode` (agent), `model`, `provider`, `theme`, `sidebarHidden`, `crt`, `tasksOpen`, `tasksHidden`.
- `ctx`: `{tokens, pct, cost}` — grows during streaming. `subRunning`: bool (drives sidebar Tasks + sub-agent spinner).
- `leaderRef`: tracks the `ctrl+x` leader chord.
- Data fetching: in a real client, replace the scripted `EVENTS` with a live event stream from the agent backend (each event maps to one block kind). The tasks board would read `tasks.md` / GitHub issues.

## Design Tokens (quick reference)
See the **Design Tokens** section above — all are CSS custom properties in `styles.css` (`:root`). Density variants via `:root[data-density="compact"|"cozy"]`.

## Assets
- **Silkscreen** (Google Fonts) — splash wordmark only. Everything else is system monospace. No images/icons; all glyphs are Unicode (`→ ↳ * ← # $ ● ▸ ✓ • ⠋…` braille spinner, box-drawing). A real TUI can use the same Unicode set.

## Files
Design source (in this bundle):
- `Opcode42 TUI.html` — entry; loads React/Babel + the scripts below. Supports `?view=<name>` to deep-link a screen state (used by the gallery): `splash, chat, tools, output, summary, palette, models, agents, themes, sessions, timeline, status`.
- `styles.css` — **all design tokens + component styles**. The single source of truth for color/type/spacing.
- `data.js` — scripted session (`EVENTS`), slash commands, files, palette, models, agents, themes, sessions, timeline, and `TASKS`.
- `components.jsx` — block renderers + chrome: `Sidebar, StatusBar, UserTurn, Thought, Markdown, ToolRow, Panel, Diff, Write, Bash, Todos, SubAgent, Summary, TasksDock`.
- `modals.jsx` — the seven command modals + shared list-nav.
- `app.jsx` — app shell, streaming runner, keyboard handling, composer, modal routing, `?view=` seeding.
- `tweaks-panel.jsx` — web-only tweak controls (font/density/accent/scanlines); ignore for a real TUI.
- `Opcode42 Screens.html` + `design-canvas.jsx` — a gallery that embeds every screen state side-by-side for review.

To browse all screens at once, open `Opcode42 Screens.html`. To explore the live app, open `Opcode42 TUI.html` (type a prompt, try `ctrl+p`, `tab`, `/`, `@`, `ctrl+x` then a key).
