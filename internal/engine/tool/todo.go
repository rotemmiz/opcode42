package tool

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// TodoItem is one entry in a session's todo list.
type TodoItem struct {
	ID      string `json:"id,omitempty"`
	Content string `json:"content"`
	Status  string `json:"status"` // pending | in_progress | completed | cancelled
}

// TodoStore holds per-session todo lists. It is safe for concurrent use.
type TodoStore struct {
	mu    sync.Mutex
	lists map[string][]TodoItem
}

// NewTodoStore creates an empty todo store.
func NewTodoStore() *TodoStore { return &TodoStore{lists: map[string][]TodoItem{}} }

// Set replaces a session's todo list.
func (s *TodoStore) Set(sessionID string, items []TodoItem) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lists[sessionID] = items
}

// Get returns a copy of a session's todo list.
func (s *TodoStore) Get(sessionID string) []TodoItem {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]TodoItem(nil), s.lists[sessionID]...)
}

// TodoWrite replaces the session's todo list and echoes it back.
type TodoWrite struct {
	Store *TodoStore
}

// Info describes the todowrite tool.
func (TodoWrite) Info() Info {
	return Info{
		ID:          "todowrite",
		Description: "Replace the session's TODO list to track multi-step work.",
		Parameters: obj(map[string]any{
			"todos": map[string]any{
				"type": "array",
				"items": obj(map[string]any{
					"id":      strProp("Stable id for the item."),
					"content": strProp("What the task is."),
					"status":  strProp("pending | in_progress | completed | cancelled"),
				}, "content", "status"),
			},
		}, "todos"),
	}
}

type todoParams struct {
	Todos []TodoItem `json:"todos"`
}

// Run stores the new list and returns a rendered summary.
func (t TodoWrite) Run(_ context.Context, input map[string]any, tctx Context) (Result, error) {
	var p todoParams
	if err := decode(input, &p); err != nil {
		return Result{}, err
	}
	if t.Store != nil {
		t.Store.Set(tctx.SessionID, p.Todos)
	}
	var b strings.Builder
	for _, item := range p.Todos {
		fmt.Fprintf(&b, "- [%s] %s\n", item.Status, item.Content)
	}
	return Result{Title: fmt.Sprintf("%d todos", len(p.Todos)), Output: b.String(),
		Metadata: map[string]any{"todos": p.Todos}}, nil
}
