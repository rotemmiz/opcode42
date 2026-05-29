# Forge Plan 02 — Agent Engine

## Context

Plan 02 defines Forge's **agent engine**: the LLM streaming loop, message/part model, tool
registry, built-in tools, permission system, compaction, and run-state locking. It is the hardest
and most value-producing component of the Go daemon; without it Forge is a proxy, with it Forge is
a real daemon.

**Dependency chain:**
- Plan 01 (daemon-core) provides: HTTP/SSE transport, SQLite store, session CRUD, `x-opencode-directory` routing, auth. The agent engine calls down into plan 01's storage and SSE bus; it does not own its own persistence layer.
- Plan 03 (MCP) and Plan 04 (ecosystem) deliver MCP-sourced dynamic tools and the agent/config loaders that feed the engine's registry at runtime. The engine is designed so those slots can start empty and fill in later.
- Plan 12 (conformance) defines how the engine's output (SSE event stream, SQLite state) is verified to match real opencode.

**Wire-compat mandate:** the SSE event types and the SQLite row shape produced by the agent engine
must produce an identical observable sequence to opencode for a scripted prompt. Plan 12 owns the
diff harness; plan 02 owns the claims this section validates.

---

## opencode References Validated (file:line + takeaways)

### Prompt entry and run-loop
- `packages/opencode/src/session/prompt.ts:90-97` — `Interface` definition: the five exported entry points are `cancel`, `prompt`, `loop`, `shell`, `command`, `resolvePromptParts`.
- `packages/opencode/src/session/prompt.ts:1215-1233` — `prompt()` fn: (1) calls `createUserMessage`, (2) touches session, (3) sets per-session permission rules from `input.tools`, (4) calls `loop()` unless `noReply === true`. The Go engine mirrors this exact sequence.
- `packages/opencode/src/session/prompt.ts:1244-1496` — `runLoop()`: the master while-loop. Key observations:
  - Calls `MessageV2.filterCompactedEffect(sessionID)` at the top of every iteration to get the compaction-ordered message list.
  - Calls `MessageV2.latest(msgs)` to extract `{user, assistant, finished, tasks}`.
  - **Loop-exit condition** (line 1272-1291): exits when `lastAssistant.finish` is set, not in `["tool-calls"]`, no pending non-orphaned tool parts, and `lastUser.id < lastAssistant.id`.
  - Checks compaction overflow *before* calling the processor (line 1323-1329).
  - Creates an empty `MessageV2.Assistant` row and calls `processor.create({assistantMessage, sessionID, model})`.
  - Calls `SessionTools.resolve(...)` to compile tool map (line 1387-1401).
  - Appends `MAX_STEPS` sentinel to messages on last step (line 1450).
  - `toolChoice: "required"` for JSON schema format (line 1454).
  - Step-1 forks title generation and summary generation (lines 1295, 1413).
- `packages/opencode/src/session/prompt.ts:1500-1503` — `loop()` wraps `runLoop` with `state.ensureRunning`, which is the busy-lock.

### Processor (LLM event → parts)
- `packages/opencode/src/session/processor.ts:36-53` — `Handle` interface: `message`, `updateToolCall`, `completeToolCall`, `process(StreamInput)`.
- `packages/opencode/src/session/processor.ts:64-82` — `ProcessorContext`: holds `toolcalls` map (keyed by LLM tool-call ID), `shouldBreak`, `snapshot`, `blocked`, `needsCompaction`, `currentText`, `reasoningMap`.
- `packages/opencode/src/session/processor.ts:305-688` — `handleEvent()`: the per-event dispatch. Full event taxonomy: `reasoning-start/delta/end`, `tool-input-start/delta/end`, `tool-call`, `tool-result`, `tool-error`, `provider-error`, `step-start`, `step-finish`, `text-start/delta/end`, `finish`.
- `packages/opencode/src/session/processor.ts:377-448` — on `tool-call`: transitions ToolPart state from pending→running, checks doom-loop threshold (`DOOM_LOOP_THRESHOLD = 3` identical calls), asks permission if doom-loop detected.
- `packages/opencode/src/session/processor.ts:452-501` — on `tool-result`: normalises output, handles image attachment normalization, calls `completeToolCall`.
- `packages/opencode/src/session/processor.ts:555-616` — on `step-finish`: accumulates cost+tokens into `assistantMessage`, writes `step-finish` part, triggers compaction check via `isOverflow`.
- `packages/opencode/src/session/processor.ts:780-848` — `process()`: wraps `llm.stream()`, runs `Stream.tap(handleEvent)`, retries via `SessionRetry.policy`, catches to `halt`, `Effect.ensuring(cleanup())`.
- `packages/opencode/src/session/processor.ts:691-748` — `cleanup()`: finalises in-flight text/reasoning parts, awaits all tool-call deferreds (250ms timeout), marks any still-running tools as `status:"error", interrupted:true`.

### LLM stream wrapper
- `packages/opencode/src/session/llm.ts:33-46` — `StreamInput` type: `{user, sessionID, parentSessionID, model, agent, permission, system: string[], messages: ModelMessage[], small?, tools, retries?, toolChoice?}`.
- `packages/opencode/src/session/llm.ts:343-367` — `stream()`: acquires AbortController in scope, calls internal `run()`, if native runtime → returns its stream directly; if AI SDK → wraps `fullStream` via `LLMAISDK.toLLMEvents` adapter. Both paths produce `Stream<LLMEvent>`. The Go engine's streaming abstraction must match this: it returns a `chan LLMEvent` (or equivalent).
- `packages/opencode/src/session/llm.ts:271-339` — AI SDK path: calls `streamText(...)` with `wrapLanguageModel` + middleware for provider-specific message transforms.

### System prompt (provider variants)
- `packages/opencode/src/session/system.ts:19-33` — `provider()` function (not a service method): maps model ID strings to prompt variants. Key mappings:
  - `gpt-4`, `o1`, `o3` → `PROMPT_BEAST`
  - `gpt` (without codex) → `PROMPT_GPT`
  - `gpt` + `codex` → `PROMPT_CODEX`
  - `gemini-` → `PROMPT_GEMINI`
  - `claude` → `PROMPT_ANTHROPIC`
  - `trinity` → `PROMPT_TRINITY`
  - `kimi` → `PROMPT_KIMI`
  - default → `PROMPT_DEFAULT`
- `packages/opencode/src/session/system.ts:47-61` — `environment()`: builds the `<env>` block injected as a system message: model ID, working directory, workspace root, git flag, platform, date.
- `packages/opencode/src/session/system.ts:63-77` — `skills()`: appends skills listing to system if "skill" permission not disabled.
- `packages/opencode/src/session/prompt.ts:1435-1441` — final system assembly: `[...env, ...instructions, ...(skills ? [skills] : [])]` — env first, then instruction overrides, then skills.

