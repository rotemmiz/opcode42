# Plan 16 — Question/Permission UI fixes (execution-ready)

> **Context:** Workstream I of `plans/15-android-ux-overhaul.md` is implemented (permission model
> upgrade, three-way reply, QuestionCard, reconcile-on-reconnect, double-tap guard, etc.). The
> build is green and installed on 3 emulators. Three runtime bugs remain — this plan fixes them.
>
> **Reference daemon:** opencode on `http://127.0.0.1:4096` (reached by the app via `10.0.2.2:4096`).
> The app package is `dev.opcode42.app.debug`.

## Bug 1 — 404 "Not Found" snackbar when pressing an option from the left menu

### Root cause
The `question` tool called from a TUI blocks the agent. When the user dismisses the TUI dialog,
the daemon sends `question.rejected` and clears the pending question. The Android app received
`question.asked` and rendered the menu chips. But by the time the user taps an option on the
device, the question is already dead on the daemon → HTTP 404.

The optimistic store clear (in `SessionRepository`) already removes the question from the store
*before* the REST call fires, so the UI disappears instantly. But the 404 still surfaces as a
snackbar error via `emitError("reply", it)` / `emitError("reject", it)`.

This is **correct behavior** (the question is gone), but the 404 should be **swallowed silently**
— the question was already answered/cancelled elsewhere, which is fine.

### Fix
In `ChatViewModel.kt` (`replyQuestion`, `rejectQuestion`, `replyPermission`) and
`SessionListViewModel.kt` (`replyQuestion`, `rejectQuestion`, `replyPermission`):

Change the `onFailure` handler to **swallow HTTP 404** silently (the question/permission is
already gone — no snackbar). Only surface non-404 errors.

```kotlin
fun replyQuestion(requestId: String, answers: List<List<String>>) {
    viewModelScope.launch {
        sessionRepo.replyQuestion(requestId, answers)
            .onFailure { if (!isNotFound(it)) emitError("reply", it) }
    }
}
```

Add a helper (in a shared file or inline) that checks if the error is an HTTP 404:

```kotlin
private fun isNotFound(error: Throwable): Boolean =
    error is dev.opcode42.core.sdk.HttpException && error.code == 404
```

Check `HttpException.kt` for the exact field name (`code`, `statusCode`, `status`, etc.) — it's
in `android/core/sdk/src/main/kotlin/dev/opcode42/core/sdk/HttpException.kt`.

### Files
- `android/feature/chat/src/main/kotlin/dev/opcode42/feature/chat/ChatViewModel.kt` — `replyQuestion` (line ~486), `rejectQuestion` (line ~492), `replyPermission` (line ~479)
- `android/feature/sessions/src/main/kotlin/dev/opcode42/feature/sessions/SessionListViewModel.kt` — `replyQuestion` (line ~286), `rejectQuestion` (line ~293), `replyPermission` (line ~279)
- `android/core/sdk/src/main/kotlin/dev/opcode42/core/sdk/HttpException.kt` — check the status code field

### Test
1. Fire a question via the TUI `question` tool → dismiss it in the TUI (rejects on daemon)
2. On the device: tap an option in the left menu
3. **Expected:** no snackbar error; the question disappears from the menu silently (optimistic clear)

---

## Bug 2 — In-chat question card appears "empty" (no content)

### Root cause
The question fires in the TUI's session. The app may be showing a **different session**. The
in-chat `QuestionCard` only renders for `pendingQuestion = uiState.pendingQuestions.firstOrNull()`
which comes from `snap.questions = s.questions[sessionId]` — the **open session's** questions
(`ChatRepository.kt:115`). If the open session isn't the one with the question, the card won't
show at all.

When the user opens the correct session (taps the row in the left menu), the card should render
with content — but by then the question may already be dismissed (race with TUI dismissal).

### Fix
This is partly a testing/workflow issue (use agent-fired questions, not TUI-dismissed ones), but
there are code improvements:

**2a. Auto-navigate to the session with the pending question when tapped from the menu.**
This already works — `onOpen = { session -> onSelectSession(session.id) }` in `SessionBrowser.kt:115`.
The user needs to tap the session **title** (not the option chips) to open it and see the full card.

