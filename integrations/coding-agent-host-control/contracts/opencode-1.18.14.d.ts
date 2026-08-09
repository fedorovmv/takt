declare module "@opencode-ai/plugin" {
  export type Part = { type: string; text?: string; content?: unknown; parts?: unknown }

  export type PluginInput = {
    directory: string
  }

  export type Hooks = {
    "chat.message"?: (
      input: { sessionID: string },
      output: { parts: Part[] },
    ) => Promise<void>
    "tool.execute.before"?: (
      input: { tool: string; sessionID: string; callID: string },
      output: { args: unknown },
    ) => Promise<void>
  }

  export type Plugin = (input: PluginInput) => Promise<Hooks>
}
