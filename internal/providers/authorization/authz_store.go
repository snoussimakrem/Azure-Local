package authorization

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/azure-local/azure-local/internal/kernel"
)

type Store struct {
	root string
	mu   sync.RWMutex
}

func NewStore(persist *kernel.PersistenceManager) (*Store, error) {
	root, err := persist.ServiceDir("authorization")
	if err != nil {
		return nil, err
	}
	for _, sub := range []string{"roledefs", "roleassignments", "policydefs", "policyassignments"} {
		if err := os.MkdirAll(filepath.Join(root, sub), 0o755); err != nil {
			return nil, err
		}
	}
	s := &Store{root: root}
	if err := s.seedBuiltIns(); err != nil {
		return nil, err
	}
	return s, nil
}

// ---- role definitions -----------------------------------------------------

func (s *Store) roleDefPath(name string) string {
	return filepath.Join(s.root, "roledefs", name+".json")
}

func (s *Store) ReadRoleDef(name string) (RoleDefinition, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	raw, err := os.ReadFile(s.roleDefPath(name))
	if err != nil {
		return RoleDefinition{}, false
	}
	var rd RoleDefinition
	if err := json.Unmarshal(raw, &rd); err != nil {
		return RoleDefinition{}, false
	}
	return rd, true
}

func (s *Store) WriteRoleDef(rd RoleDefinition) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return writeJSONFile(s.roleDefPath(rd.Name), rd)
}

