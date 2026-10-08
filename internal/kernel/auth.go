package kernel

import "net/http"

// AuthFunc is called by providers that enforce authentication. Returning a
// non-nil error means the request must be rejected with 401.
//
// Providers accept a nil AuthFunc, in which case they skip enforcement. This
// keeps the local dev experience frictionless by default while making strict
// mode a one-line change at the wiring layer.
type AuthFunc func(*http.Request) error
