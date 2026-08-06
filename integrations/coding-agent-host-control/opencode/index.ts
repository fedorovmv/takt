import { execFile } from "node:child_process"
import { promisify } from "node:util"
import { Plugin } from "@opencode-ai/plugin"

const execFileAsync = promisify(execFile)
const ACTIVE = new Set(["preview", "managed", "waiting"])
const MARKER = "TAKT_HOST_BEGIN:"

type Envelope<T> = { ok: boolean; result?: T; error?: { message?: string } }
type Session = { id: string; status: string; plan_id: string }
type View = { session: Session; plan: { preview?: string } }
type Begin = { session: Session; plan: { preview: string } }
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

function textOf(value: unknown): string {
  if (typeof value === "string") return value
  if (Array.isArray(value)) return value.map(textOf).join("\n")
  if (value && typeof value === "object") {
    const v = value as Record<string, unknown>
    return textOf(v.text ?? v.content ?? v.parts ?? "")
  }
  return ""
}

function latestUserText(messages: unknown): string {
  if (!Array.isArray(messages)) return ""
  for (let i = messages.length - 1; i >= 0; i--) {
    const item = messages[i] as Record<string, unknown>
    if (item?.role === "user") return textOf(item.content ?? item.parts)
  }
  return ""
}

export default Plugin.define({
  id: "takt.host-control",
  setup: async (ctx) => {
    const cwd = typeof ctx.options.workspace === "string" ? ctx.options.workspace : process.cwd()
    const sessions = new Map<string, Session>()

    const find = async (sessionID: string): Promise<View | undefined> => {
      try {
        const view = await takt<View>(cwd, ["host", "find", "--host", "opencode", "--host-session", sessionID])
        sessions.set(sessionID, view.session)
        return view
      } catch {
        sessions.delete(sessionID)
        return undefined
      }
    }

    // Slash commands are delivered by the companion command files. The context
    // hook consumes their markers immediately before provider dispatch.


    await ctx.session.hook("context", async (event) => {
      const sessionID = event.sessionID
      const input = latestUserText(event.messages).trim()
      let view = await find(sessionID)

      if (input.startsWith(MARKER)) {
        if (view && ACTIVE.has(view.session.status)) throw new Error(`Takt already controls plan ${view.session.plan_id}`)
        const goal = input.slice(MARKER.length).trim()
        if (!goal) throw new Error("Usage: /takt <task>")
        await execFileAsync("takt", ["daemon", "start", "--workspace", cwd], { maxBuffer: 4 * 1024 * 1024 }).catch(() => undefined)
        const result = await takt<Begin>(cwd, [
          "host", "begin", goal, "--host", "opencode", "--host-session", sessionID,
          "--profile", "code", "--enforcement", "strict", "--command-interception",
          "--input-interception", "--tool-call-blocking", "--completion-blocking", "--session-recovery",
        ])
        sessions.set(sessionID, result.session)
        throw new Error(`${result.plan.preview}\n\nRun /takt-confirm to start. The main LLM was not invoked.`)
      }

      if (!view || !ACTIVE.has(view.session.status)) return
      if (input === "TAKT_HOST_CONFIRM") {
        view = await takt<View>(cwd, ["host", "confirm", view.session.id, "--confirm"])
        sessions.set(sessionID, view.session)
        throw new Error(`Takt plan ${view.session.plan_id} started with status ${view.session.status}. The main LLM was not invoked.`)
      }
      if (input === "TAKT_HOST_STATUS") throw new Error(JSON.stringify(view.plan, null, 2))
      if (input === "TAKT_HOST_RELEASE") {
        await takt(cwd, ["host", "release", view.session.id])
        sessions.delete(sessionID)
        throw new Error("Takt managed mode released. Repeat the user request to run it normally.")
      }
      await takt(cwd, ["steer", view.session.plan_id, input])
      throw new Error("Input routed to the active Takt checkpoint; the main LLM was not invoked.")
    })

    await ctx.tool.hook("execute.before", async (event) => {
      const sessionID = event.sessionID
      const view = await find(sessionID)
      if (!view || !ACTIVE.has(view.session.status)) return
      const decision = await takt<Guard>(cwd, ["host", "guard-tool", view.session.id, "--tool", event.tool])
      if (!decision.allowed) throw new Error(decision.reason)
    })
  },
})
