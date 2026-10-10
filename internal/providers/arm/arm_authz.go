package arm

import (
	"net/http"
	"strings"

	"github.com/azure-local/azure-local/internal/kernel"
)

// Authorizer is the interface the ARM provider uses to enforce policy and
// (optionally) RBAC. Implemented by the authorization package.
type Authorizer interface {
	EvaluatePolicy(sub, rg, ns, rtype, name, location string, tags map[string]string) error
	EvaluateRBAC(principalID, action, scope string) error
}

// PrincipalExtractor pulls the caller's principal ID from the request.
type PrincipalExtractor func(req *http.Request) (string, error)

// SetAuthorizer wires policy/RBAC into the ARM provider. When authz is nil,
// no enforcement happens. When extract is nil, RBAC enforcement is skipped
// even if authz is set.
func (p *Provider) SetAuthorizer(authz Authorizer, extract PrincipalExtractor, enforceRBAC bool) {
	p.authz = authz
	p.extract = extract
	p.enforceRBAC = enforceRBAC
}

// checkPolicy returns a non-nil error if policy forbids the write.
func (p *Provider) checkPolicy(sub, rg, ns, rtype, name, location string, tags map[string]string) error {
	if p.authz == nil {
		return nil
	}
	return p.authz.EvaluatePolicy(sub, rg, ns, rtype, name, location, tags)
}

// checkRBAC returns a non-nil error if RBAC forbids the write.
func (p *Provider) checkRBAC(req *http.Request, sub, rg, ns, rtype, name string) error {
	if !p.enforceRBAC || p.authz == nil || p.extract == nil {
		return nil
	}
	principal, err := p.extract(req)
	if err != nil {
		return err
	}
	action := computeAction(ns, rtype)
	scope := resourceScopeID(sub, rg, ns, rtype, name)
	return p.authz.EvaluateRBAC(principal, action, scope)
}

func computeAction(ns, rtype string) string {
	if ns == "" || rtype == "" {
		return "*"
	}
	return ns + "/" + rtype + "/write"
}

func resourceScopeID(sub, rg, ns, rtype, name string) string {
	if rg == "" {
		return "/subscriptions/" + sub + "/providers/" + ns + "/" + rtype + "/" + name
	}
	return "/subscriptions/" + sub + "/resourceGroups/" + rg + "/providers/" + ns + "/" + rtype + "/" + name
}

// projectTags safely extracts tags from a raw properties map.
func projectTags(props map[string]any) map[string]string {
	if props == nil {
		return nil
	}
	raw, ok := props["tags"]
	if !ok {
		return nil
	}
	m, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = toString(v)
	}
	return out
}

func toString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

var _ = strings.TrimSpace
var _ = kernel.OIDFor