**2b. Verify the QuestionCard renders content when the right session is open.**
The `QuestionCard` now uses an expand/collapse pattern. The `expanded` state defaults to
`!resolved` and is keyed by `remember(question.id)`. If the question arrives fresh, `expanded`
should be `true` (auto-expand). Verify:
- `QuestionBlockHeader` shows the question header text (not blank)
- The expanded `QuestionWizardBody` shows the question text + options + buttons

If the content is blank, check that `info.header` and `info.question` are populated — the SSE
decode test (`SseWireParsingTest.kt`) proves they decode correctly from the wire. If they're
blank in the UI, the issue is that the app received a `question.asked` with a different shape
(e.g. from a different daemon version) — add logging to confirm.

**2c. If the question is already rejected by the time the user opens the session, the card
should NOT render at all** (the store should have cleared it via the `question.rejected` SSE
event). If there's a race where the card renders briefly then vanishes, that's acceptable —
the reconcile-on-reconnect (Bug 3) will clean it up.

### Files
- `android/feature/chat/src/main/kotlin/dev/opcode42/feature/chat/ui/PermissionSheet.kt` — `QuestionCard`, `QuestionBlockHeader`, `QuestionWizardBody`
- `android/feature/chat/src/main/kotlin/dev/opcode42/feature/chat/ChatViewModel.kt` — verify `pendingQuestions` comes from the open session
- `android/feature/chat/src/main/kotlin/dev/opcode42/feature/chat/ui/ChatScreen.kt` — the `QuestionCard` item in the LazyColumn (line ~552)

### Test
1. Clear stale questions: reject all pending via `curl -X POST http://127.0.0.1:4096/question/{id}/reject`
2. Create a session: `curl -X POST http://127.0.0.1:4096/session -d '{"directory":"/Users/rotemmiz/git/opcode42_1"}'`
3. Prompt the agent: `curl -X POST http://127.0.0.1:4096/session/{sid}/message -d '{"parts":[{"type":"text","text":"Use the question tool immediately. One question, header Pick one, text Which option do you want?, three options labeled 1/2/3 with descriptions. Ask and wait."}]}'`
4. Wait for `GET /question` to show 1 pending (poll with `curl http://127.0.0.1:4096/question`)
5. On the device: **tap that session's row in the left menu** (the title, not the option chips) → opens the session
6. **Expected:** the in-chat `QuestionCard` renders as an expandable block with "Pick one" header, "Which option do you want?" text, and three selectable options
7. Tap an option → **200 OK** (the question is alive on the daemon)
8. Do NOT dismiss the question in the TUI

---

## Bug 3 — Stale questions linger in the menu after the daemon clears them

### Root cause
When the daemon cancels a question (agent finalizer, or another client answers/rejects it), it
publishes `question.replied` or `question.rejected` — the app clears the store. But when the
agent **cancels** (finalizer fails the deferred without publishing an event), no SSE event fires
and the question lingers in `state.questions` forever.

The reconcile-on-reconnect code is implemented (`reconcilePending()` in `SessionRepository.kt`,
triggered on SSE reconnect in `ChatViewModel.kt:265`) but may not be firing. Verify:

### Fix
**3a. Verify `reconcilePending()` is called on reconnect.**
In `ChatViewModel.kt`, the connection watcher (`:259-266`) calls `loadMessages()` +
`sessionRepo.reconcilePending()` on reconnect. Verify this fires by adding a log line or
checking logcat for the `GET /permission` and `GET /question` calls after a reconnect.

**3b. Add reconcile on `session.status → idle`.**
When a session goes idle, reconcile that session's pending requests. Add to the
`session.status` watcher in `ChatViewModel.kt` (where it already filters `it == "idle"` and
calls `refresh()` ~line 256):

```kotlin
viewModelScope.launch {
    chatRepo.observe(sessionId)
        .map { it.status }
        .distinctUntilChanged()
        .filter { it == "idle" }
        .collect {
            refresh()
            sessionRepo.reconcilePending()  // clear stale questions/permissions
        }
}
```

