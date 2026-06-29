package server

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/rotemmiz/opcode42/internal/instance"
)

// registerQuestionRoutes wires the question reply/reject endpoints onto the
// per-directory instance's question manager (M6).
func registerQuestionRoutes(reg func(method, path string, h http.HandlerFunc), instances *instance.Manager) {
	reg(http.MethodPost, "/question/{requestID}/reply", questionReplyHandler(instances))
	reg(http.MethodPost, "/question/{requestID}/reject", questionRejectHandler(instances))
}

// questionReplyBody is the POST /question/:id/reply request shape: one answer
// (an array of selected option labels) per question, in order (openapi).
type questionReplyBody struct {
	Answers [][]string `json:"answers"`
}

// questionReplyHandler answers a pending question request. A missing/already-
// answered request maps to a 404 QuestionNotFoundError; success returns `true`
// (server/.../handlers/question.ts:16-37).
func questionReplyHandler(instances *instance.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		requestID := chi.URLParam(r, "requestID")
		var body questionReplyBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "BadRequest", "invalid JSON body")
			return
		}
		inst := instances.Get(DirectoryFromContext(r.Context()))
		if err := inst.Questions.Reply(requestID, body.Answers); err != nil {
			writeNotFoundRequest(w, "QuestionNotFoundError", "Question", requestID)
			return
		}
		writeJSON(w, http.StatusOK, true)
	}
}

// questionRejectHandler dismisses a pending question request
// (server/.../handlers/question.ts:39-50).
func questionRejectHandler(instances *instance.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		requestID := chi.URLParam(r, "requestID")
		inst := instances.Get(DirectoryFromContext(r.Context()))
		if err := inst.Questions.Reject(requestID); err != nil {
			writeNotFoundRequest(w, "QuestionNotFoundError", "Question", requestID)
			return
		}
		writeJSON(w, http.StatusOK, true)
	}
}
