package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/rotemmiz/opcode42/conformance/result"
)

// Finding is one structural difference between the two runs.
type Finding struct {
	Scenario string
	Step     string
	Expected string
	Actual   string
	Detail   string // e.g. `properties.state.status: "completed" != "running"`
}

// Report is the full set of findings from a comparison.
type Report struct {
	Findings  []Finding
	ScenarioN int // total scenarios compared
}

// Compare diffs two result files step-by-step and returns all findings.
func Compare(expected, actual *result.File) *Report {
	r := &Report{ScenarioN: len(expected.Scenarios)}
	for i := range expected.Scenarios {
		exp := &expected.Scenarios[i]
		act := actual.ScenarioByName(exp.Name)
		if act == nil {
			r.add(exp.Name, "(scenario)", "present", "missing", "scenario absent from actual run")
			continue
		}
		if exp.Skipped || act.Skipped {
			continue
		}
		compareSteps(r, exp, act)
	}
	return r
}

func compareSteps(r *Report, exp, act *result.Scenario) {
	if len(exp.Steps) != len(act.Steps) {
		r.add(exp.Name, "(steps)",
			fmt.Sprintf("%d steps", len(exp.Steps)),
			fmt.Sprintf("%d steps", len(act.Steps)),
			"step count differs")
		return
	}
	for i := range exp.Steps {
		es, as := exp.Steps[i], act.Steps[i]
		label := stepLabel(i, es)

		if es.Status != as.Status {
			r.add(exp.Name, label,
				fmt.Sprintf("status %d", es.Status),
				fmt.Sprintf("status %d", as.Status),
				fmt.Sprintf("HTTP status: %d != %d", es.Status, as.Status))
		}
		for _, d := range diffJSONStrings(es.Body, as.Body) {
			r.add(exp.Name, label, es.Body, as.Body, "body."+d)
		}
		compareHeaders(r, exp.Name, label, es.Headers, as.Headers)
		compareSSE(r, exp.Name, label, es.SSE, as.SSE)
		compareEventTypes(r, exp.Name, label, es.EventTypes, as.EventTypes)
	}
}

// compareEventTypes diffs the de-duplicated SSE event-TYPE sequences captured by
// the live dual-run. It is a plain ordered string compare — the bodies are not
// diffed here (they are non-deterministic LLM output); only which event kinds
// appeared, and in what order, is the conformance signal.
func compareEventTypes(r *Report, scenario, label string, exp, act []string) {
	if len(exp) != len(act) {
		r.add(scenario, label,
			strings.Join(exp, ","), strings.Join(act, ","),
			fmt.Sprintf("SSE event-type count: %d != %d", len(exp), len(act)))
		return
	}
	for i := range exp {
		if exp[i] != act[i] {
			r.add(scenario, label, exp[i], act[i],
				fmt.Sprintf("SSE event-type #%d: %q != %q", i, exp[i], act[i]))
		}
	}
}

func compareHeaders(r *Report, scenario, label string, exp, act map[string]string) {
	for k, ev := range exp {
		av, ok := act[k]
		if !ok {
			r.add(scenario, label, ev, "(missing)", fmt.Sprintf("header.%s: present != missing", k))
			continue
		}
		if ev != av {
			r.add(scenario, label, ev, av, fmt.Sprintf("header.%s: %q != %q", k, ev, av))
		}
	}
	for k := range act {
		if _, ok := exp[k]; !ok {
			r.add(scenario, label, "(missing)", act[k], fmt.Sprintf("header.%s: missing != present", k))
		}
	}
}

func compareSSE(r *Report, scenario, label string, exp, act []string) {
	if len(exp) != len(act) {
		r.add(scenario, label,
			fmt.Sprintf("%d SSE events", len(exp)),
			fmt.Sprintf("%d SSE events", len(act)),
			fmt.Sprintf("SSE event count: %d != %d", len(exp), len(act)))
		return
	}
	for i := range exp {
		for _, d := range diffJSONStrings(exp[i], act[i]) {
			r.add(scenario, fmt.Sprintf("%s SSE event #%d", label, i),
				exp[i], act[i], d)
		}
	}
}

func stepLabel(i int, s result.Step) string {
	if s.Name != "" {
		return fmt.Sprintf("step %d (%s)", i+1, s.Name)
	}
	if s.Method != "" {
		return fmt.Sprintf("step %d (%s %s)", i+1, s.Method, s.Path)
	}
	return fmt.Sprintf("step %d", i+1)
}

func (r *Report) add(scenario, step, expected, actual, detail string) {
	r.Findings = append(r.Findings, Finding{
		Scenario: scenario, Step: step, Expected: expected, Actual: actual, Detail: detail,
	})
}

