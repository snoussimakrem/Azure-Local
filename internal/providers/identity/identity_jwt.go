package identity

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// Sign issues an RS256 JWT with the given claims map. The map is used as-is;
// callers are responsible for setting aud/iss/exp/nbf/iat.
func (ks *KeyStore) Sign(claims map[string]any) (string, error) {
	header := map[string]any{
		"typ": "JWT",
		"alg": "RS256",
		"kid": ks.Kid,
	}
	hb, err := json.Marshal(header)
	if err != nil {
		return "", err
	}
	pb, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	h64 := base64.RawURLEncoding.EncodeToString(hb)
	p64 := base64.RawURLEncoding.EncodeToString(pb)
	signingInput := h64 + "." + p64

	sum := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, ks.Key, crypto.SHA256, sum[:])
	if err != nil {
		return "", err
	}
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

// VerifiedClaims carries a parsed, signature-verified token body.
type VerifiedClaims map[string]any

// Verify checks the signature, alg, kid, exp, and nbf. It does NOT check aud
// or iss; callers do that if they care.
func (ks *KeyStore) Verify(token string) (VerifiedClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, errors.New("malformed token")
	}

	hb, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, errors.New("malformed header")
	}
	var header struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
	}
	if err := json.Unmarshal(hb, &header); err != nil {
		return nil, errors.New("malformed header")
	}
	if header.Alg != "RS256" {
		return nil, errors.New("unsupported alg")
	}
	if header.Kid != ks.Kid {
		return nil, errors.New("unknown kid")
	}

	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, errors.New("malformed signature")
	}
	signingInput := parts[0] + "." + parts[1]
	sum := sha256.Sum256([]byte(signingInput))
	if err := rsa.VerifyPKCS1v15(&ks.Key.PublicKey, crypto.SHA256, sum[:], sig); err != nil {
		return nil, errors.New("signature verification failed")
	}

	pb, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, errors.New("malformed payload")
	}
	var claims VerifiedClaims
	if err := json.Unmarshal(pb, &claims); err != nil {
		return nil, errors.New("malformed claims")
	}

	now := time.Now().Unix()
	if exp, ok := claims["exp"].(float64); ok && now > int64(exp) {
		return nil, errors.New("token expired")
	}
	if nbf, ok := claims["nbf"].(float64); ok && now < int64(nbf)-30 {
		return nil, errors.New("token not yet valid")
	}
	return claims, nil
}
