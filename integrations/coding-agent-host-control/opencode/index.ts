import { createHash } from "node:crypto"
import { execFile } from "node:child_process"
import { mkdir, readFile, rm, writeFile } from "node:fs/promises"
import { dirname, join } from "node:path"
import { promisify } from "node:util"
import type { Hooks, Plugin, PluginInput } from "@opencode-ai/plugin"

const execFileAsync = promisify(execFile)
const ACTIVE = new Set(["preview", "managed", "waiting", "paused"])
const MARKER = "TAKT_HOST_BEGIN:"

type Envelope<T> = { ok: boolean; result?: T; error?: { message?: string } }
type Session = { id: string; status: string; plan_id: string }
type View = { session: Session; plan: { preview?: string; record?: { current_run_id?: string } } }
type Begin = { session: Session; plan: { preview: string } }
type Guard = { allowed: boolean; reason: string }
type CachedState = { session: Session; host_session_id: string; updated_at: string }
type RunList = { runs: Array<{ id: string }> }

function unwrap<T>(stdout: string): T {
  const envelope = JSON.parse(stdout) as Envelope<T>
  if (!envelope.ok || envelope.result === undefined) throw new Error(envelope.error?.message ?? "Takt command failed")
  return envelope.result
}

async function takt<T>(cwd: string, args: string[]): Promise<T> {
  const separator = args.indexOf("--")
  const at = separator < 0 ? args.length : separator
  const { stdout } = await execFileAsync("takt", [...args.slice(0, at), "--workspace", cwd, "--daemon", "--json", ...args.slice(at)], { maxBuffer: 16 * 1024 * 1024 })
  return unwrap<T>(stdout)
}

function textOf(value: unknown): string {
  if (typeof value === "string") return value
  if (Array.isArray(value)) return value.map(textOf).join("\n")
  if (value && typeof value === "object") {
    const v = value as Record<string, unknown>
    return textOf(v.text ?? v.content ?? v.parts ?? "")
  }
  return ""
}

function block(message: string): never {
  process.stderr.write(`Takt: ${message}\n`)
  throw new Error(message)
}

function active(session: Session | undefined): session is Session {
  return Boolean(session && ACTIVE.has(session.status))
}

function cachePath(cwd: string, sessionID: string): string {
  const key = createHash("sha256").update(sessionID).digest("hex").slice(0, 24)
  return join(cwd, ".takt", "host-client-state", `opencode-${key}.json`)
}

async function saveCache(cwd: string, sessionID: string, session: Session): Promise<void> {
  const path = cachePath(cwd, sessionID)
  await mkdir(dirname(path), { recursive: true, mode: 0o700 })
  await writeFile(path, JSON.stringify({ session, host_session_id: sessionID, updated_at: new Date().toISOString() } satisfies CachedState, null, 2) + "\n", { mode: 0o600 })
}

async function loadCache(cwd: string, sessionID: string): Promise<Session | undefined> {
  try {
    const raw = await readFile(cachePath(cwd, sessionID), "utf8")
    const value = JSON.parse(raw) as CachedState
    return value.host_session_id === sessionID ? value.session : undefined
  } catch {
    return undefined
  }
}

async function clearCache(cwd: string, sessionID: string): Promise<void> {
  await rm(cachePath(cwd, sessionID), { force: true })
}

