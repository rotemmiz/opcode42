package push

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// genServiceAccountJSON builds a valid service-account JSON key with a fresh RSA
// key, pointing token_uri at tokenURL so the test can intercept the exchange.
func genServiceAccountJSON(t *testing.T, tokenURL string) []byte {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("gen key: %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	sa := map[string]string{
		"type":         "service_account",
		"project_id":   "test-project",
		"private_key":  string(pemBytes),
		"client_email": "forge@test-project.iam.gserviceaccount.com",
		"token_uri":    tokenURL,
	}
	b, err := json.Marshal(sa)
	if err != nil {
		t.Fatalf("marshal sa: %v", err)
	}
	return b
}

func TestNewFCMSenderRejectsBadCredential(t *testing.T) {
	if _, err := NewFCMSender([]byte(`{"type":"not_a_service_account"}`)); err == nil {
		t.Fatal("want error for non-service_account credential")
	}
	if _, err := NewFCMSender([]byte(`not json`)); err == nil {
		t.Fatal("want error for invalid JSON")
	}
	if _, err := NewFCMSender([]byte(`{"type":"service_account","project_id":"p","client_email":"e"}`)); err == nil {
		t.Fatal("want error for missing private_key")
	}
}

func TestFCMSenderSend(t *testing.T) {
	var gotAuth, gotBody string
	// token endpoint + fcm send endpoint share one test server, routed by path.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "token"):
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "ya29.test", "expires_in": 3600})
		default: // FCM send
			gotAuth = r.Header.Get("Authorization")
			buf, _ := io.ReadAll(r.Body)
			gotBody = string(buf)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	defer srv.Close()

	saJSON := genServiceAccountJSON(t, srv.URL+"/token")
	s, err := NewFCMSender(saJSON)
	if err != nil {
		t.Fatalf("NewFCMSender: %v", err)
	}
	// Redirect the send URL at the stub.
	fs := s.(*fcmSender)
	fs.sendURL = srv.URL + "/send"

	err = fs.Send(context.Background(), "device-token", Notification{
		Title: "Agent finished", Body: "done", SessionID: "ses_1", EventType: "session.idle",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if gotAuth != "Bearer ya29.test" {
		t.Errorf("auth header = %q, want Bearer ya29.test", gotAuth)
	}
	if !strings.Contains(gotBody, `"device-token"`) || !strings.Contains(gotBody, "Agent finished") {
		t.Errorf("fcm body missing token/title: %s", gotBody)
	}
	if !strings.Contains(gotBody, `"session_id":"ses_1"`) {
		t.Errorf("fcm body missing data.session_id: %s", gotBody)
	}
}

func TestFCMSenderUnregistered(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "token") {
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "ya29.test", "expires_in": 3600})
			return
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"status":"NOT_FOUND","details":[{"errorCode":"UNREGISTERED"}]}}`))
	}))
	defer srv.Close()

	s, err := NewFCMSender(genServiceAccountJSON(t, srv.URL+"/token"))
	if err != nil {
		t.Fatalf("NewFCMSender: %v", err)
	}
	fs := s.(*fcmSender)
	fs.sendURL = srv.URL + "/send"

	err = fs.Send(context.Background(), "dead", Notification{Title: "t", Body: "b"})
	if err != errUnregistered {
		t.Fatalf("Send returned %v, want errUnregistered", err)
	}
}
