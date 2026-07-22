# Plan 19 — TUI incremental rendering: only rebuild what changed

> **Outcome.** Scrolling the TUI drops from ~90% CPU to <10% on an idle
> session. The fix is not "add more caches" — it's changing the render
> pipeline to only rebuild content when content actually changes. During a
> pure scroll, the only state mutation is `m.scroll.Offset` (one int); the
> entire body, footer, and sidebar are byte-identical to the previous
> frame. The pipeline should recognize this and skip the rebuild.

## Root cause

A 12-second CPU profile with scroll-wheel events injected every 50ms
(140×40 terminal, `--no-anim`, live daemon, post-lexer-cache fix):

| Hot path | % CPU | Cause |
|---|---|---|
| `encoding/json.Unmarshal` | **52%** | `parseToolState` + `childStatus` + `taskChildStatusFromParent` re-decode `p.State` (immutable JSON) every frame |
| `composeCanvas` base fill | **40%** | allocates + zero-fills a 5600-cell canvas grid every frame (`memclr` 25% + `mallocgc` 15%) |
| `lipgloss.Style.Render` | **15%** | ~500-800 Render calls per frame, ~1500 `ansi.stringWidth` calls |
| `gitBranch` subprocess | (syscall) | `exec.Command("git", ...)` spawns a subprocess every frame in `sidebarView` |

**Bubble Tea v2 does NOT coalesce `View()` calls** — each `MouseWheelMsg`
triggers `Update` → `p.render(model)` → `model.View()` → `composeCanvas()`.
The terminal output is already incremental (ultraviolet does cell-level
diffing + scroll detection), but the Go-side `View()` rebuild runs at the
wheel-event rate (20-200/s).

## What actually changes during scroll

During a pure `MouseWheelMsg` with no concurrent SSE, no theme switch, no
width change, no view toggle, and `animating() == false`:

- `m.scroll.Offset` is the **only** mutation (one int)
- `m.store` is untouched
- `m.view` is untouched
- `m.styles`/`m.themeName` untouched
- `m.width`/`m.height`/`m.streamWidth` untouched
- `m.animFrame` is NOT incremented (the tick self-stops when `animating()`
  returns false — no `animTickMsg` is even scheduled)

**Every byte of `sessionStreamBlocks(sid)` output is identical to the
previous frame.** The footer and sidebar are also byte-identical. Yet the
current pipeline fully rebuilds all of it.

## What opencode does differently

opencode uses a **persistent renderable tree** (SolidJS + native Zig
opentui):
- Each message/part is a persistent native object created once, mutated in
  place via setters — never rebuilt
- **Viewport culling**: only visible messages paint cells per frame;
  off-screen messages skip their paint hooks entirely
- **Scroll = translation**: `content.translateY = -position` — just shifts
  where the content is drawn, no re-rendering of message content
- **Cell-level terminal diffing**: the native Zig renderer diffs the new
  frame buffer vs the last, emitting ANSI only for changed cells

## What Bubble Tea v2 already does

- **`viewEquals` short-circuit**: if `View.Content` is byte-identical to
  last frame, `flush()` returns immediately (no rasterization, no terminal
  write). But during scroll, the content IS different (different lines
  visible), so this doesn't help.
- **Ultraviolet cell diffing**: per-line dirty tracking, per-cell
  comparison, hashmap-based scroll detection (`ScrollUp`/`DeleteLine`/
  `InsertLine`), run-length encoding (ECH/REP). The terminal output is
  already efficient.
- **Ticker coalescing**: multiple messages between ticks (16.7ms at 60fps)
  collapse into one terminal write. But `View()` runs per message.

The bottleneck is **not the terminal output** — it's the Go-side `View()`
rebuild that runs on every wheel event.

---

## Architecture: dirty flag + cached line slice

### Step 1 — Store version counter (the "content changed" signal)

Add a `version int` to the `store` struct, incremented in `Reduce()` (the
only mutation point — every SSE event goes through `store.Reduce`):

```go
// store.go
type store struct {
    version int  // incremented on every Reduce
    // ... existing fields
}

func (s store) Reduce(ev SSEEvent) store {
    // ... existing switch
    s.version++
    return s
}
```

Also add a `viewVersion int` on the Model, incremented on every view-state
toggle (`handleLeaderKey`), theme switch (`applyTheme`), and width change
(`WindowSizeMsg`). These are the only content-affecting mutations outside
the store.

### Step 2 — Cached `bodyLines []string` (eliminate the rebuild)

Add to Model:
```go
type bodyLinesKey struct {
    storeVersion  int
    sessionID     string
    viewVersion   int
    themeName     string
    streamWidth   int
    animFrame     int  // only included when animating()
}
type bodyLinesCacheMap map[bodyLinesKey][]string

bodyLinesCache bodyLinesCacheMap
```

