package arm

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/azure-local/azure-local/internal/kernel"
)

func resourceID(sub, rg, ns, rtype, name string) string {
	if rg == "" {
		return fmt.Sprintf("/subscriptions/%s/providers/%s/%s/%s", sub, ns, rtype, name)
	}
	return fmt.Sprintf("/subscriptions/%s/resourceGroups/%s/providers/%s/%s/%s",
		sub, rg, ns, rtype, name)
}

// ---- list ----------------------------------------------------------------

func (p *Provider) listResourcesInResourceGroup(w http.ResponseWriter, req *http.Request, sub, rg string) {
	if _, ok := p.store.ReadResourceGroup(sub, rg); !ok {
		writeARMError(w, http.StatusNotFound, "ResourceGroupNotFound",
			"Resource group '"+rg+"' could not be found.")
		return
	}
	stored := p.store.ListResources(sub, rg)
	out := make([]GenericResource, 0, len(stored))
	for _, s := range stored {
		out = append(out, s.Body)
	}
	writeARMJSON(w, http.StatusOK, ResourceList{Value: out})
}

func (p *Provider) listResourcesInSubscription(w http.ResponseWriter, req *http.Request, sub string) {
	if _, ok := p.store.ReadSubscription(sub); !ok {
		writeARMError(w, http.StatusNotFound, "SubscriptionNotFound",
			"The subscription '"+sub+"' could not be found.")
		return
	}
	stored := p.store.ListResources(sub, "")
	out := make([]GenericResource, 0, len(stored))
	for _, s := range stored {
		out = append(out, s.Body)
	}
	writeARMJSON(w, http.StatusOK, ResourceList{Value: out})
}

func (p *Provider) listResourcesByType(w http.ResponseWriter, req *http.Request, sub, rg, ns, rtype string) {
	stored := p.store.ListResources(sub, rg)
	out := make([]GenericResource, 0, len(stored))
	for _, s := range stored {
		if strings.EqualFold(s.Namespace, ns) && s.ResourceType == rtype {
			out = append(out, s.Body)
		}
	}
	writeARMJSON(w, http.StatusOK, ResourceList{Value: out})
}

// ---- CRUD ----------------------------------------------------------------

func (p *Provider) getResource(w http.ResponseWriter, req *http.Request, sub, rg, ns, rtype, name string) {
	res, ok := p.store.ReadResource(sub, rg, ns, rtype, name)
	if !ok {
		writeARMError(w, http.StatusNotFound, "ResourceNotFound",
			"The Resource '"+name+"' under resource group '"+rg+"' was not found.")
		return
	}
	writeARMJSON(w, http.StatusOK, res.Body)
}

func (p *Provider) headResource(w http.ResponseWriter, req *http.Request, sub, rg, ns, rtype, name string) {
	if _, ok := p.store.ReadResource(sub, rg, ns, rtype, name); !ok {
		writeARMError(w, http.StatusNotFound, "ResourceNotFound",
			"The Resource '"+name+"' under resource group '"+rg+"' was not found.")
		return
	}
	setARMHeaders(w)
	w.WriteHeader(http.StatusNoContent)
}

func (p *Provider) putResource(w http.ResponseWriter, req *http.Request, sub, rg, ns, rtype, name string) {
	if _, ok := p.store.ReadSubscription(sub); !ok {
		writeARMError(w, http.StatusNotFound, "SubscriptionNotFound",
			"The subscription '"+sub+"' could not be found.")
		return
	}
	if rg != "" {
		if _, ok := p.store.ReadResourceGroup(sub, rg); !ok {
			writeARMError(w, http.StatusNotFound, "ResourceGroupNotFound",
				"Resource group '"+rg+"' could not be found.")
			return
		}
	}

	var in GenericResource
	if err := json.NewDecoder(req.Body).Decode(&in); err != nil {
		writeARMError(w, http.StatusBadRequest, "InvalidRequestContent",
			"Failed to parse request body: "+err.Error())
		return
	}

	in.ID = resourceID(sub, rg, ns, rtype, name)
	in.Name = name
	in.Type = ns + "/" + rtype
	if in.Location == "" {
		in.Location = "westeurope"
	}
	if in.Properties == nil {
		in.Properties = map[string]any{}
	}
	// Azure guarantees this field on every resource.
	in.Properties["provisioningState"] = "Succeeded"

	// Storage account special case: inject primaryEndpoints so SDKs see
	// something real-looking (the URLs point at our own blob server).
	if strings.EqualFold(ns, "Microsoft.Storage") && rtype == "storageAccounts" {
		base := "http://" + req.Host
		in.Properties["primaryEndpoints"] = map[string]string{
			"blob":  base + "/" + name + "/",
			"queue": base + "/" + name + "-queue/",
			"table": base + "/" + name + "-table/",
			"file":  base + "/" + name + "-file/",
		}
		in.Properties["primaryLocation"] = in.Location
		in.Properties["statusOfPrimary"] = "available"
		if _, ok := in.Properties["creationTime"]; !ok {
			in.Properties["creationTime"] = time.Now().UTC().Format(time.RFC3339)
		}
	}

	stored := StoredResource{
		Subscription:  sub,
		ResourceGroup: rg,
		Namespace:     ns,
		ResourceType:  rtype,
		Name:          name,
		Location:      in.Location,
		Body:          in,
	}
	prev, existed := p.store.ReadResource(sub, rg, ns, rtype, name)
	if existed {
		stored.Created = prev.Created
	} else {
		stored.Created = time.Now().UTC()
	}

	if err := p.store.WriteResource(stored); err != nil {
		writeARMError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	status := http.StatusCreated
	if existed {
		status = http.StatusOK
	}

	writeARMJSON(w, status, in)

	p.bus.Publish(kernel.Event{
		Type: "ResourceCreated",
		Data: map[string]any{
			"id":       in.ID,
			"provider": ns,
			"type":     rtype,
			"name":     name,
			"location": in.Location,
		},
	})
}

func (p *Provider) deleteResource(w http.ResponseWriter, req *http.Request, sub, rg, ns, rtype, name string) {
	if _, ok := p.store.ReadResource(sub, rg, ns, rtype, name); !ok {
		writeARMError(w, http.StatusNotFound, "ResourceNotFound",
			"The Resource '"+name+"' was not found.")
		return
	}
	if err := p.store.DeleteResource(sub, rg, ns, rtype, name); err != nil {
		writeARMError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	setARMHeaders(w)
	w.WriteHeader(http.StatusOK)

	p.bus.Publish(kernel.Event{
		Type: "ResourceDeleted",
		Data: map[string]any{
			"id":   resourceID(sub, rg, ns, rtype, name),
			"name": name,
		},
	})
}
