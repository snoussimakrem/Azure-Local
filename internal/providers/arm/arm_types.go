package arm

import "time"

// Subscription represents a local Azure subscription.
type Subscription struct {
	ID               string             `json:"id"`
	SubscriptionID   string             `json:"subscriptionId"`
	TenantID         string             `json:"tenantId"`
	DisplayName      string             `json:"displayName"`
	State            string             `json:"state"`
	AuthorizationSrc string             `json:"authorizationSource,omitempty"`
	Policies         *SubscriptionPol   `json:"subscriptionPolicies,omitempty"`
}

type SubscriptionPol struct {
	LocationPlacementID string `json:"locationPlacementId"`
	QuotaID             string `json:"quotaId"`
	SpendingLimit       string `json:"spendingLimit"`
}

type SubscriptionList struct {
	Value    []Subscription `json:"value"`
	NextLink string         `json:"nextLink,omitempty"`
}

// ResourceGroup represents an ARM resource group.
type ResourceGroup struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Type       string            `json:"type"`
	Location   string            `json:"location"`
	Tags       map[string]string `json:"tags,omitempty"`
	ManagedBy  string            `json:"managedBy,omitempty"`
	Properties RGProperties      `json:"properties"`
}

type RGProperties struct {
	ProvisioningState string `json:"provisioningState"`
}

type ResourceGroupList struct {
	Value    []ResourceGroup `json:"value"`
	NextLink string          `json:"nextLink,omitempty"`
}

// GenericResource is any ARM resource.
type GenericResource struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Type       string            `json:"type"`
	Location   string            `json:"location"`
	Tags       map[string]string `json:"tags,omitempty"`
	SKU        map[string]any    `json:"sku,omitempty"`
	Kind       string            `json:"kind,omitempty"`
	Properties map[string]any    `json:"properties,omitempty"`
}

type ResourceList struct {
	Value    []GenericResource `json:"value"`
	NextLink string            `json:"nextLink,omitempty"`
}

// StoredResource is the on-disk representation. It wraps a GenericResource
// with the metadata we need to reconstruct it and to route future requests.
type StoredResource struct {
	Subscription string          `json:"subscription"`
	ResourceGroup string         `json:"resourceGroup,omitempty"`
	Namespace    string          `json:"namespace"`
	ResourceType string          `json:"resourceType"`
	Name         string          `json:"name"`
	Location     string          `json:"location"`
	Body         GenericResource `json:"body"`
	Created      time.Time       `json:"created"`
}
