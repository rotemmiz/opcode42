// Command downconvert rewrites the OpenAPI 3.1 reference into a derived 3.0.x
// document that oapi-codegen's (kin-openapi) loader accepts. It exists only to
// feed code generation (task S3) — the derived file is a throwaway build
// artifact, NOT the wire contract. The frozen contract stays 3.1 at
// conformance/openapi-reference.json and is never edited.
//
// The transforms are the minimal, deterministic set this spec needs (measured,
// not guessed); each is also a no-op when absent, so future spec updates that
// (re)introduce other 3.1 constructs degrade gracefully rather than corrupt:
//
//   - openapi: "3.1.x"             -> "3.0.3"
//   - exclusiveMinimum: <num>      -> minimum: <num>, exclusiveMinimum: true
//   - exclusiveMaximum: <num>      -> maximum: <num>, exclusiveMaximum: true
//   - type: ["T","null"]           -> type: "T", nullable: true
//   - type: "null"                 -> nullable: true (type dropped)
//   - anyOf/oneOf duplicate members -> deduped (a union of [A,A,B] is [A,B])
//   - anyOf/oneOf w/ {type:"null"} -> drop null member, parent nullable: true;
//     a lone remaining member is hoisted into the parent
//   - prefixItems: [...]           -> items: <first prefix item or {}> (tuple precision dropped)
//   - colliding schema Go-names    -> x-go-name injected on the non-canonical schema
//   - drop in-schema "$schema"     (JSON Schema dialect marker; not valid OpenAPI 3.0)
//
// On success it prints a one-line-per-transform summary to stderr so the set of
// "loosened" schemas is recorded on every regeneration.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// converter holds the document transforms and counts what it changed.
type converter struct {
	exclusiveMin int
	exclusiveMax int
	nullableType int
	unionDedupe  int
	nullUnion    int
	prefixItems  int
	renamed      []string
	// clientMode enables disambiguating component schemas whose Go name collides
	// with a generated "<OperationId>Response" client wrapper (only the client
	// generator emits those wrappers, so the server derived spec leaves it off).
	clientMode bool
}

func main() {
	in := flag.String("in", "", "path to the OpenAPI 3.1 reference (input)")
	out := flag.String("out", "", "path to write the derived 3.0 spec (output)")
	client := flag.Bool("client", false, "also disambiguate schema names that collide with client <OperationId>Response wrappers")
	flag.Parse()
	if *in == "" || *out == "" {
		fmt.Fprintln(os.Stderr, "usage: downconvert -in <3.1.json> -out <3.0.json>")
		os.Exit(2)
	}

	raw, err := os.ReadFile(*in)
	if err != nil {
		fatal(err)
	}
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		fatal(fmt.Errorf("parse %s: %w", *in, err))
	}

	c := &converter{clientMode: *client}
	doc = c.run(doc)

	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		fatal(err)
	}
	if err := os.WriteFile(*out, b, 0o644); err != nil {
		fatal(err)
	}
	c.report(os.Stderr)
}

// run applies every transform to a parsed document and returns it.
func (c *converter) run(doc any) any {
	if m, ok := doc.(map[string]any); ok {
		if v, ok := m["openapi"].(string); ok && strings.HasPrefix(v, "3.") {
			m["openapi"] = "3.0.3"
		}
	}
	doc = c.transform(doc)

	// Break Go-type-name collisions among component schemas (e.g. the SSE
	// envelope "Event.tui.command.execute" vs the payload "EventTuiCommandExecute"
	// both normalize to EventTuiCommandExecute). oapi-codegen can't auto-rename,
	// so we inject x-go-name on the colliding non-canonical schemas. $ref keys
	// are unchanged, so references still resolve.
	if m, ok := doc.(map[string]any); ok {
		if comps, ok := m["components"].(map[string]any); ok {
			if schemas, ok := comps["schemas"].(map[string]any); ok {
				c.disambiguateSchemaNames(schemas)
				if c.clientMode {
					c.disambiguateResponseCollisions(schemas, operationIDs(m))
				}
			}
		}
	}
	return doc
}

// operationIDs collects every operationId declared in the document's paths.
func operationIDs(doc map[string]any) []string {
	var ids []string
	paths, ok := doc["paths"].(map[string]any)
	if !ok {
		return ids
	}
	for _, item := range paths {
		ops, ok := item.(map[string]any)
		if !ok {
			continue
		}
		for _, op := range ops {
			if o, ok := op.(map[string]any); ok {
				if id, ok := o["operationId"].(string); ok && id != "" {
					ids = append(ids, id)
				}
			}
		}
	}
	return ids
}

