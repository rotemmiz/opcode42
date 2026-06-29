package message

import (
	"fmt"

	"github.com/rotemmiz/opcode42/internal/engine/llm"
)

// abortedErrorName is the NamedError name for a cancelled run (message-v2.ts:41).
const abortedErrorName = "MessageAbortedError"

// syntheticAttachmentPrompt prefixes media lifted out of a tool result into a
// follow-up user message (message-v2.ts:38).
const syntheticAttachmentPrompt = "Attached media from tool result:"

// SerializeModel identifies the model a request targets, for the serializer's
// model-dependent branches (provider-metadata replay, media handling).
type SerializeModel struct {
	ProviderID string
	ModelID    string
	// SupportsMediaInToolResult reports whether the target model accepts the
	// given media MIME type inline in a tool result. When nil, the
	// OpenAI-compatible default (true) applies. Opcode42 always promotes media to a
	// follow-up user message regardless (see ToModelMessages) — this flag only
	// gates opencode parity branches and is a no-op in M1.
	SupportsMediaInToolResult func(mime string) bool
}

func (m SerializeModel) ref() string { return m.ProviderID + "/" + m.ModelID }

// SerializeOptions tunes serialization (compaction-time pruning).
type SerializeOptions struct {
	// StripMedia replaces media file parts with a text placeholder.
	StripMedia bool
	// ToolOutputMaxChars truncates tool output (0 = unlimited).
	ToolOutputMaxChars int
}

// ToModelMessages converts persisted messages+parts into the provider-neutral
// []llm.ModelMessage a request carries. It ports opencode's toModelMessages
// (message-v2.ts:630-913) plus the AI SDK convertToModelMessages flattening
// that opencode delegates to: an assistant turn is split at step-start
// boundaries into (assistant, tool) message pairs so tool calls and their
// results land on the wire in the role-separated shape OpenAI/Anthropic expect.
//
// Known divergences (conformance-flagged):
//   - media attachments on tool results are uniformly promoted to a trailing
//     synthetic user message rather than ever kept inline in the tool result.
//     Opcode42's first providers are text-first; the media-heavy Anthropic path is
//     deferred (plan 02 addendum).
//   - provider-executed tools (metadata.providerExecuted) are not yet modeled:
//     opencode keeps such a tool's call+result inline in the assistant message,
//     while Opcode42 always splits a tool-role message. No provider-executed tools
//     exist before the Anthropic/server-tool path, where this must be handled.
func ToModelMessages(input []WithParts, model SerializeModel, opts SerializeOptions) []llm.ModelMessage {
	var out []llm.ModelMessage
	for _, msg := range input {
		if len(msg.Parts) == 0 {
			continue
		}
		switch {
		case msg.Info.User != nil:
			if m, ok := serializeUser(msg.Parts, opts); ok {
				out = append(out, m)
			}
		case msg.Info.Assistant != nil:
			out = append(out, serializeAssistant(msg, model, opts)...)
		}
	}
	return out
}

func serializeUser(parts []Part, opts SerializeOptions) (llm.ModelMessage, bool) {
	msg := llm.ModelMessage{Role: llm.RoleUser}
	for _, p := range parts {
		switch part := p.(type) {
		case *TextPart:
			if !part.Ignored && part.Text != "" {
				msg.Content = append(msg.Content, llm.ContentPart{Kind: llm.ContentText, Text: part.Text})
			}
		case *FilePart:
			// text/plain and directory files are folded into text upstream.
			if part.MIME == "text/plain" || part.MIME == "application/x-directory" {
				continue
			}
			if opts.StripMedia && isMedia(part.MIME) {
				msg.Content = append(msg.Content, llm.ContentPart{
					Kind: llm.ContentText,
					Text: fmt.Sprintf("[Attached %s: %s]", part.MIME, fileName(part.Filename)),
				})
			} else {
				msg.Content = append(msg.Content, llm.ContentPart{
					Kind:      llm.ContentFile,
					URL:       part.URL,
					MediaType: part.MIME,
					Filename:  part.Filename,
				})
			}
		case *CompactionPart:
			msg.Content = append(msg.Content, llm.ContentPart{Kind: llm.ContentText, Text: "What did we do so far?"})
		case *SubtaskPart:
			msg.Content = append(msg.Content, llm.ContentPart{Kind: llm.ContentText, Text: "The following tool was executed by the user"})
		}
	}
	if len(msg.Content) == 0 {
		return llm.ModelMessage{}, false
	}
	return msg, true
}

// stepBuf accumulates one provider step's assistant content and tool results.
type stepBuf struct {
	assistant []llm.ContentPart
	results   []llm.ContentPart
}

