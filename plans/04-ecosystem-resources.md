# Forge Plan 04 — Ecosystem: Agents, Commands, Rules, Skills, Providers

---

## Context

Forge must load and serve the exact same community resources that opencode loads, from the same
file paths, with the same precedence rules, so that any `.opencode/` directory that works with
opencode works identically with Forge.  This plan covers the five resource families:

| Resource   | Loaded from                                 | Consumed by                     |
|------------|---------------------------------------------|---------------------------------|
| Agents     | `.opencode/agent(s)/**/*.md` (YAML + body)  | Agent engine (plan 02)          |
| Modes      | `.opencode/mode(s)/*.md`                    | Agent engine — forced `primary` |
| Commands   | `.opencode/command(s)/**/*.md`              | `/command` REST endpoint        |
| Rules/instructions | `AGENTS.md`, `CLAUDE.md`, `CONTEXT.md` (globUp) | System-prompt builder  |
| Skills     | `{skill,skills}/**/SKILL.md` + remote URLs  | `/skill` REST endpoint          |
| Providers  | `opencode.json[c]` (`provider` key) + auth store | LLM router (plan 02)       |

MCP and LSP config live in the same JSON file but are loaded in plan 03.  Plugin discovery
is plan 05.  Transport and auth middleware are plan 01.

---

## opencode references validated (file:line + takeaways), grouped by resource type

### Agents

**`packages/opencode/src/config/agent.ts`**

- **Lines 21–50** — `AgentSchema`: YAML frontmatter fields are `model` (string, `provider/id`
  format), `variant` (string), `temperature` (float), `top_p` (float), `prompt` (string, overrides
  markdown body), `tools` (deprecated `Record<string,bool>` → rewritten to `permission`),
  `disable` (bool), `description` (string), `mode` (`"subagent"|"primary"|"all"`), `hidden`
  (bool), `options` (pass-through map), `color` (hex `#RRGGBB` or named theme token), `steps`
  (positive int), `maxSteps` (deprecated alias for `steps`), `permission` (Permission.Info).

- **Lines 52–69** — `KNOWN_KEYS` set; any unrecognised key is promoted into `options` during
  `normalize()` (lines 77–96) so unknown vendor keys round-trip.

- **Lines 98–103** — `normalize()` also translates the deprecated `tools:{name:bool}` map into
  the new `permission` shape and coalesces `steps ?? maxSteps`.

- **Lines 106–130** — `load(dir)`: glob `{agent,agents}/**/*.md` relative to `dir`; parse with
  `gray-matter`; entry name = `configEntryNameFromPath(relative(dir, file), ["agent/","agents/"])`.
  Markdown body becomes `prompt` (trimmed).

- **Lines 132–160** — `loadMode(dir)`: glob `{mode,modes}/*.md`; same parse; forces
  `mode: "primary"` on all results regardless of any frontmatter value.

**`packages/opencode/src/agent/agent.ts`**

- **Lines 129–281** — Built-in agents: `build` (primary), `plan` (primary), `general` (subagent),
  `explore` (subagent, loads `prompt/explore.txt`), `scout` (subagent, flag-gated,
  `prompt/scout.txt`), `compaction` (primary, hidden, `prompt/compaction.txt`), `title` (primary,
  hidden, `prompt/title.txt`, temperature 0.5), `summary` (primary, hidden, `prompt/summary.txt`).

- **Lines 283–310** — Merge: for each key in `cfg.agent`, if `value.disable` → delete from map;
  otherwise deep-merge fields individually; `permission` is merged with `Permission.merge()`.

- **Lines 332–356** — `list()` sorts by `default_agent` match first, then name; `defaultInfo()`
  skips subagent-mode and hidden agents.

**`packages/opencode/src/config/entry-name.ts` lines 1–19**

- Entry name = strip leading `agent/` or `agents/` prefix from relative path, then strip file
  extension. E.g. `agent/foo/bar.md` → `foo/bar`.

**`packages/opencode/src/config/markdown.ts` lines 70–96**

- Parser: `gray-matter` (npm). On YAML parse failure, applies `fallbackSanitization` (converts
  bare colon-containing values to block scalars) then retries. Hard error on second failure
  (`FrontmatterError`).

### Commands

**`packages/opencode/src/config/command.ts`**

- **Lines 14–20** — `Info` schema: `template` (string, required — from markdown body),
  `description` (optional string), `agent` (optional string — agent name), `model` (optional
  `provider/id`), `subtask` (optional bool — run as a sub-session).

- **Lines 26–55** — `load(dir)`: glob `{command,commands}/**/*.md`; entry name from
  `configEntryNameFromPath(relative, ["command/","commands/"])`. Markdown body → `template`.
  Invalid frontmatter throws `InvalidError` (config parse errors are hard in commands).

- Invocation prefix: `/` (noted in config docs; handled by session layer, not in this file).

### Instructions / Rules

**`packages/opencode/src/session/instruction.ts`**

- **Lines 14–18** — Priority file list: `["AGENTS.md", ...(!disableClaudeCodePrompt ?
  ["CLAUDE.md"] : []), "CONTEXT.md"]`. `CONTEXT.md` is deprecated but still read.

- **Lines 63–66** — Global candidates: `~/.config/opencode/AGENTS.md` and (if not disabled)
  `~/.claude/CLAUDE.md`.

