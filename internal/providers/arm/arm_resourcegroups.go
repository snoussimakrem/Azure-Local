package arm

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/azure-local/azure-local/internal/kernel"
)

func rgID(sub, name string) string {
	return fmt.Sprintf("/subscriptions/%s/resourceGroups/%s", sub, name)
}

func (p *Provider) listResourceGroups(w http.ResponseWriter, req *http.Request, sub string) {
	if _, ok := p.store.ReadSubscription(sub); !ok {
		writeARMError(w, http.StatusNotFound, "SubscriptionNotFound",
			"The subscription '"+sub+"' could not be found.")
		return
	}
	rgs := p.store.ListResourceGroups(sub)
	writeARMJSON(w, http.StatusOK, ResourceGroupList{Value: rgs})
}

func (p *Provider) getResourceGroup(w http.ResponseWriter, req *http.Request, sub, name string) {
	rg, ok := p.store.ReadResourceGroup(sub, name)
	if !ok {
		writeARMError(w, http.StatusNotFound, "ResourceGroupNotFound",
			"Resource group '"+name+"' could not be found.")
		return
	}
	writeARMJSON(w, http.StatusOK, rg)
}

func (p *Provider) headResourceGroup(w http.ResponseWriter, req *http.Request, sub, name string) {
	if _, ok := p.store.ReadResourceGroup(sub, name); !ok {
		writeARMError(w, http.StatusNotFound, "ResourceGroupNotFound",
			"Resource group '"+name+"' could not be found.")
		return
	}
	setARMHeaders(w)
	w.WriteHeader(http.StatusNoContent)
}

func (p *Provider) putResourceGroup(w http.ResponseWriter, req *http.Request, sub, name string) {
	if _, ok := p.store.ReadSubscription(sub); !ok {
		writeARMError(w, http.StatusNotFound, "SubscriptionNotFound",
			"The subscription '"+sub+"' could not be found.")
		return
	}

	var in struct {
		Location string            `json:"location"`
		Tags     map[string]string `json:"tags"`
		ManagedBy string           `json:"managedBy"`
	}
	if err := json.NewDecoder(req.Body).Decode(&in); err != nil {
		writeARMError(w, http.StatusBadRequest, "InvalidRequestContent",
			"Failed to parse request body: "+err.Error())
		return
	}
	if in.Location == "" {
		in.Location = "westeurope"
	}

	// Policy check: resource groups carry location and tags, so the same
	// allowed-locations / require-tag policies can apply.
	if err := p.checkPolicy(sub, "", "Microsoft.Resources", "resourceGroups",
		name, in.Location, in.Tags); err != nil {
		writeARMError(w, http.StatusForbidden, "RequestDisallowedByPolicy", err.Error())
		return
	}

	_, existed := p.store.ReadResourceGroup(sub, name)

	rg := ResourceGroup{
		ID:        rgID(sub, name),
		Name:      name,
		Type:      "Microsoft.Resources/resourceGroups",
		Location:  in.Location,
		Tags:      in.Tags,
		ManagedBy: in.ManagedBy,
		Properties: RGProperties{
			ProvisioningState: "Succeeded",
		},
	}
	if err := p.store.WriteResourceGroup(rg); err != nil {
		writeARMError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	status := http.StatusCreated
	if existed {
		status = http.StatusOK
	}

	p.bus.Publish(kernel.Event{
		Type: "ResourceCreated",
		Data: map[string]any{
			"id":       rg.ID,
			"provider": "Microsoft.Resources",
			"type":     "resourceGroups",
			"name":     rg.Name,
		},
	})

	writeARMJSON(w, status, rg)
}

func (p *Provider) deleteResourceGroup(w http.ResponseWriter, req *http.Request, sub, name string) {
	if _, ok := p.store.ReadResourceGroup(sub, name); !ok {
		writeARMError(w, http.StatusNotFound, "ResourceGroupNotFound",
			"Resource group '"+name+"' could not be found.")
		return
	}
	if err := p.store.DeleteResourceGroup(sub, name); err != nil {
		writeARMError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	setARMHeaders(w)
	w.WriteHeader(http.StatusOK)

	p.bus.Publish(kernel.Event{
		Type: "ResourceDeleted",
		Data: map[string]any{
			"id":   rgID(sub, name),
			"name": name,
		},
	})
}

var _ = time.Now
