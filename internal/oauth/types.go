package oauth

// Prompt is a single auth-method input prompt. opencode supports "text" and
// "select" prompts (provider/auth.ts:16-38); Opcode42 mirrors the wire shape so
// existing opencode clients can render the picker. Only the fields opencode
// emits are present; unused ones stay zero/omitted.
type Prompt struct {
	Type        string         `json:"type"` // "text" | "select"
	Key         string         `json:"key"`
	Message     string         `json:"message"`
	Placeholder string         `json:"placeholder,omitempty"`
	Options     []SelectOption `json:"options,omitempty"`
	When        *When          `json:"when,omitempty"`
}

// SelectOption is one choice in a select Prompt (provider/auth.ts:24-28).
type SelectOption struct {
	Label string `json:"label"`
	Value string `json:"value"`
	Hint  string `json:"hint,omitempty"`
}

// When gates a prompt on a prior input value (provider/auth.ts:10-14).
type When struct {
	Key   string `json:"key"`
	Op    string `json:"op"` // "eq" | "neq"
	Value string `json:"value"`
}

// Method is one authentication method for a provider (provider/auth.ts:40-44).
// Opcode42 only surfaces OAuth methods through this package; API-key entry stays on
// the PUT /auth/{providerID} path (auth_handlers.go).
type Method struct {
	Type    string   `json:"type"` // "oauth" | "api"
	Label   string   `json:"label"`
	Prompts []Prompt `json:"prompts,omitempty"`
}

// Authorization is the authorize() response: where to send the user and how the
// callback completes (provider/auth.ts:49-53; ProviderAuthAuthorization).
//   - method "code": the provider redirects with a code the user pastes back via
//     POST .../oauth/callback {code}.
//   - method "auto": Opcode42's loopback server (or device-code poll) captures the
//     result; the client polls POST .../oauth/callback with no code.
type Authorization struct {
	URL          string `json:"url"`
	Method       string `json:"method"` // "auto" | "code"
	Instructions string `json:"instructions"`
}

// Token is a normalized OAuth token result an exchange/poll yields, ready to be
// persisted as an Auth "oauth" record (auth/index.ts:13-20).
type Token struct {
	Access    string
	Refresh   string
	Expires   int64 // unix-millis expiry; 0 = unknown/non-expiring
	AccountID string
}

// pendingKind distinguishes how an in-flight authorize completes.
type pendingKind int

const (
	// pendingLoopback waits for the OAuth provider to redirect the browser to
	// Opcode42's loopback callback server (xai.ts startOAuthServer).
	pendingLoopback pendingKind = iota
	// pendingCode waits for the user to paste an authorization code back via
	// POST .../oauth/callback {code}.
	pendingCode
)
