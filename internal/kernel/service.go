package kernel

import (
	"context"
	"net/http"
)

// HealthStatus is returned by every service's Health method.
type HealthStatus struct {
	Status  string `json:"status"`            // "healthy" | "degraded" | "unhealthy"
	Message string `json:"message,omitempty"`
}

func Healthy() HealthStatus { return HealthStatus{Status: "healthy"} }

// Service is the contract every Azure Local provider implements.
//
// Handle returns true if it fully handled the request. Returning false means
// the router must continue to the next service. This is what keeps the
// gateway from ever returning a fake success for an unhandled path.
type Service interface {
	Name() string
	Version() string

	Init(ctx context.Context) error
	Start(ctx context.Context) error
	Stop(ctx context.Context) error

	Health(ctx context.Context) HealthStatus
	Handle(w http.ResponseWriter, req *http.Request) bool
}
