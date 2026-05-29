package main

import (
	"encoding/json"
	"testing"
)

// parse is a tiny helper to turn a JSON literal into the generic tree.
func parse(t *testing.T, s string) any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatalf("bad test JSON: %v", err)
	}
	return v
}

func asMap(t *testing.T, v any) map[string]any {
	t.Helper()
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("expected object, got %T", v)
	}
	return m
}

func TestExclusiveMinimumNumberToBool(t *testing.T) {
	c := &converter{}
	got := asMap(t, c.transform(parse(t, `{"type":"integer","exclusiveMinimum":0}`)))
	if got["exclusiveMinimum"] != true {
		t.Errorf("exclusiveMinimum: want true, got %v", got["exclusiveMinimum"])
	}
	if got["minimum"] != float64(0) {
		t.Errorf("minimum: want 0, got %v", got["minimum"])
	}
}

func TestNullableAnyOfCollapsesToSingleMember(t *testing.T) {
	// The dominant pattern: anyOf:[{type:string},{type:null}] -> nullable string.
	c := &converter{}
	got := asMap(t, c.transform(parse(t, `{"anyOf":[{"type":"string"},{"type":"null"}]}`)))
	if _, ok := got["anyOf"]; ok {
		t.Errorf("anyOf should have been hoisted away, got %v", got["anyOf"])
	}
	if got["type"] != "string" {
		t.Errorf("type: want string, got %v", got["type"])
	}
	if got["nullable"] != true {
		t.Errorf("nullable: want true, got %v", got["nullable"])
	}
}

func TestDuplicateUnionMembersDeduped(t *testing.T) {
	c := &converter{}
	got := asMap(t, c.transform(parse(t, `{"anyOf":[{"$ref":"#/c/A"},{"$ref":"#/c/A"},{"$ref":"#/c/B"}]}`)))
	arr, ok := got["anyOf"].([]any)
	if !ok {
		t.Fatalf("anyOf missing or wrong type: %v", got["anyOf"])
	}
	if len(arr) != 2 {
		t.Fatalf("want 2 deduped members, got %d: %v", len(arr), arr)
	}
	if c.unionDedupe != 1 {
		t.Errorf("unionDedupe count: want 1, got %d", c.unionDedupe)
	}
}

func TestTypeArrayNullableSplit(t *testing.T) {
	c := &converter{}
	got := asMap(t, c.transform(parse(t, `{"type":["string","null"]}`)))
	if got["type"] != "string" {
		t.Errorf("type: want string, got %v", got["type"])
	}
	if got["nullable"] != true {
		t.Errorf("nullable: want true, got %v", got["nullable"])
	}
}

func TestSchemaNameDisambiguation(t *testing.T) {
	c := &converter{}
	schemas := asMap(t, parse(t, `{"Event.tui.x":{"type":"object"},"EventTuiX":{"type":"object"}}`))
	c.disambiguateSchemaNames(schemas)
	// The separator-free key keeps its natural name (no x-go-name).
	if m := asMap(t, schemas["EventTuiX"]); m["x-go-name"] != nil {
		t.Errorf("EventTuiX should keep natural name, got x-go-name=%v", m["x-go-name"])
	}
	// The dotted key gets a unique suffixed name.
	if m := asMap(t, schemas["Event.tui.x"]); m["x-go-name"] != "EventTuiX2" {
		t.Errorf("Event.tui.x: want x-go-name=EventTuiX2, got %v", m["x-go-name"])
	}
}

func TestDropsSchemaDialectKey(t *testing.T) {
	c := &converter{}
	got := asMap(t, c.transform(parse(t, `{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object"}`)))
	if _, ok := got["$schema"]; ok {
		t.Errorf("$schema should be dropped")
	}
}