In `sessionLayers`, replace the current body-building path:
```go
// Before (every frame):
blocks := m.sessionStreamBlocks(sid)                    // O(messages × parts)
body := header + "\n\n" + strings.Join(blocks, "\n\n")  // O(total body)
lines := strings.Split(body, "\n")                      // O(total body) — redundant!
windowed := m.scroll.Window(lines, bodyH)               // O(bodyH) — cheap
stream := strings.Join(windowed, "\n")                  // O(bodyH) — cheap

// After (pure scroll — cache hit):
bodyLines := m.cachedBodyLines(sid)                     // O(1) map lookup
windowed := m.scroll.Window(bodyLines, bodyH)           // O(bodyH) — cheap slice
stream := strings.Join(windowed, "\n")                  // O(bodyH) — cheap
```

`cachedBodyLines(sid)` checks the cache key. On hit (pure scroll), returns
the cached `[]string`. On miss (content changed), calls
`sessionStreamBlocks`, splits each block into lines, concatenates with the
blank separators, caches the result, and returns it.

**What this eliminates on a cache hit:**
- The `sessionStreamBlocks` iteration over ALL messages and ALL parts
- All `json.Unmarshal` calls (parseToolState, childStatus, etc.)
- All `lipgloss.Style.Render` calls for message blocks
- The `strings.Join(blocks, "\n\n")`
- The `strings.Split(body, "\n")`

**What remains:**
- `scrollregion.Window(bodyLines, bodyH)` — a slice operation, O(bodyH)
- `strings.Join(windowed, "\n")` — O(bodyH)
- The canvas compositing (step 3 addresses this)

### Step 3 — Cached footer + sidebar strings

The footer and sidebar don't read `m.scroll.Offset`. Cache their rendered
strings with the same content-version key:

```go
type footerCacheKey struct {
    storeVersion int
    viewVersion  int
    themeName    string
    width        int
    animFrame    int  // only when animating()
}
type footerCacheMap map[footerCacheKey]footerCacheEntry

type footerCacheEntry struct {
    string string
    height int
}

footerCache  footerCacheMap
sidebarCache footerCacheMap  // same shape
```

On a cache hit (pure scroll), return the cached string. This eliminates:
- The `git rev-parse` subprocess (chrome.go:117)
- The `childStatus`/`taskChildStatusFromParent` JSON decode loops
- The ~500 `lipgloss.Style.Render` calls in the footer + sidebar
- The 2-3× footer rebuild (sessionLayers + acPopupY + scrollBodyHeight
  all call `buildFooter` — they now all hit the same cache)

### Step 4 — `gitBranch` TTL cache (the per-frame subprocess)

Regardless of the architecture above, `exec.Command("git", ...)` every
frame is indefensible. Cache it with a `sync.Map` + 5s TTL per directory:

```go
var gitBranchCache sync.Map // map[string]gitBranchEntry

type gitBranchEntry struct {
    branch  string
    fetched time.Time
}

const gitBranchTTL = 5 * time.Second
```

This is a subset of step 3 (the sidebar cache would skip `sidebarView`
entirely during scroll), but worth doing independently as a safety net.

### Step 5 (deferred) — Reuse the canvas across scroll frames

`composeCanvas` allocates a fresh 5600-cell canvas + fills it cell-by-cell
every frame. Instead, store a `*lipgloss.Canvas` on the Model. On a
content-unchanged scroll, reuse the cached canvas with only the stream
layer re-drawn. The sidebar/footer/base-bg cells stay untouched.

This eliminates `memclrNoHeapPointers` (25%) and `mallocgc` (15%) — the
remaining hot path after steps 1-4.

**Decision: defer step 5.** Steps 1-4 address the dominant costs (JSON
decodes, lipgloss renders, subprocess, join/split). The canvas allocation
is a lipgloss/ultraviolet-level cost that's harder to fix without touching
library internals. Re-evaluate after measuring post-step-1-4.

---

## What forces a cache miss (content actually changed)

| Trigger | Where | Sets dirty? |
|---|---|---|
| SSE event (`sseEventMsg`) | `store.Reduce()` | YES — `storeVersion++` |
| Theme switch | `applyTheme` | YES — `viewVersion++` |
| Width change | `WindowSizeMsg` | YES — `viewVersion++` |
| View toggle (ctrl+x v/r/o/b/t/i) | `handleLeaderKey` | YES — `viewVersion++` |
| Animation tick (running tool / streaming) | `animTickMsg` | YES — `animFrame` in key (only when `animating()`) |
| **Pure scroll** (MouseWheelMsg / scroll keys) | `m.scroll.Back/Forward` | **NO** — only `Offset` changes |

## What this does NOT do

- **Not a cache on top of inefficiency.** The existing per-block caches
  (`mdBlockCache`, `diffCache`, `lexerCache`) remain — they make cache
  misses cheap. The new `bodyLinesCache` sits one level up: it avoids
  entering the per-block path at all when only the window moved.
