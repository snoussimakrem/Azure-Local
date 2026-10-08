package identity

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/azure-local/azure-local/internal/kernel"
)

func newProvider(t *testing.T) *Provider {
	t.Helper()
	persist, err := kernel.NewPersistenceManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	p, err := New(persist, kernel.NewEventBus(), logger)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestOIDCDiscovery(t *testing.T) {
	p := newProvider(t)
	req := httptest.NewRequest(http.MethodGet,
		"/local/v2.0/.well-known/openid-configuration", nil)
	w := httptest.NewRecorder()
	if !p.Handle(w, req) {
		t.Fatal("not claimed")
	}
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d", w.Code)
	}
	body := w.Body.String()
	for _, want := range []string{
		`"issuer"`, `"jwks_uri"`, `"token_endpoint"`, `"RS256"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %s in discovery: %s", want, body)
		}
	}
}

func TestJWKS(t *testing.T) {
	p := newProvider(t)
	req := httptest.NewRequest(http.MethodGet,
		"/local/discovery/v2.0/keys", nil)
	w := httptest.NewRecorder()
	p.Handle(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d", w.Code)
	}
	body := w.Body.String()
	for _, want := range []string{`"kty":"RSA"`, `"alg":"RS256"`, `"n":"`, `"e":"AQAB"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("JWKS missing %s: %s", want, body)
		}
	}
}

func TestClientCredentialsToken(t *testing.T) {
	p := newProvider(t)

	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", "local-client")
	form.Set("client_secret", "local-secret")
	form.Set("scope", "http://localhost:4577/.default")

	req := httptest.NewRequest(http.MethodPost,
		"/local/oauth2/v2.0/token",
		strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	if !p.Handle(w, req) {
		t.Fatal("not claimed")
	}
	if w.Code != http.StatusOK {
		t.Fatalf("token: code=%d body=%s", w.Code, w.Body.String())
	}

	var resp TokenResponse
	if err := jsonUnmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.TokenType != "Bearer" {
		t.Fatalf("token_type=%s", resp.TokenType)
	}
	if resp.AccessToken == "" {
		t.Fatal("empty access_token")
	}

	claims, err := p.keys.Verify(resp.AccessToken)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if claims["tid"] != "local" {
		t.Fatalf("tid=%v", claims["tid"])
	}
	if claims["appid"] != "local-client" {
		t.Fatalf("appid=%v", claims["appid"])
	}
}

func TestBadClientSecret(t *testing.T) {
	p := newProvider(t)
	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", "local-client")
	form.Set("client_secret", "wrong")
	req := httptest.NewRequest(http.MethodPost,
		"/local/oauth2/v2.0/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	p.Handle(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "invalid_client") {
		t.Fatalf("body=%s", w.Body.String())
	}
}

func TestKeySurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	persist1, _ := kernel.NewPersistenceManager(dir)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	p1, _ := New(persist1, kernel.NewEventBus(), logger)
	kid1 := p1.keys.Kid

	persist2, _ := kernel.NewPersistenceManager(dir)
	p2, _ := New(persist2, kernel.NewEventBus(), logger)
	if p2.keys.Kid != kid1 {
		t.Fatalf("kid changed across restart: %s -> %s", kid1, p2.keys.Kid)
	}
}

func TestDeclinesForeignPaths(t *testing.T) {
	p := newProvider(t)
	for _, path := range []string{
		"/",
		"/health",
		"/metadata/endpoints",
		"/subscriptions/local-sub",
		"/devstoreaccount1/invoices",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		if p.Handle(w, req) {
			t.Fatalf("claimed %q", path)
		}
	}
}

// small helper so we don't pull in encoding/json in every test file
func jsonUnmarshal(b []byte, v any) error {
	return jsonUnmarshalImpl(b, v)
}
