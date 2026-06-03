# Forge Plan 03 — Ecosystem: MCP + LSP

## Context

This plan covers two protocol integrations that are central to Forge's ecosystem compatibility
promise: **MCP (Model Context Protocol)** client support and **LSP (Language Server Protocol)**
client support. Both must be wire-compatible with opencode's config formats and runtime behavior
so that users migrating from opencode to Forge see identical tool surfaces, diagnostics, and
status payloads.

This plan is **Phase C** in the master sequencing (see plan 00). It depends on plan 01
(daemon core with SSE bus and instance routing) and plan 02 (agent engine and tool-loop dynamic
tool registration). It feeds plan 04 (ecosystem resource loaders) and plan 12 (conformance
harness).

Both integrations are **per-instance**: each directory/instance that the daemon serves has its
own independent set of MCP clients and LSP clients. The instance boundary is preserved throughout
the Go design below.

---

## opencode References Validated (file:line + takeaways)

### MCP

**`packages/opencode/src/config/mcp.ts` (entire file, 60 lines)**

- `Local` schema (lines 4–18): `type: "local"`, `command: string[]` (required), `environment?:
  Record<string,string>`, `enabled?: boolean`, `timeout?: PositiveInt`. No default timeout is
  encoded in the schema; the default is applied in the service layer.
- `OAuth` schema (lines 21–36): `clientId?`, `clientSecret?`, `scope?`, `callbackPort?`
  (int 1–65535), `redirectUri?`. All optional — absence triggers RFC 7591 dynamic client
  registration.
- `Remote` schema (lines 39–55): `type: "remote"`, `url: string`, `enabled?: boolean`,
  `headers?: Record<string,string>`, `oauth?: OAuth | false`, `timeout?: PositiveInt`. Setting
  `oauth: false` opts out of all OAuth handling.
- `Info = Union([Local, Remote])` — discriminated on `type` field (line 57).
- The top-level config key is `mcp: Record<string, Info>`.

**`packages/opencode/src/mcp/index.ts` (982 lines)**

- Default timeout constant: `DEFAULT_TIMEOUT = 30_000` ms (line 37), **not** 5000. The
  `timeout` field in config overrides this; the description text says "defaults to 5000" but the
  code uses 30 000. Forge must match the code, not the description text.
- Status union (lines 76–100): five variants — `connected`, `disabled`, `failed { error:string }`,
  `needs_auth`, `needs_client_registration { error:string }`. Discriminated on `status` field.
- Transport fallback order for remote servers (lines 340–356): try `StreamableHTTP` first, then
  `SSE`. If either triggers an auth error the loop breaks immediately.
- `convertMcpTool()` (lines 158–186): wraps an MCP `ToolDef` into an AI SDK `dynamicTool`.
  The schema is always forced to `type: "object"` and `additionalProperties: false`. The key
  insight for Forge: the output is a **dynamic tool with a JSON Schema** — no static Zod/Yup
  type. Go's equivalent is a struct with `json.RawMessage` for the schema (ties to plan 02).
- Tool key naming (line 697): `sanitize(clientName) + "_" + sanitize(toolName)` where
  `sanitize` (line 115) replaces all non-`[a-zA-Z0-9_-]` with `_`.
- `tools()` (lines 670–703): iterates connected clients, looks up cached `defs`, calls
  `convertMcpTool` for each, returns a flat `Record<string, Tool>`. Only clients with status
  `connected` are included.
- `prompts()` / `resources()` (lines 718–726): fetched on demand from all connected clients;
  keyed as `sanitize(clientName) + ":" + sanitize(item.name)` with a `client` field injected.
- `watch()` (lines 509–521): registers a `ToolListChangedNotification` handler on each client.
  On change, re-fetches defs and publishes `mcp.tools.changed` bus event.
- OAuth flow functions (lines 781–928): `startAuth` → `authenticate` → `finishAuth` →
  `removeAuth`. `startAuth` resolves the effective `redirectUri` from config, starts the local
  callback server at `http://127.0.0.1:19876/mcp/oauth/callback` (default), creates an
  `McpOAuthProvider`, attempts to connect; if `UnauthorizedError` is thrown it captures the
  redirect URL and returns it. `finishAuth` calls `transport.finishAuth(code)` on the pending
  transport, then re-connects. `authenticate` chains `startAuth` → opens browser → waits for
  callback code → `finishAuth`.
- `add()` (line 650): adds a new MCP entry at runtime and immediately connects it.
- `connect()` / `disconnect()` (lines 657–668): force-enable or close an existing entry.
- Tool listing error tolerance (lines 128–155): if `tools/list` fails with an output-schema
  validation error, retries with a `TolerantListToolsResultSchema` that omits `outputSchema`.

**`packages/opencode/src/server/routes/instance/httpapi/groups/mcp.ts` (157 lines)**

HTTP API endpoints exposed under `/mcp`:
- `GET /mcp` → `status()` — returns `Record<string, Status>`
- `POST /mcp` (payload `{name, config}`) → `add()`
- `POST /mcp/:name/auth` → `startAuth()` — returns `{authorizationUrl, oauthState}`
- `POST /mcp/:name/auth/callback` (payload `{code}`) → `finishAuth()` — returns `Status`
- `POST /mcp/:name/auth/authenticate` → `authenticate()` — returns `Status`
- `DELETE /mcp/:name/auth` → `removeAuth()`
- `POST /mcp/:name/connect` → `connect()`
- `POST /mcp/:name/disconnect` → `disconnect()`

**`packages/opencode/src/server/routes/instance/httpapi/handlers/mcp.ts` (112 lines)**

Handler wiring: maps the above group endpoints to `MCP.Service` calls, wrapping
`NotFoundError` → `McpServerNotFoundError` (HTTP 404), `UnsupportedOAuthError` (HTTP 400).

**`packages/opencode/src/session/tools.ts` (line 118)**

```ts
for (const [key, item] of Object.entries(yield* mcp.tools())) {
```
MCP tools are merged into the session tool map unconditionally (subject to the same permission
gate as built-in tools). The `inputSchema` is potentially transformed by `ProviderTransform`
(provider-specific schema massaging) before being forwarded to the model. This is the exact
injection point Forge must replicate in plan 02's tool-loop.

**`packages/opencode/src/session/prompt.ts` (lines 1387–1401)**

`SessionTools.resolve(...)` is called inside the agent loop with `MCP.Service` provided. The
returned flat tool map (built-ins + MCP) is passed directly to the LLM streaming call at
line 1452.

### LSP

**`packages/opencode/src/config/lsp.ts` (44 lines)**

