package tui

// Model variants (plan 08b §7). A model can expose alternate configurations
// (e.g. a reasoning/effort "variant") via Model.variants; the chosen variant id
// rides POST /session/{id}/message as an optional `variant` field. The picker +
// ctrl+t cycle hang off the active model.

// activeVariants returns the variant ids of the currently selected model.
func (m Model) activeVariants() []string {
	for _, ch := range m.choices {
		if ch.Provider == m.model.Provider && ch.Model == m.model.Model {
			return ch.Variants
		}
	}
	return nil
}

// indexOfString returns the position of v in ss, or -1.
func indexOfString(ss []string, v string) int {
	for i := range ss {
		if ss[i] == v {
			return i
		}
	}
	return -1
}

// cycleVariant advances to the next variant of the active model (wrapping). The
// variants list already includes "default", so cycling reaches the no-variant
// state without a synthetic entry.
func (m Model) cycleVariant() Model {
	vs := m.activeVariants()
	if len(vs) == 0 {
		m.status = "this model has no variants"
		return m
	}
	next := vs[(indexOfString(vs, m.model.Variant)+1)%len(vs)]
	m.model.Variant = next
	m.status = "variant · " + next
	m.persist()
	return m
}

// variantSelIndex is the active variant's row in the picker (0 when unset).
func (m Model) variantSelIndex() int {
	if i := indexOfString(m.activeVariants(), m.model.Variant); i > 0 {
		return i
	}
	return 0
}