func serializeAssistant(msg WithParts, model SerializeModel, opts SerializeOptions) []llm.ModelMessage {
	info := msg.Info.Assistant

	// Skip errored turns unless an abort left real (non step-start/reasoning) content.
	if info.Error != nil {
		aborted := info.Error.Name == abortedErrorName
		hasRealContent := false
		for _, p := range msg.Parts {
			if t := p.partType(); t != "step-start" && t != "reasoning" {
				hasRealContent = true
				break
			}
		}
		if !aborted || !hasRealContent {
			return nil
		}
	}

	differentModel := model.ref() != msg.Info.Assistant.ProviderID+"/"+msg.Info.Assistant.ModelID
	hasSignedReasoning := false
	for _, p := range msg.Parts {
		if r, ok := p.(*ReasoningPart); ok && signature(r.Metadata) != nil {
			hasSignedReasoning = true
			break
		}
	}

	var out []llm.ModelMessage
	var media []llm.ContentPart
	cur := &stepBuf{}
	flush := func() {
		if len(cur.assistant) > 0 {
			out = append(out, llm.ModelMessage{Role: llm.RoleAssistant, Content: cur.assistant})
		}
		if len(cur.results) > 0 {
			out = append(out, llm.ModelMessage{Role: llm.RoleTool, Content: cur.results})
		}
		cur = &stepBuf{}
	}

	for _, p := range msg.Parts {
		switch part := p.(type) {
		case *StepStartPart:
			flush()
		case *TextPart:
			text := part.Text
			if text == "" && hasSignedReasoning {
				text = " " // structural separator for signed reasoning (see message-v2.ts:760-770).
			}
			cp := llm.ContentPart{Kind: llm.ContentText, Text: text}
			if !differentModel {
				cp.ProviderMetadata = part.Metadata
			}
			cur.assistant = append(cur.assistant, cp)
		case *ReasoningPart:
			if differentModel {
				if hasNonSpace(part.Text) {
					cur.assistant = append(cur.assistant, llm.ContentPart{Kind: llm.ContentText, Text: part.Text})
				}
				continue
			}
			cur.assistant = append(cur.assistant, llm.ContentPart{
				Kind: llm.ContentReasoning, Text: part.Text, ProviderMetadata: part.Metadata,
			})
		case *ToolPart:
			call, result, toolMedia := serializeTool(part, differentModel, opts)
			cur.assistant = append(cur.assistant, call)
			cur.results = append(cur.results, result)
			media = append(media, toolMedia...)
		}
	}
	flush()

	if len(out) > 0 && len(media) > 0 {
		userMsg := llm.ModelMessage{Role: llm.RoleUser, Content: append(
			[]llm.ContentPart{{Kind: llm.ContentText, Text: syntheticAttachmentPrompt}}, media...)}
		out = append(out, userMsg)
	}
	return out
}

// serializeTool turns a tool part into its assistant tool-call content, the
// matching tool-result content, and any media to promote to a user message.
func serializeTool(part *ToolPart, differentModel bool, opts SerializeOptions) (call, result llm.ContentPart, media []llm.ContentPart) {
	call = llm.ContentPart{Kind: llm.ContentToolCall, ToolCallID: part.CallID, ToolName: part.Tool}
	result = llm.ContentPart{Kind: llm.ContentToolResult, ToolCallID: part.CallID, ToolName: part.Tool}
	if !differentModel {
		call.ProviderMetadata = providerMeta(part.Metadata)
		result.ProviderMetadata = providerMeta(part.Metadata)
	}

	switch part.Status() {
	case ToolCompleted:
		var st ToolStateCompleted
		_ = decodeState(part.State, &st)
		call.Input = st.Input
		result.Input = st.Input
		if st.Time.Compacted != nil {
			result.Output = "[Old tool result content cleared]"
		} else {
			result.Output = truncateToolOutput(st.Output, opts.ToolOutputMaxChars)
			if !opts.StripMedia {
				media = mediaAttachments(st.Attachments)
			}
		}
	case ToolError:
		var st ToolStateError
		_ = decodeState(part.State, &st)
		call.Input = st.Input
		result.Input = st.Input
		// An interrupted tool may carry a string output to replay.
		if out, ok := st.Metadata["output"].(string); ok && st.Metadata["interrupted"] == true {
			result.Output = out
		} else {
			result.IsError = true
			result.Output = st.Error
		}
	default: // pending / running: dangling tool_use must get a result.
		var st ToolStatePending
		_ = decodeState(part.State, &st)
		call.Input = st.Input
		result.Input = st.Input
		result.IsError = true
		result.Output = "[Tool execution was interrupted]"
	}
	return call, result, media
}

func mediaAttachments(atts []FilePart) []llm.ContentPart {
	var media []llm.ContentPart
	for _, a := range atts {
		if isMedia(a.MIME) {
			media = append(media, llm.ContentPart{Kind: llm.ContentFile, URL: a.URL, MediaType: a.MIME, Filename: a.Filename})
		}
	}
	return media
}
