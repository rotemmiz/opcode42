package server

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/rotemmiz/forge/internal/engine"
	"github.com/rotemmiz/forge/internal/engine/permission"
	"github.com/rotemmiz/forge/internal/engine/runstate"
	"github.com/rotemmiz/forge/internal/instance"
)

// allowAllRulesets is the Phase-B default permission policy for the HTTP prompt
// path: allow every tool. Config/agent-driven permission rules (which would let
// tools default to "ask" and block on a permission.asked SSE) arrive with the
// resource loaders in plan 04. Until a client can answer permission prompts,
// allow-all keeps the local single-user daemon usable end-to-end.
var allowAllRulesets = []permission.Ruleset{{{Permission: "*", Pattern: "*", Action: permission.ActionAllow}}}

// registerPromptRoutes wires the prompt/message endpoints onto the agent engine.
func registerPromptRoutes(reg func(method, path string, h http.HandlerFunc), opts Options) {
	reg(http.MethodPost, "/session/{sessionID}/message", promptHandler(opts, false))
	reg(http.MethodPost, "/session/{sessionID}/prompt_async", promptHandler(opts, true))
	reg(http.MethodGet, "/session/{sessionID}/message", listMessagesHandler(opts))
	reg(http.MethodPost, "/session/{sessionID}/abort", abortHandler(opts))
}

// promptBody is the POST /session/:id/message request shape (openapi).
type promptBody struct {
	MessageID string `json:"messageID"`
	Model     struct {
		ProviderID string `json:"providerID"`
		ModelID    string `json:"modelID"`
	} `json:"model"`
	Agent   string          `json:"agent"`
	NoReply bool            `json:"noReply"`
	System  string          `json:"system"`
	Tools   map[string]bool `json:"tools"`
	Parts   []promptPart    `json:"parts"`
}

type promptPart struct {
	Type string `json:"type"`
	Text string `json:"text"`
	MIME string `json:"mime"`
	URL  string `json:"url"`
}

func (b promptBody) toInput(sessionID string) engine.PromptInput {
	parts := make([]engine.PartInput, 0, len(b.Parts))
	for _, p := range b.Parts {
		parts = append(parts, engine.PartInput{Type: p.Type, Text: p.Text, MIME: p.MIME, URL: p.URL})
	}
	return engine.PromptInput{
		SessionID: sessionID, Parts: parts, Agent: b.Agent,
		Provider: b.Model.ProviderID, Model: b.Model.ModelID,
		System: b.System, Tools: b.Tools,
	}
}

// buildEngine constructs the per-request engine from shared deps + the request's
// instance (its bus, permission manager, and per-session run lock).
func buildEngine(opts Options, inst *instance.Context, directory string) *engine.Engine {
	return engine.New(engine.Config{
		Store:       opts.Messages,
		Catalog:     opts.Catalog,
		Registry:    opts.Registry,
		Providers:   opts.Providers,
		Permissions: inst.Permissions,
		Bus:         inst.Bus,
		RunState:    inst.RunState,
		Directory:   directory,
		Rulesets:    allowAllRulesets,
	})
}

func promptHandler(opts Options, async bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sessionID := chi.URLParam(r, "sessionID")
		var body promptBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "BadRequest", "invalid JSON body")
			return
		}
		if body.Model.ProviderID == "" || body.Model.ModelID == "" {
			writeError(w, http.StatusBadRequest, "BadRequest", "model.providerID and model.modelID are required")
			return
		}
		directory := DirectoryFromContext(r.Context())
		inst := opts.Instances.Get(directory)
		eng := buildEngine(opts, inst, directory)

		if async {
			// Run on the daemon's base context so the loop survives the request.
			go func() { _, _ = eng.Prompt(opts.BaseCtx, body.toInput(sessionID)) }()
			writeJSON(w, http.StatusOK, map[string]any{"sessionID": sessionID})
			return
		}

		out, err := eng.Prompt(r.Context(), body.toInput(sessionID))
		if err != nil {
			var busy *runstate.BusyError
			if errors.As(err, &busy) {
				writeError(w, http.StatusConflict, "BusyError", err.Error())
				return
			}
			writeError(w, http.StatusInternalServerError, "PromptError", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, out)
	}
}

func listMessagesHandler(opts Options) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sessionID := chi.URLParam(r, "sessionID")
		msgs, err := opts.Messages.List(r.Context(), sessionID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "StorageError", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, msgs)
	}
}

func abortHandler(opts Options) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sessionID := chi.URLParam(r, "sessionID")
		inst := opts.Instances.Get(DirectoryFromContext(r.Context()))
		inst.RunState.Cancel(sessionID)
		writeJSON(w, http.StatusOK, true)
	}
}

func writeError(w http.ResponseWriter, status int, tag, msg string) {
	writeJSON(w, status, map[string]any{"_tag": tag, "message": msg})
}
