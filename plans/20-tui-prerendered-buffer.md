# Plan 20 — Pre-rendered buffer architecture: render once, scroll for free

> **Outcome.** Scrolling the TUI is a pure slice operation — zero re-rendering,
> zero JSON decoding, zero lipgloss calls. The body, footer, and sidebar are
> rendered once when content changes (in `Update`), stored as pre-rendered
> strings, and `View` just windows + assembled them. This replaces the
> plan-19 cache approach (caches on top of a full rebuild) with the correct
> architecture: render in `Update`, slice in `View`.

## Why not caches (plan 19)

Plan 19 added caches (`bodyLinesCache`, `sidebarCache`, `gitBranchCache`) that
sit on top of the existing rebuild-everything pipeline. The caches help
during idle scroll (cache hit → skip rebuild), but they have three fatal
flaws:

1. **Incomplete coverage.** The footer can't be cached — it depends on
   composer text, PTY output, status bar, todos, all of which mutate without
   bumping any version counter. The footer is rebuilt every frame.
2. **Fragile invalidation.** Every cache needs a version counter, and every
   mutation site must bump it. A missed bump = stale render. The PR #231
   review found 5 blocking stale-render bugs in the footer cache; we dropped
   it entirely.
3. **The hot path isn't covered.** `subagentFooterView` (part of the footer,
   uncached) calls `childStatus` for 52 children, each doing O(parent-msgs ×
   parent-parts) JSON decodes. This is **75% of CPU during scroll** — and
   the cache doesn't cover the footer.

## The correct architecture: render in Update, slice in View

opencode uses a **retained renderable tree** — each message/part is a
persistent object created once, mutated in place via setters, and the
terminal output is cell-diffed. Bubble Tea's `View()` returns a full-frame
string, so we can't match the retained-tree architecture directly. But we
can match the **principle**: render each thing once when it changes, and
keep the rendered output.

The key insight: **rendering belongs in `Update`, not in `View`**. When an
SSE event changes a part, we re-render that part's block immediately and
store the rendered string. When the user types in the composer, we
re-render the footer. When a tool completes, we re-compute child statuses.
`View` just assembles pre-rendered strings + windows the body buffer.

## Architecture

### Layer 1: Pre-computed derived state (computed in Update)

#### 1a. `childStatusMap map[string]string`

Currently `childStatus` is called per child per frame, each doing
O(parent-msgs × parent-parts) JSON decodes. With 52 subagents this is 75% of
CPU.

**Fix:** Compute all child statuses once when the store changes. After
every `store.Reduce()` and every direct store mutation, call
`m.recomputeChildStatuses()` which builds a `map[childID]string` by
scanning all parent task tool parts once. `subagentFooterView` and
`sidebarView` read the map — zero JSON decodes per frame.

#### 1b. `animatingCache bool`

`animating()` iterates ALL session messages × parts, JSON-decoding every
tool part's state. This runs **every frame** because `statusBarView` is
uncached.

**Fix:** Cache the `animating()` result. Compute it once in `Update` (after
any store mutation or anim tick) and store `m.animatingCache bool`.

### Layer 2: Pre-rendered strings (computed in Update)

#### 2a. `footerRendered string + footerHeight int`

The footer is rebuilt every frame. Instead, re-render the footer **when the
inputs actually change** — which is in `Update`, where we know what changed.

#### 2b. `sidebarRendered string`

Replace `sidebarCache` with a pre-rendered string computed in `Update`.

#### 2c. `bodyLines []string`

Replace `bodyLinesCache` with a pre-rendered line buffer computed in `Update`.
`sessionLayers` calls `m.frameStreamLines(m.bodyLines, bodyH)`.

### Layer 3: View is assembly only

No `sessionStreamBlocks`, no `buildFooter`, no `sidebarView`, no
`childStatus`, no `animating()`, no JSON decoding, no lipgloss rendering.
`View` is pure assembly.

### Layer 4: Canvas reuse (deferred)

Only if the base Bg fill O(w×h) is still hot after Layers 1-3.

## What forces a re-render (in Update)

| Trigger | What to re-render | Where |
|---|---|---|
| SSE event (`sseEventMsg`) | body + footer + sidebar + childStatus + animating | after `store.Reduce()` |
| Theme switch | body + footer + sidebar | `applyTheme` |
| Width change | body + footer + sidebar | `WindowSizeMsg` |
| View toggle | body + footer | `handleLeaderKey` |
| Animation tick | footer + sidebar (spinner) | tick handler |
| Composer keypress/paste | footer | `KeyPressMsg` / `PasteMsg` |
| PTY output | footer | `ptyOutputMsg` |
| Model switch | footer + sidebar | `modalSelect` / `configLoadedMsg` |
| Provider/MCP load | sidebar | `providersLoadedMsg` / `mcpLoadedMsg` |
| Todos load | footer | `todosLoadedMsg` |
| Session switch | body + footer + sidebar | `openSession` |
| **Pure scroll** | **nothing** | `View` just re-windows |

## What View does (per frame, no rendering)

1. `frameStreamLines(m.bodyLines, bodyH)` — O(bodyH) slice + join
2. `composeCanvas()` — base Bg fill + composite pre-rendered layers
3. Overlay check — O(1) predicates

Zero JSON decoding. Zero lipgloss rendering. Zero subprocess.

## Implementation steps

### Step 1: `childStatusMap` + `animatingCache` (derived state in Update)
### Step 2: Pre-rendered footer (rendered in Update)
### Step 3: Pre-rendered sidebar (rendered in Update)
### Step 4: Pre-rendered body lines (rendered in Update)
### Step 5: Remove plan-19 cache infrastructure
### Step 6 (deferred): Canvas reuse

## Acceptance

- **CPU profile after steps 1-5:** `encoding/json.Unmarshal` < 1% (was 75%),
  `lipgloss.Style.Render` < 5%, total CPU during 12s scroll < 1s (was 11s).
- **New tests:** `TestChildStatusMap_RecomputedOnStoreChange`,
  `TestView_NoRenderingOnScroll`.
- Existing golden + render tests stay green.