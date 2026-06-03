/**
 * Forge plugin host — Node/Bun sidecar (plan 05).
 *
 * Loads opencode-format plugins and bridges their hooks to the Forge Go daemon
 * over JSON-RPC 2.0 on a local unix socket. Framing matches the Go side
 * (internal/pluginbridge/jsonrpc.go): a 4-byte big-endian uint32 length header
 * followed by the UTF-8 JSON body.
 *
 * The host reuses opencode's plugin contract:
 *   - PluginModule.server / legacy bare function
 *       (opencode/packages/plugin/src/index.ts:74-80)
 *   - the Hooks table
 *       (opencode/packages/plugin/src/index.ts:222-334)
 *   - {plugin,plugins}/*.{ts,js} discovery
 *       (opencode/packages/opencode/src/config/plugin.ts:29-37)
 *
 * It deliberately does NOT import Effect or opencode daemon internals; plugins
 * that need the SDK client get one via @opencode-ai/sdk when it is installed.
 */

import net from "node:net"
import fs from "node:fs"
import path from "node:path"
import { pathToFileURL } from "node:url"

// ---------------------------------------------------------------------------
// Wire framing (must match internal/pluginbridge/jsonrpc.go)
// ---------------------------------------------------------------------------

const MAX_FRAME = 64 * 1024 * 1024

type RpcMessage = {
  jsonrpc: string
  id?: number
  method?: string
  params?: any
  result?: any
  error?: { code: number; message: string }
}

class FrameCodec {
  buf: Buffer = Buffer.alloc(0)
  onMessage: (m: RpcMessage) => void
  constructor(onMessage: (m: RpcMessage) => void) {
    this.onMessage = onMessage
  }

  push(chunk: Buffer) {
    this.buf = this.buf.length === 0 ? chunk : Buffer.concat([this.buf, chunk])
    for (;;) {
      if (this.buf.length < 4) return
      const len = this.buf.readUInt32BE(0)
      if (len > MAX_FRAME) throw new Error(`frame length ${len} exceeds limit`)
      if (this.buf.length < 4 + len) return
      const body = this.buf.subarray(4, 4 + len)
      this.buf = this.buf.subarray(4 + len)
      let msg: RpcMessage
      try {
        msg = JSON.parse(body.toString("utf8"))
      } catch (err) {
        log("dropping malformed frame", err)
        continue
      }
      this.onMessage(msg)
    }
  }
}

function encodeFrame(v: unknown): Buffer {
  const body = Buffer.from(JSON.stringify(v), "utf8")
  const header = Buffer.alloc(4)
  header.writeUInt32BE(body.length, 0)
  return Buffer.concat([header, body])
}

// ---------------------------------------------------------------------------
// Logging — host log output goes to stderr so it never corrupts the socket.
// ---------------------------------------------------------------------------

function log(...args: unknown[]) {
  // eslint-disable-next-line no-console
  console.error("[forge-plugin-host]", ...args)
}

// ---------------------------------------------------------------------------
// Environment / config
// ---------------------------------------------------------------------------

const socketPath = process.env.FORGE_PLUGIN_SOCKET
const forgeUrl = process.env.FORGE_URL ?? ""
const authHeader = process.env.FORGE_AUTH_HEADER ?? ""
const directory = process.env.FORGE_DIRECTORY ?? process.cwd()
const specsRaw = process.env.FORGE_PLUGIN_SPECS ?? "[]"

if (!socketPath) {
  log("FORGE_PLUGIN_SOCKET is required")
  process.exit(2)
}

let configuredSpecs: string[] = []
try {
  const parsed = JSON.parse(specsRaw)
  if (Array.isArray(parsed)) {
    // A spec may be a bare string or [spec, options]; we bridge only the
    // identifier (opencode config/plugin.ts Spec union).
    configuredSpecs = parsed.map((s) => (Array.isArray(s) ? s[0] : s)).filter((s) => typeof s === "string")
  }
} catch {
  log("FORGE_PLUGIN_SPECS is not valid JSON; ignoring")
}

// ---------------------------------------------------------------------------
// opencode-format hook types (structural; we do not import the package so the
// host runs even when @opencode-ai/plugin is not installed).
// ---------------------------------------------------------------------------

