package pluginbridge

import "time"

// Hook names mirror opencode's authoritative hook set
// (opencode/packages/plugin/src/index.ts:222-334, the `Hooks` interface).
// Opcode42 routes each call site through Bridge.Trigger using these constants so
// the wire method name matches what the Node/Bun host dispatches to plugins.
const (
	HookConfig                 = "config"
	HookChatMessage            = "chat.message"
	HookChatParams             = "chat.params"
	HookChatHeaders            = "chat.headers"
	HookPermissionAsk          = "permission.ask"
	HookCommandExecuteBefore   = "command.execute.before"
	HookToolExecuteBefore      = "tool.execute.before"
	HookToolExecuteAfter       = "tool.execute.after"
	HookShellEnv               = "shell.env"
	HookMessagesTransform      = "experimental.chat.messages.transform"
	HookSystemTransform        = "experimental.chat.system.transform"
	HookSessionCompacting      = "experimental.session.compacting"
	HookCompactionAutoContinue = "experimental.compaction.autocontinue"
	HookTextComplete           = "experimental.text.complete"
	HookToolDefinition         = "tool.definition"
)

// hookTimeouts is the per-hook blocking budget from plan 05 §"Hook Bridge
// Table". A hook that does not finish within its budget is abandoned and the
// caller keeps the unmodified output (matching opencode's per-hook error
// tolerance, plugin/index.ts:288-300). Unlisted hooks use defaultHookTimeout.
var hookTimeouts = map[string]time.Duration{
	HookChatParams:             5 * time.Second,
	HookChatHeaders:            5 * time.Second,
	HookPermissionAsk:          5 * time.Second,
	HookCommandExecuteBefore:   5 * time.Second,
	HookToolExecuteBefore:      5 * time.Second,
	HookToolExecuteAfter:       5 * time.Second,
	HookShellEnv:               3 * time.Second,
	HookMessagesTransform:      30 * time.Second,
	HookSystemTransform:        10 * time.Second,
	HookSessionCompacting:      10 * time.Second,
	HookCompactionAutoContinue: 5 * time.Second,
	HookTextComplete:           10 * time.Second,
	HookToolDefinition:         5 * time.Second,
	HookConfig:                 5 * time.Second,
}

const defaultHookTimeout = 5 * time.Second

// hookTimeout returns the blocking budget for a hook, defaulting when unlisted.
func hookTimeout(name string) time.Duration {
	if d, ok := hookTimeouts[name]; ok {
		return d
	}
	return defaultHookTimeout
}
