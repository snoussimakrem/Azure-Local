package identity

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/azure-local/azure-local/internal/kernel"
)

// handleToken implements the OAuth 2.0 token endpoint for client_credentials
// and ROPC (password). Every response is a real RS256-signed JWT.
func (p *Provider) handleToken(w http.ResponseWriter, req *http.Request, tenant string) {
	if req.Method != http.MethodPost {
		writeOAuthError(w, http.StatusMethodNotAllowed, "invalid_request",
			"token endpoint requires POST")
		return
	}
	if err := req.ParseForm(); err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	grant := req.Form.Get("grant_type")
	switch grant {
	case "client_credentials":
		p.grantClientCredentials(w, req, tenant)
	case "password":
		p.grantPassword(w, req, tenant)
	case "":
		writeOAuthError(w, http.StatusBadRequest, "invalid_request",
			"grant_type is required")
	default:
		writeOAuthError(w, http.StatusBadRequest, "unsupported_grant_type",
			"grant_type must be client_credentials or password")
	}
}

func (p *Provider) grantClientCredentials(w http.ResponseWriter, req *http.Request, tenant string) {
	clientID, clientSecret := extractClientCreds(req)

	if clientID == "" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_client",
			"client_id is required")
		return
	}
	if clientID != p.dir.ClientID || clientSecret != p.dir.ClientSecret {
		writeOAuthError(w, http.StatusUnauthorized, "invalid_client",
			"Client authentication failed.")
		return
	}
	if tenant != p.dir.TenantID && tenant != "common" && tenant != "organizations" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request",
			"Unsupported tenant: "+tenant)
		return
	}

	scope := req.Form.Get("scope")
	aud := resourceFromScope(scope, req)

	now := time.Now()
	claims := map[string]any{
		"aud":   aud,
		"iss":   p.issuerFor(req, p.dir.TenantID),
		"iat":   now.Unix(),
		"nbf":   now.Unix(),
		"exp":   now.Add(tokenLifetime).Unix(),
		"tid":   p.dir.TenantID,
		"oid":   oidFor(clientID),
		"sub":   oidFor(clientID),
		"appid": clientID,
		"azp":   clientID,
		"roles": p.dir.Roles,
		"ver":   "2.0",
	}

	token, err := p.keys.Sign(claims)
	if err != nil {
		writeOAuthError(w, http.StatusInternalServerError, "server_error", err.Error())
		return
	}

	p.bus.Publish(kernel.Event{
		Type: "TokenIssued",
		Data: map[string]any{
			"grant_type": "client_credentials",
			"client_id":  clientID,
			"tenant":     p.dir.TenantID,
		},
	})

	writeJSON(w, http.StatusOK, TokenResponse{
		TokenType:    "Bearer",
		Scope:        scope,
		ExpiresIn:    int(tokenLifetime.Seconds()),
		ExtExpiresIn: int(tokenLifetime.Seconds()),
		AccessToken:  token,
	})
}

func (p *Provider) grantPassword(w http.ResponseWriter, req *http.Request, tenant string) {
	username := req.Form.Get("username")
	password := req.Form.Get("password")
	clientID, _ := extractClientCreds(req)
	if clientID == "" {
		clientID = "local-client"
	}

	if username == "" || password == "" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request",
			"username and password are required")
		return
	}
	if username != p.dir.Username || password != p.dir.Password {
		writeOAuthError(w, http.StatusUnauthorized, "invalid_grant",
			"Incorrect username or password.")
		return
	}
	if tenant != p.dir.TenantID && tenant != "common" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request",
			"Unsupported tenant: "+tenant)
		return
	}

	scope := req.Form.Get("scope")
	aud := resourceFromScope(scope, req)

	now := time.Now()
	claims := map[string]any{
		"aud":   aud,
		"iss":   p.issuerFor(req, p.dir.TenantID),
		"iat":   now.Unix(),
		"nbf":   now.Unix(),
		"exp":   now.Add(tokenLifetime).Unix(),
		"tid":   p.dir.TenantID,
		"oid":   oidFor(username),
		"sub":   oidFor(username),
		"appid": clientID,
		"name":  username,
		"email": username,
		"roles": p.dir.Roles,
		"ver":   "2.0",
	}

	token, err := p.keys.Sign(claims)
	if err != nil {
		writeOAuthError(w, http.StatusInternalServerError, "server_error", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, TokenResponse{
		TokenType:    "Bearer",
		Scope:        scope,
		ExpiresIn:    int(tokenLifetime.Seconds()),
		ExtExpiresIn: int(tokenLifetime.Seconds()),
		AccessToken:  token,
	})
}

// extractClientCreds reads credentials from HTTP Basic or form fields.
func extractClientCreds(req *http.Request) (string, string) {
	if user, pass, ok := req.BasicAuth(); ok {
		return user, pass
	}
	return req.Form.Get("client_id"), req.Form.Get("client_secret")
}

// resourceFromScope derives the audience from the request. If the client asks
// for a scope like "https://management.azure.com/.default", we echo back a
// reasonable audience. Otherwise we default to the local ARM endpoint.
func resourceFromScope(scope string, req *http.Request) string {
	if scope != "" {
		first := strings.Fields(scope)[0]
		if strings.HasSuffix(first, "/.default") {
			return strings.TrimSuffix(first, ".default")
		}
		return first
	}
	return baseURL(req) + "/"
}

// oidFor produces a stable, opaque-looking object ID for a given subject.
func oidFor(subject string) string {
	return kernel.OIDFor(subject)
}

var _ = fmt.Sprintf