- **Lines 109–151** — `systemPaths()`: (1) global files — first existing one wins (break);
  (2) project-level — for each file in priority list, `fs.findUp(file, cwd, worktree)` — first
  list with at least one match wins, all matches in that list are added; (3) `config.instructions`
  array — local paths (absolute or `~/`-prefixed) are globbed; HTTP(S) URLs are deferred to fetch.

- **Lines 154–168** — `system()`: reads all path-based files, fetches all URL-based instructions,
  formats each as `"Instructions from: <path/url>\n<content>"`.

- **Lines 178–220** — `resolve()`: when the agent reads a file during a session, walks upward
  from that file's directory, attaches any un-claimed instruction files encountered (stops at
  worktree root). Each file is claimed once per `messageID` to avoid duplication.

- **Line 57–58** — `instructions` array in config is **concatenated** (deduped union), not
  replaced, when configs are merged (see `mergeConfigConcatArrays` in config.ts lines 55–61).

### Skills

**`packages/opencode/src/skill/index.ts`**

- **Lines 22–25** — Scan patterns: `EXTERNAL_SKILL_PATTERN = "skills/**/SKILL.md"` (for
  `.claude/` and `.agents/` external dirs), `OPENCODE_SKILL_PATTERN = "{skill,skills}/**/SKILL.md"`
  (for `.opencode/` dirs), `SKILL_PATTERN = "**/SKILL.md"` (for explicit paths and remote cache).

- **Lines 173–233** — `discoverSkills()`: order: (a) global `~/.claude/skills/**/SKILL.md` and
  `~/.agents/skills/**/SKILL.md` (flag-controlled); (b) project upward-walk for `.claude/` and
  `.agents/` dirs, scan each; (c) all `.opencode/` config directories scanned with opencode
  pattern; (d) explicit `config.skills.paths[]` dirs scanned; (e) remote URLs from
  `config.skills.urls[]` — delegated to `Discovery.pull()`.

- **Lines 270–281** — Built-in `customize-opencode` skill registered first; a user-disk skill
  with the same name can override it.

- **Lines 36–41** — `Info` type: `{ name, description?, location, content }`. `content` is
  the markdown body (post-frontmatter). Only `name` and (optional) `description` come from
  frontmatter.

**`packages/opencode/src/skill/discovery.ts`**

- **Lines 54–103** — `Discovery.pull(url)`: GETs `{url}/index.json` which must decode as
  `{ skills: [{ name, files: string[] }] }`; filters to skills whose `files` contains `SKILL.md`;
  downloads each file of each skill into `~/.cache/opencode/skills/{skill.name}/`; returns list
  of local dirs.

**`packages/opencode/src/config/skills.ts` lines 1–14** — Config schema: `skills.paths[]`
(additional local dirs), `skills.urls[]` (remote skill servers).

### Providers and Models

**`packages/opencode/src/config/provider.ts`**

- **Lines 5–71** — `Model` schema: `id`, `name`, `family`, `release_date`, `attachment`,
  `reasoning`, `temperature`, `tool_call`, `interleaved` (bool or `{field:...}`), `cost`
  (`{input, output, cache_read?, cache_write?, context_over_200k?}`), `limit`
  (`{context, input?, output}`), `modalities` (`{input?, output?}` arrays of media type strings),
  `experimental`, `status`, `provider` (`{npm?, api?}`), `options`, `headers`, `variants`
  (map of variant-id → `{disabled?, ...extras}`).

- **Lines 73–118** — `Info` (provider config) schema: `api` (base URL string), `name`,
  `env` (env var names to search for API key), `id`, `npm` (package name), `whitelist`/`blacklist`
  (model ID glob lists), `options` (`{apiKey?, baseURL?, enterpriseUrl?, setCacheKey?,
  timeout? (int|false), headerTimeout? (int|false), chunkTimeout? (int)}`), `models`
  (map of model-id → `Model`).

**`packages/llm/src/providers/index.ts` lines 1–12** — Built-in providers: `anthropic`,
`amazon-bedrock`, `azure`, `cloudflare`, `github-copilot`, `google`, `openai`,
`openai-compatible`, `openrouter`, `xai`.

### Auth Store

**`packages/opencode/src/auth/index.ts`**

- **Lines 13–34** — Three auth record types: `Oauth` (`{type:"oauth", refresh, access, expires,
  accountId?, enterpriseUrl?}`), `Api` (`{type:"api", key, metadata?}`), `WellKnown`
  (`{type:"wellknown", key, token}`).

- **Lines 8–9** — Storage: `~/.local/share/opencode/auth.json` (mode 0600). Env override:
  `OPENCODE_AUTH_CONTENT` (JSON string).

- **Lines 57–92** — `get(providerID)` / `all()` / `set(key, info)` / `remove(key)`.

### Config Precedence and Merge

**`packages/opencode/src/config/config.ts`**

- **Lines 51–61** — `mergeConfig` wraps `remeda.mergeDeep` (deep object merge, last-write-wins
  for scalars). `mergeConfigConcatArrays` additionally deduplicates and concatenates
  `instructions` arrays across layers.

- **Lines 443–476 (`loadGlobal`)** — Global layer: reads `config.json`, then `opencode.json`,
  then `opencode.jsonc` from `~/.config/opencode/`, in that order, each merged on top of the
  previous. Legacy TOML `config` file migrated to JSON on first load.

