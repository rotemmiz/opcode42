package tui

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	opcode42client "github.com/rotemmiz/opcode42/sdk/go"
)

// promptModel is the provider/model a submitted prompt targets.
type promptModel struct {
	Provider string
	Model    string
	Variant  string // model variant id (plan 08b §7); "" / "default" = no variant
}

func (p promptModel) ok() bool { return p.Provider != "" && p.Model != "" }

func (p promptModel) label() string {
	if !p.ok() {
		return "no model"
	}
	l := p.Provider + "/" + p.Model
	if v := p.effectiveVariant(); v != "" {
		l += " (" + v + ")"
	}
	return l
}

// effectiveVariant is the variant to send on the wire ("" when none/"default").
func (p promptModel) effectiveVariant() string {
	if p.Variant == "default" {
		return ""
	}
	return p.Variant
}

// Composer / submit messages.
type (
	promptSentMsg     struct{ err error }
	shellSentMsg      struct{ err error }
	sessionCreatedMsg struct {
		session   Session
		text      string // a prompt to send after creation, or…
		command   string // …a daemon command to run after creation (with arguments)
		arguments string
		err       error
	}
	configLoadedMsg struct{ provider, model string }
)

// shellBody is the POST /session/:id/shell request body (opencode ShellInput).
// agent is required by the endpoint; model is optional.
type shellBody struct {
	Command string          `json:"command"`
	Agent   string          `json:"agent"`
	Model   promptModelWire `json:"model,omitempty"`
}

// shellCmd runs a shell command in the session context (POST /session/:id/shell).
// The command's output streams back as normal tool parts via message.part.* SSE.
func shellCmd(ctx context.Context, c *opcode42client.Opcode42Client, sessionID, command, agent string, pm promptModel) tea.Cmd {
	return func() tea.Msg {
		body := shellBody{Command: command, Agent: agent}
		if pm.ok() {
			body.Model = promptModelWire{ProviderID: pm.Provider, ModelID: pm.Model}
		}
		return shellSentMsg{err: c.PostJSON(ctx, "/session/"+sessionID+"/shell", body, nil)}
	}
}

// promptBody is the POST /session/:id/message request body.
type promptBody struct {
	Model   promptModelWire `json:"model"`
	Agent   string          `json:"agent,omitempty"`
	Variant string          `json:"variant,omitempty"` // model variant (plan 08b §7)
	Parts   []partInput     `json:"parts"`
}

type promptModelWire struct {
	ProviderID string `json:"providerID"`
	ModelID    string `json:"modelID"`
}

type partInput struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// loadConfigCmd fetches /config to resolve a default model when no --provider/
// --model was given. opencode stores it as "providerID/modelID".
func loadConfigCmd(ctx context.Context, c *opcode42client.Opcode42Client) tea.Cmd {
	return func() tea.Msg {
		var cfg struct {
			Model string `json:"model"`
		}
		if err := c.GetJSON(ctx, "/config", &cfg); err != nil {
			return configLoadedMsg{}
		}
		provider, model, _ := strings.Cut(cfg.Model, "/")
		return configLoadedMsg{provider: provider, model: model}
	}
}

// promptCmd submits a prompt to an existing session (under the given agent).
func promptCmd(ctx context.Context, c *opcode42client.Opcode42Client, sessionID, text string, pm promptModel, agent string) tea.Cmd {
	return func() tea.Msg {
		body := promptBody{
			Model:   promptModelWire{ProviderID: pm.Provider, ModelID: pm.Model},
			Agent:   agent,
			Variant: pm.effectiveVariant(),
			Parts:   []partInput{{Type: "text", Text: text}},
		}
		err := c.PostJSON(ctx, "/session/"+sessionID+"/message", body, nil)
		return promptSentMsg{err: err}
	}
}

// createSessionCmd creates a session, carrying the prompt text to send next.
func createSessionCmd(ctx context.Context, c *opcode42client.Opcode42Client, text string) tea.Cmd {
	return func() tea.Msg {
		var ss Session
		err := c.PostJSON(ctx, "/session", map[string]any{}, &ss)
		return sessionCreatedMsg{session: ss, text: text, err: err}
	}
}
