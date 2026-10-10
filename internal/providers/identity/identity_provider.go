package identity

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/azure-local/azure-local/internal/kernel"
)

type Provider struct {
	logger *slog.Logger
	keys   *KeyStore
	bus    *kernel.EventBus
	dir    TenantDirectory

	// enforce controls whether ARM requests must carry a valid Bearer token.
	// Default false so `curl` continues to work during development.
	enforce bool
}

func New(persist *kernel.PersistenceManager, bus *kernel.EventBus, logger *slog.Logger) (*Provider, error) {
	keys, err := LoadOrGenerate(persist)
	if err != nil {
		return nil, err
	}
	return &Provider{
		logger: logger,
		keys:   keys,
		bus:    bus,
		dir:    DefaultDirectory(),
	}, nil
}

func (p *Provider) Name() string    { return "identity" }
func (p *Provider) Version() string { return "2.0" }

func (p *Provider) Init(context.Context) error  { return nil }
func (p *Provider) Start(context.Context) error { return nil }
func (p *Provider) Stop(context.Context) error  { return nil }

func (p *Provider) Health(context.Context) kernel.HealthStatus { return kernel.Healthy() }

func (p *Provider) SetEnforce(v bool) { p.enforce = v }

func (p *Provider) Handle(w http.ResponseWriter, req *http.Request) bool {
	path := strings.TrimPrefix(req.URL.Path, "/")
	if path == "" {
		return false
	}

	// OIDC discovery: {tenant}/[v2.0/].well-known/openid-configuration
	if strings.HasSuffix(path, "/.well-known/openid-configuration") {
		tenant := firstSegment(path)
		p.handleOIDCDiscovery(w, req, tenant)
		return true
	}

	// JWKS: {tenant}/[v2.0/]discovery/keys or {tenant}/discovery/v2.0/keys
	if strings.HasSuffix(path, "/discovery/v2.0/keys") ||
		strings.HasSuffix(path, "/discovery/keys") {
		p.handleJWKS(w, req)
		return true
	}

	// Token endpoint: {tenant}/oauth2[/v2.0]/token
	if strings.HasSuffix(path, "/oauth2/v2.0/token") ||
		strings.HasSuffix(path, "/oauth2/token") {
		tenant := firstSegment(path)
		p.handleToken(w, req, tenant)
		return true
	}

	return false
}

// ValidateRequest is the AuthFunc used by providers that enforce auth.
// It returns nil if the request carries a valid Bearer token or if
// enforcement is disabled.
func (p *Provider) ValidateRequest(req *http.Request) error {
	if !p.enforce {
		return nil
	}
	auth := req.Header.Get("Authorization")
	if auth == "" {
		return errors.New("Authorization header is required")
	}
	if !strings.HasPrefix(auth, "Bearer ") {
		return errors.New("Authorization header must be Bearer")
	}
	token := strings.TrimPrefix(auth, "Bearer ")
	_, err := p.keys.Verify(token)
	return err
}

// ExtractClaims pulls the Bearer token out of the request, verifies it,
// and returns the claims. Used by the authorization provider.
func (p *Provider) ExtractClaims(req *http.Request) (VerifiedClaims, error) {
	auth := req.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") {
		return nil, errors.New("missing or malformed Authorization header")
	}
	return p.keys.Verify(strings.TrimPrefix(auth, "Bearer "))
}

// VerifyBearer is exported for tests and for the CLI to inspect tokens.
func (p *Provider) VerifyBearer(token string) (VerifiedClaims, error) {
	return p.keys.Verify(token)
}

func firstSegment(path string) string {
	if i := strings.Index(path, "/"); i >= 0 {
		return path[:i]
	}
	return path
}

// ---- JSON helpers ---------------------------------------------------------

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeOAuthError(w http.ResponseWriter, status int, code, desc string) {
	writeJSON(w, status, OAuthError{Error: code, ErrorDescription: desc})
}