- `Info = boolean | Record<string, Entry>` (line 39).
- `true` means "enable all built-in servers with defaults"; `false`/missing disables all LSP.
- `Entry = Disabled | { command: string[], extensions?: string[], disabled?: boolean,
  env?: Record<string,string>, initialization?: Record<string,unknown> }`.
- For custom (non-built-in) servers, `extensions` is required (validated at lines 26–37).
- Built-in server IDs are determined by enumerating `Object.values(LSPServer)` — i.e., the
  exported server objects in `server.ts`.

**`packages/opencode/src/lsp/server.ts` (2065 lines) — full built-in server table**

Every built-in server implements `interface Info { id, extensions[], global?, root(file,ctx),
spawn(root,ctx,flags) }`. The `root()` function walks up the directory tree looking for project
marker files, using `NearestRoot()` (lines 34–56). Complete server table:

| id | extensions | root markers | binary |
|----|-----------|--------------|--------|
| `deno` | .ts .tsx .js .jsx .mjs | deno.json / deno.jsonc | `deno lsp` |
| `typescript` | .ts .tsx .js .jsx .mjs .cjs .mts .cts | package-lock/bun/pnpm/yarn (excl. deno.json) | `typescript-language-server --stdio` |
| `vue` | .vue | package lock files | `vue-language-server --stdio` |
| `eslint` | .ts .tsx .js .jsx .mjs .cjs .mts .cts .vue | package lock files | vscode-eslint server (downloaded) |
| `oxlint` | .ts .tsx .js .jsx .mjs .cjs .mts .cts .vue .astro .svelte | .oxlintrc / package lock / package.json | `oxlint --lsp` or `oxc_language_server` |
| `biome` | .ts .tsx .js .jsx .mjs .cjs .mts .cts .json .jsonc .vue .astro .svelte .css .graphql .gql .html | biome.json / package lock | `biome lsp-proxy --stdio` |
| `gopls` | .go | go.work then go.mod/go.sum | `gopls` (auto-installed via `go install`) |
| `ruby-lsp` | .rb .rake .gemspec .ru | Gemfile | `rubocop --lsp` |
| `ty` | .py .pyi | pyproject.toml / ty.toml / setup.py etc. | `ty server` (flag-gated: `experimentalLspTy`) |
| `pyright` | .py .pyi | pyproject.toml / setup.py etc. | `pyright-langserver --stdio` |
| `elixir-ls` | .ex .exs | mix.exs / mix.lock | elixir-ls (downloaded) |
| `zls` | .zig .zon | build.zig | `zls` (downloaded from GitHub) |
| `csharp` | .cs .csx | .slnx/.sln/.csproj/global.json | `roslyn-language-server --stdio --autoLoadProjects` |
| `razor` | .razor .cshtml | .slnx/.sln/.csproj/global.json | roslyn + VS Code Razor extension |
| `fsharp` | .fs .fsi .fsx .fsscript | .slnx/.sln/.fsproj/global.json | `fsautocomplete` (via dotnet tool) |
| `sourcekit-lsp` | .swift .objc .objcpp | Package.swift / *.xcodeproj / *.xcworkspace | `sourcekit-lsp` or `xcrun sourcekit-lsp` |
| `rust` | .rs | Cargo.toml (walks to workspace root) | `rust-analyzer` |
| `clangd` | .c .cpp .cc .cxx .c++ .h .hpp .hh .hxx .h++ | compile_commands.json / compile_flags.txt / .clangd | `clangd --background-index --clang-tidy` |
| `svelte` | .svelte | package lock files | `svelteserver --stdio` |
| `astro` | .astro | package lock files | `astro-ls --stdio` (requires tsserver) |
| `jdtls` | .java | pom.xml / build.gradle / gradlew / settings.gradle | Java JDTLS (downloaded from Eclipse) |
| `kotlin-ls` | .kt .kts | settings.gradle.kts / gradlew / build.gradle / pom.xml | `kotlin-lsp.sh --stdio` (downloaded) |
| `yaml-ls` | .yaml .yml | package lock files | `yaml-language-server --stdio` |
| `lua-ls` | .lua | .luarc.json / .luacheckrc / .stylua.toml etc. | `lua-language-server` (downloaded) |
| `php intelephense` | .php | composer.json / .php-version | `intelephense --stdio` |
| `prisma` | .prisma | schema.prisma / prisma/ | `prisma language-server` |
| `dart` | .dart | pubspec.yaml / analysis_options.yaml | `dart language-server --lsp` |
| `ocaml-lsp` | .ml .mli | dune-project / dune-workspace / .merlin / opam | `ocamllsp` |
| `bash` | .sh .bash .zsh .ksh | always ctx.directory | `bash-language-server start` |
| `terraform` | .tf .tfvars | .terraform.lock.hcl / terraform.tfstate / *.tf | `terraform-ls serve` |
| `texlab` | .tex .bib | .latexmkrc / texlabroot | `texlab` |
| `dockerfile` | .dockerfile Dockerfile | always ctx.directory | `docker-langserver --stdio` |
| `gleam` | .gleam | gleam.toml | `gleam lsp` |
| `clojure-lsp` | .clj .cljs .cljc .edn | deps.edn / project.clj / shadow-cljs.edn etc. | `clojure-lsp listen` |
| `nixd` | .nix | flake.nix then worktree then ctx.directory | `nixd` |
| `tinymist` | .typ .typc | typst.toml | `tinymist` (downloaded) |
| `haskell-language-server` | .hs .lhs | stack.yaml / cabal.project / hie.yaml / *.cabal | `haskell-language-server-wrapper --lsp` |
| `julials` | .jl | Project.toml / Manifest.toml / *.jl | `julia -e "using LanguageServer; runserver()"` |

Two servers are mutually exclusive based on the `experimentalLspTy` flag (lines 101–112 of
`lsp.ts`): when `ty` is enabled, `pyright` is removed from the active set.

**`packages/opencode/src/lsp/lsp.ts` (508 lines)**

- `Event.Updated` (line 20): bus event `lsp.updated` with empty payload — emitted after a new
  client spawns successfully (line 294).
- `Status` schema (lines 53–59): `{ id, name, root: string, status: "connected"|"error" }`.
- State (lines 116–121): `clients: LSPClient.Info[]`, `servers: Record<string,LSPServer.Info>`,
  `broken: Set<string>` (tracks failed spawn keys), `spawning: Map<string, Promise<...>>`.
- `getClients(file)` (lines 211–298): **lazy spawn** — for each server whose `extensions`
  matches the file's extension, calls `server.root(file,ctx)`, checks `broken` set, checks
  existing clients, if none found calls `server.spawn()` and stores the result. Uses
  `spawning` map to deduplicate concurrent requests for the same `root+serverID` key.
