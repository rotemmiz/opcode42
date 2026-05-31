package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/rotemmiz/forge/internal/engine"
	"github.com/rotemmiz/forge/internal/engine/permission"
	"github.com/rotemmiz/forge/internal/engine/runstate"
	"github.com/rotemmiz/forge/internal/engine/tool"
	"github.com/rotemmiz/forge/internal/instance"
	"github.com/rotemmiz/forge/internal/resource"
	"github.com/rotemmiz/forge/internal/session"
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
// instance (its bus, permission manager, and per-session run lock). rulesets are
// the resolved agent's permission rules (the executor evaluates tool calls
// against them; an unmatched call defaults to "ask" and blocks on a
// permission.asked event).
func buildEngine(opts Options, inst *instance.Context, directory string, rulesets []permission.Ruleset, subagent tool.SubagentRunner) *engine.Engine {
	return engine.New(engine.Config{
		Store:       opts.Messages,
		Catalog:     opts.Catalog,
		Registry:    opts.Registry,
		Providers:   opts.Providers,
		Permissions: inst.Permissions,
		Questions:   inst.Questions,
		Subagent:    subagent,
		Bus:         inst.Bus,
		RunState:    inst.RunState,
		Directory:   directory,
		Rulesets:    rulesets,
	})
}

// resolveAgent loads the directory's agents and returns the one named by the
// prompt (defaulting to "build"); an unknown name falls back to "build" so a
// prompt always has a runnable agent (agent/agent.ts list/default semantics).
func resolveAgent(directory, name string) resource.Agent {
	if name == "" {
		name = "build"
	}
	agents := resource.LoadAgents(directory, loadConfig(directory))
	var build resource.Agent
	for _, a := range agents {
		switch a.Name {
		case name:
			return a
		case "build":
			build = a
		}
	}
	return build
}

// agentRulesets wraps the agent's permission ruleset for the engine. A resolved
// agent's rules are used as-is (build is allow-all; a restrictive agent leaves
// tools to "ask"); only a wholly-unresolved agent falls back to allow-all so
// tools stay usable.
func agentRulesets(a resource.Agent) []permission.Ruleset {
	if a.Name == "" {
		return allowAllRulesets
	}
	return []permission.Ruleset{a.Permission}
}

func promptHandler(opts Options, async bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sessionID := chi.URLParam(r, "sessionID")
		if !requireSession(w, r, opts, sessionID) {
			return
		}
		var body promptBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "BadRequest", "invalid JSON body")
			return
		}
		directory := DirectoryFromContext(r.Context())
		agent := resolveAgent(directory, body.Agent)

		// The agent supplies defaults the request may override: its model (when
		// the request omits one) and its system prompt (when none is given).
		if body.Model.ProviderID == "" && agent.Model != nil {
			body.Model.ProviderID = agent.Model.ProviderID
			body.Model.ModelID = agent.Model.ModelID
		}
		if body.Model.ProviderID == "" || body.Model.ModelID == "" {
			writeError(w, http.StatusBadRequest, "BadRequest", "model.providerID and model.modelID are required")
			return
		}
		if body.System == "" {
			body.System = agent.Prompt
		}

		inst := opts.Instances.Get(directory)
		runner := subagentRunner{opts: opts, inst: inst, directory: directory,
			provider: body.Model.ProviderID, model: body.Model.ModelID}
		eng := buildEngine(opts, inst, directory, agentRulesets(agent), runner)

		// Detach from the request context: a client disconnect must NOT abort the
		// run — only POST /abort cancels it (via the run lock), matching opencode.
		// WithoutCancel keeps request-scoped values but drops cancellation/deadline.
		runCtx := context.WithoutCancel(r.Context())

		if async {
			// opencode returns 204 No Content (handlers/session.ts:321).
			go func() { _, _ = eng.Prompt(runCtx, body.toInput(sessionID)) }()
			w.WriteHeader(http.StatusNoContent)
			return
		}

		out, err := eng.Prompt(runCtx, body.toInput(sessionID))
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
		if !requireSession(w, r, opts, sessionID) {
			return
		}
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

// requireSession writes a 404 and returns false if the session does not exist,
// matching opencode's requireSession gate (handlers/session.ts:78-80).
func requireSession(w http.ResponseWriter, r *http.Request, opts Options, sessionID string) bool {
	if _, err := opts.Sessions.Get(r.Context(), sessionID); err != nil {
		if errors.Is(err, session.ErrNotFound) {
			writeError(w, http.StatusNotFound, "NotFound", "session not found: "+sessionID)
		} else {
			writeError(w, http.StatusInternalServerError, "StorageError", err.Error())
		}
		return false
	}
	return true
}

func writeError(w http.ResponseWriter, status int, tag, msg string) {
	writeJSON(w, status, map[string]any{"_tag": tag, "message": msg})
}

// writeNotFoundRequest emits opencode's 404 shape for a missing permission or
// question request: {_tag, requestID, message} (handlers/permission.ts,
// handlers/question.ts).
func writeNotFoundRequest(w http.ResponseWriter, tag, noun, requestID string) {
	writeJSON(w, http.StatusNotFound, map[string]any{
		"_tag":      tag,
		"requestID": requestID,
		"message":   noun + " request not found: " + requestID,
	})
}