// disambiguateResponseCollisions injects x-go-name on any component schema whose
// effective Go name equals a generated client wrapper name ("<OperationId>Go" +
// "Response"). oapi-codegen would otherwise redeclare the type. Only the wire Go
// identifier changes; $ref keys (and thus the JSON shape) are untouched.
func (c *converter) disambiguateResponseCollisions(schemas map[string]any, opIDs []string) {
	// Assumption: oapi-codegen derives the client wrapper name from the raw
	// operationId without Go-initialism casing, so goApprox(operationId)+"Response"
	// predicts it; schema component names DO get initialism casing, so goApprox
	// (which preserves existing casing) won't false-collide for ids like "tool.ids"
	// (wrapper ToolIdsResponse vs schema ToolIDs). Holds for the frozen contract.
	wrappers := make(map[string]bool, len(opIDs))
	for _, id := range opIDs {
		wrappers[goApprox(id)+"Response"] = true
	}
	names := make([]string, 0, len(schemas))
	for name := range schemas {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		sm, ok := schemas[name].(map[string]any)
		if !ok {
			continue
		}
		eff := goApprox(name)
		if x, ok := sm["x-go-name"].(string); ok && x != "" {
			eff = x
		}
		if wrappers[eff] {
			renamed := eff + "Schema"
			sm["x-go-name"] = renamed
			c.renamed = append(c.renamed, fmt.Sprintf("%s -> %s (response-wrapper collision)", name, renamed))
		}
	}
}

// transform walks the JSON tree applying the 3.1→3.0 schema rewrites in place.
func (c *converter) transform(node any) any {
	switch n := node.(type) {
	case map[string]any:
		delete(n, "$schema")

		if v, ok := n["exclusiveMinimum"]; ok {
			if num, isNum := v.(float64); isNum {
				if _, hasMin := n["minimum"]; !hasMin {
					n["minimum"] = num
				}
				n["exclusiveMinimum"] = true
				c.exclusiveMin++
			}
		}
		if v, ok := n["exclusiveMaximum"]; ok {
			if num, isNum := v.(float64); isNum {
				if _, hasMax := n["maximum"]; !hasMax {
					n["maximum"] = num
				}
				n["exclusiveMaximum"] = true
				c.exclusiveMax++
			}
		}

		switch t := n["type"].(type) {
		case string:
			if t == "null" {
				n["nullable"] = true
				delete(n, "type")
				c.nullableType++
			}
		case []any:
			nonNull := make([]any, 0, len(t))
			nullable := false
			for _, e := range t {
				if s, _ := e.(string); s == "null" {
					nullable = true
					continue
				}
				nonNull = append(nonNull, e)
			}
			if nullable {
				n["nullable"] = true
				c.nullableType++
			}
			switch len(nonNull) {
			case 1:
				n["type"] = nonNull[0]
			case 0:
				delete(n, "type")
			default:
				n["type"] = nonNull[0] // 3.0 has no union types; keep the first
			}
		}

		if pi, ok := n["prefixItems"].([]any); ok {
			if _, hasItems := n["items"]; !hasItems {
				if len(pi) > 0 {
					n["items"] = pi[0]
				} else {
					n["items"] = map[string]any{}
				}
			}
			delete(n, "prefixItems")
			c.prefixItems++
		}

		for k, v := range n {
			n[k] = c.transform(v)
		}

		c.fixUnions(n)
		return n
	case []any:
		for i, v := range n {
			n[i] = c.transform(v)
		}
		return n
	default:
		return node
	}
}

// fixUnions dedupes and collapses anyOf/oneOf arrays. It runs after children are
// transformed, so a {"type":"null"} member is already {"nullable":true}.
func (c *converter) fixUnions(n map[string]any) {
	for _, key := range []string{"anyOf", "oneOf"} {
		arr, ok := n[key].([]any)
		if !ok {
			continue
		}
		// Drop exact-duplicate members. opencode's emitted Event union lists the
		// same $ref twice for 26 session.next.* events, which makes oapi-codegen
		// emit duplicate union accessor methods. A union of [A,A,B] is [A,B].
		if dd := dedupeMembers(arr); len(dd) != len(arr) {
			c.unionDedupe += len(arr) - len(dd)
			arr = dd
		}
		n[key] = arr

		nonNull := make([]any, 0, len(arr))
		hadNull := false
		for _, m := range arr {
			if isNullSchema(m) {
				hadNull = true
				continue
			}
			nonNull = append(nonNull, m)
		}
		if !hadNull {
			continue
		}
		c.nullUnion++
		n["nullable"] = true
		switch len(nonNull) {
		case 0:
			delete(n, key)
		case 1:
			// Hoist the lone remaining member's keys into the parent.
			delete(n, key)
			if only, ok := nonNull[0].(map[string]any); ok {
				for mk, mv := range only {
					if _, exists := n[mk]; !exists {
						n[mk] = mv
					}
				}
			}
		default:
			n[key] = nonNull
		}
	}
}

