package authorization

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/azure-local/azure-local/internal/kernel"
)

type Provider struct {
	logger *slog.Logger
	store  *Store
	bus    *kernel.EventBus

	// defaultPrincipalID is the principal to seed with a Contributor role at
	// the subscription scope on first boot. Without it, a fresh install with
	// enforcement on would deny every write.
	defaultPrincipalID string

	// enforceRBAC toggles RBAC evaluation on ARM writes. Policy is always
	// evaluated. RBAC is opt-in so `curl` continues to work out of the box.
	enforceRBAC bool
}

func New(persist *kernel.PersistenceManager, bus *kernel.EventBus, logger *slog.Logger, defaultPrincipalID string) (*Provider, error) {
	store, err := NewStore(persist)
	if err != nil {
		return nil, err
	}
	p := &Provider{
		logger:             logger,
		store:              store,
		bus:                bus,
		defaultPrincipalID: defaultPrincipalID,
	}
	if err := p.seedDefaultAssignment(); err != nil {
		return nil, err
	}
	return p, nil
}

// seedDefaultAssignment grants the local-client principal the Contributor
// role at the subscription scope on first boot, so enforcement doesn't
// immediately lock everyone out.
func (p *Provider) seedDefaultAssignment() error {
	if p.defaultPrincipalID == "" {
		return nil
	}
	scope := "/subscriptions/" + kernelDefaultSubscription()
	// Skip if any assignment for this principal already exists at this scope.
	for _, ra := range p.store.ListRoleAssignments(scope) {
		if ra.Properties.PrincipalID == p.defaultPrincipalID && ra.Properties.Scope == scope {
			return nil
		}
	}
	name := "local-client-contributor"
	ra := RoleAssignment{
		ID:   scope + "/providers/Microsoft.Authorization/roleAssignments/" + name,
		Name: name,
		Type: "Microsoft.Authorization/roleAssignments",
		Properties: RoleAssignmentProps{
			RoleDefinitionID: "/providers/Microsoft.Authorization/roleDefinitions/" + RoleContributorGUID,
			PrincipalID:      p.defaultPrincipalID,
			PrincipalType:    "ServicePrincipal",
			Scope:            scope,
		},
	}
	return p.store.WriteRoleAssignment(ra)
}

func kernelDefaultSubscription() string { return "local-sub" }

func (p *Provider) Name() string    { return "authorization" }
func (p *Provider) Version() string { return "2022-04-01" }

func (p *Provider) Init(context.Context) error  { return nil }
func (p *Provider) Start(context.Context) error { return nil }
func (p *Provider) Stop(context.Context) error  { return nil }

func (p *Provider) Health(context.Context) kernel.HealthStatus { return kernel.Healthy() }

func (p *Provider) SetEnforceRBAC(v bool) { p.enforceRBAC = v }

// ---- HTTP routing --------------------------------------------------------

func (p *Provider) Handle(w http.ResponseWriter, req *http.Request) bool {
	path := strings.TrimPrefix(req.URL.Path, "/")
	if path == "" {
		return false
	}
	parts := strings.Split(path, "/")

	// /subscriptions/{sub}/providers/Microsoft.Authorization/{type}[/{name}]
	if len(parts) >= 4 && parts[0] == "subscriptions" && parts[2] == "providers" &&
		parts[3] == "Microsoft.Authorization" {
		return p.handleAuthorizationPath(w, req, parts[1], parts[4:])
	}
	// /providers/Microsoft.Authorization/{type}[/{name}]  (built-in scope)
	if len(parts) >= 3 && parts[0] == "providers" && parts[1] == "Microsoft.Authorization" {
		return p.handleAuthorizationPath(w, req, "", parts[2:])
	}
	// /subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.Authorization/...
	if len(parts) >= 7 && parts[0] == "subscriptions" && parts[2] == "resourceGroups" &&
		parts[4] == "providers" && parts[5] == "Microsoft.Authorization" {
		rg := parts[3]
		return p.handleAuthorizationPathRG(w, req, parts[1], rg, parts[6:])
	}
	return false
}

func (p *Provider) handleAuthorizationPath(w http.ResponseWriter, req *http.Request, sub string, rest []string) bool {
	if len(rest) == 0 {
		return false
	}
	kind := rest[0]
	switch kind {
	case "roleDefinitions":
		return p.routeRoleDefinitions(w, req, sub, rest)
	case "roleAssignments":
		return p.routeRoleAssignments(w, req, sub, "", rest)
	case "policyDefinitions":
		return p.routePolicyDefinitions(w, req, sub, rest)
	case "policyAssignments":
		return p.routePolicyAssignments(w, req, sub, "", rest)
	}
	return false
}

func (p *Provider) handleAuthorizationPathRG(w http.ResponseWriter, req *http.Request, sub, rg string, rest []string) bool {
	if len(rest) == 0 {
		return false
	}
	kind := rest[0]
	switch kind {
	case "roleAssignments":
		return p.routeRoleAssignments(w, req, sub, rg, rest)
	case "policyAssignments":
		return p.routePolicyAssignments(w, req, sub, rg, rest)
	}
	return false
}

// ---- roleDefinitions -----------------------------------------------------

func (p *Provider) routeRoleDefinitions(w http.ResponseWriter, req *http.Request, sub string, rest []string) bool {
	if req.Method != http.MethodGet {
		return false
	}
	if len(rest) == 1 {
		defs := p.store.ListRoleDefs()
		writeJSON(w, http.StatusOK, RoleDefinitionList{Value: defs})
		return true
	}
	name := rest[1]
	rd, ok := p.store.ReadRoleDef(name)
	if !ok {
		writeARMError(w, http.StatusNotFound, "RoleDefinitionNotFound",
			"Role definition '"+name+"' not found.")
		return true
	}
	writeJSON(w, http.StatusOK, rd)
	return true
}

