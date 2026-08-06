import { execFile } from "node:child_process"
import { promisify } from "node:util"
import type { ExtensionAPI, ExtensionContext } from "@mariozechner/pi-coding-agent"

const execFileAsync = promisify(execFile)
const ACTIVE = new Set(["preview", "managed", "waiting"])

type Envelope<T> = { ok: boolean; result?: T; error?: { message?: string } }
type HostSession = { id: string; status: string; plan_id: string }
type HostView = { session: HostSession; plan: { preview?: string; record?: { status?: string } } }
type HostBegin = { session: HostSession; plan: { preview: string; requires_confirmation: boolean } }
type Guard = { allowed: boolean; reason: string }

function unwrap<T>(stdout: string): T {
  const envelope = JSON.parse(stdout) as Envelope<T>
  if (!envelope.ok || envelope.result === undefined) throw new Error(envelope.error?.message ?? "Takt command failed")
  return envelope.result
}

async function takt<T>(cwd: string, args: string[]): Promise<T> {
  const { stdout } = await execFileAsync("takt", [...args, "--workspace", cwd, "--daemon", "--json"], { maxBuffer: 16 * 1024 * 1024 })
  return unwrap<T>(stdout)
}

function hostSessionID(ctx: ExtensionContext): string {
  const manager = ctx.sessionManager
  return manager.getSessionId?.() ?? manager.getSessionFile?.() ?? `pi:${ctx.cwd}`
}

function active(session: HostSession | undefined): session is HostSession {
  return Boolean(session && ACTIVE.has(session.status))
}

export default function taktHostControl(pi: ExtensionAPI): void {
  let managed: HostSession | undefined
  let savedTools: string[] | undefined

  const applyToolMode = () => {
    if (active(managed)) {
      savedTools ??= pi.getActiveTools()
      const readOnly = savedTools.filter((name) => ["read", "grep", "find", "ls", "diagnostics"].includes(name))
      pi.setActiveTools(readOnly)
    } else if (savedTools) {
      pi.setActiveTools(savedTools)
      savedTools = undefined
    }
  }

  const refresh = async (ctx: ExtensionContext): Promise<HostView | undefined> => {
    try {
      const view = await takt<HostView>(ctx.cwd, ["host", "find", "--host", "pi", "--host-session", hostSessionID(ctx)])
      managed = view.session
      applyToolMode()
      ctx.ui.setStatus("takt", `${managed.status}: ${managed.plan_id}`)
      return view
    } catch {
      managed = undefined
      applyToolMode()
      ctx.ui.setStatus("takt", undefined)
      return undefined
    }
  }

  pi.registerCommand("takt", {
    description: "Run a task under strict Takt workflow control",
    handler: async (args, ctx) => {
      const goal = args.trim()
      if (!goal) {
        ctx.ui.notify("Usage: /takt <task>", "error")
        return
      }
      await ctx.waitForIdle()
      await execFileAsync("takt", ["daemon", "start", "--workspace", ctx.cwd], { maxBuffer: 4 * 1024 * 1024 }).catch(() => undefined)
      const result = await takt<HostBegin>(ctx.cwd, [
        "host", "begin", goal,
        "--host", "pi", "--host-session", hostSessionID(ctx), "--profile", "code",
        "--enforcement", "strict", "--command-interception", "--input-interception",
        "--tool-call-blocking", "--completion-blocking", "--session-recovery",
      ])
      managed = result.session
      applyToolMode()
      const confirmed = await ctx.ui.confirm("Takt workflow", `${result.plan.preview}\n\nStart this workflow?`)
      if (!confirmed) {
        await takt(ctx.cwd, ["host", "release", managed.id])
        managed = undefined
        applyToolMode()
        return
      }
      const view = await takt<HostView>(ctx.cwd, ["host", "confirm", managed.id, "--confirm"])
      managed = view.session
      applyToolMode()
      ctx.ui.notify(`Takt ${managed.status}: ${managed.plan_id}`, "info")
    },
  })

  pi.registerCommand("takt-status", {
    description: "Show the active Takt managed session",
    handler: async (_args, ctx) => {
      const view = await refresh(ctx)
      ctx.ui.notify(view ? JSON.stringify(view.plan, null, 2) : "No managed Takt session", "info")
    },
  })

  pi.registerCommand("takt-release", {
    description: "Explicitly leave Takt managed mode without cancelling its Run",
    handler: async (_args, ctx) => {
      if (managed) await takt(ctx.cwd, ["host", "release", managed.id])
      managed = undefined
      applyToolMode()
      ctx.ui.setStatus("takt", undefined)
    },
  })

  pi.on("session_start", async (_event, ctx) => { await refresh(ctx) })

  pi.on("input", async (event, ctx) => {
    if (event.source === "extension") return { action: "continue" as const }
    await refresh(ctx)
    if (!active(managed)) return { action: "continue" as const }
    await takt(ctx.cwd, ["steer", managed.plan_id, event.text])
    ctx.ui.notify("Input routed to the active Takt checkpoint; the main LLM was not invoked.", "info")
    return { action: "handled" as const }
  })

  pi.on("before_agent_start", async (_event, ctx) => {
    await refresh(ctx)
    if (active(managed)) {
      ctx.abort()
      return { message: { customType: "takt-managed", content: "Main agent turn blocked while Takt controls this task.", display: true } }
    }
  })

  pi.on("tool_call", async (event, ctx) => {
    await refresh(ctx)
    if (!active(managed)) return
    const decision = await takt<Guard>(ctx.cwd, ["host", "guard-tool", managed.id, "--tool", event.toolName])
    if (!decision.allowed) return { block: true, reason: decision.reason }
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
