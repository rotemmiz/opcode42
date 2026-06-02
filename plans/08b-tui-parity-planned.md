# Plan 08b — TUI feature parity: items that need a plan

> **Scope.** The Forge-TUI-vs-opencode-TUI gaps that are **not** drop-in: each introduces a new
> subsystem, depends on the Forge daemon (Phase B), or encodes opencode-specific semantics that must
> be understood before building. The cheap, drop-in gaps are in **plan 08a**.
>
> **Framing.** Same as 08a: the TUI is the **dogfood / conformance vehicle**, not the primary client.
> That changes the calculus sharply here — several of these (workspaces, provider auth, plugin host)
> are large and **belong to the daemon or to Android (the primary client) first**. For each item
> this plan states *whether the TUI is even the right place to build it*, not just how.
>
> **Status of each is a recommendation, not a commitment.** Build order should be decided per-item
> against the daemon's progress.

> **Implementation status (2026-06).** The five TUI-appropriate items shipped (one PR each):
> **§9 sub-agent nav** (#78), **§1 diff viewer** (#79), **§2 PTY pane** (#80), and **§6 stash + §7
> variant** (#81) — all in `internal/tui/`. The remaining four (**§3 workspaces, §4 provider/OAuth,
> §5 plugin host, §8 tags**) stay parked: they are daemon-gated or architectural and, per this plan's
> bottom line, should not be built TUI-first. PTY adopted `github.com/hinshun/vt10x` (over the
> untagged `charmbracelet/x/vt`); diff/PTY view-state persists in the 08a KV.

## Links
- Parent: `plans/08-client-tui.md` (T11 PTY is here). Sibling: `plans/08a-tui-parity-now.md`.
- Related daemon plans: `plans/05-plugin-host.md` (plugin runtime), `plans/01-daemon-core.md`,
  `plans/13-remote-ops.md`.
- Reference TUI: `/Users/rotemmiz/git/opencode/packages/opencode/src/cli/cmd/tui/`.
- Frozen contract: `/Users/rotemmiz/git/opencode/packages/sdk/openapi.json`.

## Dependency / gating overview
| Item | New subsystem | Daemon-gated? | Right home | Rough size |
|---|---|---|---|---|
| 1. Diff viewer | ANSI/scroll/file-tree pane | No (`GET /session/{id}/diff` exists) | TUI (+ Android) | L |
| 2. PTY pane (T11) | Go terminal emulator | No (`/pty` exists; Android has it) | TUI | L |
| 3. Workspace mgmt | worktree orchestration | **Yes** (`/experimental/workspace`) | Daemon, then clients | XL |
| 4. Provider connect / OAuth | auth/credential flow | **Yes** (`/provider/{id}/oauth`, `/auth/{id}`) | Daemon, then clients | L–XL |
| 5. TUI plugin host | client plugin runtime + slots | Partly (`plans/05`) | TUI (mirrors plan 05) | XL |
| 6. Stash (prompt + git) | draft store / VCS | Partly | TUI | M |
| 7. Variant | model-variant semantics | **Yes** (provider/model data) | Daemon, then clients | M |
| 8. Tag | session metadata | **Yes** (session schema) | Daemon, then clients | M |
| 9. Sub-agent UX | child-session navigation | No (`GET /session/{id}/children`) | TUI (+ Android has cards) | M |

L≈3–5d · XL≈2wk+. Sizes assume the daemon endpoint already exists; daemon-gated items add the
daemon's own effort on top.

---

## 1. Diff viewer  ✅ SHIPPED (#79)
**What opencode does.** A full-screen diff reviewer: `feature-plugins/system/diff-viewer.tsx` +
`diff-viewer-file-tree.tsx` + `diff-viewer-ui.tsx`. It fetches patches via
`props.api.client.session.diff(...)` (**`GET /session/{id}/diff`**, already in Forge's SDK) and
working-tree diffs via `client.vcs.diff(...)`. Features (~30 `diff_*` keybinds in `config/keybind.ts`):
a **file tree** pane (`buildFileTree`/`flattenFileTree`) you can toggle (`diff_toggle_file_tree`),
focus-switch between patches and files (`diff_switch_focus`), **unified vs split** view
(`diff_toggle_view`, `tui_config.diff_style` = `stacked|unified`), per-file collapse/expand
(`diff_collapse`/`expand`/`expand_all`), next/prev file (`diff_next_file`/`previous_file`),
single-patch mode (`diff_single_patch`, persisted in KV `diff_viewer_single_patch`), source switch
(`diff_switch_source` — session patch vs VCS), word-wrap (`app_toggle_diffwrap`), and scroll
acceleration (`getScrollAcceleration`). Split layout math: `patchPaneWidth = width − (fileTree?33:0)
− 4`, file tree fixed 33 cols, `MIN_SPLIT_WIDTH` gate.

**Why it needs a plan.** It's a self-contained sub-application: a scrollable, foldable, two-pane,
syntax-aware diff renderer with its own keymap and persisted view state. Forge's Android client has a
diff *card* (`UnifiedDiffView`, C4) but nothing like the file-tree-plus-split reviewer.

**Proposed Forge approach.**
- New `internal/tui/diff/` package: `model.go` (focus state, scroll, fold map, view mode),
  `filetree.go` (build/flatten/expand mirroring opencode), `render.go` (unified + side-by-side; reuse
  Android's DiffRow gutter grammar — 1ch colored sign + body).
- Data: `SDK.SessionDiff(sessionID, messageID)` (Forge already calls `GET /session/{id}/diff` in the
  Android `ForgeClient`; add the Go SDK equivalent). Working-tree source (`vcs.diff`) is **deferred**
  until the daemon exposes a VCS-diff endpoint (not in the current openapi surface) — ship session-patch
  source first.
- Open as a full-screen route (not a modal overlay); bind `d` to open on the selected message (needs
  the message cursor from 08a §C).
- Persist `single_patch`, `diff_style`, `filetree_enabled` in the 08a §H local KV.
- Syntax highlighting is **optional/stretch** (a Go highlighter like `chroma`); plain colored
  add/del is the v1.

**Dependencies:** 08a §C (message cursor) to anchor "diff this message"; 08a §H (KV) for view state.
**Risks:** side-by-side wrapping/À-la-opencode scroll feel is fiddly; cap v1 at unified + file tree,
add split later. **Conformance value:** medium (exercises `GET /session/{id}/diff` deeply).

---

## 2. PTY pane (T11 — the TUI's interactive terminal)  ✅ SHIPPED (#80)
**What opencode does.** The GUI app embeds a real terminal (`packages/app/src/components/terminal.tsx`
via **`ghostty-web`**) over **`GET /pty/{id}/connect`** (WebSocket), created with `POST /pty`
(`pty/index.ts`). Forge framing: `0x00` + UTF-8 JSON `{cursor}` control frames alongside raw bytes
(CLAUDE.md). Forge's Go SDK **already has `CreatePTY`/`ConnectPTY`/`ResizePTY`** (`sdk/go/pty.go`), and
**Android already ships a PTY pane** (C6, `TerminalScreen`).

**Why it needs a plan.** Every other TUI pane renders styled text Bubble Tea controls. A PTY pane
streams raw ANSI (cursor moves, colors, scroll regions, clear-screen) that must be **interpreted into
a virtual screen grid** and handed to Bubble Tea — a terminal emulator nested inside a terminal app.
The GUIs "buy" an emulator (ghostty-web/xterm.js); the TUI has no drop-in equivalent.

**Proposed Forge approach.**
- Adopt a Go VT parser/grid: evaluate `github.com/hinshun/vt10x` or `github.com/charmbracelet/x/...`
  (a cell-grid emulator) — **decision point: which library** (a spike is step 1; if none fit, the
  fallback is a minimal CSI/SGR subset, which is a real risk).
- New `internal/tui/pty/` package: a Bubble Tea sub-model owning the WS (`SDK.ConnectPTY`), feeding
  bytes into the VT grid, rendering the grid into a pane region, forwarding key events out, and
  sending `ResizePTY` on pane-resize.
- Layout: a toggleable split (bottom or right half) composed in `model.go`'s `View`; one PTY per
  session directory (reuse the create/connect-token flow Android uses).
- Honor the `0x00`+JSON cursor control frame for cursor sync.

**Dependencies:** none on the daemon (endpoints + Go SDK exist). **Risks:** the VT-emulation library
choice dominates; raw-key forwarding vs Bubble Tea's own key handling needs a focus model (keys go to
the PTY only when the pane is focused). **Conformance value:** medium (the WS-PTY framing is a frozen
contract surface; a TUI PTY gives a second client to dual-run it against).

---

## 3. Workspace management  *(daemon-gated — daemon first)*
**What opencode does.** `dialog-workspace-create/list/file-changes/unavailable.tsx`,
`feature-plugins/sidebar/...`, keybind `workspace_set`. Endpoints are **experimental**:
`GET/POST /experimental/workspace`, `GET /experimental/workspace/status`,
`POST /experimental/workspace/sync-list`, `POST /experimental/workspace/warp`,
`GET /experimental/workspace/adapter`, `DELETE /experimental/workspace/{id}` → `Workspace` schema.
Workspaces are git-worktree-backed isolated working copies the agent operates in.

**Why it needs a plan.** This is a **daemon capability**, not a client feature: creating/warping/
syncing worktrees is server-side orchestration. The TUI is only a thin control surface. It also sits
behind `/experimental/*`, so the contract may move.

**Proposed approach.** **Do not build in the TUI until the Forge daemon implements
`/experimental/workspace*`** (a daemon plan, likely an extension of `plans/01`/`plans/13`). Once it
does: a `dialog-workspace-list` + create/delete overlay + a sidebar workspace label, all thin wrappers
over the experimental endpoints, plus a `workspace.*` SSE reducer for status. **Recommendation:**
park entirely until the daemon side is planned; the TUI work is small relative to the daemon work.

---

## 4. Provider connect / OAuth / org auth  *(daemon-gated)*
**What opencode does.** `dialog-provider.tsx` (`provider_connect`), `dialog-console-org.tsx`
(`console_org_switch`), provider auth via **`GET /provider/auth`**, **`PUT`/`DELETE /auth/{providerID}`**,
and OAuth via **`POST /provider/{providerID}/oauth/authorize`** + **`/oauth/callback`**. MCP has a
parallel set (`POST /mcp/{name}/auth/authenticate` + `/callback`).

**Why it needs a plan.** Credential management + an OAuth redirect/device-code dance is a security-
sensitive subsystem, and where the secrets live is a **daemon** decision (the daemon holds provider
keys; the client just initiates flows). Auth is explicitly a daemon concern in `plans/01`.

**Proposed approach.** Gate on the daemon's auth model. Client side, once endpoints exist: an
"Add/connect provider" overlay that (a) for API-key providers `PUT /auth/{id}` with a masked input,
(b) for OAuth providers calls `/oauth/authorize`, shows the URL/device code, polls `/oauth/callback`.
**Mirror Android**, which will need the identical flow — so design the flow **once** (a shared plan
with Android) rather than TUI-first. **Recommendation:** fold into a daemon+Android auth plan; the TUI
is a secondary surface.

---

## 5. TUI plugin host  *(large; mirrors plan 05)*
**What opencode does.** A client-side plugin runtime: `plugin/runtime.ts`
(`init/list/activatePlugin/addPlugin/installPlugin/dispose`, `Slot`), `plugin/api.tsx`, `plugin/slots.tsx`,
`plugin/command-shim.ts`. The TUI is itself assembled from `feature-plugins/*` (home, sidebar, system)
mounted into named **slots**, and third-party plugins can register into those slots, add commands, and
draw UI. Plugins run in a worker (`worker.ts`).

**Why it needs a plan.** This is a whole extensibility architecture (slot registry, plugin lifecycle,
sandboxed execution, a plugin API surface) — opencode rebuilt its TUI *around* it. Forge's
`plans/05-plugin-host.md` covers the **daemon** plugin host (mark3labs/MCP, JS via a runtime); a *TUI*
plugin host is a separate client-side concern with different constraints (Go host, no JS runtime in a
Go binary without embedding one).

**Proposed approach.** **Almost certainly out of scope for a dogfood TUI.** If pursued: (a) first
introduce an **internal slot architecture** (refactor Forge TUI panes into named slots) — valuable
regardless, modest effort; (b) a *third-party* plugin host (loading external Go plugins via
`hashicorp/go-plugin`, or a wasm/JS runtime) is a major project to spec separately and only if there's
demand. **Recommendation:** at most do the internal-slot refactor (a) as an enabler; defer external
plugins indefinitely. Coordinate with `plans/05`.

---

## 6. Stash (prompt-draft + git stash)  ✅ SHIPPED (#81)
**What opencode does.** `component/prompt/stash.tsx` + `dialog-stash.tsx`, keybinds
`prompt_stash`/`prompt_stash_list`/`prompt_stash_pop`, `stash_delete`. Lets the user park the current
composer draft (and recall/pop it later) — a named draft store adjacent to history/frecency. (Despite
the name it is **prompt-draft** stashing, not git stash.)

**Why it needs a plan (lightly).** It's the smallest of the planned items — a local named-draft store
with its own list/pop/delete dialog — but it interlocks with the 08a §H history/frecency KV and wants a
coherent "composer drafts" design rather than a bolt-on.

**Proposed approach.** Build **after** 08a §H lands: extend the local KV with a `stash` namespace
(named drafts), add `prompt_stash` (save current buffer under an auto/typed name), a `dialog-stash`
list overlay, and pop/delete. **Dependency:** 08a §H. **Recommendation:** promote to 08a-tier once §H
exists — it's then nearly drop-in. **Conformance value:** none (local UX).

---

## 7. Variant  ✅ SHIPPED (#81) — *was tagged daemon-gated; `Model.variants` from GET /provider sufficed, no daemon work needed*
**What opencode does.** `dialog-variant.tsx`. The `Model` schema carries a `variants` field; a variant
selects an alternate configuration of the same model (e.g. a reasoning/effort variant). Surfaced as a
picker tied to the model switch.

**Why it needs a plan.** "Variant" is opencode-specific model-config semantics. Before building a
picker, Forge must decide whether its daemon/provider model exposes variants at all (the data comes
from `GET /provider` → `Model.variants`), and how a chosen variant threads into
`POST /session/{id}/message` (alongside `model`/`agent`).

**Proposed approach.** Research `Model.variants` semantics in opencode (`provider` data +
`prompt.ts` handling), confirm the request-side field, then a small picker hung off the 08a model
modal. **Recommendation:** investigate as part of the model-switcher follow-up; trivial UI once the
semantics + request field are pinned. **Dependency:** model data from the daemon.

---

## 8. Tag  *(daemon-gated — session metadata)*
**What opencode does.** `dialog-tag.tsx` — assign tags/labels to sessions for organization/filtering.
**Why it needs a plan.** Tags are **session metadata** that must be stored and queryable server-side;
the current `Session` schema and openapi don't expose a tag field/endpoint, so this is gated on the
daemon adding session-tag storage (and likely a `PATCH /session/{id}` extension or a tags endpoint).
**Proposed approach.** Park until the daemon models session tags; then a tag overlay + filter in the
session list. **Recommendation:** lowest priority; daemon-gated and organizational-only.

---

## 9. Sub-agent UX  ✅ SHIPPED (#78)
**What opencode does.** `routes/session/dialog-subagent.tsx` + `subagent-footer.tsx` +
`session_child_*` keybinds, over **`GET /session/{id}/children`** (child sessions = sub-agent runs).
Lets you see and cycle into sub-agent child sessions and watch their status
(`fix(tui): surface subagent retry status`). Forge **Android renders sub-agent cards** (the `task`
tool → SubAgentBlock) but has no child-session navigation.

**Why it needs a plan.** Needs a child-session model in the TUI store (the reducer currently tracks one
session's stream), navigation between parent/child, and a footer/status surface — a moderate
structural change to how the TUI scopes "the current session".

**Proposed approach.** Add `SDK.SessionChildren(id)` (`GET /session/{id}/children`); extend the store to
hold child sessions; a `subagent` footer listing children + status; `session_child_cycle` to enter a
child's stream and back. Forge already renders `task` parts inline (Android parity), so this is the
*navigation* layer on top. **Dependency:** none on the daemon (endpoint exists). **Conformance value:**
medium (exercises `GET /session/{id}/children` + child-session event routing).

---

## Recommended sequencing
1. **TUI-appropriate, not daemon-gated, real conformance value:** **#9 sub-agent nav**, **#1 diff
   viewer**, **#2 PTY pane (T11)** — in that order (ascending risk; #2's emulator spike is the wildcard).
   These are the only three worth doing TUI-first.
2. **Promote when 08a lands:** **#6 stash** (trivial once 08a §H KV exists).
3. **Investigate-then-trivial:** **#7 variant** (pin the semantics during model-switcher follow-up).
4. **Daemon-first, design with Android, not TUI-first:** **#3 workspaces**, **#4 provider/OAuth auth**,
   **#8 tags** — these belong to daemon plans (`01`/`13`) and the primary client; the TUI is a thin
   follow-on.
5. **Probably never (for a dogfood TUI):** **#5 plugin host** — at most the internal-slot refactor.

## Testing posture (per item, when built)
- Go table-driven unit tests for the new sub-models (diff fold/scroll math, VT-grid output, child-session
  reducer, stash KV).
- Dual-run conformance (plan 12) for every endpoint touched: `GET /session/{id}/diff`,
  `GET /session/{id}/children`, `/pty/*` framing, and (when un-gated) `/experimental/workspace*`,
  `/provider/{id}/oauth/*`, `/auth/{id}`.
- PTY pane: a recorded-bytes golden test (feed a captured ANSI stream, assert the rendered grid).

## Bottom line
Of the nine, only **three (diff viewer, PTY pane, sub-agent nav)** are genuinely "build it in the TUI
next." **Stash** and **variant** are near-trivial once small prerequisites land. The remaining four
(**workspaces, provider/OAuth auth, tags, plugin host**) are **daemon-gated or architectural** and
should be planned at the daemon/Android level — building them TUI-first would be miscalibrated for a
dogfood/conformance vehicle.
