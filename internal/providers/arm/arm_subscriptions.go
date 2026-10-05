package arm

import (
	"net/http"
	"strings"
)

func (p *Provider) listSubscriptions(w http.ResponseWriter, req *http.Request) {
	subs := p.store.ListSubscriptions()
	writeARMJSON(w, http.StatusOK, SubscriptionList{Value: subs})
}

func (p *Provider) getSubscription(w http.ResponseWriter, req *http.Request, subID string) {
	sub, ok := p.store.ReadSubscription(subID)
	if !ok {
		writeARMError(w, http.StatusNotFound, "SubscriptionNotFound",
			"The subscription '"+subID+"' could not be found.")
		return
	}
	writeARMJSON(w, http.StatusOK, sub)
}

func (p *Provider) handleTenants(w http.ResponseWriter, req *http.Request) {
	// Minimal /tenants response for az CLI discovery.
	body := map[string]any{
		"value": []map[string]any{
			{
				"id":             "/tenants/" + DefaultTenantID,
				"tenantId":       DefaultTenantID,
				"tenantCategory": "Home",
				"displayName":    "Local Tenant",
				"domains":        []string{"local"},
			},
		},
	}
	writeARMJSON(w, http.StatusOK, body)
}

func tenantFromPath(path string) string {
	return strings.TrimPrefix(path, "/tenants/")
}
