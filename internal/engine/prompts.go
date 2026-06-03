package engine

import (
	"embed"
	"strings"
)

//go:embed prompts/*.txt
var promptFS embed.FS

// maxStepsSentinel is appended as an assistant message on the final allowed step
// so the model knows tools are disabled and it must answer with text only. Ported
// verbatim from opencode's prompt/max-steps.txt (prompt.ts:1451).
var maxStepsSentinel = mustPrompt("max-steps.txt")

// titlePrompt is the system prompt for the built-in title agent's title-generation
// stream (agent/prompt/title.txt, agent.ts:250-264).
var titlePrompt = mustPrompt("title.txt")

// structuredOutputDescription is the StructuredOutput tool's description, shown to
// the model when a json_schema response format is requested (prompt.ts:71).
const structuredOutputDescription = `Use this tool to return your final response in the requested structured format.

IMPORTANT:
- You MUST call this tool exactly once at the end of your response
- The input must be valid JSON matching the required schema
- Complete all necessary research and tool calls BEFORE calling this tool
- This tool provides your final answer - no further actions are taken after calling it`

// structuredOutputSystemPrompt is pushed as an extra system message when a
// json_schema response format is requested (prompt.ts:79).
const structuredOutputSystemPrompt = `IMPORTANT: The user has requested structured output. You MUST use the StructuredOutput tool to provide your final response. Do NOT respond with plain text - you MUST call the StructuredOutput tool with your answer formatted according to the schema.`

// structuredOutputToolName is the synthetic tool injected for json_schema output.
const structuredOutputToolName = "StructuredOutput"

func mustPrompt(name string) string {
	data, err := promptFS.ReadFile("prompts/" + name)
	if err != nil {
		panic("engine: missing embedded prompt " + name + ": " + err.Error())
	}
	return strings.TrimRight(string(data), "\n")
}
