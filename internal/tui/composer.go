package tui

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	forgeclient "github.com/rotemmiz/forge/sdk/go"
)

// promptModel is the provider/model a submitted prompt targets.
type promptModel struct {
	Provider string
	Model    string
}

func (p promptModel) ok() bool { return p.Provider != "" && p.Model != "" }

func (p promptModel) label() string {
	if !p.ok() {
		return "no model"
	}
	return p.Provider + "/" + p.Model
}

// Composer / submit messages.
type (
	promptSentMsg     struct{ err error }
	sessionCreatedMsg struct {
		session   Session
		text      string // a prompt to send after creation, or…
		command   string // …a daemon command to run after creation (with arguments)
		arguments string
		err       error
	}
	configLoadedMsg struct{ provider, model string }
)

// promptBody is the POST /session/:id/message request body.
type promptBody struct {
	Model promptModelWire `json:"model"`
	Agent string          `json:"agent,omitempty"`
	Parts []partInput     `json:"parts"`
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
func loadConfigCmd(ctx context.Context, c *forgeclient.ForgeClient) tea.Cmd {
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
func promptCmd(ctx context.Context, c *forgeclient.ForgeClient, sessionID, text string, pm promptModel, agent string) tea.Cmd {
	return func() tea.Msg {
		body := promptBody{
			Model: promptModelWire{ProviderID: pm.Provider, ModelID: pm.Model},
			Agent: agent,
			Parts: []partInput{{Type: "text", Text: text}},
		}
		err := c.PostJSON(ctx, "/session/"+sessionID+"/message", body, nil)
		return promptSentMsg{err: err}
	}
}

// createSessionCmd creates a session, carrying the prompt text to send next.
func createSessionCmd(ctx context.Context, c *forgeclient.ForgeClient, text string) tea.Cmd {
	return func() tea.Msg {
		var ss Session
		err := c.PostJSON(ctx, "/session", map[string]any{}, &ss)
		return sessionCreatedMsg{session: ss, text: text, err: err}
	}
}