- **Lines 510–795 (`loadInstanceState`)** — Full precedence stack (lowest to highest):
  1. WellKnown-auth remote config (fetched from `{url}/.well-known/opencode` for each
     `wellknown` auth entry).
  2. Global config (`~/.config/opencode/opencode.json[c]`).
  3. `OPENCODE_CONFIG` env-var file (if set).
  4. Project config files — `opencode.json[c]` found by `ConfigPaths.files()` walking up from
     cwd to worktree root (results reversed so nearest file wins over ancestors).
  5. `.opencode/opencode.json[c]` in each directory from `ConfigPaths.directories()` (walks up).
  6. `OPENCODE_CONFIG_CONTENT` env var (inline JSON, treated as local scope).
  7. Active-org remote config fetched from opencode console API.
  8. Managed config dir (`ConfigManaged.managedConfigDir()`).
  9. macOS MDM managed preferences (highest priority).

- **Lines 657–659** — After JSON merge: `ConfigCommand.load(dir)`, `ConfigAgent.load(dir)`, and
  `ConfigAgent.loadMode(dir)` are called for **each** `.opencode/` directory; agent/command maps
  are deep-merged with `mergeDeep` (later dirs shadow earlier dirs for same-name entries).

- **Lines 735–742** — `mode` entries are promoted into `agent` map with `mode: "primary"`.

- **`packages/opencode/src/config/paths.ts` lines 23–41`** — `ConfigPaths.directories()`:
  `[global.config, ...upward .opencode dirs from cwd, ...upward .opencode from home, OPENCODE_CONFIG_DIR?]`.

---

## Design — file discovery + precedence (one subsection per resource type)

### 2.1 Agents

**Scan roots:** same ordered list produced by the config directory walk (see §2.6 for the
directory-walk algorithm):

```
for each dir in configDirectories(cwd, worktree):
    glob("{agent,agents}/**/*.md", cwd=dir)  → agent entries
    glob("{mode,modes}/*.md",      cwd=dir)  → mode entries (forced primary)
```

**Entry name derivation** (mirrors `configEntryNameFromPath`):

```
relativePath = filepath relative to scanRoot
strip prefix "agent/" or "agents/"
strip file extension
result = remaining path  (e.g. "foo/bar")
```

**Merge order:** built-ins first; then config-json `agent` map overlaid; then disk-loaded
agents overlaid in config-directory order (last directory = lowest priority, i.e. deepest
`.opencode/` nearest cwd wins). A `disable: true` frontmatter field deletes the built-in.

**Go registry:**

```go
// internal/resource/agent/registry.go
type AgentInfo struct {
    Name        string
    Description string
    Mode        string   // "subagent" | "primary" | "all"
    Native      bool
    Hidden      bool
    Temperature *float64
    TopP        *float64
    Color       string
    Steps       *int
    Variant     string
    ModelID     string   // "provider/model"
    Prompt      string
    Options     map[string]any
    Permission  map[string]any
}

type Registry struct {
    mu     sync.RWMutex
    agents map[string]*AgentInfo
}

func (r *Registry) Get(name string) (*AgentInfo, bool)
func (r *Registry) List() []*AgentInfo        // sorted: default_agent first, then alpha
func (r *Registry) DefaultAgent() *AgentInfo   // first visible primary non-hidden
```

### 2.2 Commands

**Scan roots:** same `configDirectories` list.

```
for each dir in configDirectories:
    glob("{command,commands}/**/*.md", cwd=dir)
```

**Entry name:** strip `command/` or `commands/` prefix, strip extension.

**Merge:** last `.opencode/` directory wins for same-name commands (nearest cwd has highest
priority). The `config.command` JSON map is merged first (lowest priority), then disk-loaded
commands overlay.

**Go registry:**

```go
// internal/resource/command/registry.go
type CommandInfo struct {
    Template    string
    Description string
    Agent       string
    ModelID     string
    Subtask     bool
}

type Registry struct {
    mu       sync.RWMutex
    commands map[string]*CommandInfo
}
```

### 2.3 Instructions / Rules

**System paths algorithm** (mirrors `systemPaths()`):

```
1. GLOBAL:
   - Try ~/.config/opencode/AGENTS.md
   - If !disableClaudeCodePrompt: try ~/.claude/CLAUDE.md
   → first existing file among these candidates is added; stop after first hit.

2. PROJECT (if !OPENCODE_DISABLE_PROJECT_CONFIG):
   - For each filename in ["AGENTS.md", "CLAUDE.md" (unless disabled), "CONTEXT.md"]:
     - findUp(filename, cwd, worktreeRoot)
     - If any paths found: add all, break (do not continue to next filename)

3. config.instructions[] entries:
   - Each HTTP(S) entry → deferred fetch list
   - Each absolute or ~/... path → glob by basename from parent dir
   - Each relative path → globUp(path, cwd, worktreeRoot)
```

**Resolve-on-read** (for the `read` tool): when the agent reads a file during a session, walk
upward from the file's directory to worktree root; attach any unclaimed AGENTS.md/CLAUDE.md
encountered. Claims are tracked per `messageID` to avoid duplicate injection.

**Go struct:**

```go
// internal/resource/instruction/service.go
type Service struct {
    globalFiles     []string  // absolute paths, checked at startup
    projectFiles    []string  // found by upward walk
    configURLs      []string  // from config.instructions
    claims          sync.Map  // map[messageID]Set<filepath>
}