// Print writes the report in plan 12 §d format and returns an exit code: 0 if
// every finding is covered by a known divergence, 1 otherwise.
func (r *Report) Print(w io.Writer, divs []Divergence) int {
	p := func(format string, a ...any) { _, _ = fmt.Fprintf(w, format, a...) }

	var blocking, warned int
	failedScenarios := map[string]bool{}

	for _, f := range r.Findings {
		known := matchDivergence(divs, f.Scenario, f.Detail)
		tag := "DIFF"
		if known != "" {
			tag = "KNOWN-DIVERGENCE"
			warned++
		} else {
			blocking++
			failedScenarios[f.Scenario] = true
		}
		p("SCENARIO: %s\n", f.Scenario)
		p("%s: %s\n", strings.ToUpper(f.Step), tag)
		p("  EXPECTED: %s\n", truncate(f.Expected))
		p("  ACTUAL:   %s\n", truncate(f.Actual))
		p("  DETAIL:   %s\n", f.Detail)
		if known != "" {
			p("  REASON:   %s\n", known)
		}
		p("\n")
	}

	p("SUMMARY: %d blocking difference(s) in %d scenario(s); %d known-divergence warning(s); %d scenario(s) compared.\n",
		blocking, len(failedScenarios), warned, r.ScenarioN)
	if blocking > 0 {
		return 1
	}
	return 0
}

func truncate(s string) string {
	const maxLen = 200
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "…"
}

// diffJSONStrings parses both sides as JSON and returns path-qualified
// differences. If either side is not JSON, it compares them as plain strings.
func diffJSONStrings(expected, actual string) []string {
	if expected == actual {
		return nil
	}
	var ev, av any
	if json.Unmarshal([]byte(expected), &ev) != nil || json.Unmarshal([]byte(actual), &av) != nil {
		if expected == actual {
			return nil
		}
		return []string{fmt.Sprintf("%q != %q", expected, actual)}
	}
	var diffs []string
	diffJSON("", ev, av, &diffs)
	sort.Strings(diffs)
	return diffs
}

// diffJSON walks two decoded JSON values and appends "path: a != b" lines.
func diffJSON(path string, a, b any, out *[]string) {
	switch av := a.(type) {
	case map[string]any:
		bv, ok := b.(map[string]any)
		if !ok {
			*out = append(*out, fmt.Sprintf("%s: object != %s", at(path), typeName(b)))
			return
		}
		keys := map[string]bool{}
		for k := range av {
			keys[k] = true
		}
		for k := range bv {
			keys[k] = true
		}
		for _, k := range sortedKeys(keys) {
			ae, aok := av[k]
			be, bok := bv[k]
			switch {
			case aok && !bok:
				*out = append(*out, fmt.Sprintf("%s: present != missing", child(path, k)))
			case !aok && bok:
				*out = append(*out, fmt.Sprintf("%s: missing != present", child(path, k)))
			default:
				diffJSON(child(path, k), ae, be, out)
			}
		}
	case []any:
		bv, ok := b.([]any)
		if !ok {
			*out = append(*out, fmt.Sprintf("%s: array != %s", at(path), typeName(b)))
			return
		}
		if len(av) != len(bv) {
			*out = append(*out, fmt.Sprintf("%s: len %d != len %d", at(path), len(av), len(bv)))
			return
		}
		for i := range av {
			diffJSON(fmt.Sprintf("%s[%d]", path, i), av[i], bv[i], out)
		}
	default:
		if !scalarEqual(a, b) {
			*out = append(*out, fmt.Sprintf("%s: %s != %s", at(path), scalar(a), scalar(b)))
		}
	}
}

func sortedKeys(m map[string]bool) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

func child(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}

func at(path string) string {
	if path == "" {
		return "(root)"
	}
	return path
}

func scalarEqual(a, b any) bool { return scalar(a) == scalar(b) }

func scalar(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func typeName(v any) string {
	switch v.(type) {
	case map[string]any:
		return "object"
	case []any:
		return "array"
	case nil:
		return "null"
	default:
		return "scalar"
	}
}

// Divergence is one entry in the known-divergence registry. Scenario matches the
// finding's scenario (exact, or a trailing "*" prefix). Detail, when set, further
// scopes the entry to findings whose detail CONTAINS that substring — so a single
// known field gap (e.g. "info.mode") is suppressed without masking every other
// difference in the same scenario. An empty Detail suppresses the whole scenario
// (the original, coarse behavior).
type Divergence struct {
	Scenario string `json:"scenario"`
	Detail   string `json:"detail,omitempty"`
	Reason   string `json:"reason"`
	Phase    string `json:"phase"`
}

func loadDivergences(path string) ([]Divergence, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // no registry => everything is blocking
		}
		return nil, err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, nil // empty file => no registry, same as missing
	}
	var d []Divergence
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	return d, nil
}

// matchDivergence returns the reason if the finding is a known divergence. A
// registry entry "foo-*" matches any scenario with the prefix "foo-"; an entry
// with a non-empty Detail only matches when the finding's detail contains it.
func matchDivergence(divs []Divergence, scenario, detail string) string {
	for _, d := range divs {
		if !scenarioMatches(d.Scenario, scenario) {
			continue
		}
		if d.Detail != "" && !strings.Contains(detail, d.Detail) {
			continue
		}
		return d.Reason
	}
	return ""
}

func scenarioMatches(pattern, scenario string) bool {
	if pat, ok := strings.CutSuffix(pattern, "*"); ok {
		return strings.HasPrefix(scenario, pat)
	}
	return pattern == scenario
}
