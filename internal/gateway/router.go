package gateway

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/azure-local/azure-local/internal/kernel"
)

type Router struct {
	Logger   *slog.Logger
	Registry *kernel.Registry
	Started  time.Time
}

func New(logger *slog.Logger, reg *kernel.Registry) *Router {
	return &Router{Logger: logger, Registry: reg, Started: time.Now()}
}

func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	switch req.URL.Path {
	case "/health", "/healthz":
		r.handleHealth(w, req)
		return
	case "/_azlocal/version":
		r.handleVersion(w, req)
		return
	}

	// Delegate to services. First match wins.
	for _, svc := range r.Registry.All() {
		if svc.Handle(w, req) {
			return
		}
	}

	// Nothing claimed it. Be honest.
	writeJSONError(w, http.StatusNotFound, "ResourceNotFound",
		"The specified resource does not exist.")
}

type serviceHealth struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

type healthResponse struct {
	Status   string          `json:"status"`
	Endpoint string          `json:"endpoint"`
	Uptime   string          `json:"uptime"`
	Services []serviceHealth `json:"services"`
}

func (r *Router) handleHealth(w http.ResponseWriter, req *http.Request) {
	resp := healthResponse{
		Status:   "healthy",
		Uptime:   time.Since(r.Started).Round(time.Second).String(),
		Services: []serviceHealth{},
	}
	for _, svc := range r.Registry.All() {
		h := svc.Health(req.Context())
		resp.Services = append(resp.Services, serviceHealth{
			Name:    svc.Name(),
			Version: svc.Version(),
			Status:  h.Status,
			Message: h.Message,
		})
	}
	writeJSON(w, http.StatusOK, resp)
}

func (r *Router) handleVersion(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"name":    "azure-local",
		"version": "0.1.0-milestone1",
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeJSONError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]string{"code": code, "message": message},
	})
}
