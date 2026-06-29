# Android data-layer refactor: client split → repositories → error/loading UX

**Status:** proposed · **Scope:** `android/` only · **Wire impact:** none (all three PRs are wire-neutral)

## Why

The Android client's UI layer is idiomatic modern Compose (immutable `UiState`,
`StateFlow` + `stateIn(WhileSubscribed)`, unidirectional `AppStore`/reducer = MVI). The **data
layer is the weak seam**:

1. **`Opcode42Client` is a 545-line God object** (`core/sdk/Opcode42Client.kt`) fusing three
   concerns — HTTP plumbing (`get/post/patch/put/delete`, `buildUrl`), JSON→domain mapping
   (`parsePart`, the inline `getMessages` unwrap), and ~30 endpoint methods.
2. **No repository layer.** ViewModels depend directly on `Opcode42Client` **and** `AppStore`
   **and** `SseManager` — three data concerns leaking into the UI layer, none mockable.
   Orchestration that belongs in a data layer is stranded in ViewModels: the multi-project
   session fan-out (`SessionListViewModel.loadSessions`), the optimistic send add/rollback
   (`ChatViewModel.sendPrompt` + `AppStore.addOptimistic/removeOptimistic`), and the diff-fetch
   dedup (`ChatViewModel._diffInFlight`).
3. **Failures are swallowed.** Nearly every VM method is `try { … } catch (e) { Log.w(...) }`;
   permission/question replies use `catch (_: Exception) {}`. `ChatUiState.isLoading/isSending`
   and `SessionListUiState.isLoading/error` are **declared but never set** — dead fields where a
   user-facing error/loading channel should be. A failed send just silently drops its optimistic
   bubble.

