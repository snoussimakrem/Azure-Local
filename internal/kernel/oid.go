package kernel

import "encoding/base64"

// OIDFor produces a stable, opaque object ID for a given subject string.
// Used both by the identity provider (to populate `oid` in tokens) and by
// the authorization provider (to match role assignments). Deterministic so
// the same client always maps to the same principal ID.
func OIDFor(subject string) string {
	return base64.RawURLEncoding.EncodeToString([]byte("oid:" + subject))
}