### Run-state locking
- `packages/opencode/src/session/run-state.ts:10-24` — `Interface`: `assertNotBusy`, `cancel`, `ensureRunning`, `startShell`.
- `packages/opencode/src/session/run-state.ts:34-48` — state holds a `Map<SessionID, Runner>`. Each session gets exactly one `Runner`. `ensureRunning` is idempotent: existing runner is reused, meaning concurrent `prompt()` calls on the same session queue rather than conflict.
- `packages/opencode/src/session/run-state.ts:87-93` — `ensureRunning()`: delegates to `runner.ensureRunning(work)`. The `Runner` handles the single-fiber guarantee.
- `packages/opencode/src/session/run-state.ts:95-107` — `startShell()`: like `ensureRunning` but surfaces `BusyError` if already occupied (shells cannot queue; they fail fast).
- `packages/opencode/src/session/run-state.ts:149` — `BusyError` = `Session.BusyError{sessionID}`.

### Message / Part model
- `packages/opencode/src/session/message-v2.ts:97-111` — `TextPart`: `{id, sessionID, messageID, type:"text", text, synthetic?, ignored?, time?, metadata?}`.
- `packages/opencode/src/session/message-v2.ts:113-123` — `ReasoningPart`: `{..., type:"reasoning", text, metadata?, time{start, end?}}`.
- `packages/opencode/src/session/message-v2.ts:160-168` — `FilePart`: `{..., type:"file", mime, filename?, url, source?}`.
- `packages/opencode/src/session/message-v2.ts:248-308` — `ToolState` union: `ToolStatePending{status:"pending", input, raw}` | `ToolStateRunning{status:"running", input, title?, metadata?, time{start}}` | `ToolStateCompleted{status:"completed", input, output, title, metadata, time{start,end,compacted?}, attachments?}` | `ToolStateError{status:"error", input, error, metadata?, time{start,end}}`.
- `packages/opencode/src/session/message-v2.ts:310-320` — `ToolPart`: `{..., type:"tool", callID, tool, state:ToolState, metadata?}`.
- `packages/opencode/src/session/message-v2.ts:222-246` — `StepStartPart` and `StepFinishPart`: the step-finish carries `{reason, snapshot?, cost, tokens{total?, input, output, reasoning, cache{read,write}}}`.
- `packages/opencode/src/session/message-v2.ts:88-95` — `PatchPart`: `{..., type:"patch", hash, files:[]}` — emitted when git snapshot diff is non-empty.
- `packages/opencode/src/session/message-v2.ts:184-191` — `CompactionPart`: `{..., type:"compaction", auto, overflow?, tail_start_id?}`.
- `packages/opencode/src/session/message-v2.ts:452-490` — `Assistant` message: `{role:"assistant", time{created,completed?}, error?, parentID, modelID, providerID, mode, agent, path{cwd,root}, summary?, cost, tokens, structured?, variant?, finish?}`.
- `packages/opencode/src/session/message-v2.ts:327-349` — `User` message: `{role:"user", time{created}, format?, summary?, agent, model{providerID,modelID,variant?}, system?, tools?}`.

### Tool registry
- `packages/opencode/src/tool/registry.ts:73-78` — `Interface`: `ids()`, `all()`, `named()`, `tools(model)`.
- `packages/opencode/src/tool/registry.ts:316-360` — `tools(model)` implementation: filters `all()` by model-specific rules (e.g. GPT-4o gets `ApplyPatch` instead of `Edit/Write`; websearch gated by provider/flags), then per-tool calls `plugin.trigger("tool.definition", ...)` to allow description/schema override.
- `packages/opencode/src/tool/registry.ts:246-270` — builtin list (in order): `invalid`, (optional) `question`, `shell`, `read`, `glob`, `grep`, `edit`, `write`, `task`, `fetch` (webfetch), `todo`, `search` (websearch), (flag-gated) `repo_clone`, `repo_overview`, `skill`, `patch`, (flag-gated) `lsp`, (flag-gated) `plan`.
- `packages/opencode/src/tool/registry.ts:139-198` — plugin/custom tools: loaded from `{tool,tools}/*.{js,ts}` in config dirs, converted from Zod schema or raw JSON Schema to `Tool.Def`.

### Permission system
- `packages/opencode/src/permission/index.ts:22-29` — `Rule{permission, pattern, action}` where `action` ∈ `{ask,allow,deny}`. A `Ruleset` is `Rule[]`.
- `packages/opencode/src/permission/index.ts:138` — `evaluate(permission, pattern, ...rulesets)`: walks rulesets in order, last match wins. Pure function, no I/O.
- `packages/opencode/src/permission/index.ts:171-211` — `ask()`: evaluates every pattern; deny → immediate `DeniedError`; all allow → return; else → create `Deferred`, publish `permission.asked` SSE event, await Deferred. Unblocked by `reply()`.
- `packages/opencode/src/permission/index.ts:213-268` — `reply()`: `once` → succeed deferred; `always` → append to approved list, also unblock other pending requests for same session that now pass; `reject` → fail deferred AND cascade-reject all other pending for same session.
- `packages/opencode/src/config/permission.ts:16-37` — Known permission keys: `read`, `edit`, `glob`, `grep`, `list`, `bash`, `task`, `external_directory`, `todowrite`, `question`, `webfetch`, `websearch`, `repo_clone`, `repo_overview`, `lsp`, `doom_loop`, `skill`.

### Compaction
- `packages/opencode/src/session/compaction.ts:35-77` — constants: `PRUNE_MINIMUM=20_000`, `PRUNE_PROTECT=40_000`, `TOOL_OUTPUT_MAX_CHARS=2_000`, `DEFAULT_TAIL_TURNS=2`, token preserve range `[2000, 8000]` with dynamic 25% of usable context.
- `packages/opencode/src/session/compaction.ts:245-293` — `select()`: identifies the tail turns to preserve within token budget, returns `{head, tail_start_id}`.
- `packages/opencode/src/session/compaction.ts:344-581` — `processCompaction()`: builds a summary prompt using the SUMMARY_TEMPLATE, creates a special `summary:true` assistant message, runs it through the processor with no tools, on success emits `session.compacted` bus event; on overflow returns `"stop"`.
- `packages/opencode/src/session/compaction.ts:296-341` — `prune()`: post-loop garbage collection, erases old tool outputs when `>PRUNE_MINIMUM` tokens prunable after protecting `PRUNE_PROTECT` tokens worth of recent tool calls.
- `packages/opencode/src/session/prompt.ts:1310-1329` — compaction trigger in the main loop: if `tasks` contains a `compaction` part → `compaction.process(...)`; else if `lastFinished` overflows → `compaction.create(...)` then `continue`.

