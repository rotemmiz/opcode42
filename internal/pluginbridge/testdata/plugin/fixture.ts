// Fixture opencode-format plugin for the plugin-host integration test.
// Uses the new PluginModule.server shape (opencode/packages/plugin/src/index.ts:74-80).
// Exercises: chat.params mutation, a registered tool, and an event hook.

export const server = async () => {
  return {
    "chat.params": async (_input: any, output: any) => {
      output.temperature = 0.123
    },
    tool: {
      fixture_echo: {
        description: "Echoes its input back",
        parameters: { type: "object", properties: { text: { type: "string" } } },
        execute: async (args: any) => ({
          title: "echo",
          output: String(args?.text ?? ""),
        }),
      },
    },
    event: async (_input: any) => {
      // observe-only; nothing to assert here beyond no-throw
    },
  }
}
