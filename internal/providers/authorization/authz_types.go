package authorization

// RoleDefinition is an ARM role definition (built-in or custom).
type RoleDefinition struct {
	ID         string              `json:"id"`
	Name       string              `json:"name"`
	Type       string              `json:"type"`
	Properties RoleDefinitionProps `json:"properties"`
}

type RoleDefinitionProps struct {
	RoleName         string       `json:"roleName"`
	Description      string       `json:"description,omitempty"`
	Type             string       `json:"type"` // "BuiltInRole" | "CustomRole"
	Permissions      []Permission `json:"permissions"`
	AssignableScopes []string     `json:"assignableScopes"`
}

type Permission struct {
	Actions        []string `json:"actions,omitempty"`
	NotActions     []string `json:"notActions,omitempty"`
	DataActions    []string `json:"dataActions,omitempty"`
	NotDataActions []string `json:"notDataActions,omitempty"`
}

type RoleDefinitionList struct {
	Value []RoleDefinition `json:"value"`
}

// RoleAssignment binds a principal to a role at a scope.
type RoleAssignment struct {
	ID         string              `json:"id"`
	Name       string              `json:"name"`
	Type       string              `json:"type"`
	Properties RoleAssignmentProps `json:"properties"`
}

type RoleAssignmentProps struct {
	RoleDefinitionID string `json:"roleDefinitionId"`
	PrincipalID      string `json:"principalId"`
	PrincipalType    string `json:"principalType,omitempty"`
	Scope            string `json:"scope"`
	CreatedOn        string `json:"createdOn,omitempty"`
	UpdatedOn        string `json:"updatedOn,omitempty"`
}

type RoleAssignmentList struct {
	Value []RoleAssignment `json:"value"`
}

// PolicyDefinition is an ARM policy definition.
type PolicyDefinition struct {
	ID         string                `json:"id"`
	Name       string                `json:"name"`
	Type       string                `json:"type"`
	Properties PolicyDefinitionProps `json:"properties"`
}

type PolicyDefinitionProps struct {
	DisplayName string         `json:"displayName"`
	Description string         `json:"description,omitempty"`
	PolicyType  string         `json:"policyType"` // "BuiltIn" | "Custom"
	Mode        string         `json:"mode"`
	PolicyRule  map[string]any `json:"policyRule"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

type PolicyDefinitionList struct {
	Value []PolicyDefinition `json:"value"`
}

// PolicyAssignment binds a policy definition to a scope.
type PolicyAssignment struct {
	ID         string                `json:"id"`
	Name       string                `json:"name"`
	Type       string                `json:"type"`
	Properties PolicyAssignmentProps `json:"properties"`
}

type PolicyAssignmentProps struct {
	DisplayName        string         `json:"displayName,omitempty"`
	PolicyDefinitionID string         `json:"policyDefinitionId"`
	Scope              string         `json:"scope,omitempty"`
	Parameters         map[string]any `json:"parameters,omitempty"`
	EnforcementMode    string         `json:"enforcementMode,omitempty"`
}

type PolicyAssignmentList struct {
	Value []PolicyAssignment `json:"value"`
}

// ViolationError is returned by policy evaluation when a request must be denied.
type ViolationError struct {
	AssignmentID string
	DefinitionID string
	Message      string
}

func (e *ViolationError) Error() string { return e.Message }