func (s *Service) SystemPaths(ctx context.Context) ([]string, error)
func (s *Service) System(ctx context.Context) ([]string, error)
func (s *Service) Resolve(ctx context.Context, filepath string, msgID string) ([]InstructionFragment, error)
```

### 2.4 Skills

**Scan order** (mirrors `discoverSkills`):

```
1. If !disableExternalSkills && !disableClaudeCodeSkills:
     scan ~/.claude/skills/**/SKILL.md

2. If !disableExternalSkills:
     scan ~/.agents/skills/**/SKILL.md

3. Upward walk: collect .claude/ and .agents/ dirs from cwd to worktree;
   scan each for skills/**/SKILL.md

4. For each dir in configDirectories:
     scan {skill,skills}/**/SKILL.md

5. For each path in config.skills.paths[]:
     expand ~/..., resolve relative to cwd, scan **/SKILL.md

6. For each url in config.skills.urls[]:
     Discovery.Pull(url) → local cache dirs
     scan **/SKILL.md in each returned dir
```

**First-registered wins for name collisions** (the built-in `customize-opencode` is registered
before disk scan, but can be overridden by a disk skill with the same name — disk scan runs
after and overwrites).

**Remote skill discovery** (`Discovery.Pull`):

```
GET {url}/index.json
  → { skills: [ { name, files: ["SKILL.md", ...] } ] }
For each skill:
  download each file to ~/.cache/opencode/skills/{name}/{file}
  return local dir if SKILL.md exists
```

**Go struct:**

```go
// internal/resource/skill/registry.go
type SkillInfo struct {
    Name        string
    Description string
    Location    string  // absolute local path or "<built-in>"
    Content     string  // markdown body (post-frontmatter)
}

type Registry struct {
    mu     sync.RWMutex
    skills map[string]*SkillInfo
}
```

### 2.5 Providers and Auth

**Config-layer source:** the `provider` map from the merged config JSON (see §2.6). Each
key is a provider ID; value is `ConfigProvider.Info`.

**Auth-store source:** `~/.local/share/opencode/auth.json` (JSON, mode 0600). Env override:
`OPENCODE_AUTH_CONTENT`. Records keyed by provider ID (URL-normalized: trailing slashes
stripped). Three types: `api` (key string), `oauth` (refresh/access/expires), `wellknown`
(bearer token).

**Built-in provider registry** (compiled-in, mirrors
`packages/llm/src/providers/index.ts`): `anthropic`, `amazon-bedrock`, `azure`, `cloudflare`,
`github-copilot`, `google`, `openai`, `openai-compatible`, `openrouter`, `xai`.

**Go struct:**

```go
// internal/resource/provider/registry.go
type ModelInfo struct {
    ID           string
    Name         string
    Reasoning    bool
    ToolCall     bool
    Attachment   bool
    Temperature  bool
    Cost         *ModelCost
    Limit        *ModelLimit
    Modalities   *ModelModalities
    Options      map[string]any
    Headers      map[string]string
    Variants     map[string]map[string]any
}

type ProviderInfo struct {
    ID        string
    API       string
    Name      string
    EnvVars   []string
    NPM       string
    Whitelist []string
    Blacklist []string
    Options   ProviderOptions
    Models    map[string]*ModelInfo
}

type ProviderOptions struct {
    APIKey        string
    BaseURL       string
    EnterpriseURL string
    Timeout       *int   // nil = default; 0 = disabled
    HeaderTimeout *int
    ChunkTimeout  *int
    SetCacheKey   bool
}

type AuthRecord struct {
    Type        string  // "api" | "oauth" | "wellknown"
    Key         string  // api: key; wellknown: token
    Refresh     string  // oauth only
    Access      string  // oauth only
    Expires     int64   // oauth unix seconds
    AccountID   string
    EnterpriseURL string
    Metadata    map[string]string
}

type Registry struct {
    mu        sync.RWMutex
    providers map[string]*ProviderInfo
    auth      map[string]*AuthRecord
}

func (r *Registry) GetAuth(providerID string) (*AuthRecord, bool)
func (r *Registry) SetAuth(providerID string, a *AuthRecord) error
func (r *Registry) RemoveAuth(providerID string) error
func (r *Registry) ResolveAPIKey(providerID string) (string, bool)
```

### 2.6 Config directory walk and JSON merge

The config directory list (used by all resource loaders) is:

```
directories = unique([
    ~/.config/opencode/,
    ...upwardWalk(".opencode", cwd, worktreeRoot),   // nearest first, reversed for merge
    ...upwardWalk(".opencode", ~/, ~/),              // home-level .opencode
    OPENCODE_CONFIG_DIR (if set),
])
```

JSON config files within each directory: `opencode.json` then `opencode.jsonc`, merged
in that order (JSONC file wins over JSON when both exist in the same directory).

**Merge function:** deep-merge (last value wins for scalars; objects are recursed; arrays
replace except `instructions` which is union-deduped).

**Full precedence stack (lowest → highest):**

1. WellKnown-auth remote config
2. Global config (`~/.config/opencode/`)
3. `OPENCODE_CONFIG` file (env)
4. Project config: `opencode.json[c]` walking up from cwd (reversed so nearest wins)
5. Per-directory `.opencode/opencode.json[c]` (config-dir order)
6. `OPENCODE_CONFIG_CONTENT` env inline JSON
7. Console/org remote config
8. Managed config dir
9. macOS MDM preferences (highest)

---

## Frontmatter/markdown parsing approach (Go libs)

### Library choices

| Concern | Library | Why |
|---------|---------|-----|
| YAML frontmatter split | `github.com/adrg/frontmatter` v0.2+ | Pure Go; splits `---` fences, returns raw YAML bytes + body |
| YAML decode | `gopkg.in/yaml.v3` | Standard; strict by default; `KnownFields(false)` for the pass-through `options` map |
| JSONC parse | `github.com/tailscale/hujson` | Strips comments and trailing commas from JSONC before `encoding/json.Unmarshal` |
| File glob | `github.com/bmatcuk/doublestar/v4` | Supports `{a,b}` alternation and `**` patterns, mirrors Bun's Glob |
| HTTP client | `net/http` std + `golang.org/x/net` | Skills remote fetch; instruction URL fetch |

### Frontmatter splitter

opencode uses `gray-matter`, which has a `fallbackSanitization` path for invalid YAML
(bare colon-containing values converted to block scalars). The Go equivalent:

```go
// internal/resource/parse/frontmatter.go