type ToolDefinition = {
  description: string
  args?: unknown
  parameters?: unknown
  execute: (args: any, context: any) => Promise<{ title: string; output: string; metadata?: any }>
}

type Hooks = {
  dispose?: () => Promise<void>
  event?: (input: { event: any }) => Promise<void>
  config?: (input: any) => Promise<void>
  tool?: Record<string, ToolDefinition>
  [hookName: string]: any
}

type PluginInput = {
  client: unknown
  project: unknown
  directory: string
  worktree: string
  serverUrl: URL
  $: unknown
}

// ---------------------------------------------------------------------------
// Plugin discovery + loading (mirrors opencode config/plugin.ts + loader.ts)
// ---------------------------------------------------------------------------

/** Scan {plugin,plugins}/*.{ts,js} under dir → file:// URL specs. */
function discoverLocalPlugins(dir: string): string[] {
  const out: string[] = []
  for (const sub of ["plugin", "plugins"]) {
    const base = path.join(dir, sub)
    let entries: string[]
    try {
      entries = fs.readdirSync(base)
    } catch {
      continue
    }
    for (const name of entries) {
      if (/\.(ts|js|mjs)$/.test(name)) out.push(pathToFileURL(path.join(base, name)).href)
    }
  }
  return out
}

/** Try to build the SDK client; tolerate the package being absent. */
async function makeClient(): Promise<unknown> {
  if (!forgeUrl) return undefined
  try {
    // @ts-expect-error optional dependency resolved at runtime
    const mod: any = await import("@opencode-ai/sdk/v2/client").catch(() => import("@opencode-ai/sdk"))
    const create = mod.createOpencodeClient ?? mod.default?.createOpencodeClient
    if (typeof create !== "function") return undefined
    return create({ baseUrl: forgeUrl, headers: { authorization: authHeader }, directory })
  } catch (err) {
    log("SDK client unavailable; plugins using input.client will fail", err)
    return undefined
  }
}

/** Detect the new module shape (PluginModule.server) vs legacy bare functions. */
function extractPluginFns(mod: any): Array<(input: PluginInput, opts?: any) => Promise<Hooks>> {
  const fns: Array<(input: PluginInput, opts?: any) => Promise<Hooks>> = []
  if (mod && typeof mod.server === "function") {
    fns.push(mod.server)
    return fns
  }
  if (mod && mod.default && typeof mod.default.server === "function") {
    fns.push(mod.default.server)
    return fns
  }
  // Legacy: every exported function is a bare plugin.
  for (const key of Object.keys(mod ?? {})) {
    if (typeof mod[key] === "function") fns.push(mod[key])
  }
  return fns
}

async function loadPlugins(input: PluginInput, specs: string[]): Promise<Hooks[]> {
  const hooks: Hooks[] = []
  const seen = new Set<string>()
  for (const spec of specs) {
    if (seen.has(spec)) continue
    seen.add(spec)
    try {
      const mod: any = await import(spec)
      for (const fn of extractPluginFns(mod)) {
        const h = await fn(input, {})
        if (h) hooks.push(h)
      }
    } catch (err) {
      // Per plan 05: a failed plugin load is logged and skipped, not fatal.
      log(`failed to load plugin ${spec}`, err)
    }
  }
  return hooks
}

// ---------------------------------------------------------------------------
// JSON-RPC server over the unix socket
// ---------------------------------------------------------------------------

let hooks: Hooks[] = []
let send: (m: RpcMessage) => void = () => {}

/** Collect all plugin-registered tools, keyed by id, across all plugins. */
function collectTools(): { specs: Array<{ id: string; description: string; parameters: any }>; impls: Map<string, ToolDefinition> } {
  const specs: Array<{ id: string; description: string; parameters: any }> = []
  const impls = new Map<string, ToolDefinition>()
  for (const h of hooks) {
    if (!h.tool) continue
    for (const [id, def] of Object.entries(h.tool)) {
      if (impls.has(id)) continue
      impls.set(id, def)
      specs.push({ id, description: def.description ?? "", parameters: def.parameters ?? def.args ?? {} })
    }
  }
  return { specs, impls }
}

let toolImpls = new Map<string, ToolDefinition>()

/** Run a named output-mutating hook across all plugins, serially. */
async function triggerHook(name: string, input: any, output: any): Promise<any> {
  for (const h of hooks) {
    const fn = h[name]
    if (typeof fn !== "function") continue
    try {
      await fn(input, output)
    } catch (err) {
      // Per-hook error boundary: log and continue with the current output.
      log(`hook ${name} threw`, err)
    }
  }
  return output
}