// ---- roleAssignments -----------------------------------------------------

func (p *Provider) routeRoleAssignments(w http.ResponseWriter, req *http.Request, sub, rg string, rest []string) bool {
	scope := "/subscriptions/" + sub
	if rg != "" {
		scope += "/resourceGroups/" + rg
	}

	if len(rest) == 1 {
		if req.Method == http.MethodGet {
			assignments := p.store.ListRoleAssignments(scope)
			// Also include assignments at strictly descendant scopes.
			writeJSON(w, http.StatusOK, RoleAssignmentList{Value: assignments})
			return true
		}
		return false
	}
	name := rest[1]

	switch req.Method {
	case http.MethodGet:
		ra, ok := p.store.ReadRoleAssignment(name)
		if !ok {
			writeARMError(w, http.StatusNotFound, "RoleAssignmentNotFound",
				"Role assignment '"+name+"' not found.")
			return true
		}
		writeJSON(w, http.StatusOK, ra)
		return true
	case http.MethodPut:
		var in RoleAssignment
		if err := json.NewDecoder(req.Body).Decode(&in); err != nil {
			writeARMError(w, http.StatusBadRequest, "InvalidRequestContent", err.Error())
			return true
		}
		in.Name = name
		in.Type = "Microsoft.Authorization/roleAssignments"
		in.ID = scope + "/providers/Microsoft.Authorization/roleAssignments/" + name
		if in.Properties.Scope == "" {
			in.Properties.Scope = scope
		}
		if in.Properties.PrincipalType == "" {
			in.Properties.PrincipalType = "ServicePrincipal"
		}
		if err := p.store.WriteRoleAssignment(in); err != nil {
			writeARMError(w, http.StatusInternalServerError, "InternalError", err.Error())
			return true
		}
		p.bus.Publish(kernel.Event{
			Type: "RoleAssignmentCreated",
			Data: map[string]any{
				"name":        name,
				"principalId": in.Properties.PrincipalID,
				"role":        in.Properties.RoleDefinitionID,
				"scope":       in.Properties.Scope,
			},
		})
		writeJSON(w, http.StatusCreated, in)
		return true
	case http.MethodDelete:
		if err := p.store.DeleteRoleAssignment(name); err != nil {
			writeARMError(w, http.StatusNotFound, "RoleAssignmentNotFound",
				"Role assignment '"+name+"' not found.")
			return true
		}
		w.WriteHeader(http.StatusOK)
		return true
	}
	return false
}

// ---- policyDefinitions ---------------------------------------------------

func (p *Provider) routePolicyDefinitions(w http.ResponseWriter, req *http.Request, sub string, rest []string) bool {
	if req.Method != http.MethodGet {
		return false
	}
	if len(rest) == 1 {
		defs := p.store.ListPolicyDefs()
		writeJSON(w, http.StatusOK, PolicyDefinitionList{Value: defs})
		return true
	}
	name := rest[1]
	pd, ok := p.store.ReadPolicyDef(name)
	if !ok {
		writeARMError(w, http.StatusNotFound, "PolicyDefinitionNotFound",
			"Policy definition '"+name+"' not found.")
		return true
	}
	writeJSON(w, http.StatusOK, pd)
	return true
}

// ---- policyAssignments ---------------------------------------------------

func (p *Provider) routePolicyAssignments(w http.ResponseWriter, req *http.Request, sub, rg string, rest []string) bool {
	scope := "/subscriptions/" + sub
	if rg != "" {
		scope += "/resourceGroups/" + rg
	}

	if len(rest) == 1 {
		if req.Method == http.MethodGet {
			assignments := p.store.ListPolicyAssignments(scope)
			writeJSON(w, http.StatusOK, PolicyAssignmentList{Value: assignments})
			return true
		}
		return false
	}
	name := rest[1]

	switch req.Method {
	case http.MethodGet:
		pa, ok := p.store.ReadPolicyAssignment(name)
		if !ok {
			writeARMError(w, http.StatusNotFound, "PolicyAssignmentNotFound",
				"Policy assignment '"+name+"' not found.")
			return true
		}
		writeJSON(w, http.StatusOK, pa)
		return true
	case http.MethodPut:
		var in PolicyAssignment
		if err := json.NewDecoder(req.Body).Decode(&in); err != nil {
			writeARMError(w, http.StatusBadRequest, "InvalidRequestContent", err.Error())
			return true
		}
		in.Name = name
		in.Type = "Microsoft.Authorization/policyAssignments"
		in.ID = scope + "/providers/Microsoft.Authorization/policyAssignments/" + name
		if in.Properties.Scope == "" {
			in.Properties.Scope = scope
		}
		if err := p.store.WritePolicyAssignment(in); err != nil {
			writeARMError(w, http.StatusInternalServerError, "InternalError", err.Error())
			return true
		}
		writeJSON(w, http.StatusCreated, in)
		return true
	case http.MethodDelete:
		if err := p.store.DeletePolicyAssignment(name); err != nil {
			writeARMError(w, http.StatusNotFound, "PolicyAssignmentNotFound",
				"Policy assignment '"+name+"' not found.")
			return true
		}
		w.WriteHeader(http.StatusOK)
		return true
	}
	return false
}

// ---- JSON helpers --------------------------------------------------------

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeARMError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]any{"code": code, "message": message},
	})
}
