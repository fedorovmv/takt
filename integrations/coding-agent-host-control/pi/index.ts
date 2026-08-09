import { createHash } from "node:crypto"
import { execFile } from "node:child_process"
import { mkdir, readFile, rm, writeFile } from "node:fs/promises"
import { dirname, join } from "node:path"
import { promisify } from "node:util"
import type { ExtensionAPI, ExtensionContext } from "@mariozechner/pi-coding-agent"

const execFileAsync = promisify(execFile)
const ACTIVE = new Set(["preview", "managed", "waiting", "paused"])

type Envelope<T> = { ok: boolean; result?: T; error?: { message?: string } }
type HostSession = { id: string; status: string; plan_id: string }
type HostView = { session: HostSession; plan: { preview?: string; record?: { status?: string; current_run_id?: string } } }
type HostBegin = { session: HostSession; plan: { preview: string; requires_confirmation: boolean } }
type Guard = { allowed: boolean; reason: string }
type Notice = { id: string; event: string; message: string }
type NoticeList = { notifications: Notice[] }
type RunList = { runs: Array<{ id: string }> }
type CachedState = { session: HostSession; host_session_id: string; updated_at: string }

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

function hostSessionID(ctx: ExtensionContext): string {
  const manager = ctx.sessionManager
  return manager.getSessionId?.() ?? manager.getSessionFile?.() ?? `pi:${ctx.cwd}`
}

function active(session: HostSession | undefined): session is HostSession {
  return Boolean(session && ACTIVE.has(session.status))
}

function cachePath(cwd: string, hostID: string): string {
  const key = createHash("sha256").update(hostID).digest("hex").slice(0, 24)
  return join(cwd, ".takt", "host-client-state", `pi-${key}.json`)
}

async function saveCache(cwd: string, hostID: string, session: HostSession): Promise<void> {
  const path = cachePath(cwd, hostID)
  await mkdir(dirname(path), { recursive: true, mode: 0o700 })
  await writeFile(path, JSON.stringify({ session, host_session_id: hostID, updated_at: new Date().toISOString() } satisfies CachedState, null, 2) + "\n", { mode: 0o600 })
}

async function loadCache(cwd: string, hostID: string): Promise<HostSession | undefined> {
  try {
    const raw = await readFile(cachePath(cwd, hostID), "utf8")
    const value = JSON.parse(raw) as CachedState
    return value.host_session_id === hostID ? value.session : undefined
  } catch {
    return undefined
  }
}

async function clearCache(cwd: string, hostID: string): Promise<void> {
  await rm(cachePath(cwd, hostID), { force: true })
}