### Tool execution (`packages/llm/src/tool.ts`)
- `packages/llm/src/tool.ts:33-43` — `Tool<Parameters, Success>`: typed schema tool. `_definition` holds precomputed `ToolDefinition` (name, description, inputSchema).
- `packages/llm/src/tool.ts:66-69` — dynamic tool variant: `jsonSchema: JsonSchema.JsonSchema`, `execute: (params: unknown) => Effect<unknown, ToolFailure>`. This is how MCP tools arrive.
- `packages/llm/src/tool.ts:172-180` — `toDefinitions(tools)`: converts record → `ToolDefinition[]` for wire.

---

## Design — The Agent Loop (Step-by-Step, Go-Flavored)

### Entry points

```go
type AgentEngine interface {
    Prompt(ctx context.Context, input PromptInput) (MessageWithParts, error)
    Loop(ctx context.Context, sessionID string) (MessageWithParts, error)
    Cancel(ctx context.Context, sessionID string) error
    ResolvePromptParts(ctx context.Context, template string) ([]PartInput, error)
}
```

`Prompt` → validates, creates user message + parts in DB, touches session, optionally calls `Loop`.  
`Loop` → acquires per-session run-lock, calls `runLoop`.  
`Cancel` → signals the session's context cancel, which interrupts the goroutine running `runLoop`.

### Per-session run-lock

```go
type RunState struct {
    mu      sync.Mutex
    active  map[string]*sessionRun  // sessionID → active run
}

type sessionRun struct {
    cancel  context.CancelFunc
    done    chan struct{}
    result  MessageWithParts
    err     error
}
```

- `ensureRunning(sessionID, work func(ctx context.Context) (MessageWithParts, error))`:
  - Acquires `mu`; if an active run exists for `sessionID`, waits for it (returns same result — idempotent).
  - Otherwise spawns a goroutine, registers cancellable context, releases `mu`.
  - This mirrors `run-state.ts:87-93`.
- `cancel(sessionID)`: calls `cancel()` on the run's context. The goroutine's I/O must respect context cancellation.
- Maps `BusyError` to HTTP 409 on the REST layer.

### `runLoop` goroutine

```
func runLoop(ctx context.Context, sessionID string, engine *AgentEngine) (MessageWithParts, error):
  step := 0
  for {
    1. StatusSet(sessionID, "busy")
    2. msgs := filterCompacted(sessionID)              // DB read, ordered by compaction rewrite
    3. latest := computeLatest(msgs)                   // last user, last assistant, last finished, pending tasks
    4. if exitCondition(latest) → break
    5. step++
    6. if step==1: go generateTitle(...)               // fire-and-forget goroutine
    7. model := resolveModel(latest.user)
    8. if latest.tasks has compaction_part → processCompaction(...); continue
    9. if compaction overflow check → createCompactionMarker(...); continue
    10. agent := resolveAgent(latest.user.agent)
    11. isLastStep := step >= agent.maxSteps
    12. msgs = applyReminders(msgs, agent, session)
    13. assistantMsg := createAssistantMessage(sessionID, model, agent, latest.user)
    14. toolMap := compileToolMap(agent, session, model)  // see Tool Registry section
    15. systemParts := buildSystem(model, agent)
    16. modelMsgs := toModelMessages(msgs, model)
    17. if isLastStep: append MAX_STEPS sentinel to modelMsgs
    18. streamResult := stream(ctx, StreamInput{
            user: latest.user, system: systemParts, messages: modelMsgs,
            tools: toolMap, model: model, agent: agent,
            toolChoice: toolChoiceFor(latest.user.format),
        })
    19. outcome := processStream(ctx, streamResult, assistantMsg, toolMap, sessionID)
    20. switch outcome:
          "stop"    → break
          "compact" → createCompactionMarker(...); continue
          "continue"→ continue
  }
  StatusSet(sessionID, "idle")
  compaction.pruneAsync(sessionID)
  return lastAssistant(sessionID)
```

**Exit condition** (mirrors `prompt.ts:1272-1291`):
```
finish is set AND finish != "tool-calls" AND no pending non-orphaned tool parts AND lastUser.id < lastAssistant.id
```

### `processStream` — event consumption goroutine

```go
type StreamProcessor struct {
    assistantMsg  *AssistantMessage   // mutable, flushed to DB
    toolCalls     map[string]*ToolCallState
    currentText   *TextPart
    reasoningMap  map[string]*ReasoningPart
    needsCompact  bool
    shouldBreak   bool
    snapshot      string
    sessionID     string
    model         Model
    sseBus        *SSEBus
    db            *Storage
}

func (p *StreamProcessor) Run(ctx context.Context, events <-chan LLMEvent, tools map[string]Tool) Result:
    for event := range events:
        switch event.Type:
        case TextStart:     p.handleTextStart(event)
        case TextDelta:     p.handleTextDelta(event)  // updatePartDelta → SSE message.part.delta
        case TextEnd:       p.handleTextEnd(event)
        case ReasoningStart: p.handleReasoningStart(event)
        case ReasoningDelta: p.handleReasoningDelta(event)
        case ReasoningEnd:   p.handleReasoningEnd(event)
        case ToolInputStart: p.ensureToolCall(event.ID, event.Name)  // creates ToolPart{status:"pending"}
        case ToolInputDelta: // accumulate raw (no-op currently)
        case ToolInputEnd:   p.markInputEnded(event.ID)
        case ToolCall:       p.handleToolCall(ctx, event, tools)     // → runs tool in goroutine
        case ToolResult:     p.completeToolCall(event)
        case ToolError:      p.failToolCall(event)
        case StepStart:      p.handleStepStart(event)
        case StepFinish:     p.handleStepFinish(event)
        case ProviderError:  return Stop
        case Finish:         // no-op
    p.cleanup(ctx)
    if p.needsCompact: return Compact
    if p.shouldBreak || p.assistantMsg.Error != nil: return Stop
    return Continue
```

**Tool execution** (within `handleToolCall`):
```go
// Each tool call runs in its own goroutine; result fed back via channel.
// The processor does NOT await tool goroutines inline — the AI SDK feeds back
// tool-result events. We DO track pending deferred to wait on in cleanup().
go func(callID string, toolName string, input map[string]any) {
    result, err := tools[toolName].Execute(callCtx, input, ToolContext{
        SessionID: p.sessionID,
        MessageID: p.assistantMsg.ID,
        CallID:    callID,
        Abort:     callCtx.Done(),
    })
    // write result event back to internal channel
}()
```

For Go's native (non-AI SDK) path, tool execution IS inline-but-concurrently: the streaming loop
blocks awaiting each batch of tool results before the next request, matching the AI SDK's behavior.
Specifically: after the LLM response completes, all tool calls in the response are executed
concurrently (via goroutines + WaitGroup), then results are assembled and the next LLM request
is issued. This is the correct match for Vercel AI SDK's `maxSteps` behavior.

**SSE event emission:** every DB write is immediately followed by publishing to the SSE bus:
- `updatePart(part)` → emit `message.part.updated` with `{sessionID, part, time}`
- `updatePartDelta(delta)` → emit `message.part.delta` with `{sessionID, messageID, partID, field, delta}`
- `updateMessage(msg)` → emit `message.updated` with `{sessionID, info}`

