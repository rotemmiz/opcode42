package tool

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// Patch applies a unified diff to files in the working directory.
//
// It supports standard unified-diff hunks (--- / +++ / @@ -a,b +c,d @@). The
// model-specific apply_patch envelope opencode uses for GPT-4-class models is
// not yet implemented (plan 02: patch is model-gated and off the OpenAI-first
// gate path).
type Patch struct{}

// Info describes the patch tool.
func (Patch) Info() Info {
	return Info{
		ID:          "patch",
		Description: "Apply a unified diff (--- / +++ / @@ hunks) to files in the working directory.",
		Parameters: obj(map[string]any{
			"patch": strProp("The unified diff to apply."),
		}, "patch"),
	}
}

type patchParams struct {
	Patch string `json:"patch"`
}

var hunkHeader = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)

// Run parses and applies the diff file-by-file.
func (Patch) Run(_ context.Context, input map[string]any, tctx Context) (Result, error) {
	var p patchParams
	if err := decode(input, &p); err != nil {
		return Result{}, err
	}
	if strings.TrimSpace(p.Patch) == "" {
		return Result{}, fmt.Errorf("patch: patch is required")
	}
	files := splitDiffByFile(p.Patch)
	if len(files) == 0 {
		return Result{}, fmt.Errorf("patch: no file headers (+++ ) found")
	}
	var changed []string
	for path, hunks := range files {
		abs := resolve(tctx, path)
		data, err := os.ReadFile(abs)
		if err != nil {
			return Result{}, fmt.Errorf("patch: %w", err)
		}
		updated, err := applyHunks(strings.Split(string(data), "\n"), hunks)
		if err != nil {
			return Result{}, fmt.Errorf("patch %s: %w", path, err)
		}
		if err := os.WriteFile(abs, []byte(strings.Join(updated, "\n")), 0o644); err != nil {
			return Result{}, fmt.Errorf("patch: %w", err)
		}
		changed = append(changed, path)
	}
	return Result{Title: "patch", Output: fmt.Sprintf("Patched %d file(s): %s", len(changed), strings.Join(changed, ", ")),
		Metadata: map[string]any{"files": changed}}, nil
}

// splitDiffByFile groups hunk bodies under their target file path (from +++).
func splitDiffByFile(diff string) map[string][]string {
	out := map[string][]string{}
	var cur string
	for _, line := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(line, "--- "):
			// old-file header; ignored, target comes from +++.
		case strings.HasPrefix(line, "+++ "):
			cur = stripDiffPrefix(strings.TrimPrefix(line, "+++ "))
		case cur != "":
			out[cur] = append(out[cur], line)
		}
	}
	return out
}

func stripDiffPrefix(p string) string {
	p = strings.TrimSpace(p)
	p = strings.TrimPrefix(p, "a/")
	p = strings.TrimPrefix(p, "b/")
	return p
}

// applyHunks applies a file's hunks to its lines, verifying context/removed lines.
func applyHunks(lines, hunkLines []string) ([]string, error) {
	result := append([]string(nil), lines...)
	offset := 0 // net line delta applied so far

	i := 0
	for i < len(hunkLines) {
		m := hunkHeader.FindStringSubmatch(hunkLines[i])
		if m == nil {
			i++
			continue
		}
		oldStart, _ := strconv.Atoi(m[1])
		pos := oldStart - 1 + offset // 0-based insertion cursor
		i++

		var replacement []string
		consumed := 0
		for i < len(hunkLines) {
			hl := hunkLines[i]
			if hunkHeader.MatchString(hl) {
				break
			}
			switch {
			case strings.HasPrefix(hl, "+"):
				replacement = append(replacement, hl[1:])
			case strings.HasPrefix(hl, "-"):
				idx := pos + consumed
				if idx >= len(result) || result[idx] != hl[1:] {
					return nil, fmt.Errorf("context mismatch at line %d", idx+1)
				}
				consumed++
			case strings.HasPrefix(hl, " "):
				idx := pos + consumed
				if idx < len(result) {
					replacement = append(replacement, result[idx])
				}
				consumed++
			default:
				// Bare "" (trailing-newline split artifact) or markers like
				// "\ No newline at end of file": ignore.
			}
			i++
		}
		if pos+consumed > len(result) {
			return nil, fmt.Errorf("hunk overruns file (line %d)", pos+consumed)
		}
		tail := append([]string(nil), result[pos+consumed:]...)
		result = append(result[:pos], append(replacement, tail...)...)
		offset += len(replacement) - consumed
	}
	return result, nil
}