export default function taktHostControl(pi: ExtensionAPI): void {
  let managed: HostSession | undefined
  let transportAvailable = true
  let savedTools: string[] | undefined

  const applyToolMode = () => {
    if (active(managed)) {
      savedTools ??= pi.getActiveTools()
      const allowed = transportAvailable
        ? savedTools.filter((name) => ["read", "grep", "find", "ls", "diagnostics"].includes(name))
        : []
      pi.setActiveTools(allowed)
    } else if (savedTools) {
      pi.setActiveTools(savedTools)
      savedTools = undefined
    }
  }

  const failClosed = async (ctx: ExtensionContext, cause: unknown): Promise<void> => {
    const id = hostSessionID(ctx)
    managed ??= await loadCache(ctx.cwd, id)
    transportAvailable = false
    applyToolMode()
    if (active(managed)) {
      ctx.ui.setStatus("takt", `DISCONNECTED: ${managed.plan_id}`)
      ctx.ui.notify(`Takt daemon is unavailable; managed mode remains fail-closed. ${String(cause)}`, "error")
    }
  }

  const refresh = async (ctx: ExtensionContext): Promise<HostView | undefined> => {
    const id = hostSessionID(ctx)
    try {
      const view = await takt<HostView>(ctx.cwd, ["host", "find", "--host", "pi", "--host-session", id])
      transportAvailable = true
      managed = view.session
      if (active(managed)) await saveCache(ctx.cwd, id, managed)
      else await clearCache(ctx.cwd, id)
      applyToolMode()
      ctx.ui.setStatus("takt", `${managed.status}: ${managed.plan_id}`)
      return view
    } catch (error) {
      const cached = managed ?? await loadCache(ctx.cwd, id)
      if (active(cached)) {
        try {
          // host.find intentionally excludes terminal sessions so a new /takt
          // can start. Probe the cached durable ID before treating the error as
          // a transport outage.
          const terminal = await takt<HostView>(ctx.cwd, ["host", "status", cached.id])
          transportAvailable = true
          managed = terminal.session
          if (active(managed)) await saveCache(ctx.cwd, id, managed)
          else await clearCache(ctx.cwd, id)
          applyToolMode()
          ctx.ui.setStatus("takt", active(managed) ? `${managed.status}: ${managed.plan_id}` : undefined)
          return terminal
        } catch {
          managed = cached
          await failClosed(ctx, error)
          return undefined
        }
      }
      managed = undefined
      transportAvailable = true
      applyToolMode()
      ctx.ui.setStatus("takt", undefined)
      return undefined
    }
  }

  const showUnread = async (ctx: ExtensionContext): Promise<void> => {
    try {
      const result = await takt<NoticeList>(ctx.cwd, ["notify", "list", "--unread", "--limit", "5"])
      for (const notice of result.notifications ?? []) {
        ctx.ui.notify(`${notice.event}: ${notice.message}`, "info")
        await takt(ctx.cwd, ["notify", "ack", notice.id])
      }
    } catch {
      // Notification display is optional; managed-state enforcement is handled separately.
    }
  }

  pi.registerCommand("takt", {
    description: "Run a task under guarded Takt workflow control",
    handler: async (args, ctx) => {
      const goal = args.trim()
      if (!goal) {
        ctx.ui.notify("Usage: /takt <task>", "error")
        return
      }
      await ctx.waitForIdle()
      await execFileAsync("takt", ["daemon", "start", "--workspace", ctx.cwd], { maxBuffer: 4 * 1024 * 1024 })
      const id = hostSessionID(ctx)
      const result = await takt<HostBegin>(ctx.cwd, [
        "host", "begin", "--host", "pi", "--host-session", id, "--profile", "code",
        "--enforcement", "guarded", "--command-interception", "--input-interception",
        "--tool-call-blocking", "--session-recovery", "--", goal,
      ])
      transportAvailable = true
      managed = result.session
      await saveCache(ctx.cwd, id, managed)
      applyToolMode()
      const confirmed = await ctx.ui.confirm("Takt workflow", `${result.plan.preview}\n\nStart this workflow?`)
      if (!confirmed) {
        await takt(ctx.cwd, ["host", "release", managed.id])
        await clearCache(ctx.cwd, id)
        managed = undefined
        applyToolMode()
        return
      }
      const view = await takt<HostView>(ctx.cwd, ["host", "confirm", managed.id, "--confirm"])
      managed = view.session
      await saveCache(ctx.cwd, id, managed)
      applyToolMode()
      ctx.ui.notify(`Takt ${managed.status}: ${managed.plan_id}`, "info")
    },
  })

  pi.registerCommand("takt-status", {
    description: "Show the active Takt managed session",
    handler: async (_args, ctx) => {
      const view = await refresh(ctx)
      if (!transportAvailable && active(managed)) {
        ctx.ui.notify(`Takt daemon unavailable; plan ${managed.plan_id} remains locked`, "error")
        return
      }
      ctx.ui.notify(view ? JSON.stringify(view.plan, null, 2) : "No managed Takt session", "info")
    },
  })

  pi.registerCommand("takt-runs", {
    description: "List active autonomous Takt runs",
    handler: async (_args, ctx) => {
      try {
        const result = await takt<unknown>(ctx.cwd, ["runs", "--active", "--root-only", "--limit", "50"])
        ctx.ui.notify(JSON.stringify(result, null, 2), "info")
      } catch (error) {
        ctx.ui.notify(`Cannot list Takt runs: ${String(error)}`, "error")
      }
    },
  })

  pi.registerCommand("takt-attention", {
    description: "Show Takt runs that require attention",
    handler: async (_args, ctx) => {
      try {
        const result = await takt<unknown>(ctx.cwd, ["attention"])
        ctx.ui.notify(JSON.stringify(result, null, 2), "info")
      } catch (error) {
        ctx.ui.notify(`Cannot read Takt attention queue: ${String(error)}`, "error")
      }
    },
  })

  pi.registerCommand("takt-pause", {
    description: "Pause the active Takt run at a safe node boundary",
    handler: async (_args, ctx) => {
      const view = await refresh(ctx)
      const runID = view?.plan.record?.current_run_id
      if (!runID) {
        ctx.ui.notify("No active Takt execution run", "error")
        return
      }
      const result = await takt<unknown>(ctx.cwd, ["run", "pause", runID])
      ctx.ui.notify(JSON.stringify(result, null, 2), "info")
      await refresh(ctx)
    },
  })

  pi.registerCommand("takt-resume", {
    description: "Resume the active paused Takt run",
    handler: async (_args, ctx) => {
      const view = await refresh(ctx)
      const runID = view?.plan.record?.current_run_id
      if (!runID) {
        ctx.ui.notify("No paused Takt execution run", "error")
        return
      }
      const result = await takt<unknown>(ctx.cwd, ["run", "resume", runID])
      ctx.ui.notify(JSON.stringify(result, null, 2), "info")
      await refresh(ctx)
    },
  })

  pi.registerCommand("takt-result", {
    description: "Show the aggregate result of the active Takt run",
    handler: async (_args, ctx) => {
      const view = await refresh(ctx)
      let runID = view?.plan.record?.current_run_id
      if (!runID) {
        const recent = await takt<RunList>(ctx.cwd, ["runs", "--root-only", "--limit", "1"])
        runID = recent.runs?.[0]?.id
      }
      if (!runID) {
        ctx.ui.notify("No Takt execution run is available", "error")
        return
      }
      const result = await takt<unknown>(ctx.cwd, ["run", "summary", runID])
      ctx.ui.notify(JSON.stringify(result, null, 2), "info")
    },
  })

  pi.registerCommand("takt-release", {
    description: "Explicitly leave Takt managed mode without cancelling its Run",
    handler: async (_args, ctx) => {
      if (!transportAvailable && active(managed)) {
        ctx.ui.notify("Cannot release while Takt daemon is unavailable", "error")
        return
      }
      if (managed) await takt(ctx.cwd, ["host", "release", managed.id])
      await clearCache(ctx.cwd, hostSessionID(ctx))
      managed = undefined
      applyToolMode()
      ctx.ui.setStatus("takt", undefined)
    },
  })

  pi.on("session_start", async (_event, ctx) => {
    managed = await loadCache(ctx.cwd, hostSessionID(ctx))
    if (active(managed)) {
      transportAvailable = false
      applyToolMode()
    }
    await refresh(ctx)
    await showUnread(ctx)
  })

  pi.on("input", async (event, ctx) => {
    if (event.source === "extension") return { action: "continue" as const }
    await refresh(ctx)
    if (!active(managed)) return { action: "continue" as const }
    if (!transportAvailable) {
      ctx.ui.notify("Takt daemon unavailable; input blocked to preserve managed mode", "error")
      return { action: "handled" as const }
    }
    try {
      await takt(ctx.cwd, ["steer", managed.plan_id, "--", event.text])
      ctx.ui.notify("Input routed to Takt; the main LLM was not invoked.", "info")
    } catch (error) {
      ctx.ui.notify(`Takt rejected steering; input remains blocked: ${String(error)}`, "error")
    }
    return { action: "handled" as const }
  })

  pi.on("tool_call", async (event, ctx) => {
    await refresh(ctx)
    if (!active(managed)) return
    if (!transportAvailable) return { block: true, reason: "Takt daemon unavailable; managed mode is fail-closed" }
    try {
      const decision = await takt<Guard>(ctx.cwd, ["host", "guard-tool", managed.id, "--tool", event.toolName])
      if (!decision.allowed) return { block: true, reason: decision.reason }
    } catch (error) {
      await failClosed(ctx, error)
      return { block: true, reason: "Takt guard unavailable; managed mode is fail-closed" }
    }
  })

  pi.on("user_bash", async (_event, ctx) => {
    await refresh(ctx)
    if (!active(managed)) return
    return { result: { output: "Blocked while Takt managed mode is active", exitCode: 1, cancelled: false, truncated: false } }
  })

  pi.on("session_before_switch", async (_event, ctx) => {
    await refresh(ctx)
    if (active(managed)) return { cancel: true }
  })
  pi.on("session_before_fork", async (_event, ctx) => {
    await refresh(ctx)
    if (active(managed)) return { cancel: true }
  })
}
