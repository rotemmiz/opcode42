package auth

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
}

func TestNoPasswordIsPassthrough(t *testing.T) {
	c := Config{Username: "opencode", Password: ""}
	if c.Required() {
		t.Fatal("Required() should be false with empty password")
	}
	rr := httptest.NewRecorder()
	c.Middleware(okHandler()).ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/config", nil))
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (auth disabled)", rr.Code)
	}
}

func TestMissingCredentials401(t *testing.T) {
	c := Config{Username: "opencode", Password: "secret"}
	rr := httptest.NewRecorder()
	c.Middleware(okHandler()).ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/config", nil))
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rr.Code)
	}
	if got := rr.Header().Get("WWW-Authenticate"); got != wwwAuthenticate {
		t.Errorf("WWW-Authenticate = %q, want %q", got, wwwAuthenticate)
	}
	if rr.Body.Len() != 0 {
		t.Errorf("401 body = %q, want empty", rr.Body.String())
	}
}

func TestBasicHeaderOK(t *testing.T) {
	c := Config{Username: "opencode", Password: "secret"}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/config", nil)
	req.SetBasicAuth("opencode", "secret")
	c.Middleware(okHandler()).ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
}

func TestAuthTokenQueryOK(t *testing.T) {
	c := Config{Username: "opencode", Password: "secret"}
	token := base64.StdEncoding.EncodeToString([]byte("opencode:secret"))
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/config?auth_token="+token, nil)
	c.Middleware(okHandler()).ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (auth_token query)", rr.Code)
	}
}

func TestWrongPassword401(t *testing.T) {
	c := Config{Username: "opencode", Password: "secret"}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/config", nil)
	req.SetBasicAuth("opencode", "wrong")
	c.Middleware(okHandler()).ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rr.Code)
	}
}

// TestCheckBindExposure verifies Opcode42 refuses a non-loopback bind without a
// password (plan 13 §Defaults) but allows loopback binds and any bind once a
// password is set.
func TestCheckBindExposure(t *testing.T) {
	noPass := Config{Username: "opencode", Password: ""}
	withPass := Config{Username: "opencode", Password: "secret"}

	// No password + non-loopback host → refuse.
	for _, h := range []string{"0.0.0.0", "192.168.1.5", "::", "opcode42.example.com"} {
		if err := noPass.CheckBindExposure(h); err == nil {
			t.Errorf("CheckBindExposure(%q) with no password = nil, want error", h)
		}
	}
	// No password + loopback host → allowed (only a warning, like opencode).
	for _, h := range []string{"127.0.0.1", "localhost", "::1", "", " 127.0.0.1 "} {
		if err := noPass.CheckBindExposure(h); err != nil {
			t.Errorf("CheckBindExposure(%q) with no password = %v, want nil", h, err)
		}
	}
	// Password set → any host allowed.
	for _, h := range []string{"0.0.0.0", "127.0.0.1", "192.168.1.5"} {
		if err := withPass.CheckBindExposure(h); err != nil {
			t.Errorf("CheckBindExposure(%q) with password = %v, want nil", h, err)
		}
	}
}

func TestWrongUsername401(t *testing.T) {
	c := Config{Username: "opencode", Password: "secret"}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/config", nil)
	req.SetBasicAuth("intruder", "secret")
	c.Middleware(okHandler()).ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 (wrong username)", rr.Code)
	}
}

// TestAuthorizedConstantTime exercises the credential comparison directly,
// covering the both-fields-checked behaviour the timing-safe path relies on: a
// username mismatch must not pass just because the password matched, and vice
// versa, and length differences must fail closed without panicking.
func TestAuthorizedConstantTime(t *testing.T) {
	c := Config{Username: "opencode", Password: "secret"}
	cases := []struct {
		user, pass string
		want       bool
	}{
		{"opencode", "secret", true},
		{"opencode", "wrong", false},
		{"intruder", "secret", false},
		{"intruder", "wrong", false},
		{"", "", false},
		{"opencode", "", false},
		{"opencode", "secretsecret", false},
		{"op", "secret", false},
	}
	for _, tc := range cases {
		if got := c.authorized(tc.user, tc.pass); got != tc.want {
			t.Errorf("authorized(%q, %q) = %v, want %v", tc.user, tc.pass, got, tc.want)
		}
	}
}

func TestAuthTokenTakesPrecedenceOverBadHeader(t *testing.T) {
	c := Config{Username: "opencode", Password: "secret"}
	token := base64.StdEncoding.EncodeToString([]byte("opencode:secret"))
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/config?auth_token="+token, nil)
	req.Header.Set("Authorization", "Basic garbage")
	c.Middleware(okHandler()).ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (auth_token wins)", rr.Code)
	}
}
