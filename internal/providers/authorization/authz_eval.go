package authorization

import (
	"fmt"
	"strings"
)

// ---- RBAC -----------------------------------------------------------------

// actionMatches reports whether the requested action is covered by one of
// the patterns. Patterns may be exact, prefix wildcards (Microsoft.Storage/*),
// or the universal "*".
func actionMatches(patterns []string, action string) bool {
	for _, p := range patterns {
		if p == "*" {
			return true
		}
		if strings.HasSuffix(p, "/*") {
			prefix := strings.TrimSuffix(p, "*")
			if strings.HasPrefix(strings.ToLower(action), strings.ToLower(prefix)) {
				return true
			}
			continue
		}
		if strings.EqualFold(p, action) {
			return true
		}
	}
	return false
}

// scopeChain returns the list of scopes that apply to a given scope, from
// most specific to least. For example:
//
//	/subscriptions/s/resourceGroups/rg/providers/NS/T/N
//	/subscriptions/s/resourceGroups/rg
//	/subscriptions/s
//	/
func scopeChain(scope string) []string {
	scope = strings.TrimSuffix(scope, "/")
	if scope == "" {
		return []string{"/"}
	}
	out := []string{scope}
	parts := strings.Split(strings.TrimPrefix(scope, "/"), "/")
	for i := len(parts) - 1; i > 0; i-- {
		out = append(out, "/"+strings.Join(parts[:i], "/"))
	}
	out = append(out, "/")
	return out
}

// EvaluateRBAC checks whether the principal has an assignment granting the
// action at or above the given scope. Returns nil if authorized.
func (p *Provider) EvaluateRBAC(principalID, action, scope string) error {
	if principalID == "" {
		return fmt.Errorf("no principal")
	}
	// Walk scopes from the resource up to root, collecting matching assignments.
	for _, s := range scopeChain(scope) {
		assignments := p.store.ListRoleAssignments(s)
		for _, ra := range assignments {
			if ra.Properties.Scope != s {
				continue
			}
			if ra.Properties.PrincipalID != principalID && ra.Properties.PrincipalID != "*" {
				continue
			}
			roleName := lastSegment(ra.Properties.RoleDefinitionID)
			rd, ok := p.store.ReadRoleDef(roleName)
			if !ok {
				continue
			}
			if roleAllowsAction(rd, action) {
				return nil
			}
		}
	}
	return fmt.Errorf("principal %s lacks permission for %s at scope %s", principalID, action, scope)
}

func roleAllowsAction(rd RoleDefinition, action string) bool {
	for _, perm := range rd.Properties.Permissions {
		if !actionMatches(perm.Actions, action) {
			continue
		}
		if actionMatches(perm.NotActions, action) {
			continue
		}
		return true
	}
	return false
}

func lastSegment(s string) string {
	s = strings.TrimSuffix(s, "/")
	if i := strings.LastIndex(s, "/"); i >= 0 {
		return s[i+1:]
	}
	return s
}

// ---- Policy ---------------------------------------------------------------

// policyField resolves a policy "field" expression against a resource's
// attributes. Supports:
//
//	"location"
//	"tags['env']"  or  tags["env"]
//	"name", "type"
func policyField(field string, ctx policyContext) (any, bool) {
	field = strings.TrimSpace(field)
	// Handle [concat(...)] and [parameters(...)] forms by treating them
	// as literal lookups on our simplified context.
	if strings.HasPrefix(field, "[") {
		// tagName param + concat -> map to context.TagName
		if strings.Contains(field, "tagName") {
			return ctx.TagName, true
		}
		if strings.Contains(field, "listOfAllowedLocations") {
			return ctx.AllowedLocations, true
		}
		return nil, false
	}
	if strings.HasPrefix(field, "tags[") && strings.HasSuffix(field, "]") {
		inner := field[len("tags[") : len(field)-1]
		inner = strings.Trim(inner, `'"`)
		v, ok := ctx.Tags[inner]
		return v, ok
	}
	switch field {
	case "location":
		return ctx.Location, true
	case "name":
		return ctx.Name, true
	case "type":
		return ctx.Type, true
	}
	return nil, false
}

type policyContext struct {
	Location         string
	Name             string
	Type             string
	Tags             map[string]string
	TagName          string
	AllowedLocations []string
}

// EvaluatePolicy runs every applicable policy assignment against a resource
// about to be written. Returns a *ViolationError if the request must be
// rejected.
func (p *Provider) EvaluatePolicy(sub, rg, ns, rtype, name, location string, tags map[string]string) error {
	scope := resourceScope(sub, rg, ns, rtype, name)
	for _, candidate := range scopeChain(scope) {
		assignments := p.store.ListPolicyAssignments(candidate)
		for _, pa := range assignments {
			if pa.Properties.EnforcementMode == "DoNotEnforce" {
				continue
			}
			defID := pa.Properties.PolicyDefinitionID
			defName := lastSegment(defID)
			pd, ok := p.store.ReadPolicyDef(defName)
			if !ok {
				continue
			}
			verdict := evalDefinition(pd, pa, policyContext{
				Location:         location,
				Name:             name,
				Type:             ns + "/" + rtype,
				Tags:             tags,
				TagName:          extractParamString(pa, "tagName"),
				AllowedLocations: extractParamArray(pa, "listOfAllowedLocations"),
			})
			if verdict != nil {
				return verdict
			}
		}
	}
	return nil
}

