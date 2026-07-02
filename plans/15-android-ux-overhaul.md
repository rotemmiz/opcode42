# Plan 15 — Android UX Overhaul: native-feel polish pass

> **Scope.** A consolidated fix plan for the 21 UX issues surfaced in the mobile review pass.
> Every item is grounded against the current tree (`android/`) with `file:line` citations. The
> overriding goal stated in the review: **"no part in the system should look like a custom component,
> I want it to feel and act like a high-quality native Android app."**
>
> **Framing.** This is *not* a re-architecture. The MVI/SSE/store stack from plan 07 stays. What
> changes is the **presentation layer** (Compose composables, Material 3 tokens, insets, motion) and
> a handful of **small data-flow fixes** for flakes/stuck states. Items are grouped into workstreams
> (A–H) that can be shipped as independent PRs and reviewed in isolation.

## Links
- Parent: `plans/07-client-mobile.md` (the Android spec — architecture, phased delivery).
- Design reference: `design/android/README.md` ("Terminal-Material" direction, tokens).
- Reference web client: `/Users/rotemmiz/git/opencode/packages/app/` (patterns to mirror).
- Sibling: `plans/08c-tui-visual-parity.md` (the TUI's own visual pass — same spirit, different UI).

## How items map to workstreams

| WS | Theme | Items (from the review list) |
|----|-------|------------------------------|
| A | Native foundations: Material 3, insets, theme | 6, 14, 15, 16, 17, 13, 18 |
| B | Navigation & motion | 7, 19 |
| C | Sidebar, menus, new-session layout | 3, 20, 21 |
| D | Chat surface: messages, images, subagents | 2, 8, 11, 1 |
| E | Sheets & drawers | 12 |
| F | Connection & first-run | 4, 9, 10 |
| G | Settings redesign | 5 |
| H | Verification & rollout | — |

Each workstream opens its own PR against `main`, follows the project git workflow (build → vet →
gofmt → lint → test → review subagent → CI green → squash-merge), and lands independently. WS A is
the foundation — most others build on its tokens/insets.

---

## Workstream A — Native foundations: Material 3, insets, theme

**Goal:** make every surface read as Material 3 + the project's "Terminal-Material" tokens, with
correct edge-to-edge insets, a user-controllable theme, and consistent typography.

### A1. Theme toggle + theme persistence  *(item 6)*

**Current:** `MainActivity.kt:52` hard-wires `darkTheme = isSystemInDarkTheme()`; `AppPreferences.kt:13-23`
explicitly says no theme pref is stored; light + dark palettes both fully defined in
`Opcode42Theme.kt:73-122` but light is unreachable except by OS setting.

**Fix:**
- Add `ThemeMode { System, Light, Dark }` to `AppPreferences` (DataStore-backed, `core/data` or
  `feature/settings`). Key: `theme_mode` (string: `"system"|"light"|"dark"`).
- `MainActivity` collects `themeMode` from `AppPreferences` and derives `darkTheme`:
  `when (mode) { System -> isSystemInDarkTheme(); Light -> false; Dark -> true }`.
- **Menu toggle** (item 6 asks for it *in the menu*): add a 3-way icon toggle in the **rail footer**
  next to the Settings gear (`AdaptiveChatScreen.kt:561-571`) — sun/moon/auto icons, cycling
  `Dark → Light → System`. One tap, no sheet. Also add a row in `SettingsScreen` (WS G) for
  discoverability.
- Persist immediately on tap; the `MainActivity` collector recomposes the whole tree.

**Acceptance:** toggling in the rail changes theme live with no relaunch; the choice survives
process death; `System` tracks the OS dark setting.

### A2. Immersive / edge-to-edge on all devices  *(item 17)*

**Current:** `enableEdgeToEdge()` is called (`MainActivity.kt:48`), insets consumed once at the host
(`AdaptiveChatScreen.kt:349` `.systemBarsPadding()`), inner panes opt out. BUT the XML theme
(`app/src/main/res/values/themes.xml`) parent is `android:Theme.Material.Light.NoActionBar` (legacy
platform theme, not Material3) and `windowBackground`/`statusBarColor`/`navigationBarColor` are
hardcoded black — so light theme shows a **black flash on cold start** and the bars don't match the
scheme.

**Fix:**
- Switch the XML theme parent to `Theme.Material3.*` (or `Theme.MaterialComponents.*` if M3 XML
  isn't wired) and set `windowBackground = ?android:colorBackground` so the splash matches the
  chosen scheme. Remove hardcoded `statusBarColor`/`navigationBarColor` (let `enableEdgeToEdge`
  make them transparent and draw content behind).
- Adopt `WindowCompat.setDecorFitsSystemWindows(window, false)` explicitly (belt-and-suspenders
  with `enableEdgeToEdge`).
- Audit every pane for inset correctness: the host consumes `systemBarsPadding` once, so inner panes
  must NOT re-apply it (current `ChatScreen` is called with `applySystemInsets = false` at
  `AdaptiveChatScreen.kt:317` — good). Verify the composer's
  `WindowInsets.ime.union(WindowInsets.navigationBars)` (`ChatScreen.kt:197`) still behaves with
  `adjustResize` (`AndroidManifest.xml:34`).
- Use `Modifier.windowInsetsPadding(WindowInsets.safeDrawing)` on any new overlay surface so
  cutout/notch/ gesture-rail areas stay clear on all form factors (foldable cover displays, tablets
  in landscape with the camera notch).

**Acceptance:** no black flash on cold start in light theme; content draws behind the status bar
with correct scrim; IME never covers the composer; correct on phone portrait/landscape, foldable,
tablet.

### A3. Sidebar native look — Material defaults  *(item 14)*

**Current:** rail width 220/60dp (`RailMorph.kt:24-27`); raw `RoundedCornerShape(6.dp)` mixed with
`Opcode42Shapes.xs (4dp)` (`AdaptiveChatScreen.kt:462`); "SESSIONS" label is a custom `HeaderPurple`
11sp bold letterSpacing 0.6sp (`SessionBrowser.kt:299-314`); right info panel is a fixed 280dp
`Surface` with custom `SbSection` blocks (`AdaptiveChatScreen.kt:623-832`).

**Fix:**
- **Width:** align to Material 3 navigation-drawer spec — expanded rail 360dp for the overlay
  `ModalNavigationDrawer` (M3 modal drawer default), keep 220dp for the inline-push rail (M3
  "navigation rail" expanded is flexible; 220 is fine). Collapsed rail 80dp (M3 nav-rail default is
  80dp, not the current 60). Update `RailMorph.kt:24-27`.
- **Curves:** use the shape scale everywhere — replace the raw `RoundedCornerShape(6.dp)` at
  `AdaptiveChatScreen.kt:462` with `MaterialTheme.shapes.small` (8dp) or the project's
  `Opcode42Shapes.sm`. No ad-hoc dp radii.
- **Labels:** "SESSIONS" and the right-panel section kickers (`SbSection`, `AdaptiveChatScreen.kt:839-877`)
  should use `MaterialTheme.typography.labelMedium` (or `labelLarge`) with `color =
  MaterialTheme.colorScheme.onSurfaceVariant` — not a custom purple. Keep the terminal accent only
  where it earns its meaning (active focal rows, code). Re-route `HeaderPurple`/`LinkCyan` to the
  M3 `onSurfaceVariant`/`primary` slots, or keep them as semantic accents but stop using them for
  plain section labels.
- **Right panel:** make the 280dp adaptive — `widthInfoPanel` should be a window-size-class function
  (e.g. 280dp on compact, 320dp on medium, 360dp on expanded). Replace hardcoded 16dp section
  padding / 64dp KV gutter with `MaterialTheme.spacing` tokens (add a small `Spacing` scale to
  `core/design`: `sm=8, md=16, lg=24, xl=32`).
- **Dividers:** use `HorizontalDivider` with `color = MaterialTheme.colorScheme.outlineVariant`
  consistently (already mostly done).

**Acceptance:** the rail and right panel read as M3 surfaces (correct widths, shape-scale radii,
label typography, spacing tokens) while keeping the terminal accent on focal/code elements.

### A4. Line height & typography tokens  *(item 15)*

**Current:** ad-hoc `lineHeight` per call site — `20.sp` for 14.5sp body (1.38),
`18.sp` for 12sp code (1.5), `16.sp` for 12sp terminal (1.33); many texts set no `lineHeight` at all
(`TodoSheet.kt:237`, `ChatScreen.kt:791`, `SessionRow.kt`, `StatusStrip.kt`).

**Fix:**
- Define a `Opcode42Typography` (extend `MaterialTheme.typography`) with project-specific `bodyLarge`
  /`bodyMedium`/`bodySmall`/`code`/`labelSmall` `TextStyle`s, each with `lineHeight = fontSize * 1.4`
  (M3 default ratio) as a baseline; tune `code` to 1.5.
- Replace the per-call-site `lineHeight` literals in `MarkdownText.kt:272,286,307,345`,
  `TasksScreen.kt:119`, `SubAgentBlock.kt:122`, `PartRenderer.kt:459,480`, `TerminalScreen.kt:97`
  with the matching token.
- For texts that currently set no `lineHeight`, switch them to the appropriate token so they inherit
  a consistent ratio instead of Compose's platform default.

**Acceptance:** no raw `lineHeight =` literals remain outside `core/design`; body text uses one ratio
across the app; code/terminal keep their tighter ratio intentionally.

### A5. Remove left-side accent on selected views — unify on Material selection  *(items 16, 13-partial)*

**Current:** two accent systems coexist:
- `FocalRow.kt:21-30` — `SecondaryContainer` amber tint + 2.5dp `Secondary` left bar via `drawBehind`.
- The rail morph `railActiveHighlight` (`RailMorph.kt:58-91`) — a pill that resizes open↔collapsed.
- The CHANGES row at `AdaptiveChatScreen.kt:762-765` reinvents `focalRow` inline (its own 2.5dp rect).
- User messages use a 2dp `Primary` (blue) left rail (`ChatScreen.kt:758-762`) — a *different* color
  from the selection accent, which reads as inconsistent.

**Fix (item 16 — "remove left side accent on selected views"):**
- For **selection** states (session row, diff row, command/mention panel rows), replace the
  left-bar accent with the M3 selection affordance: `Modifier.background(
  MaterialTheme.colorScheme.secondaryContainer)` fill + a subtle `horizontalPadding` bump, **no left
  bar**. Keep the `railActiveHighlight` pill morph for the collapsed rail (it's a pill, not a bar —
  it reads as M3).
- Consolidate the CHANGES row onto the shared `focalRow` modifier (delete the inline reinvention at
  `AdaptiveChatScreen.kt:762-765`).
- Update `FocalRow.kt` to the no-bar variant (keep the modifier name; it's used in several places).
  If a terminal accent is still wanted on selected rows, use a 4dp **bottom** indicator inside the
  pill instead of a left bar — but the review explicitly asks to remove the left accent, so default
  to container-tint-only.

**Note (item 13-partial — "remove left side color accent" on the composer):** the composer's 2dp
`Primary` left rail (`PromptInput.kt:279` `drawBehind`) is the *same* motif. Remove it; rely on the
container `SurfaceContainer` + `Hairline` border alone for the composer's affordance.

**Acceptance:** no `drawRect(... size = Size(<w>.dp.toPx(), size.height))` left-bar code remains for
*selection*; the rail pill morph survives; user-message distinction is handled by WS A6 (background),
not a left bar.

### A6. User inputs visual distinction  *(item 18)*

**Current:** `UserMessageBlock` (`ChatScreen.kt:752-767`) — only a 2dp `Primary` left rail, no
background, no alignment shift, no avatar. `AssistantMessageBlock` — no container at all. Hard to
scan.

**Fix:**
- **User messages:** right-aligned bubble (max width ~80% of column) with
  `MaterialTheme.colorScheme.primaryContainer` fill, `MaterialTheme.shapes.large` (16dp) rounded with
  a flattened bottom-end corner (M3 chat-bubble convention), `onPrimaryContainer` text. Drop the left
  rail (WS A5). Add a small role/time meta line under the bubble in `labelSmall`.
- **Assistant messages:** full-width, no bubble, but with a subtle 8dp top spacing and a small
  assistant glyph (the project's asterisk/spark) at the start of the first part — distinguishes
  without a container. Keep markdown/code/diff rendering as-is.
- **Optimistic messages:** same user bubble but with `onSurfaceVariant` text alpha 0.6 + the existing
  1dp `LinearProgressIndicator` under the text (`ChatScreen.kt:779-800`), so pending state is clear.

**Acceptance:** user turns read clearly as sent-by-me bubbles; assistant turns read as the agent's
inline prose; optimistic turns are visibly pending.

### A7. Composer native look  *(item 13)*

**Current:** `PromptInput.kt:265-312` — bare `BasicTextField` with hand-rolled `decorationBox`,
`fontSize = 13.5.sp` (small), `RoundedCornerShape(14.dp)` container, 2dp `Primary` left rail, custom
`composerTokenTransformation`.

**Fix:**
- **Cursor & selection:** use `BasicTextField` with `TextStyle` from the typography token (A4) so the
  cursor and selection handles pick up `MaterialTheme.colorScheme.primary` automatically via
  `LocalTextSelectionColors`. Set `cursorBrush = SolidColor(LocalTextSelectionColors.current.handleColor)`.
- **Size:** bump to `bodyLarge` (16sp) — 13.5sp is below the M3 minimum readable size and reads as
  non-native. Keep `maxLines` behavior but let the field grow to ~6 lines before internal scroll.
- **Container:** use `MaterialTheme.shapes.large` (16dp) for the composer container (close to the
  current 14dp, but from the shape scale). Remove the left rail (A5). Keep the `SurfaceContainer` +
  `Hairline` border.
- **Native affordances:** ensure the `decorationBox` exposes the `BasicTextField`'s default
  cut/copy/paste toolbar and selection handles (don't suppress them). Add `keyboardOptions =
  KeyboardOptions(autoCorrectEnabled = true, …)` hints where sensible.
- **Padding:** use the spacing tokens (A3) instead of raw `13.dp`.

**Acceptance:** composer text is 16sp, cursor/handles are themed, no custom left rail, selection
toolbar is the native M3 one.

---

## Workstream B — Navigation & motion

### B1. Sidebar open/close animation on collapsed-rail tap  *(item 7)*

**Current:** on the **InlinePush** rail, tapping a row in a *collapsed* rail swaps the chat content
silently — `railOpen` stays false, the suppressed chat↔chat transition (`Opcode42NavGraph.kt:86-89`)
means no nav animation either → "opens the menu with no animation" reads as "content jumps with no
motion." From an *open* non-persistent rail, selecting collapses it (animated). The Overlay
(modal drawer) path animates correctly via `ModalNavigationDrawer`.

**Fix (match the "decollates" animation used elsewhere):**
- On a collapsed InlinePush rail, tapping a session row should **expand the rail first** (animate
  `railProgress` 0→1 via the existing `animateFloatAsState` tween at `AdaptiveChatScreen.kt:208-212`),
  then navigate. Concretely: in `onSelectSession` (`AdaptiveChatScreen.kt:248`), if
  `!railOpen && layout.leftRailMode == InlinePush`, set `railOpen = true` and defer
  `onNavigateToSession(id)` until the rail open animation completes (use
  `LaunchedEffect(railOpen)` + `awaitFrame`, or `Animatable.animateTo` completion). This mirrors how
  the overlay drawer reveals content before the chat swaps.
- Alternative (simpler, equally native): always keep the rail's own content swap animated
  independently of the chat swap — i.e. the rail row's focal highlight animates in (it already does
  via `railActiveHighlight`) and the chat crossfades. Add a short `Crossfade`/`AnimatedContent` on
  the chat pane when `sessionId` changes (currently suppressed at `Opcode42NavGraph.kt:86-89`).
  Pick this if the "expand then navigate" sequencing feels heavy.
- For the **Overlay** drawer: already animated — no change. But add a `BackHandler` (B2) so system
  back closes the drawer before popping the stack.

**Acceptance:** tapping a session in a collapsed rail produces a visible, smooth motion (rail
expands and/or chat crossfades) — no instant silent jump.

### B2. Navigation: swipes, back buttons, BackHandler  *(item 19)*

**Current:** `Opcode42NavGraph.kt:54-192` — `NavHost` with 6 destinations; back relies entirely on
`navController.popBackStack()`; **zero** `BackHandler`, **zero** swipe handlers
(`swipeable|detectHorizontalDragGestures|anchoredDraggable|SwipeToDismiss` all return no matches).

**Fix — a coherent back/swipe contract:**
- **BackHandler priority (system back gesture/button):** add `BackHandler` at each screen in this
  order of precedence:
  1. If a modal sheet is open (Question/Permission/Todo/Info/picker) → close it, don't pop.
  2. If the Overlay rail drawer is open → close it, don't pop.
  3. If the InlinePush rail is open and non-persistent → collapse it, don't pop.
  4. Else → `popBackStack()` (existing behavior).
- **Edge-swipe to open the rail (phone):** on compact width, add a `Modifier.pointerInput` with
  `detectHorizontalDragGestures` on the left ~24dp edge → open the Overlay drawer. This is the
  native Android nav-drawer gesture (matches `DrawerLayout`'s default). Keep the hamburger as the
  explicit toggle.
- **Edge-swipe to go back (chat → session list):** *optional, defer if it fights the drawer gesture.*
  M3 apps generally rely on the system back gesture (predictive back) rather than an in-app edge
  swipe. Adopt **predictive back** (`android:enableOnBackInvokedCallback="true"` in the manifest +
  `PredictiveBackHandler` for the swipe-to-home animation) instead of a custom edge swipe. This is
  the native Android 14+ path.
- **Swipe-between-sessions:** not adding — no native precedent and it conflicts with the drawer
  gesture. If desired later, a horizontal pager on the chat pane is the right primitive, not a
  custom swipe.

**Acceptance:** system back closes sheets/drawers in priority order before popping; the left edge
swipe opens the drawer on phones; predictive back animates correctly on Android 14+.

---

## Workstream C — Sidebar, menus, new-session layout

### C1. COMMANDS in the right menu — remove or repurpose  *(items 3, 21)*

**Current:** `AdaptiveChatScreen.kt:824-828` — a read-only `COMMANDS` section listing daemon commands
(`/name` + source badge + description) in the right info panel. On phone (no right panel) it's
invisible — the `/`-palette in the composer (`PromptInput.kt:218-236, 460-523`) is the actionable
surface. The review asks: "remove COMMANDS from menu, or suggest a useful thing to do with them"
and "is there anything to do with /commands section on the right menu? Seems useless."

**Decision — repurpose, don't remove:**
- The right-panel `COMMANDS` section is **read-only and redundant** with the `/`-palette. Remove it
  from `SessionInfoPanel` (delete `AdaptiveChatScreen.kt:824-828` + the `CommandRow` at 970-1006 if
  unused elsewhere).
- **Keep the actionable surface in the composer** (`/`-palette) — that's where commands are useful:
  the user is already typing. Make the palette the single source of truth for commands.
- **One useful addition:** surface **recently used / pinned commands** as chips above the composer
  when the input is empty (a "command shelf"), so discoverability isn't only via typing `/`. This
  replaces the passive right-menu list with an active, in-context affordance. (Stretch goal — ship
  the removal first, add the shelf as a follow-up if it tests well.)

**Acceptance:** the right info panel no longer has a `COMMANDS` section; commands are reachable via
the `/`-palette (and optionally the empty-input command shelf); no information is lost because the
palette already merges built-in + daemon commands.

### C2. New-session window: phone strip, tablet empty right panel  *(item 20)*

**Current:**
- Phone: `StatusStrip` (`ChatScreen.kt:199-213`, `StatusStrip.kt:31-100`) shows the "build" mode
  chip + model + provider + tokens **above the input** on a draft. The review says "on phone — do
  not show the 'build' and model view on top of the text input."
- Tablet: the right info panel shows on medium/expanded (`AdaptiveChatScreen.kt:120-133`) but is
  **blank on a draft** (every section gates on real data — `session != null`, `tokens != null`, …)
  → "on tablet — have the right side menu closed - as its empty."

**Fix:**
- **Phone draft:** hide `StatusStrip` when `isDraft && !isMultiPane` (remove the mode/model row
  above the input on a fresh draft). The mode/model selection moves into the **first-prompt flow**:
  when the user submits the first prompt on a draft, show the model/agent picker as a sheet (or use
  the `/models` `/agents` palette entries). On a non-draft session, keep `StatusStrip` (it reflects
  the active session's model). Net: a clean "What should we build?" splash with just the composer,
  like Claude Code mobile.
- **Tablet draft:** auto-collapse the right info panel when it would be empty. Compute
  `infoPanelHasContent = session != null || diffs.isNotEmpty() || todos.isNotEmpty() ||
  commands.isNotEmpty() || tokens != null` (project this in `AdaptiveChatScreen`), and default
  `infoPanelOpen = false` when `isDraft && !infoPanelHasContent`. The user can still open it
  manually; it just doesn't start blank. When the first turn produces data, auto-open it (animated).

**Acceptance:** phone draft = clean splash + composer, no mode/model strip; tablet draft = right
panel starts collapsed (not blank); panel opens when content arrives.

---

## Workstream D — Chat surface: messages, images, subagents

### D1. Subagents as first-class, navigable citizens  *(item 2)*

**Current:** `SubAgentBlock.kt` renders the `task` tool inline (spark icon + type + description +
expandable `<task_result>` text). Child sessions are **filtered out** of the session list
(`SessionListViewModel.kt:99-101` `filter { it.parentID == null }`), so the subagent's full
transcript is unreachable. The child session id isn't even surfaced from the `ToolPart` input
(`SubAgentBlock.kt:57-58` reads only `description` + `subagent_type`).

**Fix — make subagents navigable, in-session and cross-session:**
- **Surface the child session id.** The `task` tool's input carries the spawned session id (parse it
  from the tool input JSON — verify the field name against opencode's `task` tool input shape; if
  the id arrives via a different event, capture it in the store reducer when a `session.updated`
  with `parentID == <current>` arrives). Add `childSessionId: String?` to the subagent block's data.
- **In-session navigation:** `SubAgentBlock` becomes a **tappable card** that expands inline to show
  the child session's message stream *inside* the parent chat (a nested `LazyColumn` or a
  collapsible "subagent transcript" section that loads the child's messages on first expand via
  `GET /session/{childId}/message`). This keeps context while letting the user drill in.
- **Cross-session navigation:** a "Open in new view" affordance on the expanded subagent card calls
  `onNavigateToSession(childId)` — but render it in a way that signals "subagent of <parent>" (a
  subtitle in the chat top bar). Add the child session to a **subagent rail** (a secondary section
  in the left rail under the parent, indented) so it's reachable from the sidebar too. Keep them
  out of the *top-level* session list (`parentID != null` filter stays for the main list) but show
  them as an expandable subtree under the parent row in the rail.
- **Live status:** while the subagent is running, show the spinner (already in `SubAgentBlock`);
  surface `session.status` for the child (the store already tracks `sessionStatus` by id —
  `AppStore.sessionStatus`); when it completes, the card auto-expands to show the result summary +
  a "view transcript" link.

**Acceptance:** a subagent card is tappable; expanding shows its transcript inline; a secondary
affordance opens it as its own chat view; running subagents show live status; the parent row in the
rail shows an expandable subtree of its subagents.

### D2. Image references: thumbnail + full view  *(item 8)*

**Current:** `FilePartView` (`PartRenderer.kt:490-522`) renders an `AssistChip` with an icon + a
label that deliberately never shows the `data:` URL (`fileChipLabel` at 515-522). **No image is ever
decoded or displayed.** `FilePart(mime, filename?, url)` (`Part.kt:37-46`) carries the bytes/data.

**Fix — render images in-app (Coil):**
- In `FilePartView`, branch on `part.mime.startsWith("image/")` (or `url.startsWith("data:image/")`):
  - Render a **thumbnail** (96–128dp, `Modifier.clip(MaterialTheme.shapes.medium)`) using Coil's
    `AsyncImage(model = part.url, …)`. Coil handles `data:image/...;base64,...` URIs natively.
  - On click, open a **full-screen image viewer** (a new `Dialog`/`BackHandler`-gated composable, or
    a nav destination) showing the image at full resolution with `zoomable`/`pannable` modifiers
    (use a small zoomable helper — `Modifier.pointerInput` with `detectTransformGestures`). For very
    large images, Coil's `size(Size.ORIGINAL)` is fine for the viewer; the thumbnail uses a sampled
    decode. If the image is a remote `http(s)` URL, Coil fetches it; if it's a `data:` URI, no
    network needed.
  - For non-image files, keep the current chip.
- Add the Coil dependency to `:feature:chat` (it's already listed in plan 07's tech choices as the
  image loader). Wire `AsyncImage` with a placeholder + error fallback chip.
- **Composer attachments** (`PromptInput.kt:239-258` AttachmentChip): also render image thumbnails
  instead of the generic chip for image attachments (same Coil path).

**Acceptance:** an image file part shows a thumbnail inline; tapping opens a full-screen zoomable
view in-app; non-images stay as chips; works for `data:` and `http(s)` URLs.

### D3. Agent questions: render in stream + fix stuck state  *(item 11)*

**Current:** `QuestionSheet` (`PermissionSheet.kt:82-134`) — `onDismissRequest = { /* non-dismissible */ }`
is a **no-op**, so swipe-down/system-back dismisses the sheet visually but the store never gets
`QuestionReplied`/`QuestionRejected`, `pendingQuestion` stays non-null, and the composer stays
disabled forever (`ChatScreen.kt:217` `enabled = pendingQuestion == null`). Also "does not output
the questions at all": the question only appears as a transient modal; there's no `QuestionPart` in
the stream (`Part.kt:8-91` has no question type); `question.message` is nullable so a question with
no message shows just "The agent has a question:".

**Fix (two parts):**
1. **Fix the stuck state:**
   - Replace the no-op `onDismissRequest` with `onReject(req.id)` (call `viewModel.rejectQuestion`).
     Swiping away or pressing back on the sheet = rejecting the question, which dispatches
     `QuestionRejected` → store removes it → composer re-enables. This matches the web client's
     "skip" semantics.
   - Add a `BackHandler` (WS B2) so system back on the sheet rejects (don't let it fall through to
     pop the screen).
   - Defensive: even if the sheet is somehow dismissed without the callback, add a
     `LaunchedEffect(pendingQuestion?.id)` that re-shows the sheet if `pendingQuestion` is still
     non-null and no sheet is composed — i.e. the sheet is *derived* from `pendingQuestion`, not
     independently dismissible.
2. **Render questions in the conversation stream:**
   - Add a `QuestionPart` to the `Part` sealed class (`Part.kt`) — or, to avoid a wire-contract
     change, render a **synthetic question block** in the chat stream when
     `pendingQuestion != null`, keyed by the question id, positioned after the last assistant
     message. The block shows the full question text (with a graceful fallback when `message` is
     null: "The agent is waiting for input" + a tap-to-open-sheet affordance) and an inline
     "Reply / Skip" row (reuse `SessionPendingActions`'s question branch from
     `SessionActivity.kt:107-144`). This makes the question **visible in history** even after the
     sheet is dismissed — it stays as a turn in the transcript until `QuestionReplied`/`Rejected`
     arrives, then collapses to the answered state.
   - Ensure `question.message` is always shown: if null, fall back to a generic prompt and surface
     the question id/role so the user has context.

**Acceptance:** dismissing the question sheet (swipe/back) rejects it and re-enables the composer
(no stuck state); the question text appears in the chat stream with inline reply/skip; a null
`message` shows a sensible fallback.

### D4. Context bar flake on session switch  *(item 1)*

**Current:** `contextTokens` = last assistant message with `tokens.output > 0`
(`ChatViewModel.kt:128-130`); `contextLimit` looked up by provider/model in the providers catalog,
loaded async after the directory is known (`loadPickers`, line 263-281). On a fresh session switch,
no assistant turn has produced output yet → `contextTokens` is `null` and the gauge blanks; the
limit arrives async later. The "more than the others" delay is because tokens need a completed
assistant turn *and* the providers catalog.

**Fix:**
- **Carry the last-known context footprint from the session itself.** `Session` carries
  `time.active`, `tokens` aggregate, etc. — and the store tracks `sessionStatus`. On session
  switch, immediately populate the context gauge from the **session's last message's tokens**
  (fetched via `GET /session/{id}/message` as part of `loadMessages`, which already runs on switch)
  rather than waiting for a *new* turn. The session's message history has the last assistant turn's
  token usage — use the last message with `tokens != null`, not `tokens.output > 0`.
- **Cache the providers catalog** across session switches (it rarely changes within a session). Load
  it once per connection, not per session switch, so `contextLimit` resolves synchronously from the
  cache. If the catalog isn't cached yet, show an indeterminate gauge (not a blank one) — a thin
  shimmer/indeterminate bar communicates "loading" instead of "empty."
- **On a brand-new draft** (no messages): show the gauge at `0 / limit` (limit from the default
  model) instead of blank — a draft has a model picked, so the limit is known even with zero tokens.
- **Stabilize the bar:** animate the fill width on token changes (`animateFloatAsState` on the
  percentage) so it doesn't pop when values arrive; animate the number with a subtle count-up if
  desired.

**Acceptance:** switching sessions shows the context gauge immediately (from the session's last
message tokens + cached limit), with an indeterminate state only if the catalog isn't loaded; no
blank flake; draft shows `0 / <default limit>`.

---

## Workstream E — Sheets & drawers

### E1. TODOs drawer: gradual scrim  *(item 12)*

**Current:** `TodoSheet.kt:66` `ScrimColor = Color(0x80080909)` (50% black); drawn at lines 94-103
behind `if (open)` where `open` is a `derivedStateOf` threshold (`height.value > peekPx + 24dp`,
line 89). The scrim **pops in at full 50%** the instant the sheet crosses the threshold while
dragging, and vanishes instantly on collapse — because the scrim alpha is binary, not tied to the
`height` `Animatable` (line 87) that drives the sheet size smoothly.

**Fix:**
- Tie the scrim alpha to the sheet's drag progress:
  `val scrimAlpha by remember { derivedStateOf { lerp(0f, 0.5f, ((height.value - peekPx) / (expandedPx - peekPx)).coerceIn(0f, 1f)) } }`
  and draw the scrim `Box` with `Modifier.background(ScrimColor.copy(alpha = scrimAlpha))` (or use
  `Color.Black.copy(alpha = scrimAlpha)` directly). This makes the scrim **gradually darken** as the
  user drags the sheet up, matching the sheet's motion — exactly like `ModalBottomSheet`'s native
  scrim.
- When the sheet is animating (not dragging), the `height` `Animatable` already drives the value, so
  the scrim animates in lockstep with the sheet — no extra animation needed.
- On collapse, the alpha goes to 0 as `height` returns to `peekPx`; remove the `if (open)` gate
  (draw the scrim whenever `height.value > peekPx`, alpha handles the fade).
- Match `ModalBottomSheet`'s scrim color token (`Color.Scrim` / `MaterialTheme.colorScheme.scrim`)
  for native consistency instead of the hardcoded `0x80080909`.

**Acceptance:** dragging the TODOs sheet up gradually darkens the background; releasing snaps both
the sheet and the scrim together; no pop.

---

## Workstream F — Connection & first-run

### F1. Redesign the initial connect view  *(item 4)*

**Current:** there is **no initial/empty-state connect screen.** The app boots straight to the
NewChat draft (`Opcode42NavGraph.kt:76` `startDestination = Screen.NewChat.route`). A user with no
configured server lands on the chat composer; the only path to add a server is Settings → Add Server
(gear icon → Settings → Add Server). `AddServerScreen.kt` exists but is reached only via Settings.

**Fix:**
- **First-run gate:** in `Opcode42NavGraph`, compute `startDestination` dynamically:
  if `connectionManager.activeFlow.value == null && connectionManager.connections.isEmpty()` →
  start at a new `Connect` destination; else `NewChat`. This is a one-time read at startup.
- **New `ConnectScreen`** (a redesigned first-run surface, not the raw `AddServerScreen` form):
  - A clean, branded hero ("Connect to your Opcode42 server" + the project mark).
  - **Primary action:** a single URL field (`http://host:port`) + optional credentials (collapsible
    "Advanced" — username/password). Match `normalizeServerUrl` (`ServerConnection.kt:54-58`).
  - **mDNS discovery** (WS F3): a "Nearby servers" list auto-populated by mDNS — tap to fill the URL
    field. This is the native-feel fast path on a LAN.
  - **Scan QR / paste share link** (stretch): if a share URL (`/session/{id}/share` or a server
    invite) is pasted, parse it and prefill.
  - "Connect" button → `viewModel.addServer(...)` → on success, navigate to `NewChat` (replacing the
    graph start so back doesn't return to Connect).
  - Empty-state help: a short "How to run a server" expandable (`opcoded serve` / `opencode serve`).
- Keep `AddServerScreen` as the in-app "add another server" flow (reachable from Settings); the new
  `ConnectScreen` is the first-run sibling, sharing the ViewModel + manager.

**Acceptance:** a first-run user lands on a purpose-built connect screen (not the chat composer);
adding a server there takes them straight to the session list/chat; returning users with a server
skip it.

### F2. Green dot with no server  *(item 10)*

**Current:** `AdaptiveChatScreen.kt:538-546` — the rail-footer status dot is unconditionally
`Tertiary` (green). The label falls back to `"No server"` (`SessionListViewModel.kt:67`) but the dot
stays green, falsely implying a live connection.

**Fix:**
- Drive the dot color from a real connection state. The `SseManager` exposes
`connectionState: StateFlow<ConnectionState>` (`ChatViewModel.kt:236` reads
`chatRepo.connectionState`). Surface this to the rail footer (pass it through `AdaptiveChatScreen`'s
uiState, or hoist a small `ConnectionState` flow into the host).
- Colors:
  - `Connected` (SSE live) → green (`Tertiary` or `MaterialTheme.colorScheme.primary`).
  - `Connecting` / `Reconnecting` → amber (pulsing, optional).
  - `Disconnected` / no server configured → grey (`onSurfaceVariant` at low alpha) or red
    (`error`) — pick grey for "no server" (neutral) and red for "server configured but unreachable."
- The label already says "No server" — make the dot match: grey when no server, red when configured
  but disconnected, green when SSE is live.

**Acceptance:** with no server configured, the dot is grey (not green); with a server but SSE down,
red; with SSE live, green; transitions are visible.

### F3. mDNS discovery  *(item 9)*

**Current:** no code. `AndroidManifest.xml` has only `INTERNET` + `RECORD_AUDIO`; no
`CHANGE_WIFI_MULTICAST_STATE`, no `NsdManager` usage.

**Verified against opencode — the SERVER publishes mDNS (not a client-only feature):**
- `packages/opencode/src/server/mdns.ts:6-34` — `MDNS.publish(port, domain)` uses the
  `bonjour-service` npm package to advertise the HTTP server on the LAN.
- `packages/opencode/src/server/server.ts:155-169` — `setupMdns` calls `MDNS.publish` when
  `opts.mdns` is true AND the host is non-loopback (`!== 127.0.0.1 / localhost / ::1`); on loopback
  it logs a warning and skips. So mDNS only fires when bound to `0.0.0.0` or a real interface.
- **Advertised service shape** (`mdns.ts:14-20`):
  - `name`: `opencode-{port}` (e.g. `opencode-4096`)
  - `type`: **`http`** → DNS-SD service type `_http._tcp` (NOT `_opencode._tcp` — that was a
    masterplan mislabel; no `_opencode._tcp` exists in the opencode tree)
  - `host`: `opencode.local` (or `--mdns-domain`, default per `config/server.ts:11-13`)
  - `port`: the server port
  - `txt`: `{ path: "/" }`
- **Toggle:** config `server.mdns` (`packages/core/src/v1/config/server.ts:11`) or `--mdns` /
  `--no-mdns` CLI flag (`packages/opencode/src/cli/network.ts:17-24, 65-68`). Default off.
- The Opcode42 Go daemon must replicate this exactly (publish `_http._tcp` with name
  `opencode-{port}`, host `opencode.local`, txt `{path:"/"}`) for the Android client to discover it;
  the brand identifier `opcode42.local` noted in plan 14 is a *divergence* the Android discoverer
  must account for (accept both `opencode.local` and `opcode42.local` hostnames).

**Fix (net-new, LAN-only):**
- **Permissions:** add `android.permission.CHANGE_WIFI_MULTICAST_STATE` to the manifest; request it
  at runtime when discovery starts. Without a `WifiManager.MulticastLock`, multicast DNS packets are
  dropped on most Android devices.
- **Discovery service** (`feature/connections/.../MdnsDiscovery.kt`): wrap Android's `NsdManager`.
  Browse **two** service types in parallel (per plan 13's contract — opencode emits only `_http._tcp`,
  Opcode42 emits both `_http._tcp` and `_opencode._tcp`):
  1. `_http._tcp.` — catches `opencode serve --mdns` (and Opcode42's `_http._tcp` alias).
  2. `_opencode._tcp.` — catches Opcode42's brand service type.
  On service found, `NsdManager.resolveService` → `NsdServiceInfo` → `host` + `port`; filter by
  service name prefix `opencode-` or `opcode42-` (plan 13: Opcode42 names instances
  `opcode42-<port>`) so we don't surface every random HTTP service on the LAN. Dedupe by
  `(host, port)` across the two browse types. Emit `DiscoveredServer(name, host, port)` to a
  `StateFlow<List<DiscoveredServer>>`.
- **Multicast lock:** acquire `WifiManager.MulticastLock("opcode42-mdns")` while discovery is
  active; release on stop.
- **UI:** the `ConnectScreen` (F1) "Nearby servers" list consumes the `StateFlow`; tapping a
  discovered server fills the URL field (`http://<host>:<port>`) and the display name
  (`service.serviceName`). Also surface a "Scan nearby" button in `AddServerScreen` (in-app add
  flow) for non-first-run cases.
- **Lifecycle:** start discovery when the connect/add screen is shown; stop on dispose. Don't run
  discovery in the background (battery).
- **Test fixture for H1b:** to validate F3 on the emulators, run `opencode serve --mdns
  --hostname 0.0.0.0 --port 4096` on the host (the emulator's LAN bridges to the host network, so
  the Android `NsdManager` will see the `opencode-4096` `_http._tcp` service). If the Opcode42 Go
  daemon isn't advertising yet, validate against `opencode serve` first (Phase-A spirit). Once the
  Go daemon supports `--mdns` (plan 13 scope 13.7), also test `opcoded serve --mdns` to confirm the
  `_opencode._tcp` browse path and the `opcode42-{port}` name filter.

**Acceptance:** on the connect screen, nearby `opencode serve --mdns` (discovered via `_http._tcp`)
and `opcoded --mdns` (discovered via both `_http._tcp` and `_opencode._tcp`) daemons appear
automatically as `opencode-{port}` / `opcode42-{port}` entries; tap fills URL; connect works;
discovery stops when the screen leaves; non-`opencode-`/`opcode42-` HTTP services are filtered out.

---

## Workstream G — Settings redesign

### G1. Settings redesign  *(item 5)*

**Current:** `SettingsScreen.kt` (1-113) — a Scaffold + TopAppBar with a "Servers" section
(`ServerRow` list) + "Add Server" item. Very sparse: no theme toggle, no about, no notifications, no
agent/model defaults. Uses raw Material3 `ListItem`/`OutlinedTextField`, not the project's design
tokens, so it visually diverges from the rest of the app.

**Fix — make settings a proper M3 surface, grouped by concern:**
- **Appearance** — theme mode (System/Light/Dark) radio rows (mirrors the rail toggle from A1; this
  is the discoverable home for it). Add "Use dynamic color (Material You)" toggle (Android 12+) as a
  stretch — the project's `Opcode42Theme` could opt into `dynamicLightColorScheme`/
  `dynamicDarkColorScheme` when enabled.
- **Servers** — the existing server list + "Add server" + "Scan nearby" (F3). Each server row shows
  connection state (green/amber/grey dot, F2) + set-active + edit + remove.
- **Agent & model defaults** — default agent (for new sessions), default model/provider. Read from
  `AppPreferences`; applied as the draft's initial pickers in `ChatViewModel`.
- **Notifications** — (stretch, tied to plan 13) a toggle + permission prompt for push
  notifications.
- **About** — app version, daemon version (from `GET /global/health`), links to docs/repo.
- **Visual:** use the project's tokens (A3/A4) — `MaterialTheme.shapes`, spacing tokens, typography
  tokens, `onSurface`/`onSurfaceVariant` colors. Use `ListItem` with M3 defaults (it's already M3,
  just not themed). Group sections with the same `SbSection`-style headers used elsewhere (or an M3
  `Column` with `medium` spacing + a label header).

**Acceptance:** settings covers appearance/servers/defaults/notifications/about; uses the project's
tokens; the theme toggle is here AND in the rail (A1); server rows show live connection state.

---

## Workstream H — Verification & rollout

### H1. Verification per workstream

Each WS PR includes:
- **Unit tests** for pure logic changes:
  - A1 theme: `AppPreferences` theme-mode round-trip; `MainActivity` darkTheme derivation.
  - A4 typography: snapshot/no tests needed (token wiring).
  - D1 subagents: `childSessionId` parsing from tool input; rail subtree projection.
  - D3 questions: reducer still handles `QuestionReplied`/`Rejected`; the "sheet derived from
    pendingQuestion" invariant holds.
  - D4 context bar: context gauge projection from last message tokens + cached limit.
  - E1 scrim: alpha = lerp(0, 0.5, progress) over edge values.
  - F2 green dot: color mapping from `ConnectionState`.
- **Compose UI tests** (`ComposeTestRule`) for behavioral changes:
  - D1: tapping a subagent card expands its transcript; "open in new view" navigates.
  - D2: image `FilePart` renders a thumbnail; tap opens the viewer.
  - D3: dismissing the question sheet rejects it; composer re-enables; question text appears in
    the stream.
  - B1/B2: `BackHandler` priority — sheet open → closes sheet; drawer open → closes drawer; else
    pops.
  - C2: draft on phone hides `StatusStrip`; draft on tablet starts with right panel collapsed.
- **Manual/visual checks** (can't be asserted in code easily):
  - A2: no black flash on cold start (light theme); edge-to-edge on foldable/tablet.
  - A3/A5/A6/A7: rail/panel/composer/bubbles read as M3; no left-bar accents on selection.
  - E1: TODOs scrim darkens gradually while dragging.
  - F1/F3: first-run connect screen appears; mDNS lists a local `opcoded serve` / `opencode serve`.

### H1b. Three-device validation gate (mandatory before merge)

**Every WS PR is validated on all three form-factor emulators before it can merge.** The whole
point of this overhaul is "feel and act like a high-quality native Android app" — that can only be
judged by running the app on each form factor, since the adaptive layout
(`AdaptiveChatScreen.kt:110-134`, `chatLayoutFor`) branches on window size class and the review
items explicitly call out phone-vs-tablet differences (items 7, 20).

**Device matrix (all must be running and booted):**

| AVD | Form factor | adb serial | Role |
|-----|-------------|-----------|------|
| `forge-api35` | Phone (compact width) | `emulator-5554` | Compact-width checks: single pane, overlay drawer, phone draft splash, edge-swipe drawer, composer native look. |
| `forge-pixel-fold` | Foldable (medium/expanded unfolded) | `emulator-5560` | Foldable checks: hinge posture, inline-push rail, right-panel persistence, edge-to-edge across unfold. |
| `forge-tablet` | Tablet (expanded) | `emulator-5558` | Expanded checks: persistent right panel, tablet draft auto-collapse (C2), multi-pane layout, insets in landscape. |

**Start the emulators (the phone was down at review time — start it fresh):**
```
~/Library/Android/sdk/emulator/emulator -avd forge-api35 -no-snapshot-save &
~/Library/Android/sdk/emulator/emulator -avd forge-pixel-fold -no-snapshot-save -port 5560 &
~/Library/Android/sdk/emulator/emulator -avd forge-tablet -no-snapshot-save &
```
Wait for `sys.boot_completed == 1` on each (`adb -s <serial> shell getprop sys.boot_completed`)
before installing. If an AVD won't register on adb (port clash), give it an explicit `-port` and
`-wipe-data` if it was stale.

**Per-PR install + smoke flow (run on all three):**
1. `./gradlew :app:installDebug` (installs on all connected devices; or
   `./gradlew :app:installDebug -PandroidSerials=emulator-5554,emulator-5560,emulator-5558`).
2. On each device, launch the app and walk the WS-specific checklist below.
3. Record a screenshot per check per device in the PR description (or a short screen recording for
   motion items). Visual review is the gate — the reviewer subagent checks these.

**WS-specific manual checklist (run on each of the 3 devices):**
- **A1 theme:** toggle the rail sun/moon/auto icon → theme changes live; kill the app, relaunch →
  choice persists; set System → rotate the OS dark setting → app follows.
- **A2 edge-to-edge:** cold-start in light theme → no black flash; content draws behind status bar;
  open the composer → IME doesn't cover it; unfold the fold → layout reflows, insets correct.
- **A3/A5/A6/A7 native look:** rail width 80/220dp, shape-scale radii, no left-bar on selected rows,
  user bubble vs assistant prose, composer 16sp with themed cursor. Compare phone (overlay drawer)
  vs tablet (inline-push rail + right panel).
- **B1 sidebar animation:** from a collapsed inline-push rail (tablet/fold), tap a session → rail
  expands and/or chat crossfades visibly (no silent jump). On phone, open the overlay drawer and tap
  → drawer animates closed.
- **B2 navigation:** system back closes an open sheet/drawer before popping; left-edge swipe opens
  the drawer on phone; predictive back animates on API 35.
- **C1/C2 new-session:** phone draft = no `StatusStrip` above the input, clean splash; tablet draft =
  right panel starts collapsed (not blank); send first prompt → panel opens with content.
- **D1 subagents:** trigger a subagent (task tool) → card is tappable, expands to show transcript,
  "open in new view" navigates; running subagent shows live spinner; parent row shows subtree in rail.
- **D2 images:** send/reference an image → thumbnail renders inline; tap → full-screen zoomable view.
- **D3 questions:** trigger an agent question → question text appears in the stream; swipe the sheet
  away → composer re-enables (not stuck); reply/skip works.
- **D4 context bar:** switch sessions → gauge shows immediately (no blank flake); draft shows
  `0 / <limit>`.
- **E1 TODOs scrim:** drag the TODOs sheet up → background darkens gradually; release → sheet + scrim
  snap together.
- **F1/F2/F3 connect:** clear all servers, cold-start → connect screen appears (not chat); green dot
  is grey with no server; start `opcoded serve` on the host → "Nearby servers" lists it via mDNS →
  tap fills URL → connect → lands on chat; green dot turns green when SSE is live.
- **G1 settings:** open settings → appearance/servers/defaults/about sections present; theme toggle
  matches the rail; server rows show live connection dots.

### H2. Rollout order

Each step lands only after passing the **three-device validation gate (H1b)** on `forge-api35`,
`forge-pixel-fold`, and `forge-tablet`.

1. **WS A** (foundations) — lands first; everything else builds on its tokens/insets/theme.
   - A2 (edge-to-edge/theme XML) and A1 (theme toggle) are independent and can split into two PRs.
2. **WS E** (TODOs scrim) — small, isolated, ship anytime after A.
3. **WS B** (navigation/motion) — after A (uses insets + theme).
4. **WS C** (sidebar/menus/new-session) — after A + B.
5. **WS D** (chat surface) — after A (typography/bubbles); D1/D2/D3/D4 can be 4 PRs.
6. **WS F** (connection/first-run/mDNS) — independent of D; can parallelize after A. F3 (mDNS) is
   the largest single item — split into "manifest + service skeleton" and "UI wiring" PRs.
7. **WS G** (settings) — after A1 (theme toggle) and F2 (connection state).

### H3. Non-goals / explicit deferrals

- **PTY terminal pane** polish — plan 07 Phase C; not in this pass.
- **Push notifications** UI — plan 13; only a settings toggle stub here.
- **Dynamic color (Material You)** — stretch in G1; not required for "native feel."
- **KMP extraction** — plan 07 Phase C; not touched here.
- **Custom swipe-between-sessions** — explicitly rejected in B2 (no native precedent; conflicts with
  the drawer gesture).
- **Fork-from-timeline / variant / stash / diff viewer** — backlog per plan 07's command palette
  section; not in this pass.

---

## Quick reference: item → workstream → primary files

| # | Item | WS | Primary file(s) (file:line) |
|---|------|----|------------------------------|
| 1 | Context bar flake | D4 | `AdaptiveChatScreen.kt:682-736`, `ChatViewModel.kt:128-155`, `Session.kt:41-42` |
| 2 | Subagents unnavigable | D1 | `SubAgentBlock.kt`, `PartRenderer.kt:47-52`, `SessionListViewModel.kt:99-101` |
| 3 | COMMANDS in menu | C1 | `AdaptiveChatScreen.kt:824-828, 970-1006` |
| 4 | Connect view redesign | F1 | `AddServerScreen.kt`, `Opcode42NavGraph.kt:76` |
| 5 | Settings redesign | G1 | `SettingsScreen.kt`, `SettingsViewModel.kt`, `AppPreferences.kt` |
| 6 | Dark/light toggle | A1 | `Opcode42Theme.kt:73-122`, `MainActivity.kt:52,58`, `AppPreferences.kt:13-23` |
| 7 | Sidebar no-animation | B1 | `AdaptiveChatScreen.kt:208-212, 231-238, 248, 354-376`, `Opcode42NavGraph.kt:86-89` |
| 8 | Image thumbnail | D2 | `PartRenderer.kt:490-522`, `Part.kt:37-46`, `PromptInput.kt:239-258` |
| 9 | mDNS | F3 | (none — net-new; `AndroidManifest.xml`, new `MdnsDiscovery.kt`) |
| 10 | Green dot no server | F2 | `AdaptiveChatScreen.kt:538-546`, `SessionListViewModel.kt:67, 189-191`, `ChatViewModel.kt:236` |
| 11 | Questions stuck/no output | D3 | `PermissionSheet.kt:82-134`, `ChatScreen.kt:145, 217, 555-561`, `SseEvent.kt:62-66`, `StoreReducer.kt:97-112` |
| 12 | TODOs scrim | E1 | `TodoSheet.kt:66, 87-103` |
| 13 | Input native look | A7 + A5 | `PromptInput.kt:265-312, 279` |
| 14 | Sidebar native look | A3 | `RailMorph.kt:24-27`, `Opcode42Theme.kt:33-38`, `SessionBrowser.kt:299-314`, `AdaptiveChatScreen.kt:623-832` |
| 15 | Line height | A4 | `MarkdownText.kt:272,286,307,345`, `TasksScreen.kt:119`, `SubAgentBlock.kt:122`, `PartRenderer.kt:459,480`, `TerminalScreen.kt:97` |
| 16 | Left accent on selected | A5 | `FocalRow.kt:21-30`, `RailMorph.kt:58-91`, `AdaptiveChatScreen.kt:762-765`, `ChatScreen.kt:758-762` |
| 17 | Immersive/edge-to-edge | A2 | `MainActivity.kt:48`, `AdaptiveChatScreen.kt:349`, `ChatScreen.kt:197,238`, `themes.xml`, `AndroidManifest.xml:34` |
| 18 | User inputs distinction | A6 | `ChatScreen.kt:752-777, 779-800` |
| 19 | Navigation/swipes/back | B2 | `Opcode42NavGraph.kt:54-192` (no BackHandler/swipes today) |
| 20 | New-session layout | C2 | `ChatViewModel.kt:73,85`, `ChatScreen.kt:199-213, 821-835`, `AdaptiveChatScreen.kt:217, 286-305` |
| 21 | /commands right menu | C1 | `AdaptiveChatScreen.kt:824-828` (same as #3) |