- `touchFile(file, mode?)` (lines 346–366): calls `getClients` (triggering lazy spawn), then
  `client.notify.open()` on each, optionally waiting for diagnostics.
- `diagnostics()` (lines 368–379): aggregates all clients' `client.diagnostics` maps.
- Full set of query methods: `hover`, `definition`, `references`, `implementation`,
  `documentSymbol`, `workspaceSymbol`, `prepareCallHierarchy`, `incomingCalls`, `outgoingCalls`.
  All delegate to `run(file, fn)` which calls `getClients` first.
- `hasClients(file)` (lines 330–344): checks if any server matches the file's extension
  without spawning. Used by the LSP tool before attempting operations.

**`packages/opencode/src/lsp/client.ts` (708 lines)**

- Uses `vscode-jsonrpc/node` (`createMessageConnection`, `StreamMessageReader`,
  `StreamMessageWriter`) over stdin/stdout of the spawned process.
- Timeout constants: `INITIALIZE_TIMEOUT_MS = 45_000`; `DIAGNOSTICS_DOCUMENT_WAIT_TIMEOUT_MS =
  5_000`; `DIAGNOSTICS_FULL_WAIT_TIMEOUT_MS = 10_000`; `DIAGNOSTICS_REQUEST_TIMEOUT_MS = 3_000`;
  `DIAGNOSTICS_DEBOUNCE_MS = 150`.
- Initialize handshake (lines 249–294): sends `initialize` with capabilities advertising
  `textDocument/diagnostic`, `workspace/configuration`, `didChangeWatchedFiles`, etc. Then
  sends `initialized` and `workspace/didChangeConfiguration` with `initialization` settings.
- Supports both push diagnostics (`textDocument/publishDiagnostics`) and pull diagnostics
  (`textDocument/diagnostic` + `workspace/diagnostic` requests).
- Incremental sync: checks `capabilities.textDocumentSync`; if kind is 2 (`INCREMENTAL`),
  sends range-based `textDocument/didChange`.
- `diagnostics` getter (lines 671–677): merges push and pull maps, deduplicates by
  `{code, severity, message, source, range}`.

**`packages/opencode/src/tool/lsp.ts` (113 lines)**

- Tool name: `"lsp"` (fixed, not per-server).
- Parameters: `operation` (one of 9), `filePath`, `line` (1-based), `character` (1-based),
  `query?`.
- Before executing: calls `lsp.hasClients(file)` (fast check, no spawn), then
  `lsp.touchFile(file, "document")` (triggers lazy spawn and waits for document diagnostics).
- Converts 1-based line/character from the tool call to 0-based for LSP wire protocol
  (line 64: `line: args.line - 1, character: args.character - 1`).
- `workspaceSymbol` uses `args.query ?? ""` — empty string requests all symbols.
- Return: `{ title, metadata: { result }, output: JSON.stringify(result) }` or a "no results"
  message.

---

## MCP Design

### Config Schema Parity

The Go config struct mirrors the opencode schema exactly:

```go
// internal/config/mcp.go

type MCPOAuth struct {
    ClientID     *string `json:"clientId,omitempty"`
    ClientSecret *string `json:"clientSecret,omitempty"`
    Scope        *string `json:"scope,omitempty"`
    CallbackPort *int    `json:"callbackPort,omitempty"` // 1–65535
    RedirectURI  *string `json:"redirectUri,omitempty"`
}

type MCPLocal struct {
    Type        string            `json:"type"` // "local"
    Command     []string          `json:"command"`
    Environment map[string]string `json:"environment,omitempty"`
    Enabled     *bool             `json:"enabled,omitempty"`
    TimeoutMS   *int              `json:"timeout,omitempty"`
}

type MCPRemote struct {
    Type      string            `json:"type"` // "remote"
    URL       string            `json:"url"`
    Enabled   *bool             `json:"enabled,omitempty"`
    Headers   map[string]string `json:"headers,omitempty"`
    OAuth     MCPOAuthField     `json:"oauth,omitempty"` // *MCPOAuth | false | absent
    TimeoutMS *int              `json:"timeout,omitempty"`
}

// MCPOAuthField is a custom JSON type: absent=auto-detect, false=disabled, object=config
type MCPOAuthField struct {
    Disabled bool
    Config   *MCPOAuth
}
```

`MCPOAuthField` requires a custom `UnmarshalJSON` that handles `false` (literal boolean),
`null`/absent, and object. This exact three-way discriminator matches opencode's
`Schema.Union([OAuth, Schema.Literal(false)])` at `config/mcp.ts:48`.

Top-level config entry: `mcp map[string]json.RawMessage` — each entry is decoded as
`MCPLocal` or `MCPRemote` based on the `type` field after initial unmarshal.

Default timeout: **30 000 ms** matching `DEFAULT_TIMEOUT = 30_000` in `mcp/index.ts:37`.

### MCP Status

```go
type MCPStatus struct {
    Status string  `json:"status"` // connected|disabled|failed|needs_auth|needs_client_registration
    Error  *string `json:"error,omitempty"`
}
```

The five status strings must be exact (they appear in the OpenAPI spec and SSE payloads).

### Transport Selection and Go SDK Choice

**Library decision: `github.com/mark3labs/mcp-go`**

Evaluation:

| Criterion | `github.com/modelcontextprotocol/go-sdk` | `github.com/mark3labs/mcp-go` |
|-----------|------------------------------------------|-------------------------------|
| Provenance | Official MCP org repo | Community; most widely used Go MCP lib |
| Transports | stdio + streamable-http (in progress) | stdio + SSE + streamable-http |
| OAuth support | None as of mid-2025 | None (must roll custom) |
| Maturity | Unstable / pre-alpha API | Stable enough; used in production tooling |
| SSE client | Not yet | Yes (`sse.NewClient`) |
| Maintenance | Expected to stabilize | Active |

**Recommendation: `mark3labs/mcp-go`** for its complete transport coverage. The official SDK
(`modelcontextprotocol/go-sdk`) is tracked and should be switched to once it reaches stable
state, as it will have the canonical `tools/list` / `tools/call` semantics by spec. If neither
provides OAuth, Forge implements OAuth client-side (see below).

For the actual JSON-RPC framing, `mark3labs/mcp-go` handles the MCP protocol framing;
Forge wraps it in a per-server `MCPConn` abstraction:

```go
// internal/mcp/client.go
type MCPConn struct {
    Name      string
    Config    config.MCPInfo  // local or remote union
    client    *mcp.Client     // mark3labs
    defs      []mcp.Tool      // cached from tools/list
    status    MCPStatus
    mu        sync.RWMutex
    timeout   time.Duration
    cancel    context.CancelFunc
}
```

**Stdio transport** — `exec.Cmd` with `StdinPipe`/`StdoutPipe`, cwd set to instance directory,
env merged from parent + `environment` config, then passed to `mark3labs/mcp-go`'s stdio client.

**Streamable-HTTP transport** — `mcp.NewStreamableHTTPClient(url, opts)` from `mark3labs/mcp-go`.
Fallback to SSE transport if streamable-HTTP returns non-2xx on connect attempt
(replicating `mcp/index.ts:340–413` fallback logic).

**SSE transport** — `mcp.NewSSEMCPClient(url, opts)` from `mark3labs/mcp-go`.

Transport fallback order for remote: try Streamable-HTTP, on failure (or explicit `401`) try
SSE, break on any auth error as opencode does.

### OAuth Flow

Neither Go MCP library provides OAuth. Forge implements it natively:

```go
// internal/mcp/oauth.go
type OAuthFlow struct {
    ServerName   string
    ServerURL    string
    ClientID     string  // from config or obtained via RFC 7591 DCR
    ClientSecret string
    Scope        string
    RedirectURI  string  // default http://127.0.0.1:19876/mcp/oauth/callback
    // ... pkce verifier, state, token store
}
```

Steps mirror `mcp/index.ts:781–928`:

1. `StartAuth(name)` — resolve `redirectURI` (explicit > callbackPort shorthand > default),
   start local HTTP callback server on the configured port, generate PKCE code verifier +
   challenge, begin `golang.org/x/oauth2` authorization URL construction. Store `oauthState`
   in the instance's KV store (SQLite). Return `{authorizationUrl, oauthState}`.

2. `Authenticate(name)` — calls `StartAuth`, if not a headless context attempts to open a
   browser via `os/exec` (`xdg-open` / `open` / `start`). If browser fails, publishes
   `mcp.browser.open.failed` bus event (clients can surface the URL). Waits for OAuth callback.