const taktHostControl: Plugin = async (ctx: PluginInput): Promise<Hooks> => {
  const cwd = ctx.directory || process.cwd()
  const sessions = new Map<string, Session>()
  const disconnected = new Set<string>()

  const find = async (sessionID: string): Promise<View | undefined> => {
    try {
      const view = await takt<View>(cwd, ["host", "find", "--host", "opencode", "--host-session", sessionID])
      disconnected.delete(sessionID)
      sessions.set(sessionID, view.session)
      if (active(view.session)) await saveCache(cwd, sessionID, view.session)
      else await clearCache(cwd, sessionID)
      return view
    } catch (error) {
      const cached = sessions.get(sessionID) ?? await loadCache(cwd, sessionID)
      if (active(cached)) {
        try {
          const terminal = await takt<View>(cwd, ["host", "status", cached.id])
          disconnected.delete(sessionID)
          sessions.set(sessionID, terminal.session)
          if (active(terminal.session)) await saveCache(cwd, sessionID, terminal.session)
          else await clearCache(cwd, sessionID)
          return terminal
        } catch {
          sessions.set(sessionID, cached)
          disconnected.add(sessionID)
          throw new Error(`Takt daemon unavailable; managed mode for ${cached.plan_id} remains fail-closed: ${String(error)}`)
        }
      }
      sessions.delete(sessionID)
      disconnected.delete(sessionID)
      return undefined
    }
  }

  const onMessage: NonNullable<Hooks["chat.message"]> = async (event, output) => {
    const sessionID = String(event.sessionID)
    const input = textOf(output.parts).trim()
    let view: View | undefined
    try {
      view = await find(sessionID)
    } catch (error) {
      block(String(error))
    }

    if (input.startsWith(MARKER)) {
      if (view && active(view.session)) block(`Takt already controls plan ${view.session.plan_id}`)
      const goal = input.slice(MARKER.length).trim()
      if (!goal) block("Usage: /takt <task>")
      await execFileAsync("takt", ["daemon", "start", "--workspace", cwd], { maxBuffer: 4 * 1024 * 1024 })
      const result = await takt<Begin>(cwd, [
        "host", "begin", "--host", "opencode", "--host-session", sessionID,
        "--profile", "code", "--enforcement", "guarded", "--command-interception",
        "--input-interception", "--tool-call-blocking", "--session-recovery", "--", goal,
      ])
      sessions.set(sessionID, result.session)
      await saveCache(cwd, sessionID, result.session)
      block(`${result.plan.preview}\n\nRun /takt-confirm to start. The main LLM was not invoked.`)
    }

    if (input === "TAKT_RUNS") {
      const result = await takt<unknown>(cwd, ["runs", "--active", "--root-only", "--limit", "50"])
      block(JSON.stringify(result, null, 2))
    }
    if (input === "TAKT_ATTENTION") {
      const result = await takt<unknown>(cwd, ["attention"])
      block(JSON.stringify(result, null, 2))
    }
    if (input === "TAKT_HOST_RESULT") {
      let runID = view?.plan.record?.current_run_id
      if (!runID) {
        const recent = await takt<RunList>(cwd, ["runs", "--root-only", "--limit", "1"])
        runID = recent.runs?.[0]?.id
      }
      if (!runID) block("No Takt execution run is available")
      const result = await takt<unknown>(cwd, ["run", "summary", runID])
      block(JSON.stringify(result, null, 2))
    }
    if (!view || !active(view.session)) return
    if (input === "TAKT_HOST_CONFIRM") {
      view = await takt<View>(cwd, ["host", "confirm", view.session.id, "--confirm"])
      sessions.set(sessionID, view.session)
      await saveCache(cwd, sessionID, view.session)
      block(`Takt plan ${view.session.plan_id} started with status ${view.session.status}. The main LLM was not invoked.`)
    }
    if (input === "TAKT_HOST_STATUS") block(JSON.stringify(view.plan, null, 2))
    if (input === "TAKT_HOST_PAUSE") {
      const runID = view.plan.record?.current_run_id
      if (!runID) block("No active Takt execution run")
      const result = await takt<unknown>(cwd, ["run", "pause", runID])
      block(JSON.stringify(result, null, 2))
    }
    if (input === "TAKT_HOST_RESUME") {
      const runID = view.plan.record?.current_run_id
      if (!runID) block("No paused Takt execution run")
      const result = await takt<unknown>(cwd, ["run", "resume", runID])
      block(JSON.stringify(result, null, 2))
    }
    if (input === "TAKT_HOST_RELEASE") {
      await takt(cwd, ["host", "release", view.session.id])
      sessions.delete(sessionID)
      disconnected.delete(sessionID)
      await clearCache(cwd, sessionID)
      block("Takt managed mode released. Repeat the user request to run it normally.")
    }
    try {
      await takt(cwd, ["steer", view.session.plan_id, "--", input])
      block("Input routed to the active Takt checkpoint; the main LLM was not invoked.")
    } catch (error) {
      if (String(error).includes("Input routed to")) throw error
      block(`Takt rejected steering; input remains blocked and the main LLM was not invoked: ${String(error)}`)
    }
  }

  const onTool: NonNullable<Hooks["tool.execute.before"]> = async (event) => {
    const sessionID = String(event.sessionID)
    let view: View | undefined
    try {
      view = await find(sessionID)
    } catch (error) {
      block(String(error))
    }
    if (!view || !active(view.session)) return
    if (disconnected.has(sessionID)) block("Takt daemon unavailable; managed mode is fail-closed")
    let decision: Guard
    try {
      decision = await takt<Guard>(cwd, ["host", "guard-tool", view.session.id, "--tool", String(event.tool)])
    } catch (error) {
      disconnected.add(sessionID)
      block(`Takt tool guard unavailable; managed mode is fail-closed: ${String(error)}`)
    }
    if (!decision.allowed) block(decision.reason)
  }

  return {
    "chat.message": onMessage,
    "tool.execute.before": onTool,
  }
}

export const server = taktHostControl
export default taktHostControl
