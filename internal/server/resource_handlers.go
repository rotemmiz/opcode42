package server

import (
	"net/http"

	"github.com/rotemmiz/forge/internal/config"
	"github.com/rotemmiz/forge/internal/engine/catalog"
	"github.com/rotemmiz/forge/internal/resource"
)

// registerResourceRoutes wires the resource-listing endpoints the TUI reads:
// agents, commands, and the provider list (plan 04 M8).
func registerResourceRoutes(reg func(method, path string, h http.HandlerFunc), cat catalog.Catalog) {
	reg(http.MethodGet, "/agent", agentListHandler())
	reg(http.MethodGet, "/command", commandListHandler())
	reg(http.MethodGet, "/provider", providerListHandler(cat))
	reg(http.MethodGet, "/skill", skillListHandler())
}

// skillListHandler serves the .opencode/{skill,skills} skills for the request
// directory (GET /skill).
func skillListHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		dir := DirectoryFromContext(r.Context())
		skills := resource.LoadSkills(dir)
		if skills == nil {
			skills = []resource.Skill{}
		}
		writeJSON(w, http.StatusOK, skills)
	}
}

// agentListHandler serves the built-in + .opencode/agent(s) agents for the
// request directory (GET /agent).
func agentListHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		dir := DirectoryFromContext(r.Context())
		writeJSON(w, http.StatusOK, resource.LoadAgents(dir, loadConfig(dir)))
	}
}

// commandListHandler serves the .opencode/command(s) + config commands for the
// request directory (GET /command).
func commandListHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		dir := DirectoryFromContext(r.Context())
		writeJSON(w, http.StatusOK, resource.LoadCommands(dir, loadConfig(dir)))
	}
}

// providerListHandler serves the models.dev catalog with connected/default
// status (GET /provider).
func providerListHandler(cat catalog.Catalog) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		dir := DirectoryFromContext(r.Context())
		writeJSON(w, http.StatusOK, resource.BuildProviderList(cat, loadConfig(dir)))
	}
}

// loadConfig loads the merged opencode config for dir, returning an empty map on
// error so resource listing degrades gracefully rather than 500-ing.
func loadConfig(dir string) map[string]any {
	cfg, err := config.Load(dir)
	if err != nil {
		return map[string]any{}
	}
	return cfg
}
