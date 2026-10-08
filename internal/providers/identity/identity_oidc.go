package identity

import (
	"net/http"
	"strings"
)

// baseURL derives the externally visible base URL from the request, so the
// discovery document points clients at whatever host they used to reach us.
func baseURL(req *http.Request) string {
	scheme := "http"
	if req.TLS != nil {
		scheme = "https"
	}
	if xf := req.Header.Get("X-Forwarded-Proto"); xf != "" {
		scheme = strings.Split(xf, ",")[0]
	}
	host := req.Host
	if host == "" {
		host = "localhost:4577"
	}
	return scheme + "://" + host
}

func (p *Provider) issuerFor(req *http.Request, tenant string) string {
	return baseURL(req) + "/" + tenant + "/v2.0"
}

func (p *Provider) handleOIDCDiscovery(w http.ResponseWriter, req *http.Request, tenant string) {
	base := baseURL(req)
	iss := p.issuerFor(req, tenant)

	doc := map[string]any{
		"issuer":                                iss,
		"authorization_endpoint":                base + "/" + tenant + "/oauth2/v2.0/authorize",
		"token_endpoint":                        base + "/" + tenant + "/oauth2/v2.0/token",
		"end_session_endpoint":                  base + "/" + tenant + "/oauth2/v2.0/logout",
		"jwks_uri":                              base + "/" + tenant + "/discovery/v2.0/keys",
		"response_modes_supported":              []string{"query", "fragment", "form_post"},
		"response_types_supported":              []string{"code", "id_token", "code id_token", "token id_token"},
		"scopes_supported":                      []string{"openid", "profile", "email", "offline_access"},
		"subject_types_supported":               []string{"pairwise"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
		"token_endpoint_auth_methods_supported": []string{"client_secret_post", "client_secret_basic", "private_key_jwt"},
		"claims_supported": []string{
			"aud", "iss", "iat", "exp", "nbf", "name", "sub", "tid", "oid", "appid", "roles", "ver",
		},
		"grant_types_supported": []string{
			"client_credentials", "password", "refresh_token",
		},
		"userinfo_endpoint": base + "/" + tenant + "/oidc/userinfo",
	}
	writeJSON(w, http.StatusOK, doc)
}

func (p *Provider) handleJWKS(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, JWKS{Keys: []JWK{p.keys.JWK()}})
}
