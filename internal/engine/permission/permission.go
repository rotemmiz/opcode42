// Package permission implements Opcode42's tool-permission model: a pure rule
// evaluator and a blocking ask/reply manager, mirroring opencode's Permission
// service (packages/opencode/src/permission/index.ts).
//
// Rules carry a permission key, a pattern, and an action (ask/allow/deny).
// Evaluate walks ordered rulesets, last match wins, defaulting to "ask".
// The Manager blocks a tool until a client replies, persisting "always" grants
// per session and cascading "reject" to the session's other pending requests.
package permission

import (
	"regexp"
	"strings"
	"sync"
)

// Action is a rule's verdict.
type Action string

// Permission actions.
const (
	ActionAsk   Action = "ask"
	ActionAllow Action = "allow"
	ActionDeny  Action = "deny"
)

// Rule matches a permission key + pattern to an action.
type Rule struct {
	Permission string `json:"permission"`
	Pattern    string `json:"pattern"`
	Action     Action `json:"action"`
}

// Ruleset is an ordered list of rules.
type Ruleset []Rule

// Known permission keys (config/permission.ts:16-37).
const (
	KeyRead              = "read"
	KeyEdit              = "edit"
	KeyGlob              = "glob"
	KeyGrep              = "grep"
	KeyList              = "list"
	KeyBash              = "bash"
	KeyTask              = "task"
	KeyExternalDirectory = "external_directory"
	KeyTodoWrite         = "todowrite"
	KeyQuestion          = "question"
	KeyWebFetch          = "webfetch"
	KeyWebSearch         = "websearch"
	KeyRepoClone         = "repo_clone"
	KeyRepoOverview      = "repo_overview"
	KeyLSP               = "lsp"
	KeyDoomLoop          = "doom_loop"
	KeySkill             = "skill"
)

// Evaluate returns the winning rule for (permission, pattern): it walks rulesets
// in order, last matching rule wins, and defaults to an "ask" rule when nothing
// matches (permission/index.ts:138). Pure; no I/O.
func Evaluate(permission, pattern string, rulesets ...Ruleset) Rule {
	var match *Rule
	for _, rs := range rulesets {
		for i := range rs {
			if wildcardMatch(permission, rs[i].Permission) && wildcardMatch(pattern, rs[i].Pattern) {
				r := rs[i]
				match = &r
			}
		}
	}
	if match == nil {
		return Rule{Permission: permission, Pattern: pattern, Action: ActionAsk}
	}
	return *match
}

// wildcardMatch reports whether value matches a glob pattern where "*" matches
// any run of characters and "?" matches one. An empty pattern matches nothing
// except an empty value; "*" matches anything.
func wildcardMatch(value, pattern string) bool {
	if pattern == "" {
		return value == ""
	}
	if pattern == "*" {
		return true
	}
	return compileWildcard(pattern).MatchString(value)
}

var wildcardCache sync.Map // pattern -> *regexp.Regexp

func compileWildcard(pattern string) *regexp.Regexp {
	if re, ok := wildcardCache.Load(pattern); ok {
		return re.(*regexp.Regexp)
	}
	var b strings.Builder
	b.WriteString("^")
	for _, r := range pattern {
		switch r {
		case '*':
			b.WriteString(".*")
		case '?':
			b.WriteString(".")
		default:
			b.WriteString(regexp.QuoteMeta(string(r)))
		}
	}
	b.WriteString("$")
	re := regexp.MustCompile(b.String())
	wildcardCache.Store(pattern, re)
	return re
}
