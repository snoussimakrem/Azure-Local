package arm

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/azure-local/azure-local/internal/kernel"
)

type Provider struct {
	logger *slog.Logger
	store  *Store
	bus    *kernel.EventBus
	auth   kernel.AuthFunc

	// authz is optional. When set, policy is enforced on every resource write
	// and RBAC is enforced when enforceRBAC is true.
	authz       Authorizer
	extract     PrincipalExtractor
	enforceRBAC bool
}

func New(persist *kernel.PersistenceManager, bus *kernel.EventBus, logger *slog.Logger, auth kernel.AuthFunc) (*Provider, error) {
	store, err := NewStore(persist)
	if err != nil {
		return nil, err
	}
	return &Provider{logger: logger, store: store, bus: bus, auth: auth}, nil
}

func (p *Provider) Name() string    { return "arm" }
func (p *Provider) Version() string { return "2022-09-01" }

func (p *Provider) Init(context.Context) error  { return nil }
func (p *Provider) Start(context.Context) error { return nil }
func (p *Provider) Stop(context.Context) error  { return nil }

func (p *Provider) Health(context.Context) kernel.HealthStatus { return kernel.Healthy() }

func (p *Provider) Handle(w http.ResponseWriter, req *http.Request) bool {
	path := strings.TrimPrefix(req.URL.Path, "/")
	if path == "" {
		return false
	}

	// Terraform metadata host contract — public, no auth.
	if path == "metadata/endpoints" {
		p.handleMetadata(w, req)
		return true
	}

	parts := strings.Split(path, "/")

	// Only proceed if this looks like an ARM request.
	claim := false
	switch parts[0] {
	case "subscriptions":
		claim = true
	case "tenants":
		if len(parts) == 1 {
			p.handleTenants(w, req)
			return true
		}
	case "providers":
		if len(parts) == 3 && parts[2] == "operations" {
			writeARMJSON(w, http.StatusOK, map[string]any{"value": []any{}})
			return true
		}
	}
	if !claim {
		return false
	}

	// Enforce auth if configured.
	if p.auth != nil {
		if err := p.auth(req); err != nil {
			writeARMError(w, http.StatusUnauthorized, "AuthenticationFailed",
				err.Error())
			return true
		}
	}

	return p.handleSubscriptions(w, req, parts)
}

func (p *Provider) handleSubscriptions(w http.ResponseWriter, req *http.Request, parts []string) bool {
	if len(parts) == 1 {
		if req.Method == http.MethodGet {
			p.listSubscriptions(w, req)
			return true
		}
		return false
	}
	subID := parts[1]

	if len(parts) == 2 {
		if req.Method == http.MethodGet {
			p.getSubscription(w, req, subID)
			return true
		}
		return false
	}

	switch parts[2] {
	case "resourceGroups":
		return p.handleResourceGroups(w, req, parts, subID)
	case "resources":
		if len(parts) == 3 && req.Method == http.MethodGet {
			p.listResourcesInSubscription(w, req, subID)
			return true
		}
	case "providers":
		return p.handleProviderResources(w, req, parts, subID, "")
	}
	return false
}

func (p *Provider) handleResourceGroups(w http.ResponseWriter, req *http.Request, parts []string, sub string) bool {
	if len(parts) == 3 {
		if req.Method == http.MethodGet {
			p.listResourceGroups(w, req, sub)
			return true
		}
		return false
	}

	rgName := parts[3]
	if len(parts) == 4 {
		switch req.Method {
		case http.MethodGet:
			p.getResourceGroup(w, req, sub, rgName)
			return true
		case http.MethodHead:
			p.headResourceGroup(w, req, sub, rgName)
			return true
		case http.MethodPut:
			p.putResourceGroup(w, req, sub, rgName)
			return true
		case http.MethodDelete:
			p.deleteResourceGroup(w, req, sub, rgName)
			return true
		}
		return false
	}

	switch parts[4] {
	case "resources":
		if len(parts) == 5 && req.Method == http.MethodGet {
			p.listResourcesInResourceGroup(w, req, sub, rgName)
			return true
		}
	case "providers":
		return p.handleProviderResources(w, req, parts, sub, rgName)
	}
	return false
}

func (p *Provider) handleProviderResources(w http.ResponseWriter, req *http.Request, parts []string, sub, rg string) bool {
	var idx int
	if rg == "" {
		idx = 3
	} else {
		idx = 5
	}
	if idx+1 >= len(parts) {
		return false
	}
	ns := parts[idx]
	rtype := parts[idx+1]

	if idx+2 == len(parts) {
		if req.Method == http.MethodGet {
			p.listResourcesByType(w, req, sub, rg, ns, rtype)
			return true
		}
		return false
	}
	if idx+3 != len(parts) {
		return false
	}
	name := parts[idx+2]

	switch req.Method {
	case http.MethodGet:
		p.getResource(w, req, sub, rg, ns, rtype, name)
		return true
	case http.MethodHead:
		p.headResource(w, req, sub, rg, ns, rtype, name)
		return true
	case http.MethodPut:
		p.putResource(w, req, sub, rg, ns, rtype, name)
		return true
	case http.MethodDelete:
		p.deleteResource(w, req, sub, rg, ns, rtype, name)
		return true
	}
	return false
}