3. `FinishAuth(name, code)` — exchanges code for tokens using `golang.org/x/oauth2`
   (`AuthCodeOption` with PKCE verifier), stores tokens in SQLite (encrypted at rest via
   plan 01's secret store), calls `createAndStore` to reconnect with the new tokens.

4. `RemoveAuth(name)` — deletes tokens from store, cancels any pending callback.

**Headless daemon consideration**: On a remote headless daemon, `Authenticate` cannot open a
browser. The flow must support a client-driven mode: `StartAuth` returns the URL to the HTTP
client; the end-user opens it on their device; the OAuth callback server still runs on the
daemon (reachable only if the daemon's callback port is forwarded or if the redirect URI points
to a client-side URL). The `callbackPort`/`redirectUri` config fields handle this. This is an
**open risk** — see Risks section.

Token storage: `~/.config/forge/mcp-auth/<server-name>.json` (or equivalent XDG path),
mirroring `McpAuth` storage in opencode (`packages/opencode/src/mcp/auth.ts`).

### Tool and Prompt/Resource Surfacing

**Tool conversion (`convertMcpTool` equivalent)**

```go
// internal/mcp/tool.go
func (c *MCPConn) AsDynamicTools() []tools.DynamicTool {
    // for each def in c.defs:
    // - force schema to {"type":"object", "additionalProperties":false, ...spread}
    // - key = sanitize(c.Name) + "_" + sanitize(def.Name)
    // - execute = func(args) → c.client.CallTool(def.Name, args, timeout)
}
```

`DynamicTool` (defined in plan 02) carries a `json.RawMessage` JSON schema; no compile-time
type safety needed, matching opencode's `dynamicTool({ inputSchema: jsonSchema(schema) })`.

**Tool naming**: sanitize replaces `[^a-zA-Z0-9_-]` with `_` for both client name and tool
name, then joins with `_`. This must be identical to opencode's `sanitize` at
`mcp/index.ts:115`.

**ToolListChanged notifications**: register a handler on the underlying MCP client connection.
On notification: re-fetch `tools/list`, update `defs`, publish `mcp.tools.changed` bus event
(the agent loop re-queries tools before each LLM call).

**Prompts and Resources**: fetched on demand via `MCP.prompts()` / `MCP.resources()`. These
are surfaced through the `/mcp` HTTP endpoints and injected into the prompt context by the
agent engine (plan 02). Keys: `sanitize(clientName) + ":" + sanitize(itemName)` with a
`client` field.

**Merging into the tool loop (ties to plan 02)**

In the agent engine's `SessionTools.Resolve()` equivalent:

```go
mcpTools, _ := instance.MCP.Tools(ctx)
for key, tool := range mcpTools {
    allTools[key] = tool
}
```

This is the Go analog of `session/tools.ts:118`.

### HTTP Route Layer

All routes are per-instance (require `x-opencode-directory` routing middleware):

| Method | Path | Handler |
|--------|------|---------|
| GET | `/mcp` | `MCPHandler.Status` |
| POST | `/mcp` | `MCPHandler.Add` |
| POST | `/mcp/:name/auth` | `MCPHandler.AuthStart` |
| POST | `/mcp/:name/auth/callback` | `MCPHandler.AuthCallback` |
| POST | `/mcp/:name/auth/authenticate` | `MCPHandler.AuthAuthenticate` |
| DELETE | `/mcp/:name/auth` | `MCPHandler.AuthRemove` |
| POST | `/mcp/:name/connect` | `MCPHandler.Connect` |
| POST | `/mcp/:name/disconnect` | `MCPHandler.Disconnect` |

Error responses: `404` with `{ name, message }` for unknown server name; `400` for
`UnsupportedOAuthError`.

---

## LSP Design

### Config Parity

```go
// internal/config/lsp.go

type LSPEntryDisabled struct {
    Disabled bool `json:"disabled"` // must be true
}

type LSPEntryCustom struct {
    Command        []string          `json:"command"`
    Extensions     []string          `json:"extensions,omitempty"`
    Disabled       *bool             `json:"disabled,omitempty"`
    Env            map[string]string `json:"env,omitempty"`
    Initialization map[string]any    `json:"initialization,omitempty"`
}

// LSPConfig is bool | map[string]Entry
// Represented as a tagged union with custom JSON decode
type LSPConfig struct {
    Enabled bool                      // true if top-level bool = true
    Servers map[string]LSPEntryCustom // nil if Enabled=false (bool=false or absent)
}
```

`true` → all built-in servers active; `false`/absent → no LSP; object → override/extend
built-ins. For custom entries, `extensions` is required (validation at parse time, matching
`config/lsp.ts:26–37`).

### Built-in Server Table Port

Port the complete 35-server table to Go. Each server is a `LSPServerDef` struct:

```go
// internal/lsp/server.go
type LSPServerDef struct {
    ID         string
    Extensions []string
    Root       func(file string, instanceDir string, worktree string) (string, error)
    Spawn      func(root string, instanceDir string, flags RuntimeFlags) (*LSPHandle, error)
}

type LSPHandle struct {
    Cmd           *exec.Cmd
    Initialization map[string]any
}
```

`NearestRoot(targets []string, excludeTargets []string)` becomes a Go function that walks up
from `filepath.Dir(file)` to `instanceDir`, checking for target files, optionally aborting if
exclusion files appear first. This directly ports `server.ts:34–56`.

**Auto-download servers**: several servers (gopls, clangd, zls, elixir-ls, JDTLS, kotlin-ls,
etc.) have download logic. In Forge, the download flag is controlled by a config option
`lsp.disableAutoInstall` (maps to opencode's `RuntimeFlags.disableLspDownload`). Downloads use
Go's `net/http` and archive extraction via `archive/zip` + `compress/gzip` + `archive/tar`.

The mutually exclusive `ty`/`pyright` logic is replicated: if `experimentalLspTy` runtime flag
is set, `pyright` is removed from the active server map and `ty` is retained (default: `ty`
is removed, `pyright` retained).

### JSON-RPC Library Choice

**Library decision: `go.lsp.dev/jsonrpc2` + `go.lsp.dev/protocol`**

`go.lsp.dev/jsonrpc2` (`golang.org/x/tools/internal/jsonrpc2` published under the `go.lsp.dev`
namespace) is the reference Go jsonrpc2 implementation, actively maintained by the Go team as
part of `gopls`. It supports stdio framing (Content-Length headers), connection lifecycle,
request/notification/cancel semantics, and bidirectional handlers.

`go.lsp.dev/protocol` provides the complete set of LSP type definitions matching the LSP
3.17 spec, including `InitializeParams`, `ServerCapabilities`, `TextDocumentSyncKind`,
`Diagnostic`, `Position`, `Range`, `Location`, `DocumentSymbol`, `WorkspaceSymbol`,
`CallHierarchyItem`, etc.

Alternative considered: `github.com/sourcegraph/jsonrpc2` — also a solid option but less
aligned with the LSP protocol types. `go.lsp.dev` is preferred for type completeness.

```go
// internal/lsp/client.go
import (
    "go.lsp.dev/jsonrpc2"
    "go.lsp.dev/protocol"
)

type LSPClient struct {
    ServerID   string
    Root       string
    conn       jsonrpc2.Conn
    serverCaps *protocol.ServerCapabilities
    // push and pull diagnostic maps
    pushDiags  map[string][]protocol.Diagnostic
    pullDiags  map[string][]protocol.Diagnostic
    mu         sync.RWMutex
    // file version tracking
    files      map[string]fileState
    published  map[string]publishedState
}
```

Connection setup:
1. Spawn process via `exec.Cmd` with stdin/stdout piped.
2. Create `jsonrpc2.NewConn` using `jsonrpc2.NewStream` (Content-Length framed stdio).
3. Register notification handler for `textDocument/publishDiagnostics`.
4. Register request handlers for `workspace/configuration`, `client/registerCapability`,
   `client/unregisterCapability`, `workspace/workspaceFolders`, `window/workDoneProgress/create`.
5. Send `initialize` with the capability set from `client.ts:265–289`, using 45s timeout.
6. Send `initialized` + `workspace/didChangeConfiguration`.

### Diagnostics Model

Exactly port the dual push+pull model from `client.ts`:

- **Push**: `textDocument/publishDiagnostics` notifications stored in `pushDiags`.
- **Pull (document)**: `textDocument/diagnostic` request; dispatched in parallel across all
  registered identifiers; resolves on first file-matching result (`client.ts:455–466`).
- **Pull (workspace)**: `workspace/diagnostic` request; only if dynamically registered.
- **Deduplication**: by `{code, severity, message, source, range}` — exact port of
  `dedupeDiagnostics` at `client.ts:109–123`.
- **Debounce**: 150 ms after receiving a push notification before updating the merged map
  (matching `DIAGNOSTICS_DEBOUNCE_MS = 150` at `client.ts:17`).

Timeout constants in Go:
```go
const (
    InitializeTimeoutMS          = 45_000
    DiagnosticsDocumentWaitMS    = 5_000
    DiagnosticsFullWaitMS        = 10_000
    DiagnosticsRequestTimeoutMS  = 3_000
    DiagnosticsDebounceMS        = 150
)
```

### Lazy Spawn Per Extension

The `getClients` pattern from `lsp.ts:211–298` is ported directly:

```go
// internal/lsp/service.go
type LSPState struct {
    clients  []*LSPClient
    servers  map[string]*LSPServerDef
    broken   map[string]bool       // key = root+serverID
    spawning map[string]*spawnTask // deduplication lock per root+serverID
    mu       sync.Mutex
}
```

`GetClients(file string)` logic:
1. Get file extension.
2. For each active server whose `Extensions` contains the extension (or Extensions is empty):
   a. Call `server.Root(file, instanceDir, worktree)`.
   b. If root is empty, skip. If `broken[root+id]`, skip.
   c. If already connected client exists, add to result.
   d. If `spawning[root+id]` in-flight, wait for it and use result.
   e. Otherwise: create `spawnTask`, insert into `spawning`, spawn in goroutine. After spawn,
      delete from `spawning`, push to `clients`, emit `lsp.updated` SSE event.
3. Return matching clients.

After first successful spawn for any server, publish `lsp.updated` SSE event (empty payload,
type `lsp.updated`) — this is what opencode's `Bus.publish(ctx, Event.Updated, {})` does at
`lsp.ts:294`.

### Feature Set

The `LSPService` interface in Go:

```go
type LSPService interface {
    Init(ctx context.Context) error
    Status(ctx context.Context) ([]LSPStatus, error)
    HasClients(ctx context.Context, file string) (bool, error)
    TouchFile(ctx context.Context, file string, mode DiagnosticsMode) error
    Diagnostics(ctx context.Context) (map[string][]Diagnostic, error)
    Hover(ctx context.Context, input LocInput) ([]any, error)
    Definition(ctx context.Context, input LocInput) ([]any, error)
    References(ctx context.Context, input LocInput) ([]any, error)
    Implementation(ctx context.Context, input LocInput) ([]any, error)
    DocumentSymbol(ctx context.Context, uri string) ([]any, error)
    WorkspaceSymbol(ctx context.Context, query string) ([]any, error)
    PrepareCallHierarchy(ctx context.Context, input LocInput) ([]any, error)
    IncomingCalls(ctx context.Context, input LocInput) ([]any, error)
    OutgoingCalls(ctx context.Context, input LocInput) ([]any, error)
}

type LocInput struct { File string; Line int; Character int }
type DiagnosticsMode string
const (
    DiagModDocument DiagnosticsMode = "document"
    DiagModFull     DiagnosticsMode = "full"
)
```

`workspaceSymbol` filters to symbol kinds in `{Class, Function, Method, Interface, Variable,
Constant, Struct, Enum}` and caps at 10 results per client (matching `lsp.ts:441–444`).

### LSP Tool Integration

The `lsp` built-in tool in Forge (plan 02 tool registry):

```go
// internal/tool/lsp.go
var LspTool = tools.BuiltinTool{
    Name: "lsp",
    Parameters: LspParams{...}, // operation, filePath, line, character, query?
    Execute: func(ctx ToolContext, args LspParams) (ToolResult, error) {
        // 1. Resolve absolute path
        // 2. Ask permission: { permission: "lsp", patterns: ["*"] }
        // 3. Check file exists
        // 4. lsp.HasClients(file) — fast pre-check
        // 5. lsp.TouchFile(file, "document") — lazy spawn + wait for diagnostics
        // 6. Convert args.Line/Character: 1-based → 0-based (line-1, char-1)
        // 7. Switch on operation → call lsp.Definition / References / etc.
        // 8. Return JSON result or "No results found" string
    },
}
```

Operations map exactly to `tool/lsp.ts:83–103`. The `hover` operation returns a `[]any`
(results from all matching clients), same as opencode.

### `lsp.updated` SSE Event

When any new LSP client spawns successfully, Forge publishes to the global SSE bus:
```json
{ "id": "<uuid>", "type": "lsp.updated", "properties": {} }
```
Matching `BusEvent.define("lsp.updated", Schema.Struct({}))` at `lsp.ts:20` and opencode's
SSE event catalog in plan 00's reference contract.

The `/lsp` HTTP route returns the status array from `LSPService.Status()`. Status items
include `id`, `name`, `root` (relative to instance directory), `status: "connected"|"error"`.

---

## Implementation Milestones

### Milestone M3-1: MCP Config and Connection (no OAuth)

**Deliverables:**
- `internal/config/mcp.go`: `MCPLocal`, `MCPRemote`, `MCPOAuthField` with custom
  JSON unmarshalling, default timeout = 30 000 ms.
- `internal/mcp/conn.go`: `MCPConn` struct with `Connect()`, `Disconnect()`, `Status()`,
  `Tools()`, `Prompts()`, `Resources()`.
- Stdio transport (using `mark3labs/mcp-go`): spawn subprocess, connect, `tools/list`.
- Remote transport: Streamable-HTTP with SSE fallback.
- `MCPService` (per-instance): reads `cfg.MCP`, spawns all enabled servers concurrently,
  caches `defs`, implements `mcp.tools.changed` watcher.
- HTTP routes: `GET /mcp` (status), `POST /mcp` (add), `POST /mcp/:name/connect`,
  `POST /mcp/:name/disconnect`.
- Integration with plan 02 agent tool-loop: `MCPService.Tools()` merged at `SessionTools.Resolve`.

**Acceptance:** Start a stdio MCP server (e.g., `@modelcontextprotocol/server-filesystem`),
tool appears in agent tool list with correct JSON schema.

### Milestone M3-2: MCP OAuth

**Deliverables:**
- `internal/mcp/oauth.go`: `OAuthFlow` using `golang.org/x/oauth2`, RFC 7591 dynamic client
  registration, PKCE, local callback HTTP server, token storage in SQLite.
- `MCPService.StartAuth()`, `Authenticate()`, `FinishAuth()`, `RemoveAuth()`, `HasStoredTokens()`.
- HTTP routes: `/mcp/:name/auth` (POST start, DELETE remove), `/mcp/:name/auth/callback`,
  `/mcp/:name/auth/authenticate`.
- Reconnect with stored tokens on daemon restart.

**Acceptance:** Connect to a remote MCP server that requires OAuth (e.g., a mock OAuth server
or GitHub Copilot extension); complete the flow via the HTTP API; tools appear after auth.

### Milestone M3-3: LSP Config and Built-in Server Table

**Deliverables:**
- `internal/config/lsp.go`: `LSPConfig` with custom JSON decode.
- `internal/lsp/server.go`: `LSPServerDef` + `NearestRoot()`, full 35-server table ported
  from `lsp/server.ts`.
- `internal/lsp/download.go`: download helpers for auto-installing gopls, clangd, zls,
  elixir-ls, JDTLS, kotlin-ls, LuaLS, etc. Respects `disableAutoInstall` flag.
- `internal/lsp/service.go`: `LSPState`, `GetClients()` with lazy spawn + dedup + broken-set.

**Acceptance:** Open a `.go` file, gopls spawns (or is auto-installed), `lsp.updated` SSE
event fires.

### Milestone M3-4: LSP Client — Diagnostics

**Deliverables:**
- `internal/lsp/client.go`: full JSON-RPC client using `go.lsp.dev/jsonrpc2` +
  `go.lsp.dev/protocol`. Initialize handshake, capability registration, push + pull diagnostics,
  dedup, debounce.
- `LSPService.TouchFile()`, `Diagnostics()`.
- `lsp.updated` SSE event on spawn.
- `/lsp` HTTP route returning `LSPService.Status()`.

**Acceptance:** Touch a Go file, wait for diagnostics, call `GET /lsp`; diagnostics contain
gopls errors if any.

### Milestone M3-5: LSP Query Operations + Tool

**Deliverables:**
- `LSPService`: `Hover`, `Definition`, `References`, `Implementation`, `DocumentSymbol`,
  `WorkspaceSymbol`, `PrepareCallHierarchy`, `IncomingCalls`, `OutgoingCalls`.
- `internal/tool/lsp.go`: `LspTool` with 9 operations, permission check, 1-based→0-based
  conversion, `hasClients` pre-check.
- Register `LspTool` in plan 02's `ToolRegistry`.

**Acceptance:** Agent executes `lsp(goToDefinition, main.go, 10, 5)` → returns location.

### Milestone M3-6: `lsp.updated` SSE + `mcp.tools.changed` Wiring

**Deliverables:**
- Wire `lsp.updated` into plan 01's SSE bus so all connected clients receive it.
- Wire `mcp.tools.changed` into the SSE bus.
- Ensure plan 12 conformance harness can assert these events fire at the right moments.

---

## Testing

### Functional Tests

**MCP**

1. **Stdio MCP**: start `@modelcontextprotocol/server-filesystem` (Node.js), configure it in
   Forge config, call `GET /mcp` — assert `status: "connected"`, tool count > 0, tool schemas
   valid JSON Schema objects, key format matches `sanitize(name)_sanitize(toolName)`.
2. **Remote MCP (Streamable-HTTP)**: run `mcp-proxy` in streamable-http mode locally, assert
   connect + tools surface.
3. **Remote MCP (SSE fallback)**: use a mock server that rejects streamable-http but serves SSE;
   assert fallback path taken.
4. **Disabled entry**: set `enabled: false`; assert `status: "disabled"`, tool absent from loop.
5. **ToolListChanged**: connect to a server, dynamically add a tool at the server, assert
   `mcp.tools.changed` SSE event fires and new tool appears in next tool-loop iteration.
6. **Timeout override**: set `timeout: 1` (1ms), assert tool calls fail gracefully with
   `status: "failed"` rather than panic.
7. **OAuth start/callback cycle**: mock OAuth server (via `golang.org/x/net/http2` test server);
   call `POST /mcp/:name/auth` → call `POST /mcp/:name/auth/callback` → assert `status:
   "connected"`.
8. **Tool naming sanitization**: server name `my-server.v2`, tool name `get/file` → expected key
   `my_server_v2_get_file`.

**LSP**

1. **gopls lazy spawn**: open a `.go` file via `TouchFile`; assert gopls process spawned,
   `lsp.updated` event received, `GET /lsp` shows `status: "connected"`.
2. **Diagnostics push**: introduce a syntax error; call `TouchFile(file, "full")`; assert
   `Diagnostics()` returns the error.
3. **goToDefinition**: call `LspTool` with `goToDefinition` on a known symbol; assert non-empty
   location list.
4. **findReferences**: call `LspTool` with `findReferences`; assert references returned.
5. **workspaceSymbol**: call with `query: ""`, assert symbols including `Class` and `Function`
   kinds, capped at 10 per client.
6. **Custom server entry**: configure a custom server `{ command: ["bash-language-server",
   "start"], extensions: [".sh"] }`; open `.sh` file; assert server spawns.
7. **Disabled built-in**: set `lsp: { gopls: { disabled: true } }`; open `.go` file; assert
   gopls NOT spawned.
8. **Extension isolation**: open a `.py` file on a repo with gopls configured; assert gopls
   does NOT spawn for `.py`.
9. **1-based conversion**: assert `LspTool(goToDefinition, file, line=1, character=1)` sends
   `{line:0, character:0}` on the wire.
10. **deduplication**: craft a scenario where two servers return the same diagnostic; assert
    dedup produces one entry.

### Performance Tests

- **MCP connect latency**: time from `MCPService.Init()` call to all configured servers
  reaching `connected` (or a stable state), with 10 stdio servers. Target: < 5s for 10 servers
  concurrently.
- **LSP spawn + diagnostics roundtrip**: time from `TouchFile(file)` call to first diagnostic
  available. Target: < 3s for gopls on a small Go package.
- **Tool-loop overhead**: 100 agent turns with 50 MCP tools registered; measure added latency
  vs. zero MCP tools. Target: < 5ms per turn overhead.

### Compatibility Tests (plan 12)

- Run the same scenario (read a directory of files, get diagnostics, execute an LSP tool) against
  both opencode and Forge; diff the tool results and SSE event sequences.
- Assert `GET /mcp` response body schema is identical between the two daemons.
- Assert `GET /lsp` status response matches opencode's format exactly.
- Assert `lsp.updated` and `mcp.tools.changed` SSE events contain identical shapes.

---

## Verification

### MCP Verification

**Sample servers to use:**
- `@modelcontextprotocol/server-filesystem` (Node.js, stdio, well-known) — use
  `npx @modelcontextprotocol/server-filesystem /tmp/testdir` as command.
- `mcp-server-fetch` (Node.js, stdio) — for a second concurrent server.
- A local mock HTTP/SSE server written in Go (in `internal/mcp/testserver_test.go`) for
  transport fallback + OAuth tests.

**Assertions:**
1. `GET /mcp` → `{ "filesystem": { "status": "connected" } }`.
2. Agent tool map contains `filesystem_read_file`, `filesystem_write_file`, etc.
3. Each tool schema has `"type": "object"` and `"additionalProperties": false`.
4. `POST /mcp/:name/disconnect` → `{ "filesystem": { "status": "disabled" } }`.
5. `POST /mcp/:name/connect` → `{ "filesystem": { "status": "connected" } }`, tools
   re-appear in tool map.
6. With `enabled: false` in config → `status: "disabled"` at startup, no process spawned.

### LSP Verification

**Servers to use:**
- `gopls` (requires Go installed — always available in the Forge dev environment).
- `typescript-language-server` (requires Node.js + `npm i -g typescript typescript-language-server`).

**Test repository:** a small Go module with at least one intentional type error and one
`.ts` file with a type error, checked into the test fixtures directory.

**Assertions:**
1. After `TouchFile("main.go", "full")`: `Diagnostics()["<abs-path>/main.go"]` is non-empty
   and contains the expected error message.
2. `LspTool(goToDefinition, "main.go", line=<L>, character=<C>)` where L/C is a known symbol:
   returns a `Location` with correct URI and range.
3. `LspTool(workspaceSymbol, "", ...)` returns at least one result of kind `Function` (12).
4. `GET /lsp` returns `[{ "id": "gopls", "name": "gopls", "root": ".", "status": "connected" }]`.
5. SSE stream on `/event` receives `{ "type": "lsp.updated", "properties": {} }` within 5s of
   the first `TouchFile` call.
6. After introducing a TypeScript error and calling `TouchFile("index.ts", "full")`:
   `Diagnostics()["<abs-path>/index.ts"]` contains the error.

---

## Risks & Open Questions

### MCP

1. **`mark3labs/mcp-go` transport completeness**: As of mid-2025, the library's streamable-HTTP
   client may be partial or use non-standard framing for some servers. Mitigation: keep the
   fallback to SSE working and add integration tests with multiple real servers.

2. **Official `modelcontextprotocol/go-sdk` trajectory**: If the official SDK reaches stable
   with full transport + notification support before M3-1 is complete, switch to it. Track the
   repo's changelog; evaluate at milestone boundaries.

3. **OAuth on headless daemons**: The default `redirectUri` is `http://127.0.0.1:19876/...`
   which assumes the callback is reachable on the daemon's host. For fully remote daemons, the
   user must either port-forward 19876 or configure a public-facing `redirectUri`. There is no
   clean solution without a relay server. Document this limitation prominently; consider a
   `--oauth-callback-proxy-url` config option for a future iteration.

4. **Dynamic client registration (RFC 7591)**: Some MCP servers require `clientId` and reject
   DCR. opencode surfaces this as `needs_client_registration` status. Forge must implement the
   same fallback: attempt DCR, catch the rejection, set status accordingly, and prompt the user
   to supply `clientId` in config.

5. **Process cleanup on daemon shutdown / instance disposal**: opencode uses `pgrep -P` to find
   all descendant PIDs and SIGTERMs them (`mcp/index.ts:485–507`). Go can use
   `syscall.Kill(-pgid, syscall.SIGTERM)` if the subprocess is started in its own process group
   (`cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}`). Must handle this correctly to
   avoid zombie MCP servers.

6. **`TolerantListToolsResultSchema` fallback**: opencode retries `tools/list` without
   `outputSchema` validation on JSON schema resolution errors (`mcp/index.ts:128–155`). Forge
   must implement an equivalent: catch the JSON unmarshal error on `tools/list` response,
   retry with a schema that ignores the `outputSchema` field.

### LSP

7. **`go.lsp.dev/jsonrpc2` API stability**: `go.lsp.dev` uses the same codebase as
   `golang.org/x/tools/internal/jsonrpc2`, which has been stable for years as part of gopls.
   However, it is not explicitly versioned as a standalone library. Pin to a specific commit/tag
   in `go.mod`.

8. **Auto-download in production**: downloading language servers from GitHub releases / npm /
   dotnet tool at runtime is a significant attack surface and can fail in air-gapped
   environments. Strategy: the `disableAutoInstall` flag disables all downloads. Document
   that Forge respects the same flag opencode uses. For the initial release, only auto-install
   gopls (since Forge is a Go project and Go will always be present).

9. **Windows support — DECIDED (2026-06-03): not supported.** Linux/macOS only; Windows is out of
   scope, not "best-effort" (masterplan "Decisions locked" #5). Do not spend effort on
   `.cmd`/`.bat`/`.exe` variants or `win32` guards now. Revisit only if a Windows client is
   prioritized.

10. **LSP server versioning conflicts**: for projects that have both a `deno.json` and an
    `npm` lockfile, opencode's `Typescript` server excludes deno projects (via
    `NearestRoot(includes, excludes=["deno.json","deno.jsonc"])`) and `Deno` requires
    `deno.json`. This must be ported exactly or the two servers will both activate on Deno
    projects.

11. **`textDocument/diagnostic` pull vs push**: some servers (e.g., older rust-analyzer)
    only push diagnostics and don't support the pull protocol. The `documentPullState()` logic
    correctly gates pull requests on capability advertisement; Forge must faithfully check
    `ServerCapabilities.diagnosticProvider` and the registered capabilities before attempting
    pull.

---

## Review pass (2026-06-03) — status, in-milestone gaps, op-name ambiguity

This is the most thorough plan in the suite (the 11 risks already name the hard parts). Two things
the review adds: an honest status (M3-1 is only partially done) and one wire-compat ambiguity.

### Status vs the milestones above
- **M3-1 partially done** (#59/#62): per-instance config parse, **stdio** connect, `GET /mcp`
  status, and tool merge+dispatch into the agent loop all shipped. **Still unbuilt but listed under
  M3-1's own deliverables:** (1) **remote** Streamable-HTTP/SSE transport (currently returns
  `failed: remote MCP servers are not yet supported`); (2) the **`mcp.tools.changed` watcher**;
  (3) mutating routes — `POST /mcp` add, `/mcp/:name/connect|disconnect` — still **501**.
  Recorded in `conformance/known-divergences.json` (scenario `mcp`).
- **M3-2 (OAuth): not started.** **M3-3 → M3-6 (all LSP): not started** — `internal/lsp/` is absent.

### Compatibility gap to promote into a deliverable (not just a divergence note)
- **MCP tool calls are not permission-gated; opencode gates them.** Built-in tools go through the
  permission flow but MCP calls bypass it. This is a behavioral divergence, not a missing feature —
  add "route MCP tool execution through the same permission `evaluate`/`Ask` path as built-ins" as
  an explicit M3-1 (or M3-2) deliverable and a plan-12 assertion.

### Ambiguity: LSP tool `operation` wire enum
M3-5's deliverable list names **service methods** (`Hover`, `Definition`, `References`,
`Implementation`, …) but the agent-facing tool's `operation` parameter is a **fixed string enum**
that must match opencode exactly (`opencode/packages/opencode/src/tool/lsp.ts:11-22`):
`goToDefinition`, `findReferences`, `hover`, `workspaceSymbol`, `documentSymbol`, plus the
call-hierarchy ops. The Acceptance examples already use `goToDefinition`/`findReferences`; make the
deliverable explicit that the wire enum is opencode's strings (the Go service methods are internal
and may keep Go-idiomatic names). Getting this wrong silently breaks the agent's tool calls.