---

## Provider Abstraction

### Core interface

```go
// Provider is the Go equivalent of the AI SDK's LanguageModelV2.
type Provider interface {
    // Stream opens a single LLM completion stream. Returns a channel of LLMEvent.
    // Caller closes ctx to abort. The implementation drains the HTTP response
    // fully before closing the channel.
    Stream(ctx context.Context, req *LLMRequest) (<-chan LLMEvent, error)
    // Capability returns model-level flags used by the registry to filter tools.
    Capability() ProviderCapability
}

type LLMRequest struct {
    Model           string
    SystemPrompts   []string
    Messages        []ModelMessage   // wire format: role + content parts
    Tools           []ToolDefinition
    ToolChoice      string           // "auto" | "required" | "none"
    MaxOutputTokens int
    Temperature     *float64
    TopP            *float64
    TopK            *int
    ProviderOptions map[string]any   // passthrough
    Headers         map[string]string
}

type ProviderCapability struct {
    ToolCalls    bool
    Streaming    bool
    Reasoning    bool
    Vision       bool
    PDFInput     bool
}
```

### LLMEvent types (mirrors `@opencode-ai/llm` LLMEvent)

```go
type LLMEventType string
const (
    EventTextStart      LLMEventType = "text-start"
    EventTextDelta      LLMEventType = "text-delta"
    EventTextEnd        LLMEventType = "text-end"
    EventReasoningStart LLMEventType = "reasoning-start"
    EventReasoningDelta LLMEventType = "reasoning-delta"
    EventReasoningEnd   LLMEventType = "reasoning-end"
    EventToolInputStart LLMEventType = "tool-input-start"
    EventToolInputDelta LLMEventType = "tool-input-delta"
    EventToolInputEnd   LLMEventType = "tool-input-end"
    EventToolCall       LLMEventType = "tool-call"
    EventToolResult     LLMEventType = "tool-result"
    EventToolError      LLMEventType = "tool-error"
    EventStepStart      LLMEventType = "step-start"
    EventStepFinish     LLMEventType = "step-finish"
    EventProviderError  LLMEventType = "provider-error"
    EventFinish         LLMEventType = "finish"
)

type LLMEvent struct {
    Type             LLMEventType
    // text-delta / reasoning-delta
    Text             string
    ID               string      // reasoning id, tool call id
    // tool-call / tool-input-*
    Name             string
    Input            map[string]any
    ProviderMetadata map[string]any
    // tool-result / tool-error
    Result           ToolResult
    Error            error
    Message          string
    // step-finish
    Reason           string
    Usage            *TokenUsage
    // provider-error
    StatusCode       int
}
```

### Provider implementations

**Anthropic** (`github.com/anthropics/anthropic-sdk-go`):
- Uses `client.Messages.Stream(ctx, anthropic.MessageStreamParams{...})`.
- Maps SSE events: `content_block_start` / `content_block_delta` / `content_block_stop` → text/tool-input events; `message_delta` → step-finish with usage; `message_stop` → finish.
- Handles `thinking` content block type → reasoning events.
- Special: inject `"type":"thinking"` into `model_params` for extended thinking (Claude 3.7+).
- Tool execution result format: `{"type":"tool_result","tool_use_id":..., "content":[...]}` as user message.

**OpenAI** (`github.com/openai/openai-go`):
- Uses `client.Chat.Completions.NewStreaming(ctx, params)`.
- Parses SSE `data: {...}` chunks, maps `delta.content` → text-delta, `delta.tool_calls` → tool-input events.
- Maps `finish_reason` → step-finish reason.
- Note: OpenAI does not stream tool results back — tool calls are gathered from the stream, executed, then results sent in a follow-up request. The event abstraction hides this: the Go adapter fires `tool-call` events from the stream, then fires `tool-result` after execution before the next stream starts.

**OpenAI-compatible** (single implementation, parameterised base URL + auth):
- Same as OpenAI path. Used for: Groq, Together, Mistral, Ollama, custom LM Studio, etc.
- `providerID` config specifies `type: openai-compatible` + `baseURL`.

**Provider-specific system prompt routing** (mirrors `system.ts:19-33`):
```go
func systemPromptVariant(modelID string) SystemPromptSet {
    lower := strings.ToLower(modelID)
    switch {
    case containsAny(lower, "gpt-4", "o1", "o3"):    return PromptBeast
    case contains(lower, "codex"):                    return PromptCodex
    case contains(lower, "gpt"):                      return PromptGPT
    case contains(lower, "gemini-"):                  return PromptGemini
    case contains(lower, "claude"):                   return PromptAnthropic
    case contains(lower, "trinity"):                  return PromptTrinity
    case contains(lower, "kimi"):                     return PromptKimi
    default:                                          return PromptDefault
    }
}
```

System prompt text files are embedded at build time via `//go:embed prompts/*.txt`.

---

## Message & Part Model (Go Structs Mirroring message-v2)