// ParseMarkdown splits ---\n...\n--- YAML front-matter from the markdown body.
// On YAML parse failure it applies the same colon-sanitization that opencode's
// gray-matter fallback does, then retries.
func ParseMarkdown(raw []byte) (data map[string]any, body string, err error) {
    rest, err := frontmatter.Parse(bytes.NewReader(raw), &data)
    if err == nil {
        return data, strings.TrimSpace(string(rest)), nil
    }
    // fallback: sanitize bare colon values, retry
    sanitized := sanitizeColonValues(raw)
    rest, err2 := frontmatter.Parse(bytes.NewReader(sanitized), &data)
    if err2 != nil {
        return nil, "", fmt.Errorf("frontmatter parse: %w (original: %v)", err2, err)
    }
    return data, strings.TrimSpace(string(rest)), nil
}

// sanitizeColonValues rewrites lines whose unquoted scalar value contains ":"
// into YAML block-scalar form, mirroring opencode's fallbackSanitization in
// packages/opencode/src/config/markdown.ts:19-68.
func sanitizeColonValues(src []byte) []byte { ... }
```

### Schema validation

Rather than Effect's `Schema` DSL, Go uses plain structs with `yaml:",omitempty"` tags plus
a small validation pass:

```go
// internal/resource/agent/schema.go
type AgentFrontmatter struct {
    Model       string             `yaml:"model"`
    Variant     string             `yaml:"variant"`
    Temperature *float64           `yaml:"temperature"`
    TopP        *float64           `yaml:"top_p"`
    Prompt      string             `yaml:"prompt"`
    Tools       map[string]bool    `yaml:"tools"`       // deprecated
    Disable     bool               `yaml:"disable"`
    Description string             `yaml:"description"`
    Mode        string             `yaml:"mode"`        // "subagent"|"primary"|"all"
    Hidden      *bool              `yaml:"hidden"`
    Color       string             `yaml:"color"`
    Steps       *int               `yaml:"steps"`
    MaxSteps    *int               `yaml:"maxSteps"`    // deprecated alias
    Permission  map[string]any     `yaml:"permission"`
    Options     map[string]any     `yaml:"options"`     // explicit pass-through
    // all unknown keys are also collected here during custom decode
}
```

Unknown YAML keys are collected into `Options` during a two-pass decode: first strict
(`yaml.Decoder.KnownFields(true)` errors captured), then a `map[string]any` re-decode to
harvest unknowns; they are merged into `Options` (mirrors `normalize()` in
`packages/opencode/src/config/agent.ts:77-96`).

Deprecated `tools: {name: bool}` → `permission` translation is applied in a
`NormalizeAgent(f *AgentFrontmatter)` function mirroring the TS `normalize()`.

---

## Built-in agents/prompts (how embedded and overridable)

Built-in agent prompts live in the opencode repo at
`packages/opencode/src/agent/prompt/{explore,scout,compaction,summary,title}.txt` and
`packages/opencode/src/agent/generate.txt`.

In Forge they are embedded with Go's `//go:embed` directive:

```go
// internal/resource/agent/builtins/embed.go
package builtins

import _ "embed"

//go:embed prompts/explore.txt
var PromptExplore string

//go:embed prompts/scout.txt
var PromptScout string

//go:embed prompts/compaction.txt
var PromptCompaction string

//go:embed prompts/summary.txt
var PromptSummary string

//go:embed prompts/title.txt
var PromptTitle string
```

The prompt text files are copied verbatim from the opencode repo at generation time (see
plan 06 / Makefile target `sync-builtins`). A SHA256 manifest (`builtins/manifest.json`)
records the source commit so drift is detectable in CI.

**Built-in agent initialization** (mirrors
`packages/opencode/src/agent/agent.ts:129-281`):

```go
func DefaultAgents(userPermission map[string]any) map[string]*AgentInfo {
    defaults := buildDefaultPermissions()
    user     := permissionFromConfig(userPermission)

    return map[string]*AgentInfo{
        "build":      buildAgent(defaults, user),
        "plan":       planAgent(defaults, user),
        "general":    generalAgent(defaults, user),
        "explore":    exploreAgent(defaults, user, builtins.PromptExplore),
        "compaction": compactionAgent(defaults, user, builtins.PromptCompaction),
        "title":      titleAgent(defaults, user, builtins.PromptTitle),
        "summary":    summaryAgent(defaults, user, builtins.PromptSummary),
    }
}
```