### Validation additions
- Add a conformance assertion that an MCP tool call emits `permission.asked` (once gating lands).
- Add an assertion that the `lsp` tool rejects any `operation` not in opencode's enum.
- The 11 risks are good; explicitly schedule risks #5 (process-group cleanup), #6
  (`TolerantListToolsResultSchema`), #10 (NearestRoot deno exclusion), #11 (pull-vs-push capability
  gate) as **acceptance-tested**, not just mitigations — each is a silent-failure source.

## Links to Sibling Plans

- **Plan 00** (`00-masterplan.md`): Overall architecture, Phase C sequencing; this plan is
  explicitly listed as plan 03 in the index.
- **Plan 01** (`01-daemon-core.md`): SSE bus (used for `lsp.updated` and `mcp.tools.changed`
  events), instance routing middleware, secret/token storage, auth.
- **Plan 02** (`02-agent-engine.md`): `DynamicTool` type, `SessionTools.Resolve()` injection
  point, tool-loop, `ToolRegistry` — MCP tools and the LSP built-in tool plug into this.
- **Plan 04** (`04-ecosystem-resources.md`): agents/commands/rules/providers loaders; MCP
  server entries may be loaded from per-project config files discovered by plan 04's loaders.
- **Plan 12** (`12-test-compatibility.md`): conformance harness; MCP and LSP produce SSE
  events and HTTP payloads that must be verified event-for-event against the real opencode
  daemon.
