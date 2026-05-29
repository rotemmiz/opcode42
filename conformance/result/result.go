// Package result defines the dual-run result file schema shared by the
// conformance suite (which writes it, task C3) and the diff tool (which compares
// two of them, task C5).
//
// The suite normalizes volatile fields (via conformance/normalize) as it writes,
// because each run knows its own temp directory and paths. Result files are
// therefore already canonical/normalized, and the diff tool is a pure structural
// comparator.
package result

import (
	"encoding/json"
	"fmt"
	"os"
)

// File is one full run of the scenario suite against one target.
type File struct {
	Target    string     `json:"target"` // daemon URL or label, e.g. "opencode"
	Scenarios []Scenario `json:"scenarios"`
}

// Scenario is one named test scenario and its ordered steps.
type Scenario struct {
	Name    string `json:"name"`
	Skipped bool   `json:"skipped,omitempty"`
	Steps   []Step `json:"steps"`
}

// Step is one observable interaction: an HTTP request/response and/or a captured
// SSE event sequence. Bodies and SSE events hold normalized canonical JSON.
type Step struct {
	Name   string   `json:"name"`
	Method string   `json:"method,omitempty"`
	Path   string   `json:"path,omitempty"`
	Status int      `json:"status,omitempty"`
	Body   string   `json:"body,omitempty"`
	SSE    []string `json:"sse,omitempty"`
}

// Load reads a result file.
func Load(path string) (*File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var f File
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("decode result file %s: %w", path, err)
	}
	return &f, nil
}

// Save writes a result file as indented JSON.
func (f *File) Save(path string) error {
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// ScenarioByName returns the scenario with the given name, or nil.
func (f *File) ScenarioByName(name string) *Scenario {
	for i := range f.Scenarios {
		if f.Scenarios[i].Name == name {
			return &f.Scenarios[i]
		}
	}
	return nil
}