**3c. Verify the reducer handles `PermissionsReconciled`/`QuestionsReconciled`.**
`StoreReducer.kt` already has:
```kotlin
is AppEvent.PermissionsReconciled -> state.copy(permissions = event.bySession)
is AppEvent.QuestionsReconciled -> state.copy(questions = event.bySession)
```
This **replaces** the entire map — verify it works by checking that after reconcile, stale
questions disappear from the menu.

### Files
- `android/feature/chat/src/main/kotlin/dev/opcode42/feature/chat/ChatViewModel.kt` — connection watcher (line ~259), session.status watcher (line ~253)
- `android/core/data/src/main/kotlin/dev/opcode42/core/data/SessionRepository.kt` — `reconcilePending()` (line ~160)
- `android/core/store/src/commonMain/kotlin/dev/opcode42/core/store/StoreReducer.kt` — reconcile reducers (line ~114)

### Test
1. Fire a question via agent prompt (see Bug 2 test)
2. On the daemon: reject it directly with `curl -X POST http://127.0.0.1:4096/question/{id}/reject`
3. On the device: the question should disappear from the left menu (the `question.rejected` SSE clears it)
4. Fire another question, then cancel the agent session (dispose) — no SSE event fires
5. Force the app to reconnect (toggle airplane mode on the emulator, or `adb shell am force-stop` + relaunch)
6. **Expected:** on reconnect, `reconcilePending()` fetches `GET /question`, sees the question is gone, clears the store → the stale question disappears from the menu

---

## Build gate

After all fixes:
```bash
cd android
./gradlew :app:assembleDebug :feature:chat:testDebugUnitTest :feature:sessions:testDebugUnitTest :core:network:testDebugUnitTest --no-daemon
./gradlew :feature:chat:lintDebug :feature:sessions:lintDebug :app:lintDebug --no-daemon
```

## Install + launch on all devices
```bash
adb -s emulator-5554 install -r android/app/build/outputs/apk/debug/app-debug.apk
adb -s emulator-5556 install -r android/app/build/outputs/apk/debug/app-debug.apk
adb -s emulator-5560 install -r android/app/build/outputs/apk/debug/app-debug.apk
adb -s emulator-5554 shell am force-stop dev.opcode42.app.debug
adb -s emulator-5556 shell am force-stop dev.opcode42.app.debug
adb -s emulator-5560 shell am force-stop dev.opcode42.app.debug
adb -s emulator-5554 shell am start -n dev.opcode42.app.debug/dev.opcode42.app.MainActivity
adb -s emulator-5556 shell am start -n dev.opcode42.app.debug/dev.opcode42.app.MainActivity
adb -s emulator-5560 shell am start -n dev.opcode42.app.debug/dev.opcode42.app.MainActivity
```

## End-to-end test (final verification)

```bash
# 1. Clear stale state
for qid in $(curl -s http://127.0.0.1:4096/question | python3 -c "import sys,json; [print(q['id']) for q in json.load(sys.stdin)]"); do
  curl -s -X POST "http://127.0.0.1:4096/question/$qid/reject" -H "Content-Type: application/json" -d '{}' >/dev/null
done

# 2. Fire a question via agent prompt (stays alive on the daemon)
SID=$(curl -s -X POST http://127.0.0.1:4096/session -H "Content-Type: application/json" -d '{"directory":"/Users/rotemmiz/git/opcode42_1"}' | python3 -c "import sys,json;print(json.load(sys.stdin)['id'])")
curl -s -X POST "http://127.0.0.1:4096/session/$SID/message" -H "Content-Type: application/json" -d '{"parts":[{"type":"text","text":"Use the question tool immediately. One question, header Pick one, text Which option do you want?, three options labeled 1/2/3 with descriptions. Ask and wait."}]}'

# 3. Wait for the question to fire
for i in $(seq 1 40); do
  sleep 2
  N=$(curl -s http://127.0.0.1:4096/question | python3 -c "import sys,json;print(len(json.load(sys.stdin)))")
  if [ "$N" = "1" ]; then echo "QUESTION LIVE"; break; fi
done

# 4. On the device: open the session → answer the question → 200 OK
# 5. Check logcat for the 200
adb -s emulator-5554 logcat -d | grep "POST.*question.*reply"
# Expected: <-- 200 OK
```