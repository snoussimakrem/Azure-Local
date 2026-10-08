package identity

import "time"

// TokenResponse is the OAuth 2.0 token endpoint response body.
type TokenResponse struct {
	TokenType    string `json:"token_type"`
	Scope        string `json:"scope,omitempty"`
	ExpiresIn    int    `json:"expires_in"`
	ExtExpiresIn int    `json:"ext_expires_in"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	IDToken      string `json:"id_token,omitempty"`
}

// OAuthError is the standard OAuth 2.0 error body.
type OAuthError struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description,omitempty"`
	ErrorCodes       []int  `json:"error_codes,omitempty"`
	Timestamp        string `json:"timestamp,omitempty"`
	TraceID          string `json:"trace_id,omitempty"`
	CorrelationID    string `json:"correlation_id,omitempty"`
}

// JWK is a single JSON Web Key (RFC 7517).
type JWK struct {
	Kty string `json:"kty"`
	Use string `json:"use"`
	Kid string `json:"kid"`
	Alg string `json:"alg"`
	N   string `json:"n"`
	E   string `json:"e"`
}

type JWKS struct {
	Keys []JWK `json:"keys"`
}

// Claims is the set of Azure-shaped claims we put in access tokens.
type Claims struct {
	Aud   string   `json:"aud"`
	Iss   string   `json:"iss"`
	Iat   int64    `json:"iat"`
	Nbf   int64    `json:"nbf"`
	Exp   int64    `json:"exp"`
	Tid   string   `json:"tid"`
	Oid   string   `json:"oid"`
	Sub   string   `json:"sub"`
	AppID string   `json:"appid,omitempty"`
	Azp   string   `json:"azp,omitempty"`
	Roles []string `json:"roles,omitempty"`
	Scp   string   `json:"scp,omitempty"`
	Ver   string   `json:"ver"`
	// For id_token
	Name  string `json:"name,omitempty"`
	Email string `json:"email,omitempty"`
}

// TenantDirectory is a local mapping of tenant ID to known clients/users.
// Milestone 4 keeps this minimal: one tenant, one service principal.
type TenantDirectory struct {
	TenantID string
	Issuer   string

	// ServicePrincipal credentials. Anything else is rejected.
	ClientID     string
	ClientSecret string
	Roles        []string

	// Optional ROPC user.
	Username string
	Password string
}

func DefaultDirectory() TenantDirectory {
	return TenantDirectory{
		TenantID:     "local",
		Issuer:       "http://localhost:4577/local/v2.0",
		ClientID:     "local-client",
		ClientSecret: "local-secret",
		Roles:        []string{"Contributor"},
		Username:     "admin@local",
		Password:     "local",
	}
}

// tokenLifetime is what we hand back in the `exp` claim.
const tokenLifetime = time.Hour