```go
// --- Parts ---

type PartBase struct {
    ID        string `db:"id" json:"id"`
    SessionID string `db:"session_id" json:"sessionID"`
    MessageID string `db:"message_id" json:"messageID"`
}

type TextPart struct {
    PartBase
    Type      string          `json:"type"` // "text"
    Text      string          `json:"text"`
    Synthetic bool            `json:"synthetic,omitempty"`
    Ignored   bool            `json:"ignored,omitempty"`
    Time      *PartTime       `json:"time,omitempty"`
    Metadata  map[string]any  `json:"metadata,omitempty"`
}

type ReasoningPart struct {
    PartBase
    Type     string          `json:"type"` // "reasoning"
    Text     string          `json:"text"`
    Time     PartTime        `json:"time"`
    Metadata map[string]any  `json:"metadata,omitempty"`
}

type FilePart struct {
    PartBase
    Type     string  `json:"type"` // "file"
    MIME     string  `json:"mime"`
    Filename string  `json:"filename,omitempty"`
    URL      string  `json:"url"`
    Source   *FilePartSource `json:"source,omitempty"`
}

type ToolStatePending struct {
    Status string         `json:"status"` // "pending"
    Input  map[string]any `json:"input"`
    Raw    string         `json:"raw"`
}

type ToolStateRunning struct {
    Status   string         `json:"status"` // "running"
    Input    map[string]any `json:"input"`
    Title    string         `json:"title,omitempty"`
    Metadata map[string]any `json:"metadata,omitempty"`
    Time     struct{ Start int64 `json:"start"` } `json:"time"`
}

type ToolStateCompleted struct {
    Status      string         `json:"status"` // "completed"
    Input       map[string]any `json:"input"`
    Output      string         `json:"output"`
    Title       string         `json:"title"`
    Metadata    map[string]any `json:"metadata"`
    Time        struct {
        Start     int64  `json:"start"`
        End       int64  `json:"end"`
        Compacted *int64 `json:"compacted,omitempty"`
    } `json:"time"`
    Attachments []FilePart `json:"attachments,omitempty"`
}

type ToolStateError struct {
    Status   string         `json:"status"` // "error"
    Input    map[string]any `json:"input"`
    Error    string         `json:"error"`
    Metadata map[string]any `json:"metadata,omitempty"`
    Time     struct{ Start, End int64 } `json:"time"`
}

// ToolState is stored as JSON in the DB; decoded to the appropriate type on read.

type ToolPart struct {
    PartBase
    Type     string         `json:"type"` // "tool"
    CallID   string         `json:"callID"`
    Tool     string         `json:"tool"`
    State    json.RawMessage `json:"state"` // decoded lazily
    Metadata map[string]any  `json:"metadata,omitempty"`
}

type StepStartPart struct {
    PartBase
    Type     string `json:"type"` // "step-start"
    Snapshot string `json:"snapshot,omitempty"`
}

type StepFinishPart struct {
    PartBase
    Type     string      `json:"type"` // "step-finish"
    Reason   string      `json:"reason"`
    Snapshot string      `json:"snapshot,omitempty"`
    Cost     float64     `json:"cost"`
    Tokens   TokenCounts `json:"tokens"`
}

type PatchPart struct {
    PartBase
    Type  string   `json:"type"` // "patch"
    Hash  string   `json:"hash"`
    Files []string `json:"files"`
}

type CompactionPart struct {
    PartBase
    Type        string  `json:"type"` // "compaction"
    Auto        bool    `json:"auto"`
    Overflow    bool    `json:"overflow,omitempty"`
    TailStartID string  `json:"tail_start_id,omitempty"`
}

// --- Messages ---

type UserMessage struct {
    ID        string                 `json:"id"`
    SessionID string                 `json:"sessionID"`
    Role      string                 `json:"role"` // "user"
    Time      struct{ Created int64 `json:"created"` } `json:"time"`
    Agent     string                 `json:"agent"`
    Model     MessageModel           `json:"model"`
    Format    *OutputFormat          `json:"format,omitempty"`
    System    string                 `json:"system,omitempty"`
    Tools     map[string]bool        `json:"tools,omitempty"`
}

type AssistantMessage struct {
    ID          string      `json:"id"`
    SessionID   string      `json:"sessionID"`
    Role        string      `json:"role"` // "assistant"
    ParentID    string      `json:"parentID"`
    ModelID     string      `json:"modelID"`
    ProviderID  string      `json:"providerID"`
    Mode        string      `json:"mode"`
    Agent       string      `json:"agent"`
    Path        MessagePath `json:"path"`
    Summary     bool        `json:"summary,omitempty"`
    Cost        float64     `json:"cost"`
    Tokens      TokenCounts `json:"tokens"`
    Finish      string      `json:"finish,omitempty"`
    Error       *MessageError `json:"error,omitempty"`
    Structured  any         `json:"structured,omitempty"`
    Variant     string      `json:"variant,omitempty"`
    Time        struct {
        Created   int64  `json:"created"`
        Completed *int64 `json:"completed,omitempty"`
    } `json:"time"`
}

type TokenCounts struct {
    Total     *float64 `json:"total,omitempty"`
    Input     float64  `json:"input"`
    Output    float64  `json:"output"`
    Reasoning float64  `json:"reasoning"`
    Cache     struct {
        Read  float64 `json:"read"`
        Write float64 `json:"write"`
    } `json:"cache"`
}
```

**Storage:** parts are stored in the `parts` table as `{id, session_id, message_id, data JSONB}` — exactly mirroring opencode's `PartTable`. Messages in `messages` table: `{id, session_id, data JSONB, time_created}`. The Go structs marshal directly to the `data` column JSON.

**`toModelMessages` equivalent in Go:**
A `MessageSerializer` converts `[]MessageWithParts` to the provider's wire format. It handles:
- Filtering ignored/empty parts.
- Extracting media from tool results for providers that don't support it inline.
- Pending/running tool states → `output-error: "[Tool execution was interrupted]"`.
- `CompactionPart` → `"What did we do so far?"` user text.
- Signed reasoning (Anthropic extended thinking: empty text → `" "` separator).
This is a pure function, extensively unit-tested.

---

## Tool Registry & Built-in Tools

### Registry interface

```go
type ToolRegistry interface {
    // All returns the full compiled set (builtin + dynamic).
    All(ctx context.Context) ([]ToolDef, error)
    // Filter returns the model-appropriate subset, applying permission checks.
    Filter(ctx context.Context, input FilterInput) (map[string]ToolDef, error)
    // Named returns quick access to the task and read tools (needed by the loop).
    Named() struct{ Task, Read ToolDef }
}

type ToolDef struct {
    ID          string
    Description string
    JSONSchema  map[string]any  // JSON Schema 7 object
    Execute     ToolExecuteFn
    // FormatValidationError allows tool-specific input error messages.
    FormatValidationError func(err error) string
}

type FilterInput struct {
    ProviderID string
    ModelID    string
    Agent      AgentInfo
    Session    SessionInfo
    // BypassAgentCheck skips the agent-level task permission check.
    BypassAgentCheck bool
}
```

### Built-in tools (mirrors `registry.ts:246-270`)

| Tool ID | Permission key | Description |
|---|---|---|
| `bash` | `bash` | Execute shell command in working directory |
| `read` | `read` | Read file contents or directory listing |
| `edit` | `edit` | Edit file (search-replace) |
| `write` | `write` | Write/overwrite file |
| `glob` | `glob` | Glob pattern file search |
| `grep` | `grep` | Regex search in files (ripgrep) |
| `task` | `task` | Spawn a subagent session |
| `webfetch` | `webfetch` | Fetch URL content |
| `websearch` | `websearch` | Web search (provider-gated) |
| `patch` | (model-gated) | Apply unified diff patch (GPT-4 class models) |
| `question` | `question` | Ask user a question (client-type gated) |
| `skill` | `skill` | Load a named skill's instructions |
| `todowrite` | `todowrite` | Write to the TODO list |
| `lsp` | `lsp` | LSP workspace queries (flag-gated) |
| `plan` | (flag-gated) | Enter plan mode exit tool |
| `repo_clone` | `repo_clone` | Clone a repository reference (flag-gated) |
| `repo_overview` | `repo_overview` | Summarise a repository (flag-gated) |
| `invalid` | — | Catches unknown tool names; returned as error to model |

**Model-specific routing** (mirrors `registry.ts:316-327`):
```go
usePatch := strings.Contains(modelID, "gpt-") &&
            !strings.Contains(modelID, "oss") &&
            !strings.Contains(modelID, "gpt-4")
// usePatch: include `patch`, exclude `edit`+`write`
// !usePatch: include `edit`+`write`, exclude `patch`
```