func (s *Store) ListRoleDefs() []RoleDefinition {
	s.mu.RLock()
	defer s.mu.RUnlock()
	dir := filepath.Join(s.root, "roledefs")
	entries, _ := os.ReadDir(dir)
	var out []RoleDefinition
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var rd RoleDefinition
		if json.Unmarshal(raw, &rd) == nil {
			out = append(out, rd)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// ---- role assignments -----------------------------------------------------

// Roles assignments are stored flat: {root}/roleassignments/{name}.json
// The Scope field on the assignment determines applicability.
func (s *Store) roleAsgPath(name string) string {
	return filepath.Join(s.root, "roleassignments", name+".json")
}

func (s *Store) ReadRoleAssignment(name string) (RoleAssignment, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	raw, err := os.ReadFile(s.roleAsgPath(name))
	if err != nil {
		return RoleAssignment{}, false
	}
	var ra RoleAssignment
	if err := json.Unmarshal(raw, &ra); err != nil {
		return RoleAssignment{}, false
	}
	return ra, true
}

func (s *Store) WriteRoleAssignment(ra RoleAssignment) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return writeJSONFile(s.roleAsgPath(ra.Name), ra)
}

func (s *Store) DeleteRoleAssignment(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return os.Remove(s.roleAsgPath(name))
}

func (s *Store) ListRoleAssignments(scopePrefix string) []RoleAssignment {
	s.mu.RLock()
	defer s.mu.RUnlock()
	dir := filepath.Join(s.root, "roleassignments")
	entries, _ := os.ReadDir(dir)
	var out []RoleAssignment
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var ra RoleAssignment
		if json.Unmarshal(raw, &ra) != nil {
			continue
		}
		if scopePrefix != "" && !strings.HasPrefix(ra.Properties.Scope, scopePrefix) {
			continue
		}
		out = append(out, ra)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// ---- policy definitions ---------------------------------------------------

func (s *Store) policyDefPath(name string) string {
	return filepath.Join(s.root, "policydefs", name+".json")
}

func (s *Store) ReadPolicyDef(name string) (PolicyDefinition, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	raw, err := os.ReadFile(s.policyDefPath(name))
	if err != nil {
		return PolicyDefinition{}, false
	}
	var pd PolicyDefinition
	if err := json.Unmarshal(raw, &pd); err != nil {
		return PolicyDefinition{}, false
	}
	return pd, true
}

func (s *Store) WritePolicyDef(pd PolicyDefinition) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return writeJSONFile(s.policyDefPath(pd.Name), pd)
}

func (s *Store) ListPolicyDefs() []PolicyDefinition {
	s.mu.RLock()
	defer s.mu.RUnlock()
	dir := filepath.Join(s.root, "policydefs")
	entries, _ := os.ReadDir(dir)
	var out []PolicyDefinition
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var pd PolicyDefinition
		if json.Unmarshal(raw, &pd) == nil {
			out = append(out, pd)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// ---- policy assignments ---------------------------------------------------

func (s *Store) policyAsgPath(name string) string {
	return filepath.Join(s.root, "policyassignments", name+".json")
}

func (s *Store) ReadPolicyAssignment(name string) (PolicyAssignment, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	raw, err := os.ReadFile(s.policyAsgPath(name))
	if err != nil {
		return PolicyAssignment{}, false
	}
	var pa PolicyAssignment
	if err := json.Unmarshal(raw, &pa); err != nil {
		return PolicyAssignment{}, false
	}
	return pa, true
}

func (s *Store) WritePolicyAssignment(pa PolicyAssignment) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return writeJSONFile(s.policyAsgPath(pa.Name), pa)
}

func (s *Store) DeletePolicyAssignment(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return os.Remove(s.policyAsgPath(name))
}

func (s *Store) ListPolicyAssignments(scopePrefix string) []PolicyAssignment {
	s.mu.RLock()
	defer s.mu.RUnlock()
	dir := filepath.Join(s.root, "policyassignments")
	entries, _ := os.ReadDir(dir)
	var out []PolicyAssignment
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var pa PolicyAssignment
		if json.Unmarshal(raw, &pa) != nil {
			continue
		}
		if scopePrefix != "" && !strings.HasPrefix(pa.Properties.Scope, scopePrefix) {
			continue
		}
		out = append(out, pa)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// ---- seed built-ins -------------------------------------------------------

// Real Azure built-in role definition GUIDs.
const (
	RoleOwnerGUID        = "8e3af657-a8ff-443c-a75c-2fe8c4bcb635"
	RoleContributorGUID  = "b24988ac-6180-42a0-ab88-20f7382dd24c"
	RoleReaderGUID       = "acdd72a7-3385-48ef-bd42-f606fba81ae7"
	RoleBlobDataContribGUID = "ba92f5b4-2d11-453d-a403-e96b0029c9fe"
	RoleBlobDataReaderGUID  = "2a2b9908-6ea1-4ae2-8e65-a410df84e7d1"
)

// Real Azure built-in policy definition GUIDs.
const (
	PolicyAllowedLocationsGUID = "e56962a6-4747-49cd-b67b-bf8b01975c4c"
	PolicyRequireTagGUID       = "871b6d14-10aa-478d-b590-94f262ecfa99"
)

func (s *Store) seedBuiltIns() error {
	// Role definitions.
	roles := []RoleDefinition{
		{
			Name: RoleOwnerGUID,
			Properties: RoleDefinitionProps{
				RoleName:    "Owner",
				Description: "Full access to all resources.",
				Type:        "BuiltInRole",
				Permissions: []Permission{{Actions: []string{"*"}}},
				AssignableScopes: []string{"/"},
			},
		},
		{
			Name: RoleContributorGUID,
			Properties: RoleDefinitionProps{
				RoleName:    "Contributor",
				Description: "Manage all resources but cannot grant access.",
				Type:        "BuiltInRole",
				Permissions: []Permission{{
					Actions:    []string{"*"},
					NotActions: []string{
						"Microsoft.Authorization/*/write",
						"Microsoft.Authorization/*/delete",
					},
				}},
				AssignableScopes: []string{"/"},
			},
		},
		{
			Name: RoleReaderGUID,
			Properties: RoleDefinitionProps{
				RoleName:    "Reader",
				Description: "View all resources.",
				Type:        "BuiltInRole",
				Permissions: []Permission{{Actions: []string{"*/read"}}},
				AssignableScopes: []string{"/"},
			},
		},
		{
			Name: RoleBlobDataContribGUID,
			Properties: RoleDefinitionProps{
				RoleName:    "Storage Blob Data Contributor",
				Description: "Read, write, and delete blob data.",
				Type:        "BuiltInRole",
				Permissions: []Permission{{
					Actions:     []string{"Microsoft.Storage/storageAccounts/blobServices/containers/read"},
					DataActions: []string{"Microsoft.Storage/storageAccounts/blobServices/containers/blobs/*"},
				}},
				AssignableScopes: []string{"/"},
			},
		},
		{
			Name: RoleBlobDataReaderGUID,
			Properties: RoleDefinitionProps{
				RoleName:    "Storage Blob Data Reader",
				Description: "Read blob data.",
				Type:        "BuiltInRole",
				Permissions: []Permission{{
					DataActions: []string{"Microsoft.Storage/storageAccounts/blobServices/containers/blobs/read"},
				}},
				AssignableScopes: []string{"/"},
			},
		},
	}
	for i := range roles {
		roles[i].ID = "/providers/Microsoft.Authorization/roleDefinitions/" + roles[i].Name
		roles[i].Type = "Microsoft.Authorization/roleDefinitions"
		if _, ok := s.ReadRoleDef(roles[i].Name); !ok {
			if err := s.WriteRoleDef(roles[i]); err != nil {
				return err
			}
		}
	}

	// Policy definitions.
	defs := []PolicyDefinition{
		{
			Name: PolicyAllowedLocationsGUID,
			Properties: PolicyDefinitionProps{
				DisplayName: "Allowed locations",
				Description: "Restrict resource locations. Deny anything not in the allowed list.",
				PolicyType:  "BuiltIn",
				Mode:        "All",
				PolicyRule: map[string]any{
					"if": map[string]any{
						"not": map[string]any{
							"field": "location",
							"in":    "[parameters('listOfAllowedLocations')]",
						},
					},
					"then": map[string]any{"effect": "deny"},
				},
				Metadata: map[string]any{
					"parameters": map[string]any{
						"listOfAllowedLocations": map[string]any{"type": "Array"},
					},
				},
			},
		},
		{
			Name: PolicyRequireTagGUID,
			Properties: PolicyDefinitionProps{
				DisplayName: "Require a tag on resources",
				Description: "Deny resources that do not carry a specific tag.",
				PolicyType:  "BuiltIn",
				Mode:        "Indexed",
				PolicyRule: map[string]any{
					"if": map[string]any{
						"field":  "[concat('tags[', parameters('tagName'), ']')]",
						"exists": "false",
					},
					"then": map[string]any{"effect": "deny"},
				},
				Metadata: map[string]any{
					"parameters": map[string]any{
						"tagName": map[string]any{"type": "String"},
					},
				},
			},
		},
	}
	for i := range defs {
		defs[i].ID = "/providers/Microsoft.Authorization/policyDefinitions/" + defs[i].Name
		defs[i].Type = "Microsoft.Authorization/policyDefinitions"
		if _, ok := s.ReadPolicyDef(defs[i].Name); !ok {
			if err := s.WritePolicyDef(defs[i]); err != nil {
				return err
			}
		}
	}
	return nil
}

func writeJSONFile(path string, v any) error {
	tmp := path + ".tmp"
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