`scout` is initialized only when the `experimentalScout` runtime flag is set.

**Override rule:** after `DefaultAgents()` is built, the config `agent` map and disk-loaded
agents are applied on top via `applyAgentConfig()`. A `disable:true` field deletes the
entry. User-supplied `prompt:` in YAML frontmatter overrides the embedded prompt string.
The built-in `native:true` flag is preserved and cannot be overridden via config.

**Built-in customize-opencode skill:**

```go
// internal/resource/skill/builtins/embed.go
//go:embed customize-opencode.md
var CustomizeOpencodeSkill string
```

Registered before disk scan; disk skill with same name overwrites it (matches
`packages/opencode/src/skill/index.ts:270-280`).

---

## Provider/model + auth config parity

### Static provider catalog

The compiled-in provider catalog (`internal/provider/catalog/`) mirrors the
`packages/llm/src/providers/` TypeScript files. Each provider is a Go struct implementing:

```go
type BuiltinProvider struct {
    ID        string
    Name      string
    EnvVars   []string        // env vars to probe for API key
    BaseURL   string
    NPM       string          // kept for wire-compat with /provider endpoint
    Models    []*ModelInfo
}
```

The catalog is code-generated from opencode's provider TypeScript sources via a
`tools/gen-providers/` Go program (part of plan 06) that reads each `.ts` file, extracts
the model list and metadata, and emits a `catalog_gen.go` file. This file is regenerated
on each opencode release.

A `//go:generate` comment in `internal/provider/catalog/catalog.go` triggers regeneration.
The generated file is committed to the repo so Forge builds without the generator at runtime.

### Config-layer provider overlay

At runtime, `provider[id]` entries from the merged config are applied on top of the static
catalog:

```go
func (r *Registry) ApplyConfig(cfg map[string]*ProviderConfigInfo) {
    for id, c := range cfg {
        p := r.getOrCreate(id)
        if c.API     != "" { p.BaseURL = c.API }
        if c.Name    != "" { p.Name    = c.Name }
        p.Options = mergeOptions(p.Options, c.Options)
        p.Whitelist = c.Whitelist
        p.Blacklist = c.Blacklist
        for mid, m := range c.Models {
            p.Models[mid] = mergeModel(p.Models[mid], m)
        }
    }
}
```

### Auth store

Stored at `~/.local/share/opencode/auth.json` (matches
`packages/opencode/src/auth/index.ts:9`). Forge reads/writes with `os.OpenFile(..., 0600)`.

```go
// internal/auth/store.go
type Store struct {
    path string
    mu   sync.Mutex
}

func (s *Store) All() (map[string]*AuthRecord, error)
func (s *Store) Get(providerID string) (*AuthRecord, error)
func (s *Store) Set(providerID string, r *AuthRecord) error
func (s *Store) Remove(providerID string) error
```

JSON serialization uses the same `type` discriminator field as the TS schema. The
`OPENCODE_AUTH_CONTENT` env var bypasses file reads (matches
`packages/opencode/src/auth/index.ts:58-61`).

**Key resolution order** (for use by the LLM router):

1. Auth store `api.key` for provider ID.
2. Provider `options.apiKey` from merged config.
3. Env var: probe each name in `provider.env[]` from catalog.

---

## Implementation milestones (ordered)

### M1 — Frontmatter / YAML infrastructure (prerequisite for all loaders)

- `internal/resource/parse/frontmatter.go`: `ParseMarkdown()` with sanitization fallback.
- `internal/resource/parse/jsonc.go`: JSONC parse via `hujson`.
- `internal/resource/parse/variable.go`: `{env:VAR}` and `{file:path}` substitution
  (mirrors `packages/opencode/src/config/variable.ts`).
- Unit tests: valid YAML, colon-in-value fallback, missing frontmatter (body only), `{env:}` and
  `{file:}` tokens.

### M2 — Config directory walk + JSON merge

- `internal/config/paths.go`: `Directories(cwd, worktree string) []string` and
  `Files(name, cwd, worktree string) []string` implementing the upward-walk algorithm.
- `internal/config/merge.go`: `MergeConfig(a, b Info) Info` and `MergeConfigConcatArrays`.
- `internal/config/loader.go`: full precedence stack loading (items 1–9 from §2.6).
- Integration test: load from a temp dir tree with global + project + `.opencode/` config,
  assert merged result.

### M3 — Agent loader and registry

- `internal/resource/agent/schema.go`: `AgentFrontmatter` struct + `NormalizeAgent()`.
- `internal/resource/agent/loader.go`: `LoadDir(dir string)` for agents and modes.
- `internal/resource/agent/builtins/`: embedded prompts + `DefaultAgents()`.
- `internal/resource/agent/registry.go`: `Registry` with `Get`, `List`, `DefaultAgent`.
- Unit tests: load `.opencode/agent/` fixtures from opencode repo; assert name derivation,
  deprecated `tools` → `permission` translation, `maxSteps` → `steps` coalescing.

### M4 — Command loader and registry

- `internal/resource/command/schema.go` + `internal/resource/command/loader.go`.
- `internal/resource/command/registry.go`.
- Unit tests: load opencode's `.opencode/command/` dir; assert names and `template` content.

### M5 — Instruction service

- `internal/resource/instruction/service.go`: `SystemPaths`, `System`, `Resolve`.
- `internal/resource/instruction/upwalk.go`: `FindUp(name, start, stop)` recursive walker.
- Unit tests: temp dir tree with AGENTS.md files at multiple levels; assert claim deduplication.