async function dispatch(method: string, params: any): Promise<any> {
  if (method.startsWith("plugin.trigger:")) {
    const name = method.slice("plugin.trigger:".length)
    return triggerHook(name, params?.input ?? {}, params?.output ?? {})
  }
  if (method === "tool.execute") {
    const def = toolImpls.get(params?.id)
    if (!def) throw new Error(`unknown tool ${params?.id}`)
    const res = await def.execute(params?.args ?? {}, params?.context ?? {})
    return { title: res.title, output: res.output, metadata: res.metadata }
  }
  throw new Error(`unknown method ${method}`)
}

function wireConnection(socket: net.Socket) {
  send = (m: RpcMessage) => {
    if (!socket.destroyed) socket.write(encodeFrame(m))
  }

  const codec = new FrameCodec((msg) => {
    void handleMessage(msg)
  })
  socket.on("data", (chunk) => {
    try {
      codec.push(chunk)
    } catch (err) {
      log("frame decode error; closing", err)
      socket.destroy()
    }
  })
  socket.on("error", (err) => log("socket error", err))
  socket.on("close", () => {
    // The daemon went away; nothing more to do — exit cleanly.
    void shutdown(0)
  })

  // Announce readiness and the plugin tool set.
  const { specs } = collectTools()
  send({ jsonrpc: "2.0", method: "host.ready", params: {} })
  if (specs.length > 0) send({ jsonrpc: "2.0", method: "plugin.tools", params: { tools: specs } })
}

async function handleMessage(msg: RpcMessage) {
  if (msg.method === "host.shutdown") {
    await shutdown(0)
    return
  }
  if (msg.method === "plugin.event") {
    // Fire-and-forget fan-out to every plugin's event hook.
    for (const h of hooks) {
      try {
        await h.event?.({ event: msg.params?.event })
      } catch (err) {
        log("event hook threw", err)
      }
    }
    return
  }
  if (msg.method && msg.id != null) {
    try {
      const result = await dispatch(msg.method, msg.params)
      send({ jsonrpc: "2.0", id: msg.id, result })
    } catch (err: any) {
      send({ jsonrpc: "2.0", id: msg.id, error: { code: -32000, message: String(err?.message ?? err) } })
    }
    return
  }
}

let shuttingDown = false
async function shutdown(code: number) {
  if (shuttingDown) return
  shuttingDown = true
  for (const h of hooks) {
    try {
      await h.dispose?.()
    } catch (err) {
      log("dispose hook threw", err)
    }
  }
  process.exit(code)
}

// ---------------------------------------------------------------------------
// Bootstrap
// ---------------------------------------------------------------------------

async function main() {
  const specs = [...discoverLocalPlugins(directory), ...configuredSpecs]
  const client = await makeClient()
  // BunShell ($) is available natively under Bun; Node gets undefined.
  const bunShell = (globalThis as any).Bun?.$
  const input: PluginInput = {
    client,
    project: { id: path.basename(directory), worktree: directory },
    directory,
    worktree: directory,
    serverUrl: forgeUrl ? new URL(forgeUrl) : new URL("http://localhost"),
    $: bunShell,
  }

  hooks = await loadPlugins(input, specs)
  const collected = collectTools()
  toolImpls = collected.impls
  log(`loaded ${hooks.length} plugin(s), ${collected.specs.length} tool(s)`)

  // Notify config hooks (best-effort; we have no merged config to pass yet).
  for (const h of hooks) {
    try {
      await h.config?.({})
    } catch (err) {
      log("config hook threw", err)
    }
  }

  // The Go daemon owns the listening socket (plan 05 §Startup: "Plugin host
  // connects to socket"); the host dials in as the client.
  const socket = net.createConnection(socketPath as string, () => {
    log(`connected to ${socketPath}`)
    wireConnection(socket)
  })
  socket.on("error", (err) => {
    log("failed to connect to daemon socket", err)
    process.exit(1)
  })
}

process.on("SIGTERM", () => void shutdown(0))
process.on("SIGINT", () => void shutdown(0))

main().catch((err) => {
  log("fatal", err)
  process.exit(1)
})