// disambiguateSchemaNames assigns x-go-name to schemas that would otherwise
// generate the same Go type. The canonical schema (already in PascalCase, no
// separators) keeps the natural name; others get a numeric suffix. Deterministic
// (names are processed in sorted order).
func (c *converter) disambiguateSchemaNames(schemas map[string]any) {
	groups := map[string][]string{}
	for name := range schemas {
		g := goApprox(name)
		groups[g] = append(groups[g], name)
	}
	for canonical, names := range groups {
		if len(names) < 2 {
			continue
		}
		sort.Strings(names)
		keeper := names[0]
		for _, n := range names {
			if !nameSep.MatchString(n) { // a separator-free key is the natural owner
				keeper = n
				break
			}
		}
		used := map[string]bool{canonical: true}
		for _, name := range names {
			if name == keeper {
				continue
			}
			cand := canonical
			for i := 2; used[cand]; i++ {
				cand = canonical + strconv.Itoa(i)
			}
			used[cand] = true
			if sm, ok := schemas[name].(map[string]any); ok {
				sm["x-go-name"] = cand
				c.renamed = append(c.renamed, fmt.Sprintf("%s -> %s", name, cand))
			}
		}
	}
}

func (c *converter) report(w io.Writer) {
	sort.Strings(c.renamed)
	lines := []string{
		"downconvert: 3.1->3.0 transforms applied:",
		fmt.Sprintf("  exclusiveMinimum (number->bool): %d", c.exclusiveMin),
		fmt.Sprintf("  exclusiveMaximum (number->bool): %d", c.exclusiveMax),
		fmt.Sprintf("  nullable type collapses:         %d", c.nullableType),
		fmt.Sprintf("  nullable union collapses:        %d", c.nullUnion),
		fmt.Sprintf("  duplicate union members dropped: %d", c.unionDedupe),
		fmt.Sprintf("  prefixItems -> items:            %d", c.prefixItems),
		fmt.Sprintf("  schema name disambiguations:     %d", len(c.renamed)),
	}
	for _, r := range c.renamed {
		lines = append(lines, "    "+r)
	}
	_, _ = io.WriteString(w, strings.Join(lines, "\n")+"\n")
}

// dedupeMembers drops exact-duplicate members from a union, preserving order.
// Equality is by canonical JSON (encoding/json sorts map keys), so identical
// $refs and identical inline schemas both collapse to one.
func dedupeMembers(arr []any) []any {
	seen := make(map[string]bool, len(arr))
	out := make([]any, 0, len(arr))
	for _, m := range arr {
		b, err := json.Marshal(m)
		if err != nil {
			out = append(out, m)
			continue
		}
		if seen[string(b)] {
			continue
		}
		seen[string(b)] = true
		out = append(out, m)
	}
	return out
}

var nameSep = regexp.MustCompile(`[^a-zA-Z0-9]+`)

// goApprox approximates oapi-codegen's PascalCase type-name normalization
// closely enough to bucket schemas that would collide.
func goApprox(name string) string {
	var b strings.Builder
	for _, part := range nameSep.Split(name, -1) {
		if part == "" {
			continue
		}
		b.WriteString(strings.ToUpper(part[:1]))
		b.WriteString(part[1:])
	}
	return b.String()
}

// isNullSchema reports whether an anyOf/oneOf member represents JSON null.
// In the 3.1 source this is {"type":"null"}; after transform the type key has
// been dropped and {"nullable":true} remains with no other schema content.
func isNullSchema(m any) bool {
	mm, ok := m.(map[string]any)
	if !ok {
		return false
	}
	if mm["type"] == "null" {
		return true
	}
	if n, ok := mm["nullable"].(bool); ok && n {
		for k := range mm {
			if k != "nullable" {
				return false
			}
		}
		return true
	}
	return false
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "downconvert:", err)
	os.Exit(1)
}