### M6 — Skill loader, registry, remote discovery

- `internal/resource/skill/schema.go`: `SkillFrontmatter` (only `name` and `description`).
- `internal/resource/skill/loader.go`: multi-root scanner.
- `internal/resource/skill/discovery.go`: `Pull(url)` — fetches `index.json`, downloads files
  to cache, returns local dirs.
- `internal/resource/skill/registry.go` + built-in skill embed.
- Unit tests: load opencode's `.opencode/skills/` dir; assert `name` and `description` parsed.
- Integration test: mock HTTP server serving `index.json` + `SKILL.md`; assert files cached.

### M7 — Provider catalog generation + registry

- `tools/gen-providers/main.go`: parses opencode TS provider files; emits `catalog_gen.go`.
- `internal/provider/catalog/catalog.go` + generated file.
- `internal/provider/registry.go`: `Registry` with config overlay (`ApplyConfig`).
- `internal/auth/store.go`: read/write `auth.json`.
- Unit tests: catalog contains all 10 built-in providers; `ApplyConfig` merges model overrides;
  key resolution order.

### M8 — REST endpoint wiring

- `/agent` endpoint: serves `Registry.List()` (matches OpenAPI spec).
- `/command` endpoint: serves `CommandRegistry.All()`.
- `/skill` endpoint: serves `SkillRegistry.All()`.
- `/provider` endpoint: serves provider list with auth status.
- `/provider/auth` endpoint: `GET` / `POST` / `DELETE` delegating to `auth.Store`.
- Integration test: point all loaders at opencode's `.opencode/` dir; call each endpoint; assert
  response shapes match the OpenAPI spec from `packages/sdk/openapi.json`.

---

## Testing — functional / performance / compatibility

### Functional tests

Each loader has a `_test.go` alongside it. Tests are table-driven with golden-file assertions.
Key cases:

- **Frontmatter edge cases:** no frontmatter (body-only files), empty body, `---` in body,
  bare colon in values (triggers sanitization), multi-document YAML (should hard-fail).
- **Agent normalization:** `tools` → `permission` rewrite; `maxSteps` → `steps`; unknown keys
  into `options`; `disable:true` removal.
- **Mode forced-primary:** a `mode/*.md` with `mode: subagent` in frontmatter still surfaces as
  `primary` after `loadMode`.
- **Entry name:** nested agents (`agent/foo/bar.md` → `foo/bar`); flat agents (`agent/foo.md`
  → `foo`); agents without prefix (`standalone.md` in an agents dir → `standalone`).
- **Precedence:** global config agent value overridden by project `.opencode/agent/*.md` with
  same name; `disable:true` removes built-in.
- **Instruction deduplication:** same file reached via two paths (e.g. both upward walk and
  explicit `instructions[]`) added only once; claim tracking prevents duplicate injection
  within a message.
- **Skill name collision:** second loaded skill with same `name` logs warning; first wins
  (built-in overridden by disk; disk first-registered wins for subsequent duplicates).
- **Auth key resolution order:** config `apiKey` wins over env var; env var wins over absent.

### Performance tests

- Load a synthetic `.opencode/` dir with 500 agent `.md` files; assert load time < 200 ms on
  a cold run.
- Skill remote pull: mock server with 100 skills each with 5 files; assert total fetch +
  write time < 5 s.
- Config merge: 9-layer stack with all fields populated; assert merge < 10 ms.

### Compatibility tests (plan 12 hook)

- Point Forge at the real opencode repo's `.opencode/` dir and `~/.config/opencode/` on the
  test machine; call Forge's `/agent`, `/command`, `/skill`, `/provider` endpoints and opencode's
  same endpoints; assert identical response bodies (modulo `native` flag which Forge may omit
  for user-defined agents).
- Run opencode's own TUI client against Forge's `/agent` endpoint; assert no client-side errors.

---

## Verification (concrete: reuse opencode's own .opencode dir as a fixture)

The opencode repository at `/Users/rotemmiz/git/opencode/.opencode/` contains:

- **Agents:** `agent/duplicate-pr.md`, `agent/triage.md`
- **Commands:** `command/ai-deps.md`, `command/changelog.md`, `command/commit.md`,
  `command/issues.md`, `command/learn.md`, `command/rmslop.md`, `command/spellcheck.md`,
  `command/translate.md`
- **Skills:** `skills/effect/SKILL.md`, `skills/improve-codebase-architecture/SKILL.md`
- **Config:** `opencode.jsonc`

A Go test in `internal/resource/integration_test.go` will:

1. Set `cwd` to `/Users/rotemmiz/git/opencode` (or a copy in `t.TempDir()`).
2. Run the full loader stack.
3. Assert:
   - `agentRegistry.Get("triage")` exists; `mode == "primary"`, `hidden == true`,
     `model == "opencode/gpt-5.4-nano"`.
   - `commandRegistry.Get("commit")` exists; `subtask == true`, template contains `git diff`.
   - `skillRegistry.Get("effect")` exists; `description` starts with `"Work with Effect"`.
   - `skillRegistry.Get("improve-codebase-architecture")` exists.
   - Built-in agents `build`, `plan`, `general`, `explore`, `compaction`, `title`, `summary`
     all present.
   - Config `provider` key from `opencode.jsonc` parses without error (it is `{}`).