- **Not incremental rendering at the View() contract.** Bubble Tea v2's
  `View()` is full-frame string; we can't change that without forking. The
  dirty flag + cached strings makes `View()` cheap to build, and
  ultraviolet's existing cell diff handles the terminal output.
- **Not viewport culling.** opencode culls off-screen messages at paint
  time. Opcode42's `sessionStreamBlocks` renders all messages then windows
  the result. The `bodyLinesCache` makes this free on a cache hit (the
  pre-split lines are reused). On a miss, all messages render — but misses
  only happen on content change, not scroll.

## Sequencing

```
Step 1 (store version)     ──┐
Step 2 (bodyLines cache)    ──┼── one PR (the dirty-flag architecture)
Step 3 (footer+sidebar)    ──┤
Step 4 (gitBranch TTL)     ──┘
Step 5 (canvas reuse)      ── deferred (follow-up if post-1-4 profile is still hot)
```

**PR 1 (steps 1-4):** ~250 lines. The dirty-flag architecture + cached
strings + git-branch TTL. Expected: 90% → <10% CPU during idle scroll.

**PR 2 (step 5, if needed):** canvas reuse. Expected: further reduction
if the canvas allocation is still hot.

## Acceptance

- **CPU profile after PR 1:** `encoding/json.Unmarshal` < 5% (was 52%),
  `lipgloss.Style.Render` < 5% (was 15%), `exec.Command` absent from
  profile. Total CPU samples during 12s scroll < 2s (was 12s).
- **New test: `TestBodyLines_CachedOnScroll`** — seed a session, call
  `cachedBodyLines` once, change only `m.scroll.Offset`, call again,
  assert the same `[]string` is returned (cache hit, no rebuild).
- **New test: `TestBodyLines_InvalidatedOnStoreChange`** — seed a session,
  call `cachedBodyLines`, dispatch an `sseEventMsg` (incrementing
  `storeVersion`), call again, assert a new `[]string` is built (cache
  miss).
- **New test: `TestFooterCache_HitOnScroll`** — render footer, scroll,
  assert the cached string is returned without re-rendering.
- **New test: `TestGitBranch_CachedWithinTTL`** — call `gitBranch` twice
  within 5s, assert `exec.Command` runs once.
- Existing golden + render tests stay green (no behavioral changes).

## Effort

| Step | Est | Notes |
|---|---|---|
| 1 — store version counter | 0.1d | `version int` in store, `++` in Reduce |
| 2 — bodyLines cache | 0.5d | New cache + `cachedBodyLines` + `sessionLayers` change + tests |
| 3 — footer + sidebar cache | 0.25d | Cache strings + height, fix 2-3× callers |
| 4 — gitBranch TTL | 0.1d | `sync.Map` + TTL, 1 function |
| 5 — canvas reuse | deferred | lipgloss/ultraviolet level |
| **PR 1 (1-4)** | **~0.75d** | |

## Risks / decisions

1. **Cache key correctness.** The key is `(storeVersion, sessionID,
   viewVersion, themeName, streamWidth, animFrame-if-animating)`. Every
   content-affecting mutation increments one of these. A missed increment
   = stale render. Mitigated by: `storeVersion` is incremented in the
   single reducer (can't miss an SSE event); `viewVersion` is incremented
   in every toggle handler.

2. **`animFrame` in the key.** When `animating()` is true (running tool /
   streaming reasoning), `animFrame` is part of the key → cache misses
   every tick. This is correct — the running tool's spinner changes every
   frame. The cache helps during idle scroll, which is the hot path.

3. **Memory.** `bodyLinesCache` holds one `[]string` (the body's lines) —
   for a 1000-line transcript that's ~100KB. The footer/sidebar caches hold
   one string each. All are plain maps (not LRU), bounded by the working
   set. On session switch, the old `sessionID` key becomes unreachable and
   is GC'd.

4. **The existing per-block caches remain.** `mdBlockCache`, `diffCache`,
   and `lexerCache` are still valuable — they make cache misses (content
   change) cheap. The `bodyLinesCache` is the layer above: it avoids
   entering the per-block path at all when only the window moved.

5. **No behavioral changes.** The rendered output is identical; we just
   skip rebuilding it when it hasn't changed. Goldens stay green.

## Validation methodology

- Profile before and after with `--scroll-profile` (inject wheel events
  every 50ms, 12s, 140×40, `--no-anim`, live daemon).
- Compare `go tool pprof -top -cum` — acceptance criteria are specific
  percentage drops.
- Full local gate: `go build`, `go vet`, `gofmt -l`, `golangci-lint run`,
  `go test ./...`, `make gen`, `scripts/run-conformance.sh self`.
- Review subagent, re-spin until clean, CI green, merge.