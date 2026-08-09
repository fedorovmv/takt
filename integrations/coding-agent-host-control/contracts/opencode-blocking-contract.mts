import taktHostControl from "../opencode/index.js"

const hooks = await taktHostControl({ directory: process.cwd() } as never)
const onMessage = hooks["chat.message"]
if (!onMessage) throw new Error("OpenCode chat.message hook is missing")
const onTool = hooks["tool.execute.before"]
if (!onTool) throw new Error("OpenCode tool.execute.before hook is missing")

async function blocked(action: () => Promise<void>): Promise<{ diagnostic: string; failure: string }> {
  let diagnostic = ""
  let failure = ""
  const stderrWrite = process.stderr.write
  process.stderr.write = ((chunk: string | Uint8Array) => {
    diagnostic += String(chunk)
    return true
  }) as typeof process.stderr.write
  try {
    await action()
  } catch (error) {
    failure = String(error)
  } finally {
    process.stderr.write = stderrWrite
  }
  return { diagnostic, failure }
}

const message = await blocked(() => onMessage(
  { sessionID: "contract" },
  { parts: [{ type: "text", text: "TAKT_HOST_BEGIN:" }] },
))
const usage = "Usage: /takt <task>"
if (!message.failure.includes(usage)) throw new Error(`OpenCode hook did not block: ${message.failure}`)
if (!message.diagnostic.includes(usage)) throw new Error(`OpenCode hook did not write diagnostic: ${message.diagnostic}`)

const begin = await blocked(() => onMessage(
  { sessionID: "begin-contract" },
  { parts: [{ type: "text", text: "TAKT_HOST_BEGIN:goal" }] },
))
if (!begin.failure.includes("preview marker")) throw new Error(`OpenCode hook lost preview: ${begin.failure}`)
if (!begin.diagnostic.includes("preview marker")) throw new Error(`OpenCode hook did not write preview: ${begin.diagnostic}`)

const tool = await blocked(() => onTool(
  { sessionID: "tool-contract", tool: "write", callID: "call" },
  { args: {} },
))
if (!tool.failure.includes("policy denied")) throw new Error(`OpenCode tool hook lost deny reason: ${tool.failure}`)
if (tool.failure.includes("unavailable")) throw new Error(`OpenCode tool hook misclassified deny: ${tool.failure}`)