// evalDefinition returns non-nil if the resource violates the policy.
func evalDefinition(pd PolicyDefinition, pa PolicyAssignment, ctx policyContext) error {
	rule := pd.Properties.PolicyRule
	ifCond, _ := rule["if"].(map[string]any)
	thenCond, _ := rule["then"].(map[string]any)
	if ifCond == nil || thenCond == nil {
		return nil
	}
	if !evalCondition(ifCond, ctx) {
		return nil
	}
	effect, _ := thenCond["effect"].(string)
	if effect == "audit" {
		// Audit-only: log via p.logger in a real impl; M5 just skips.
		return nil
	}
	if effect == "deny" {
		return &ViolationError{
			AssignmentID: pa.ID,
			DefinitionID: pd.ID,
			Message: fmt.Sprintf(
				"Resource '%s' was disallowed by policy '%s' (%s)",
				ctx.Name, pd.Properties.DisplayName, pd.Name),
		}
	}
	return nil
}

// evalCondition evaluates an ARM policy condition tree. Supports:
//
//	{"field": "location", "notIn": [...]}
//	{"field": "location", "in": [...]}
//	{"field": "location", "equals": "westeurope"}
//	{"field": "location", "notEquals": "eastus"}
//	{"field": "tags['env']", "exists": false}
//	{"allOf": [...]}, {"anyOf": [...]}, {"not": {...}}
func evalCondition(cond map[string]any, ctx policyContext) bool {
	if allOf, ok := cond["allOf"].([]any); ok {
		for _, c := range allOf {
			cm, _ := c.(map[string]any)
			if !evalCondition(cm, ctx) {
				return false
			}
		}
		return true
	}
	if anyOf, ok := cond["anyOf"].([]any); ok {
		for _, c := range anyOf {
			cm, _ := c.(map[string]any)
			if evalCondition(cm, ctx) {
				return true
			}
		}
		return false
	}
	if not, ok := cond["not"].(map[string]any); ok {
		return !evalCondition(not, ctx)
	}

	fieldExpr, _ := cond["field"].(string)
	val, present := policyField(fieldExpr, ctx)

	if existsVal, ok := cond["exists"]; ok {
		want := existsVal == "true" || existsVal == true
		return present == want
	}

	// Comparison operators. We compare as strings for simplicity.
	got := fmt.Sprintf("%v", val)
	if want, ok := cond["equals"]; ok {
		return got == fmt.Sprintf("%v", want)
	}
	if want, ok := cond["notEquals"]; ok {
		return got != fmt.Sprintf("%v", want)
	}
	if list, ok := cond["in"].([]any); ok {
		for _, item := range list {
			if got == fmt.Sprintf("%v", item) {
				return true
			}
		}
		return false
	}
	if list, ok := cond["notIn"].([]any); ok {
		for _, item := range list {
			if got == fmt.Sprintf("%v", item) {
				return false
			}
		}
		return true
	}
	// Parameterized list: "[parameters('listOfAllowedLocations')]"
	if strList, ok := cond["in"].(string); ok && strList == "[parameters('listOfAllowedLocations')]" {
		for _, item := range ctx.AllowedLocations {
			if got == item {
				return true
			}
		}
		return false
	}
	return false
}

// ---- helpers --------------------------------------------------------------

func resourceScope(sub, rg, ns, rtype, name string) string {
	if rg == "" {
		return fmt.Sprintf("/subscriptions/%s/providers/%s/%s/%s", sub, ns, rtype, name)
	}
	return fmt.Sprintf("/subscriptions/%s/resourceGroups/%s/providers/%s/%s/%s",
		sub, rg, ns, rtype, name)
}

func extractParamString(pa PolicyAssignment, key string) string {
	if pa.Properties.Parameters == nil {
		return ""
	}
	raw, ok := pa.Properties.Parameters[key]
	if !ok {
		return ""
	}
	// The parameters can be {"value": "..."} or a bare scalar.
	if m, ok := raw.(map[string]any); ok {
		if v, ok := m["value"].(string); ok {
			return v
		}
	}
	if s, ok := raw.(string); ok {
		return s
	}
	return ""
}

func extractParamArray(pa PolicyAssignment, key string) []string {
	if pa.Properties.Parameters == nil {
		return nil
	}
	raw, ok := pa.Properties.Parameters[key]
	if !ok {
		return nil
	}
	var list []any
	if m, ok := raw.(map[string]any); ok {
		list, _ = m["value"].([]any)
	} else {
		list, _ = raw.([]any)
	}
	out := make([]string, 0, len(list))
	for _, v := range list {
		out = append(out, fmt.Sprintf("%v", v))
	}
	return out
}
