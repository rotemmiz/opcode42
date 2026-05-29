package message

// FilterCompacted reorders a session's messages for model consumption,
// collapsing summarized history. It is a 1:1 port of opencode's
// filterCompacted (message-v2.ts:1014-1065).
//
// Input ordering matches opencode's MessageV2.stream: NEWEST-FIRST. The result
// is oldest-first, with any active compaction rewritten to the shape
// [compaction-user, summary-assistant, ...retained tail..., ...later turns...].
func FilterCompacted(msgs []WithParts) []WithParts {
	result := make([]WithParts, 0, len(msgs))
	completed := map[string]bool{}
	var retain string // "" == undefined
	haveRetain := false

	for _, msg := range msgs {
		result = append(result, msg)
		if haveRetain {
			if msg.Info.ID() == retain {
				break
			}
			continue
		}
		if msg.Info.IsUser() && completed[msg.Info.ID()] {
			part := findCompaction(msg.Parts)
			if part == nil {
				continue
			}
			if part.TailStartID == "" {
				break
			}
			retain = part.TailStartID
			haveRetain = true
			if msg.Info.ID() == retain {
				break
			}
			continue
		}
		// NOTE: opencode's third `if` here is unreachable — the branch above
		// always continues or breaks for a completed user message — so it is
		// intentionally omitted.
		if a := msg.Info.Assistant; a != nil && a.Summary && a.Finish != "" && a.Error == nil {
			completed[a.ParentID] = true
		}
	}

	reverse(result)

	compactionIndex := findLastIndex(result, func(m WithParts) bool {
		if !m.Info.IsUser() {
			return false
		}
		p := findCompactionWithTail(m.Parts)
		return p != nil
	})
	if compactionIndex < 0 {
		return result
	}
	compaction := result[compactionIndex]
	part := findCompactionWithTail(compaction.Parts)

	summaryIndex := findIndex(result, func(m WithParts, index int) bool {
		a := m.Info.Assistant
		return index > compactionIndex && a != nil && a.Summary && a.ParentID == compaction.Info.ID()
	})

	tailIndex := -1
	if part != nil && part.TailStartID != "" {
		tailIndex = findIndex(result, func(m WithParts, _ int) bool {
			return m.Info.ID() == part.TailStartID
		})
	}

	if tailIndex >= 0 && tailIndex < compactionIndex && summaryIndex > compactionIndex {
		out := make([]WithParts, 0, len(result))
		out = append(out, result[compactionIndex:summaryIndex+1]...)
		out = append(out, result[tailIndex:compactionIndex]...)
		out = append(out, result[summaryIndex+1:]...)
		return out
	}
	return result
}

// Latest is the result of scanning a message list for the most recent
// user/assistant/finished-assistant message plus unprocessed tasks
// (message-v2.ts:1078-1094).
type Latest struct {
	User      *UserMessage
	Assistant *AssistantMessage
	Finished  *AssistantMessage
	// Tasks are compaction/subtask parts on user messages newer than the latest
	// finished assistant — i.e. unprocessed work.
	Tasks []Part
}

// LatestOf scans msgs (any order) for the newest message of each kind, ranking
// by monotonic id. Mirrors opencode's MessageV2.latest.
func LatestOf(msgs []WithParts) Latest {
	var out Latest
	for _, msg := range msgs {
		if u := msg.Info.User; u != nil && (out.User == nil || u.ID > out.User.ID) {
			out.User = u
		}
		if a := msg.Info.Assistant; a != nil {
			if out.Assistant == nil || a.ID > out.Assistant.ID {
				out.Assistant = a
			}
			if a.Finish != "" && (out.Finished == nil || a.ID > out.Finished.ID) {
				out.Finished = a
			}
		}
	}
	for _, msg := range msgs {
		if out.Finished != nil && msg.Info.ID() <= out.Finished.ID {
			continue
		}
		for _, p := range msg.Parts {
			switch p.partType() {
			case "compaction", "subtask":
				out.Tasks = append(out.Tasks, p)
			}
		}
	}
	return out
}

func findCompaction(parts []Part) *CompactionPart {
	for _, p := range parts {
		if c, ok := p.(*CompactionPart); ok {
			return c
		}
	}
	return nil
}

func findCompactionWithTail(parts []Part) *CompactionPart {
	for _, p := range parts {
		if c, ok := p.(*CompactionPart); ok && c.TailStartID != "" {
			return c
		}
	}
	return nil
}

func reverse(s []WithParts) {
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
}

func findIndex(s []WithParts, pred func(WithParts, int) bool) int {
	for i, m := range s {
		if pred(m, i) {
			return i
		}
	}
	return -1
}

func findLastIndex(s []WithParts, pred func(WithParts) bool) int {
	for i := len(s) - 1; i >= 0; i-- {
		if pred(s[i]) {
			return i
		}
	}
	return -1
}