**WebSearch gating** (mirrors `registry.ts:59-61`):
Enabled when `providerID == "opencode"` OR `flags.enableExa` OR `flags.enableParallel`.

### Dynamic tools

Dynamic tools (from MCP servers and config-dir `tool/*.ts` files — the latter via plan 05 plugin
host) are represented at registry level as `ToolDef` with raw `JSONSchema`. They are appended to
the built-in list and participate in permission evaluation using their tool ID as the permission
key.

The `Filter` method applies `Permission.evaluate(toolID, pattern, ruleset)` for each tool; tools
where the result is `deny` are excluded from the compiled tool map passed to the LLM.

### Doom-loop detection

Mirrors `processor.ts:424-448`:
```go
const doomLoopThreshold = 3
// In handleToolCall: if the last N parts on the current assistant message are all
// tool calls for the same tool with the same input → ask permission("doom_loop").
```

---

## Permission System

### Core types

```go
type Action string
const (
    ActionAsk   Action = "ask"
    ActionAllow Action = "allow"
    ActionDeny  Action = "deny"
)

type Rule struct {
    Permission string `json:"permission"`
    Pattern    string `json:"pattern"`
    Action     Action `json:"action"`
}

type Ruleset []Rule
```

### Evaluate (pure function)

```go
// evaluate walks rulesets from first to last; last matching rule wins.
// If no rule matches, returns "ask" (default safe behavior).
func evaluate(permission, pattern string, rulesets ...Ruleset) Rule {
    var match *Rule
    for _, rs := range rulesets {
        for i := range rs {
            if wildcardMatch(permission, rs[i].Permission) &&
               wildcardMatch(pattern, rs[i].Pattern) {
                match = &rs[i]
            }
        }
    }
    if match == nil {
        return Rule{Permission: permission, Pattern: pattern, Action: ActionAsk}
    }
    return *match
}
```

Wildcard matching uses the same glob semantics as opencode's `Wildcard.match`: `*` matches any
substring (not path-separator-aware here; permission keys are simple names).

### Ask / Reply (blocking, SSE-driven)

```go
type PermissionManager struct {
    mu      sync.Mutex
    pending map[string]*pendingRequest  // requestID → deferred
    approved []Rule                     // session-level persistent approvals (loaded from DB)
    bus     *SSEBus
}

type pendingRequest struct {
    info    PermissionRequest
    resolve chan error  // nil=approved, PermissionRejectedError=denied
}

func (pm *PermissionManager) Ask(ctx context.Context, input AskInput) error:
    // 1. Evaluate every pattern against merged rulesets.
    // 2. Any deny → return DeniedError immediately.
    // 3. All allow → return nil immediately.
    // 4. Else: assign requestID, store in pm.pending, publish "permission.asked" SSE event.
    // 5. Block on pendingRequest.resolve channel (or ctx cancel → reject + cleanup).

func (pm *PermissionManager) Reply(requestID string, reply ReplyBody) error:
    // Mirrors permission/index.ts:213-268 exactly:
    // once → succeed, always → succeed + add to approved + cascade unblock, reject → fail + cascade.
```

**Known permission keys** (from `config/permission.ts:16-37`):
`read`, `edit`, `glob`, `grep`, `list`, `bash`, `task`, `external_directory`, `todowrite`,
`question`, `webfetch`, `websearch`, `repo_clone`, `repo_overview`, `lsp`, `doom_loop`, `skill`.

**SSE events:** `permission.asked` carries `PermissionRequest{id, sessionID, permission, patterns, metadata, always, tool?}`. `permission.replied` carries `{sessionID, requestID, reply}`.

**Per-tool ask integration:**
Each built-in tool calls `permission.Ask` before executing, passing the tool's permission key
and the relevant pattern (e.g., file path for `read`/`edit`/`write`/`glob`/`grep`, command
string for `bash`, URL for `webfetch`/`websearch`, agent name for `task`).

---

## Compaction & Token/Cost Accounting

### Token accounting

Token usage comes from `step-finish` events (from the LLM provider). The Go agent accumulates per-step usage into the `AssistantMessage.Tokens`:
```go
// On StepFinish event:
msg.Tokens.Input     += event.Usage.InputTokens
msg.Tokens.Output    += event.Usage.OutputTokens
msg.Tokens.Reasoning += event.Usage.ReasoningTokens
msg.Tokens.Cache.Read  += event.Usage.CacheReadTokens
msg.Tokens.Cache.Write += event.Usage.CacheWriteTokens
msg.Cost += computeCost(model, event.Usage)
```

Cost pricing table is loaded from the provider config (same JSON as opencode's
`packages/opencode/src/provider/model.ts`). The formula:
```
cost = (inputTokens  * inputPricePerToken)
     + (outputTokens * outputPricePerToken)
     + (cacheReadTokens  * cacheReadPricePerToken)
     + (cacheWriteTokens * cacheWritePricePerToken)
```

Token estimation for compaction uses a simple heuristic (4 chars ≈ 1 token, same as
`packages/opencode/src/util/token.ts`), sufficient for overflow detection.

### Overflow detection

```go
// usable = model.contextWindow * 0.8 (same ratio as opencode's overflow.ts)
// overflow = tokens.output >= model.maxOutputTokens * 0.9
//         OR tokens.input  >= usable * 0.9
func isOverflow(cfg Config, tokens TokenCounts, model Model) bool
```

Configurable thresholds in `~/.opencode/config.json`:
- `compaction.keep_tokens` (fallback: `usable * 0.25`, clamped [2000, 8000])
- `compaction.tail_turns` (default: 2)
- `compaction.prune` (bool)

### Compaction flow

1. **Trigger:** at start of `runLoop` iteration, if `lastFinished` token count overflows → `compaction.create(sessionID, agent, model, auto:true)` → adds a `CompactionPart` to a new user message → on next iteration the loop picks it up as a `task`.

2. **Process:** `compaction.process({messages, parentID, sessionID, auto, overflow})`:
   a. Find the compaction user message and its `CompactionPart`.
   b. Call `select(messages, cfg, model)` to split head (to summarise) and tail (to preserve).
   c. Build summary prompt from `SUMMARY_TEMPLATE` + optional previous summary for incremental update.
   d. Create a `summary:true` assistant message.
   e. Run through the processor with `tools: {}` (no tools during compaction).
   f. On success: update `tail_start_id` in `CompactionPart`, emit `session.compacted` SSE event.
   g. On context overflow during compaction: mark error, return `"stop"`.
   h. On `auto:true` and no overflow replay: create a synthetic continue user message.

3. **Prune** (post-loop): iterates backward through tool parts; once accumulated output exceeds `PRUNE_PROTECT` tokens, marks older completed tool outputs with `time.compacted = now`. Only runs when `pruned > PRUNE_MINIMUM`.

### `filterCompacted` (message ordering for model)

