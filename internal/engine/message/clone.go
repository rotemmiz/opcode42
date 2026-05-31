package message

// ClonePart returns a shallow copy of a part so it can be published on the event
// bus without racing the processor, which mutates parts in place (e.g. appends
// streamed text to a TextPart) after an earlier message.part.updated has been
// emitted. A shallow copy is sufficient: the racy fields are value types (the
// accumulated Text string) and maps that the processor reassigns rather than
// mutates in place, so the clone's field keeps pointing at an immutable value.
func ClonePart(p Part) Part {
	switch t := p.(type) {
	case *TextPart:
		c := *t
		if c.Time != nil { // *PartTime is mutated in place (Time.End on text-end)
			tc := *c.Time
			c.Time = &tc
		}
		return &c
	case *ReasoningPart:
		c := *t
		return &c
	case *FilePart:
		c := *t
		return &c
	case *ToolPart:
		c := *t
		return &c
	case *StepStartPart:
		c := *t
		return &c
	case *StepFinishPart:
		c := *t
		return &c
	case *PatchPart:
		c := *t
		return &c
	case *CompactionPart:
		c := *t
		return &c
	case *SubtaskPart:
		c := *t
		return &c
	default:
		return p // rawPart and unknowns are already immutable for our purposes
	}
}

// CloneAssistant returns a shallow copy of an assistant message so it can be
// published without racing the processor's in-place finalization (Time.Completed,
// Error, token counts). Those are reassigned, not mutated, so a shallow copy is
// race-safe for the event payload.
func CloneAssistant(a *AssistantMessage) *AssistantMessage {
	if a == nil {
		return nil
	}
	c := *a
	return &c
}
