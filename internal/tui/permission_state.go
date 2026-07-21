package tui

// permission_state.go — Plan 17 §B3: pure 3-stage state machine for the
// permission footer panel, mirroring opencode's permission.shared.ts.
//
// opencode separates state (permission.shared.ts) from render
// (footer.permission.tsx): a pure state machine + a render component. The
// machine has three stages (permission.shared.ts:22-23):
//
//	permission → initial view with Allow once / Allow always / Reject options
//	always     → confirmation step (Confirm / Cancel) showing the patterns
//	             that will be allowed until OpenCode is restarted
//	reject     → text input for the rejection message
//
// permissionRun() is the main transition: given the current state and the
// selected option, it returns a new state and optionally a reply to send to
// the SDK. The render path (permission.go permissionView) calls this on
// enter/click.
//
// The wire side is identical to the prior single-stage flow (reply
// "once"/"always"/"reject" + optional "message"); the 3-stage flow is a UX
// feature that matches opencode's footer.permission.tsx.

// permStage is the 3-stage permission state machine stage
// (permission.shared.ts:22-23).
type permStage int

const (
	// permStagePermission is the initial view: Allow once / Allow always /
	// Reject options.
	permStagePermission permStage = iota
	// permStageAlways is the confirmation step for "Allow always": shows the
	// patterns that will be allowed, with Confirm / Cancel options.
	permStageAlways
	// permStageReject is the rejection-message textarea step: the user
	// enters feedback ("Tell OpenCode what to do differently") before the
	// reject reply is sent.
	permStageReject
)

// permOption is a selectable option in a permission stage. The wire reply
// value is sent to POST /permission/:id/reply when the option is confirmed.
// permission.shared.ts:23 — "once" | "always" | "reject" | "confirm" | "cancel".
type permOption int

const (
	permOptOnce permOption = iota
	permOptAlways
	permOptReject
	permOptConfirm
	permOptCancel
)

// permOptLabel returns the display label for a permission option, mirroring
// permissionLabel (permission.shared.ts:137-143).
func permOptLabel(o permOption) string {
	switch o {
	case permOptOnce:
		return "Allow once"
	case permOptAlways:
		return "Allow always"
	case permOptReject:
		return "Reject"
	case permOptConfirm:
		return "Confirm"
	case permOptCancel:
		return "Cancel"
	}
	return ""
}

// permStageOptions returns the selectable options for a stage
// (permissionOptions, permission.shared.ts:80-90).
func permStageOptions(stage permStage) []permOption {
	switch stage {
	case permStagePermission:
		return []permOption{permOptOnce, permOptAlways, permOptReject}
	case permStageAlways:
		return []permOption{permOptConfirm, permOptCancel}
	}
	return nil
}

// permissionState is the per-request permission UI state. Owned by the Model
// (m.permState); reset on reply success/failure and when the active pending
// permission changes. The pure transition functions below return a new state
// (no mutation), making the 3-stage flow testable independently of the render
// path. Mirrors PermissionBodyState (permission.shared.ts:25-31).
type permissionState struct {
	stage    permStage
	selected permOption
	message  string // the reject-stage textarea content
	replying bool   // a reply is in flight (overlay stays up until it resolves)
}

// newPermissionState returns the initial state for a permission request
// (createPermissionBodyState, permission.shared.ts:70-78).
func newPermissionState() permissionState {
	return permissionState{
		stage:    permStagePermission,
		selected: permOptOnce,
	}
}

// permShift moves the selection by dir (-1 or +1), wrapping within the current
// stage's options (permissionShift, permission.shared.ts:153-165).
func permShift(s permissionState, dir int) permissionState {
	opts := permStageOptions(s.stage)
	if len(opts) == 0 {
		return s
	}
	idx := -1
	for i, o := range opts {
		if o == s.selected {
			idx = i
			break
		}
	}
	if idx < 0 {
		idx = 0
	}
	next := (idx + dir + len(opts)) % len(opts)
	return permissionState{
		stage:    s.stage,
		selected: opts[next],
		message:  s.message,
		replying: s.replying,
	}
}

// permRun is the main transition: given the current state and a selected
// option, returns the new state and the wire reply to send (empty when the
// option is a stage transition, not a final reply). Mirrors permissionRun
// (permission.shared.ts:174-224).
//
//	permission stage:
//	  once   → reply "once"
//	  always → transition to always stage (selected = confirm)
//	  reject → transition to reject stage (selected = reject)
//	always stage:
//	  cancel → transition back to permission stage (selected = always)
//	  confirm → reply "always"
//	reject stage: handled by permRejectSend (the reply includes the message)
func permRun(s permissionState, opt permOption) (permissionState, string) {
	if s.replying {
		return s, ""
	}
	switch s.stage {
	case permStagePermission:
		switch opt {
		case permOptAlways:
			return permissionState{
				stage:    permStageAlways,
				selected: permOptConfirm,
				message:  s.message,
			}, ""
		case permOptReject:
			return permissionState{
				stage:    permStageReject,
				selected: permOptReject,
				message:  s.message,
			}, ""
		case permOptOnce:
			return s, "once"
		}
	case permStageAlways:
		switch opt {
		case permOptCancel:
			return permissionState{
				stage:    permStagePermission,
				selected: permOptAlways,
				message:  s.message,
			}, ""
		case permOptConfirm:
			return s, "always"
		}
	}
	return s, ""
}

// permCancelReject returns to the permission stage from the reject stage,
// discarding the typed message (permissionCancel, permission.shared.ts:234-240).
func permCancelReject(s permissionState) permissionState {
	return permissionState{
		stage:    permStagePermission,
		selected: permOptReject,
		message:  s.message,
		replying: s.replying,
	}
}

// permEscape handles the esc key (permissionEscape, permission.shared.ts:242-256):
//   - from the always stage → back to the permission stage (selected = always)
//   - from the permission stage → transition to the reject stage
//   - from the reject stage → back to the permission stage (cancel)
func permEscape(s permissionState) permissionState {
	switch s.stage {
	case permStageAlways:
		return permissionState{
			stage:    permStagePermission,
			selected: permOptAlways,
			message:  s.message,
		}
	case permStageReject:
		return permCancelReject(s)
	default:
		return permissionState{
			stage:    permStageReject,
			selected: permOptReject,
			message:  s.message,
		}
	}
}

// permSetMessage updates the reject-stage textarea content (used by the
// RejectField onContentChange in footer.permission.tsx:314-318).
func permSetMessage(s permissionState, msg string) permissionState {
	return permissionState{
		stage:    s.stage,
		selected: s.selected,
		message:  msg,
		replying: s.replying,
	}
}

// permAlwaysLines returns the lines shown in the "always" confirmation stage
// (permissionAlwaysLines, permission.shared.ts:126-135). When the request has
// a single "*" always pattern, the summary is a one-liner; otherwise the
// patterns are listed.
func permAlwaysLines(always []string, permission string) []string {
	if len(always) == 1 && always[0] == "*" {
		return []string{"This will allow " + permission + " until OpenCode is restarted."}
	}
	out := []string{"This will allow the following patterns until OpenCode is restarted."}
	for _, p := range always {
		out = append(out, "- "+p)
	}
	return out
}