Go implementation must exactly mirror `message-v2.ts:1014-1064`:
```
[compaction-user, summary-assistant, ...retained_tail..., ...subsequent_turns...]
```
This is the reordering that makes the summary appear at the right position in the model's message history.

---

## Implementation Milestones (Ordered)

### M1 — Message model + storage (2 days)
- Define all Go structs (above).
- SQLite schema: `messages(id TEXT PK, session_id TEXT, data JSON, time_created INT)`, `parts(id TEXT PK, session_id TEXT, message_id TEXT, data JSON)`.
- CRUD operations: `updateMessage`, `updatePart`, `updatePartDelta` (delta-append on text column), `getPart`, `messages(sessionID, limit, cursor)`.
- `toModelMessages()` serialiser — unit test against known fixtures.
- `filterCompacted()` reordering algorithm — unit test with compaction scenario.

### M2 — LLM streaming: Anthropic (3 days)
- Implement `AnthropicProvider` using `anthropic-sdk-go`.
- Map all SSE event types to `LLMEvent`.
- Handle `thinking` content blocks (reasoning events).
- Unit test: mock HTTP server returning a scripted SSE response; assert `[]LLMEvent` sequence.

### M3 — LLM streaming: OpenAI + OpenAI-compatible (2 days)
- Implement `OpenAIProvider` and `OpenAICompatibleProvider` using `openai-go`.
- Same test approach.

### M4 — Processor + run-state (4 days)
- `StreamProcessor.Run()` consuming `<-chan LLMEvent`.
- Per-part DB writes and SSE emissions.
- Tool-call lifecycle: pending → running → completed/error.
- Doom-loop detection.
- Cleanup on context cancel.
- Run-state locking (`RunState` + per-session mutex + cancel).

### M5 — Built-in tools: read/glob/grep/write/edit/patch (3 days)
- Each tool as a Go package implementing `ToolDef`.
- `bash` tool using `os/exec` with timeout + abort via context.
- File tools: `read` (with line offset/limit), `write`, `edit` (search-replace), `glob` (using `doublestar`), `grep` (using `ripgrep` subprocess or pure Go fallback).
- Apply-patch tool.
- Unit tests for each.

### M6 — Built-in tools: task/question/webfetch/websearch/skill/todo (3 days)
- `task` tool: spawns a nested `Loop()` call with a sub-session.
- `question` tool: publishes `question.asked` SSE, blocks on reply (same deferred pattern as permissions).
- `webfetch`: HTTP GET with content extraction.
- `websearch`: Exa API integration (flag-gated), fallback search.
- `skill`: loads skill file from config dirs.
- `todowrite`: writes structured TODO part.

### M7 — Permission system (2 days)
- `PermissionManager` with `Ask`/`Reply`/`List`.
- `evaluate()` pure function with wildcard.
- Wire into all built-in tools.
- SSE events: `permission.asked`, `permission.replied`.
- REST endpoints: `POST /permission/:id/reply`, `GET /permission` (list pending).

### M8 — Tool registry + system prompts (2 days)
- `ToolRegistry` loading all builtins + plugin slots.
- Filter by model, flags, agent permissions.
- `SessionTools.resolve()` equivalent.
- System prompt variants from embedded `.txt` files.
- `environment()` builder.

### M9 — Agent loop integration (3 days)
- `runLoop` and `processStream` wired end-to-end.
- Title generation (forked goroutine, small-model stream).
- Structured output (`StructuredOutput` tool injection for `json_schema` format).
- `resolvePromptParts` for `@file`, `@dir`, `@reference` mentions.

### M10 — Compaction (3 days)
- `filterCompacted()` ordering.
- `isOverflow()` detection.
- `compaction.create()` and `compaction.process()`.
- `prune()` post-loop.
- Summary template embedding.

### M11 — End-to-end SSE conformance pass (3 days)
- Run plan 12 conformance harness against Forge daemon.
- Fix any SSE event ordering / field shape divergences.
- Establish green baseline.

---

## Testing — Functional / Performance / Compatibility

### Functional (unit)

Each subsystem has isolated unit tests:

1. **`toModelMessages` serialiser:** fixture-driven — JSON input of `[]MessageWithParts`, assert exact `[]ModelMessage` output. Cover: compaction reordering, media extraction for non-supporting providers, pending tool states, reasoning passthrough, empty message filtering.

2. **`filterCompacted` algorithm:** 10+ scenario fixtures — no compaction, single compaction, nested compaction, compaction with tail, overflow compaction.

3. **`evaluate` permission function:** exhaustive truth table — deny wins, last-match precedence, wildcard cascade, empty ruleset defaults to `ask`.

4. **`StreamProcessor`:** mock event channel; inject scripted `[]LLMEvent`; assert DB writes and SSE emissions. Scenarios: text streaming, tool-call with result, tool-call with error, doom-loop trigger, abort mid-stream.

5. **`isOverflow` + compaction select:** verify thresholds match opencode constants.

6. **Each built-in tool:** mock FS / subprocess; verify output format and error paths.

### Integration (end-to-end)

1. **Single prompt, text-only response:** assert exact SSE event sequence: `server.connected`, `message.updated` (user), `message.updated` (assistant placeholder), `message.part.updated` (step-start), `message.part.delta` × N (text), `message.part.updated` (text complete), `message.part.updated` (step-finish), `message.updated` (assistant complete).

2. **Single prompt, one tool call:** extend (1) with tool-call lifecycle events: `message.part.updated` (tool pending → running), `permission.asked` (if `ask`), `permission.replied`, `message.part.updated` (tool completed).

3. **Multi-turn + compaction:** scripted three-turn session that exceeds token limit; assert compaction user message created, summary assistant message created, `session.compacted` SSE event emitted, subsequent turn picks up after tail.

4. **Abort mid-stream:** cancel context during streaming; assert `AbortedError` on assistant message, all pending tool parts marked `error+interrupted:true`, `message.part.updated` emitted for each.

5. **opencode client compatibility:** point an unmodified opencode web client at the Forge daemon; manually verify the chat UI renders messages correctly.

### Performance

1. **Streaming latency:** time-to-first-byte for `text-start` SSE event ≤ 50ms after LLM emits first token (Anthropic direct, same-region).
2. **Concurrent sessions:** 20 simultaneous sessions, each with a multi-step tool-call loop; assert no shared state corruption and no goroutine leaks (using `goleak`).
3. **Compaction speed:** session with 200 turns; compaction wall time ≤ 2s (excluding LLM call).
4. **Tool registry cold start:** `Filter()` ≤ 5ms for 50 tools.

### Compatibility (plan 12 conformance)

The conformance harness replays a scripted prompt against both opencode and Forge, capturing:
- Full SSE event stream (event `type` + `properties` JSON).
- SQLite rows in `messages` and `parts` tables after completion.

