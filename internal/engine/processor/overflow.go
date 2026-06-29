package processor

import (
	"github.com/rotemmiz/opcode42/internal/engine/catalog"
	"github.com/rotemmiz/opcode42/internal/engine/message"
)

// isOverflow reports whether a usage block has filled the model's usable context
// and the run should compact, mirroring opencode's overflow.ts: the full token
// block (preferring the reported total) is compared against the usable input
// budget — the model's input limit, or context minus the reserved output budget.
// A model with no known limits never overflows. The precise reserve/threshold is
// finalized with compaction in M10.
func isOverflow(tokens message.TokenCounts, model catalog.Model) bool {
	usable := usableContext(model)
	if usable <= 0 {
		return false
	}
	return totalTokens(tokens) >= usable
}

// usableContext is the input-token budget before compaction is needed.
func usableContext(model catalog.Model) float64 {
	if model.Limit.Input > 0 {
		return float64(model.Limit.Input)
	}
	if model.Limit.Context <= 0 {
		return 0
	}
	usable := float64(model.Limit.Context) - float64(model.Limit.Output)
	if usable <= 0 {
		usable = float64(model.Limit.Context)
	}
	return usable
}

func totalTokens(t message.TokenCounts) float64 {
	if t.Total != nil && *t.Total > 0 {
		return *t.Total
	}
	return t.Input + t.Output + t.Cache.Read + t.Cache.Write
}