This is deliberately **not** an offline-first change (no Room/outbox/WorkManager — see the
masterplan: the daemon owns the SQLite source of truth). It is the structural prerequisite that
makes those later (`07` follow-ons #4/#5) a repository implementation detail with zero further VM
churn.

## Sequencing — three independent, mergeable PRs

Each PR follows the CLAUDE.md loop (branch → local gate → review subagent → green CI → squash-merge).
None changes wire bytes, so none needs a dual-run conformance gate — state that explicitly per PR.

| PR | Scope | Risk | Blast radius | Depends on |
|----|-------|------|--------------|------------|
| 1  | Split `Opcode42Client` → `HttpTransport` + `Mappers` + slim client; typed `HttpException` | Low (internal to `:core:sdk`, public API unchanged) | 1 module | — |
| 2  | New `:core:data` with `*Repository` interfaces + Hilt `@Binds`; migrate VMs off `Opcode42Client`/`AppStore` writes; repos return `Result` | Medium (every VM) | all features | 1 |
| 3  | `error`/loading + one-shot `UiEvent` per `UiState`; snackbars; wire the dead `isSending`/`isLoading` | Low | all features | 2 |

## Shared foundation (lands in PR 1)

Replace `error("HTTP $code …")` (raw `IllegalStateException` carrying a response body) with typed
exceptions the data layer can map to user messages:

```kotlin
// core/sdk
class HttpException(val code: Int, val body: String?) : java.io.IOException("HTTP $code")
class NotConfiguredException : IllegalStateException("No server configured")
```

```kotlin
// core/data (PR 2) — single mapping point for snackbars
fun Throwable.toUserMessage(): String = when (this) {
    is NotConfiguredException -> "No server configured"
    is HttpException -> if (code == 401) "Not authorized" else "Server error ($code)"
    is java.io.IOException -> "Can't reach the server"
    else -> "Something went wrong"
}
```

Repository boundaries use stdlib `Result<T>` (no new sealed type; clean with `suspend`). A typed
`OpError` is a drop-in upgrade later if exhaustiveness is wanted.

## PR 1 — Split `Opcode42Client` (#3)

All within `:core:sdk`. **Public method signatures unchanged → zero call-site changes.**

```
core/sdk/.../http/HttpTransport.kt   plumbing: get/post/patch/put/delete<T>, buildUrl,
                                     X-Opencode-Directory header, withContext(IO),
                                     non-2xx → HttpException. @Singleton, injects OkHttpClient + BaseUrlProvider.
core/sdk/.../mapping/Mappers.kt      pure JsonElement→domain: parsePart, parseMessage (the inline
                                     getMessages block), decodeSession/Provider/Agent. No I/O → unit-testable.
core/sdk/.../Opcode42Client.kt       slim: each method = transport.verb(...) + Mappers.x(...). Same DI.
```

- Fold the three hand-rolled requests (`findFiles`, `getSessionDiff`, `unregisterPush`) onto
  `HttpTransport`, removing their duplicated `Request.Builder`/`baseUrl` boilerplate.
- **Tests:** new `MappersTest` (captured opencode `/message` JSON → assert `Message`/`Part` tree);
  existing SSE/wire tests stay green. No conformance impact.

## PR 2 — Repository layer (#2)

New module **`:core:data`** → depends on `:core:sdk`, `:core:store`, `:core:network`, `:core:model`.
Features depend on `:core:data` instead of `:core:sdk` directly. New layering:
`feature → core:data → {sdk, store, network}`.

DI mirrors the existing `ConnectionsModule` `@Binds` abstract-class pattern:

```kotlin
// core/data/DataModule.kt
@Module @InstallIn(SingletonComponent::class)
abstract class DataModule {
    @Binds @Singleton abstract fun bindSessionRepo(impl: DefaultSessionRepository): SessionRepository
    @Binds @Singleton abstract fun bindChatRepo(impl: DefaultChatRepository): ChatRepository
    @Binds @Singleton abstract fun bindTerminalRepo(impl: DefaultTerminalRepository): TerminalRepository
}
```

Reads are `Flow`s off the store; writes return `Result` and own the store dispatch:

```kotlin
interface SessionRepository {
    val sessions: Flow<List<Session>>
    suspend fun refreshAll(): Result<Unit>                 // ← multi-project fan-out from SessionListVM.loadSessions
    suspend fun create(directory: String?): Result<Session>
    suspend fun fork(id: String): Result<Session>
    suspend fun rename(id: String, title: String): Result<Session>
    suspend fun archive(id: String): Result<Session>
    suspend fun delete(id: String): Result<Unit>
    suspend fun share(id: String): Result<Session>;  suspend fun unshare(id: String): Result<Session>
    suspend fun summarize(id: String, model: ModelRef): Result<Unit>
    suspend fun abort(id: String, directory: String?): Result<Unit>
    suspend fun replyPermission(reqId: String, allow: Boolean): Result<Unit>   // shared by chat + sessions VMs
    suspend fun replyQuestion(reqId: String, answer: String): Result<Unit>
    suspend fun rejectQuestion(reqId: String): Result<Unit>
}

interface ChatRepository {
    fun observe(sessionId: String): Flow<ChatSlice>        // bundles messages/parts/perms/questions/status/optimistic/diffs
    suspend fun loadMessages(sessionId: String, dir: String?): Result<Unit>
    suspend fun send(sessionId: String, text: String, dir: String?, attachments: List<FilePartInput>,
                     model: ModelRef?, agent: String?): Result<Unit>   // ← owns optimistic add + rollback
    suspend fun loadDiff(sessionId: String, messageId: String, dir: String): Result<Unit>  // ← owns _diffInFlight dedup
    suspend fun searchFiles(q: String, dir: String?): Result<List<String>>
    suspend fun listCommands(dir: String?): Result<List<CommandInfo>>
    suspend fun listProviders(dir: String?): Result<ProvidersResponse>
    suspend fun listAgents(dir: String?): Result<List<AgentInfo>>
    suspend fun runCommand(sessionId: String, name: String, args: String, dir: String?): Result<Unit>
}

interface TerminalRepository { /* createPty, connectPty, resize — wraps PtyClient lifecycle */ }
```

**Orchestration that moves out of ViewModels (the win):**
- `SessionListViewModel.loadSessions()` project-enumeration + parallel fan-out + dedupe →
  `DefaultSessionRepository.refreshAll()`.
- `ChatViewModel.sendPrompt` optimistic add/rollback → `ChatRepository.send()`.
- `ChatViewModel._diffInFlight` dedup + `loadDiff` → `ChatRepository`.
- **VMs stop importing `Opcode42Client` and `AppStore` entirely** — they hold only repositories.

**Stays in the VM (UI shaping, not data):** `projectSessionList(...)`, the
`distinctUntilChanged().flowOn(Default)` projection gate, and `lastModelled`/`contextTokens`/
`extractTodos` — now fed by `repo.observe(...)` instead of `store.state`.

**Tests:** `DefaultSessionRepositoryTest`/`DefaultChatRepositoryTest` against a fake transport —
fan-out dedupe, "one dead directory never blanks the list", optimistic rollback on failure, diff
dedup. VMs get trivial repository fakes.

## PR 3 — Error + loading UX (#1)

Two channels, per modern guidance:

1. **Persistent state** for load failures → finally set the dead `error: String?` / `isLoading`.
2. **One-shot events** for transient action failures → a `Channel`, so a snackbar fires once and
   doesn't replay on rotation:

```kotlin
private val _events = Channel<UiEvent>(Channel.BUFFERED)
val events = _events.receiveAsFlow()
sealed interface UiEvent { data class Error(val msg: String) : UiEvent }

fun rename(title: String) = viewModelScope.launch {
    sessionRepo.rename(id, title).onFailure { _events.send(UiEvent.Error(it.toUserMessage())) }
}
```

**Screen wiring** — `ChatScreen` and `SessionListScreen` already have a `Scaffold`; add a host +
collector:

```kotlin
val snackbar = remember { SnackbarHostState() }
LaunchedEffect(Unit) { vm.events.collect { if (it is UiEvent.Error) snackbar.showSnackbar(it.msg) } }
Scaffold(snackbarHost = { SnackbarHost(snackbar) }, /* … */) { … }
```

**Wire the dead flags:** `ChatViewModel.sendPrompt` flips `isSending` around `repo.send` (composer
disables + spins); initial `loadMessages` sets `isLoading`. Replace the silent permission/question
`catch (_: Exception) {}` with a snackbar on failure.

**Verify:** kill the daemon → send/rename shows a snackbar + the optimistic bubble rolls back;
cold-load a session offline → loading→error instead of a permanent blank.

## Effort & risk

- PR 1 ≈ half a day (mechanical). PR 2 ≈ 1–1.5 days (keystone). PR 3 ≈ half a day. No new deps.
- **Review focus for PR 2:** the optimistic-clear timing (reducer drops optimistic on the
  *assistant* echo) and `refreshAll`'s "one unreachable directory never blanks the list" must be
  preserved exactly.

## Explicitly out of scope

Room, durable outbox, WorkManager (the offline-first follow-ons). PR 2 creates the exact seam they
plug into: a durable outbox becomes a `ChatRepository.send` implementation detail; a read cache
becomes a flow-merge inside the repos — both with zero further VM changes.
