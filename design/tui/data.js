/* ============================================================
   OPCODE42 TUI — scripted agent session
   A coding agent adds retry-with-backoff to an HTTP client.
   Exposed as window.OPCODE42.
   ============================================================ */
(function () {
  // syntax token helper: arrays of [class, text]
  // classes: kw fn ty str num com pun var prop  (blank = plain)

  const SESSION_TITLE = "Add retry + backoff to http client";

  // The user's first prompt (shown as the seed in splash + first turn)
  const SEED_PROMPT = {
    text: "Add retry with exponential backoff to the HTTP client, then cover it with a test.",
    mention: "src/http.ts",
  };

  // Ordered stream events. The runner reveals these one by one.
  // kinds:
  //   thought   { ms }
  //   md        { html }                      (streamed prose)
  //   rule      {}
  //   tool      { glyph, label, path, meta }  (terse one-liners)
  //   diff      { title, file, error?, lines:[{t,gut,text|tokens, hl}] }
  //   write     { title, file, lines:[tokens] }
  //   bash      { title, cmd, out:[lines], collapsed }
  //   todos     { title, items:[{state,text}] }
  //   summary   { title, prose, rows:[{file,chg}], footer }
  //   subagent  { kind, name, lines:[...], state }
  const EVENTS = [
    { kind: "thought", ms: 740 },

    { kind: "md", stream: true, html:
      `<h3>Adding retry with backoff</h3>` +
      `<p>I'll wrap the request path in <code>src/http.ts</code> with a bounded retry loop that backs off exponentially on transient failures, then add a test that fakes a flaky endpoint. Plan:</p>` +
      `<ol>` +
      `<li>Read the client and find the single fetch call.</li>` +
      `<li>Add a <code>withRetry()</code> helper with jittered backoff.</li>` +
      `<li>Only retry on 5xx / network errors — never on 4xx.</li>` +
      `<li>Cover it with a flaky-server test.</li>` +
      `</ol>` +
      `<p class="quote">Capping at 3 attempts keeps tail latency bounded while still smoothing over the typical blip.</p>` +
      `<p>See the <a>client design notes</a> for prior context. Relevant call: <span class="fn">request</span>(<span class="ident">opts</span>) returns <span class="ident">Response</span>.</p>`
    },

    { kind: "rule" },

    { kind: "tool", glyph: "→", label: "Read", path: "src/http.ts", meta: "" },
    { kind: "tool", glyph: "↳", label: "Loaded", path: "src/http.ts", meta: "· 64 lines" },
    { kind: "tool", glyph: "*", label: "Grep", path: '"fetch("', meta: "(2 matches)" },
    { kind: "tool", glyph: "*", label: "Glob", path: '"src/**/*.ts"', meta: "(5 matches)" },

    {
      kind: "diff",
      title: "Edit", file: "src/http.ts",
      lines: [
        { t: "hdr-old", text: "--- src/http.ts" },
        { t: "hdr-new", text: "+++ src/http.ts" },
        { t: "hunk",    text: "@@ -1,6 +1,7 @@" },
        { t: "ctx", text: " // Minimal HTTP client over fetch." },
        { t: "ctx", text: ' import { Logger } from "./log"' },
        { t: "add", text: '+import { sleep } from "./util"', hl: [8, 30] },
        { t: "ctx", text: "" },
        { t: "ctx", text: " export interface ReqOpts {" },
        { t: "ctx", text: "   readonly url: string" },
        { t: "hunk",    text: "@@ -18,9 +19,24 @@" },
        { t: "ctx", text: " export async function request(o: ReqOpts): Promise<Response> {" },
        { t: "del", text: "-  return fetch(o.url, { method: o.method })", hl: [2, 44] },
        { t: "add", text: "+  return withRetry(() => fetch(o.url, { method: o.method }))", hl: [2, 58] },
        { t: "ctx", text: " }" },
        { t: "add", text: "+" },
        { t: "add", text: "+const RETRIABLE = new Set([502, 503, 504])" },
        { t: "add", text: "+" },
        { t: "add", text: "+async function withRetry(fn: () => Promise<Response>, max = 3) {" },
        { t: "add", text: "+  for (let attempt = 1; ; attempt++) {" },
        { t: "add", text: "+    const res = await fn()" },
        { t: "add", text: "+    if (res.ok || !RETRIABLE.has(res.status)) return res" },
        { t: "add", text: "+    if (attempt >= max) return res" },
        { t: "add", text: "+    const backoff = 2 ** attempt * 50 + Math.random() * 50" },
        { t: "add", text: "+    await sleep(backoff)" },
        { t: "add", text: "+  }" },
        { t: "add", text: "+}" },
      ],
    },

    {
      kind: "write",
      title: "Wrote", file: "test/http.test.ts",
      lines: [
        [["com", "// Flaky-server test: 503 twice, then 200."]],
        [["kw", "import"], ["pun", " { "], ["var", "test, expect"], ["pun", " } "], ["kw", "from"], ["str", ' "bun:test"']],
        [["kw", "import"], ["pun", " { "], ["var", "request"], ["pun", " } "], ["kw", "from"], ["str", ' "../src/http"']],
        [],
        [["kw", "test"], ["pun", "("], ["str", '"retries 5xx then succeeds"'], ["pun", ", "], ["kw", "async"], ["pun", " () => {"]],
        [["pun", "  "], ["kw", "let"], ["var", " hits"], ["pun", " = "], ["num", "0"]],
        [["pun", "  "], ["var", "globalThis"], ["pun", "."], ["prop", "fetch"], ["pun", " = "], ["kw", "async"], ["pun", " () => {"]],
        [["pun", "    "], ["kw", "if"], ["pun", " (++"], ["var", "hits"], ["pun", " < "], ["num", "3"], ["pun", ") "], ["kw", "return"], ["ty", " new Response"], ["pun", "(null, { "], ["prop", "status"], ["pun", ": "], ["num", "503"], ["pun", " })"]],
        [["pun", "    "], ["kw", "return"], ["ty", " new Response"], ["pun", "("], ["str", '"ok"'], ["pun", ")"]],
        [["pun", "  }"]],
        [["pun", "  "], ["kw", "const"], ["var", " res"], ["pun", " = "], ["kw", "await"], ["fn", " request"], ["pun", "({ "], ["prop", "url"], ["pun", ": "], ["str", '"/x"'], ["pun", ", "], ["prop", "method"], ["pun", ": "], ["str", '"GET"'], ["pun", " })"]],
        [["fn", "  expect"], ["pun", "("], ["var", "hits"], ["pun", ")."], ["fn", "toBe"], ["pun", "("], ["num", "3"], ["pun", ")"]],
        [["fn", "  expect"], ["pun", "("], ["var", "res"], ["pun", "."], ["prop", "status"], ["pun", ")."], ["fn", "toBe"], ["pun", "("], ["num", "200"], ["pun", ")"]],
        [["pun", "})"]],
      ],
    },

    {
      kind: "bash",
      title: "Run the test suite", cmd: "bun test",
      collapsed: false,
      out: [
        { c: "dim", t: "bun test v1.1.34" },
        { c: "dim", t: "" },
        { c: "", t: "test/http.test.ts:" },
        { c: "ok", t: "✓ retries 5xx then succeeds [3.10ms]" },
        { c: "ok", t: "✓ does not retry on 404 [0.40ms]" },
        { c: "ok", t: "✓ gives up after max attempts [0.71ms]" },
        { c: "dim", t: "" },
        { c: "ok", t: " 3 pass" },
        { c: "dim", t: " 0 fail" },
        { c: "dim", t: " 6 expect() calls" },
      ],
    },

    {
      kind: "todos",
      title: "Todos",
      items: [
        { state: "done",  text: "Add withRetry() with exponential backoff" },
        { state: "done",  text: "Skip retry on 4xx responses" },
        { state: "doing", text: "Cover with a flaky-server test" },
        { state: "pend",  text: "Document retry behaviour in README" },
      ],
    },

    {
      kind: "subagent",
      kind2: "General Task", name: "Audit remaining fetch() callers across the repo",
      state: "running",
      lines: [
        { c: "dim", t: "$ rg \"fetch\\(\" --type ts -l" },
        { c: "dim", t: "src/http.ts" },
        { c: "dim", t: "src/sync/poll.ts" },
      ],
    },

    {
      kind: "summary",
      title: "Done",
      prose:
        `<p>– <span class="ident">withRetry()</span> wraps every request with bounded exponential backoff + jitter.</p>` +
        `<p>– Only <span class="num">502/503/504</span> and network errors retry; <span class="num">4xx</span> short-circuits.</p>` +
        `<p>– Added a flaky-server test that fakes two <span class="num">503</span>s then a <span class="num">200</span>.</p>`,
      rows: [
        { file: "src/http.ts", chg: "edited" },
        { file: "test/http.test.ts", chg: "new" },
        { file: "src/util.ts", chg: "+1 export" },
      ],
      footer: "All green — 3 pass, 0 fail.",
    },
  ];

  // ---- slash commands ----
  const SLASH = [
    { cmd: "/agents",  desc: "Switch agent" },
    { cmd: "/compact", desc: "Compact session" },
    { cmd: "/connect", desc: "Connect provider" },
    { cmd: "/diff",    desc: "Open diff viewer" },
    { cmd: "/editor",  desc: "Open editor" },
    { cmd: "/exit",    desc: "Exit the app" },
    { cmd: "/help",    desc: "Help" },
    { cmd: "/init",    desc: "Guided AGENTS.md setup" },
    { cmd: "/models",  desc: "Switch model" },
    { cmd: "/new",     desc: "New session" },
    { cmd: "/sessions",desc: "Switch session" },
    { cmd: "/share",   desc: "Share session" },
    { cmd: "/themes",  desc: "Switch theme" },
  ];

  // ---- @-mention files ----
  const FILES = [
    "src/http.ts", "src/util.ts", "src/log.ts", "src/sync/poll.ts",
    "test/http.test.ts", "README.md", "package.json", "AGENTS.md",
  ];

  // ---- command palette ----
  const PALETTE = {
    suggested: [
      { label: "Switch session", short: "ctrl+x l", action: "sessions" },
      { label: "New session",    short: "ctrl+x n", action: "new" },
      { label: "Switch model",   short: "ctrl+x m", action: "models" },
      { label: "Switch agent",   short: "ctrl+x a", action: "agents" },
    ],
    session: [
      { label: "Switch theme",        short: "",         action: "themes" },
      { label: "Open editor",         short: "ctrl+x e", action: "toast" },
      { label: "Rename session",      short: "ctrl+r",   action: "toast" },
      { label: "Fork session",        short: "",         action: "toast" },
      { label: "Compact session",     short: "ctrl+x c", action: "toast" },
      { label: "View timeline",       short: "ctrl+x g", action: "timeline" },
      { label: "Show status",         short: "",         action: "status" },
      { label: "Toggle sidebar",      short: "ctrl+x b", action: "sidebar" },
      { label: "Toggle tasks board",  short: "ctrl+x t", action: "tasks" },
      { label: "Toggle scanlines",    short: "",         action: "crt" },
      { label: "Copy last message",   short: "ctrl+x y", action: "toast" },
    ],
  };

  // ---- models ----
  const MODELS = {
    "Opcode42 Cloud": [
      { name: "Anvil Mini", tag: "Free" },
      { name: "Anvil Flash", tag: "Free" },
    ],
    "Anthropic": [
      { name: "Claude Haiku 4.5" },
      { name: "Claude Sonnet 4.6" },
      { name: "Claude Opus 4.8", current: true },
    ],
    "OpenAI": [
      { name: "GPT-5.2" },
      { name: "GPT-5.2 Codex" },
      { name: "GPT-5.1 Codex Mini" },
    ],
    "Google": [
      { name: "Gemini 3 Pro" },
      { name: "Gemini 3 Flash" },
    ],
  };

  // ---- agents ----
  const AGENTS = [
    { name: "build", mode: "native", current: true },
    { name: "plan",  mode: "native" },
    { name: "review", mode: "subagent" },
  ];

  // ---- themes ----
  const THEMES = [
    "ayu", "catppuccin", "everforest", "github", "gruvbox", "kanagawa",
    "matrix", "monokai", "nightowl", "nord", "one-dark", "opcode42",
    "rosepine", "solarized", "synthwave84", "system", "tokyonight",
    "vesper", "zenburn",
  ];

  // ---- tasks (tasks.md / issue board) shown in the bottom dock ----
  const TASKS = {
    source: "tasks.md",
    branch: "main",
    items: [
      { id: 142, title: "Add retry with exponential backoff to http client", status: "doing",   labels: ["core"],            assignee: "you" },
      { id: 143, title: "Document retry behaviour in README",                status: "todo",    labels: ["docs"],            assignee: "you" },
      { id: 138, title: "Audit remaining fetch() callers across the repo",   status: "doing",   labels: ["chore"],           assignee: "agent" },
      { id: 131, title: "Flaky poll loop in sync worker drops events",       status: "blocked", labels: ["bug", "sync"],     assignee: "rai" },
      { id: 129, title: "Structured logger swallows error stacks",           status: "review",  labels: ["bug", "logging"],  assignee: "you" },
      { id: 126, title: "Bump deps and re-run typecheck",                    status: "todo",    labels: ["chore"],           assignee: "—" },
      { id: 121, title: "Rate-limit the public /search endpoint",            status: "todo",    labels: ["api"],             assignee: "mara" },
      { id: 118, title: "Cache provider model list for 60s",                 status: "todo",    labels: ["perf"],            assignee: "—" },
      { id: 109, title: "Migrate config loader to zod schema",               status: "review",  labels: ["core"],            assignee: "rai" },
    ],
  };

  // ---- sessions ----
  const SESSIONS = [
    { group: "Today", items: [
      { title: "Add retry + backoff to http client", time: "2:41 PM", current: true },
      { title: "Fix flaky poll loop in sync worker", time: "11:08 AM" },
    ]},
    { group: "Yesterday", items: [
      { title: "Migrate logger to structured output", time: "6:22 PM" },
      { title: "Bump deps and re-run typecheck", time: "9:15 AM" },
    ]},
  ];

  // ---- timeline (jump-to-message) ----
  const TIMELINE = [
    { title: "Add retry with exponential backoff to the HTTP client, then …", time: "2:41 PM", current: true },
  ];

  window.OPCODE42 = {
    SESSION_TITLE, SEED_PROMPT, EVENTS, SLASH, FILES, PALETTE,
    MODELS, AGENTS, THEMES, SESSIONS, TIMELINE, TASKS,
  };
})();