This test is the primary compatibility gate for plan 04 and feeds into the plan 12
conformance harness.

---

## Risks and open questions

| # | Risk | Severity | Mitigation |
|---|------|----------|------------|
| 1 | **YAML edge cases** — `gray-matter`'s fallback sanitization may handle more edge cases than the Go port does. | Medium | Fuzz the sanitizer against the TS implementation using 1000+ random YAML-like strings; compare parse results. |
| 2 | **Double-star glob alternation** — `{agent,agents}/**/*.md` relies on `bmatcuk/doublestar` correctly handling brace expansion. Some versions have bugs with nested `**` inside braces. | Low | Pin to doublestar v4.6+; add explicit test for both `agent/` and `agents/` prefix paths. |
| 3 | **Config merge subtlety: instructions dedupe** — `mergeConfigConcatArrays` dedupes using `Set`; Go's `mergeDeep` equivalent must do the same. | Medium | Unit test: merge two configs each with overlapping + unique `instructions` entries; assert no duplicates. |
| 4 | **Precedence stack: wellknown remote config** — opencode fetches `{url}/.well-known/opencode` for each `wellknown` auth entry; the shape is `{config?, remote_config?}`. Implementing the double-fetch (index + remote pointer) may have subtle ordering effects. | Medium | Implement but gate behind feature flag; cover with integration test using a mock server. |
| 5 | **Mode vs agent key collision** — `loadMode` forces `mode: "primary"` regardless of frontmatter, but a `mode/*.md` and an `agent/*.md` with the same name would collide in the merged map. | Low | Last-write wins (agent dir processed after mode dir per config.ts:658-659). Document and test. |
| 6 | **`tools` permission translation** — opencode maps `write`/`edit`/`patch` to `permission.edit`; other tool names are passed through as-is. Ensure Go port handles the same set. | Low | Direct copy of the key list from `agent.ts:86-91`; unit test all three deprecated aliases. |
| 7 | **Skill URL caching** — opencode caches remote skills under `~/.cache/opencode/skills/`; Forge must use the same path to share a cache with opencode in mixed-use environments. | Low | Hardcode same path; add a cache invalidation flag for testing. |
| 8 | **Provider catalog drift** — opencode adds/removes models frequently; the generated `catalog_gen.go` must be regenerated on each opencode release. | High | Add a CI check that diffs the generated file against a fresh generation; fail on drift. |
| 9 | **`OPENCODE_DISABLE_PROJECT_CONFIG` flag** — used in both config loading and instruction loading. Must be read from env in the same way. | Low | Check env var once at startup; store in a `RuntimeFlags` struct passed to all loaders. |
| 10 | **macOS MDM managed preferences** — highest-priority config layer in opencode. Scope: macOS only; Forge should implement as a no-op stub on other platforms with a `//go:build darwin` file. | Low | Stub on non-darwin; implement darwin read in a separate `managed_darwin.go` file. |

---

## Review pass (2026-06-03) — done, with two carry-forwards

Best-validated plan in the suite (golden files + the opencode-`.opencode/` integration test). Built
and green. Two real items:

- **Provider auth is partial.** Credential CRUD (`PUT`/`DELETE /auth/{id}` against the shared
  `~/.local/share/opencode/auth.json`) shipped, but **`GET /provider/auth` (per-provider
  auth-method listing) and the OAuth flow (`POST /provider/:id/oauth/authorize` + `/callback`)
  remain 501** (`internal/server/auth_handlers.go:16`; `conformance/known-divergences.json` scenario
  `provider-auth`). This is the same OAuth callback/loopback problem the masterplan now flags as
  ownerless across plans 03/04/13 — pick one owner.
- **Verification couples to the live opencode repo.** The integration test asserts against
  `/Users/rotemmiz/git/opencode/.opencode/` (e.g. `triage` → `mode:primary, hidden:true,
  model:opencode/gpt-5.4-nano`). These still match today (verified), but a reference-repo update
  silently breaks the gate. **Make the `t.TempDir()` copy authoritative** — snapshot the fixture
  set into `internal/resource/testdata/` so the test pins a known input, and keep the live-repo run
  as an *optional* opportunistic check, not the gate. This also lets risk #8 (catalog drift, High)
  be enforced independently: confirm the CI drift check (`make gen` + `git diff --exit-code`) is
  actually wired for the generated provider catalog, not just described.

## Links to sibling plans

- **Plan 00** (`00-masterplan.md`) — Vision, architecture, sequencing. This plan is Phase C.
- **Plan 01** (`01-daemon-core.md`) — HTTP transport, auth middleware, directory routing; the
  server that exposes the `/agent`, `/command`, `/skill`, `/provider` endpoints wired in M8.
- **Plan 02** (`02-agent-engine.md`) — Agent loop, tool dispatch; consumes `AgentRegistry`,
  `InstructionService`, and `ProviderRegistry` built here.
- **Plan 03** (`03-ecosystem-mcp-lsp.md`) — MCP and LSP config loaders; reads from the same
  merged `config.Info` JSON object produced by M2.
- **Plan 05** (`05-plugin-host.md`) — Plugin discovery from `.opencode/plugin(s)/`; shares the
  config-directory walk infrastructure from M2.
- **Plan 12** (`12-test-compatibility.md`) — Conformance harness; the integration test in
  §Verification is the primary compatibility gate feeding into plan 12's cross-comparison suite.