**Claims this plan must prove:**
1. For every `type` in the SSE stream, the shape of `properties` matches (field names, types). Missing optional fields are acceptable; extra fields are a warning, not a failure.
2. Part `type` discriminators match: `text`, `reasoning`, `tool`, `step-start`, `step-finish`, `patch`, `compaction`, `file`, `agent`, `retry`, `subtask`, `snapshot`.
3. `ToolState.status` transitions match: `pending` → `running` → `completed|error`.
4. `AssistantMessage.finish` values match: `stop`, `tool-calls`, `length`, `content-filter`, `error`.
5. `message.part.delta` events are emitted for streaming text (required by TUI/web live rendering).
6. Token counts and cost fields in `step-finish` parts are present and non-negative.

---

## Verification (Concrete)

1. **`make test-unit`** passes with ≥ 95% line coverage on packages: `engine/processor`, `engine/compaction`, `engine/registry`, `engine/permission`, `engine/message`.

2. **`make test-conformance`** (plan 12 harness): replay 5 scripted prompts (text-only, tool-call, multi-turn, abort, compaction) against both daemons; diff report shows 0 field-shape failures and ≤ 5 ordering warnings.

3. **opencode TUI `attach`:** `opencode attach http://localhost:3001` successfully renders a live streaming response from the Forge daemon with no JavaScript errors in the web client.

4. **Anthropic streaming:** `curl -N http://localhost:3001/session/$ID/prompt` (POST with prompt body) produces SSE events including `message.part.delta` for each token chunk.

5. **Permission flow:** scripted prompt that triggers `bash` without a pre-approved rule; client sees `permission.asked` SSE event; `POST /permission/$ID/reply` with `{"reply":"always"}` unblocks the tool; tool completes successfully.

6. **Compaction:** session forced into overflow by `go test -run TestCompactionFlow`; assert `session.compacted` SSE event and subsequent assistant response references the summary.

7. **Abort:** `POST /session/$ID/cancel` during an active stream returns 200 and the assistant message receives `error.name = "MessageAbortedError"`.

---

## Risks & Open Questions

| Risk | Severity | Mitigation |
|---|---|---|
| AI SDK tool execution model differences | High | opencode wraps tool results as model messages on the *next* request; our Go adapter must do the same for OpenAI. Covered by `toModelMessages` tests + conformance harness. |
| Anthropic extended thinking (signed reasoning blocks) | Medium | The `" "` separator for empty text between signed reasoning blocks must be preserved exactly (`message-v2.ts:765-768`). Unit test: signed reasoning fixture. |
| Provider-specific `ProviderMetadata` shapes | Medium | Only Anthropic `signature` field is required for correctness (thinking passthrough); others are best-effort. Flag divergences in plan 12. |
| `filterCompacted` correctness | High | The reordering is subtle (4 cases). Port TS tests to Go 1:1 before wiring into the loop. |
| Context cancellation propagation | Medium | Every DB call and HTTP call must accept `context.Context` and respect cancellation. Use `pgx`/`modernc.org/sqlite`-compatible context-aware APIs. |
| Doom-loop false positives | Low | Threshold of 3 is conservative. Can tune via config flag. |
| MCP tools not available at M2-M10 | Low | Registry has a dynamic slot; starts empty; MCP fills it in plan 03. |
| Token cost table accuracy | Low | Prices change; load from a versioned JSON file bundled with the binary; allow override via config. |
| `resolvePromptParts` LSP integration | Low | For phase B, skip LSP symbol resolution in `@file:line` mentions; fall back to raw line offset. LSP wired in plan 03. |
| Concurrent stream + permission ask | Medium | `permission.Ask` blocks the tool goroutine; ensure the streaming loop's goroutine continues consuming LLM events. The pending Deferred must be on its own goroutine, not on the event-consumer goroutine. |

---

## Links to Sibling Plans

- **Plan 00 (masterplan):** wire-compat contract, sequencing, architecture diagram.
- **Plan 01 (daemon-core):** HTTP/SSE transport, SQLite persistence, session CRUD, auth, directory routing. The agent engine calls into plan 01's `Storage` and `SSEBus` interfaces.
- **Plan 03 (ecosystem-mcp-lsp):** provides dynamic tool injection (`ToolDef` entries from MCP servers) into the registry's dynamic slot, and LSP integration for `resolvePromptParts` symbol lookup.
- **Plan 04 (ecosystem-resources):** provides `AgentInfo`, `CommandDef`, `SkillDef`, `ProviderDef` loaders that feed `compileToolMap`, `buildSystem`, and `resolveAgent`.
- **Plan 06 (sdk-generation):** Go SDK generated from `packages/sdk/openapi.json`; the agent engine's REST endpoints must match the spec exactly so generated clients work.
- **Plan 12 (test-compatibility):** conformance harness that validates the agent engine's SSE output and DB state against real opencode. All claims in the "Verification" section above are checked by plan 12 scenarios.

---

## Addendum — Build-order refinements (2026-05-29, user-approved)

This refines (does not contradict) the milestone order above; recorded per CLAUDE.md's
"update a plan only if it contradicts reality, and say so explicitly" rule.

**Decisions locked with the user:**
1. **Provider order swapped.** Build **OpenAI + OpenAI-compatible first** (was M3) because the
   OpenAI-compatible path reaches many free-tier providers (Groq, Cerebras, OpenRouter free
   models, local Ollama) and needs no Anthropic key. **Anthropic (was M2) is deferred to a new
   milestone after M9.** The system-prompt variant routing scaffold (`gpt`/`gemini`/`claude`/
   default) still lands at M8 so Anthropic is drop-in later; the Anthropic-specific extended-thinking
   signed-reasoning handling (`message-v2.ts:765-768`) rides along with that deferred milestone.
2. **LLM transport is hand-rolled.** No vendored provider SDK — a thin HTTP+SSE client POSTs to the
   provider and parses the stream directly into `LLMEvent`. Lighter binary, full control of event
   mapping, and a perfect fit for the deterministic mock-LLM test gate (we own the whole transport).
3. **Proof bar = deterministic mock LLM.** An `httptest` server replays scripted provider SSE;
   unit + integration tests assert the exact `LLMEvent → SSE → DB` sequence. No API key, no cost,
   CI-reproducible. Live dual-run vs real opencode (plan 12) is stood up but runs opportunistically
   (needs a real opencode + a shared deterministic provider).
4. **Model catalog = live models.dev fetch** with an on-disk cache (mirrors opencode) and a
   last-good offline fallback; tests inject a **checked-in JSON fixture** via a `catalog.Source`
   interface so they never touch the network.
5. **Integration suite is a living deliverable**, scaffolded at M1 (`internal/engine/enginetest`,
   with the deterministic mock provider) and grown each milestone, not a one-shot M11 step.

**Effective milestone order:** M1 → **M2 (OpenAI/OpenAI-compatible + catalog)** → M4 → M5 → M6 →
M7 → M8 → **M9 (loop wiring; first end-to-end green)** → **M3′ (Anthropic, deferred)** → M10 → M11.
