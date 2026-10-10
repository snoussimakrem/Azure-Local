package authorization

import (
	"io"
	"log/slog"
	"testing"

	"github.com/azure-local/azure-local/internal/kernel"
)

func newProvider(t *testing.T) *Provider {
	t.Helper()
	persist, err := kernel.NewPersistenceManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	p, err := New(persist, kernel.NewEventBus(), logger, kernel.OIDFor("local-client"))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestBuiltInRolesSeeded(t *testing.T) {
	p := newProvider(t)
	defs := p.store.ListRoleDefs()
	if len(defs) != 5 {
		t.Fatalf("expected 5 built-in roles, got %d", len(defs))
	}
}

func TestBuiltInPoliciesSeeded(t *testing.T) {
	p := newProvider(t)
	defs := p.store.ListPolicyDefs()
	if len(defs) != 2 {
		t.Fatalf("expected 2 built-in policies, got %d", len(defs))
	}
}

func TestRBACGrantedByDefaultAssignment(t *testing.T) {
	p := newProvider(t)
	oid := kernel.OIDFor("local-client")
	err := p.EvaluateRBAC(oid, "Microsoft.Storage/storageAccounts/write",
		"/subscriptions/local-sub/resourceGroups/demo/providers/Microsoft.Storage/storageAccounts/a")
	if err != nil {
		t.Fatalf("expected allow, got %v", err)
	}
}

func TestRBACDeniedForUnknownPrincipal(t *testing.T) {
	p := newProvider(t)
	err := p.EvaluateRBAC("unknown", "Microsoft.Storage/storageAccounts/write",
		"/subscriptions/local-sub")
	if err == nil {
		t.Fatal("expected deny")
	}
}

func TestRBACReaderCannotWrite(t *testing.T) {
	p := newProvider(t)
	// Create a reader assignment.
	ra := RoleAssignment{
		Name: "reader-1",
		Type: "Microsoft.Authorization/roleAssignments",
		Properties: RoleAssignmentProps{
			RoleDefinitionID: "/providers/Microsoft.Authorization/roleDefinitions/" + RoleReaderGUID,
			PrincipalID:      "reader-oid",
			Scope:            "/subscriptions/local-sub",
		},
	}
	_ = p.store.WriteRoleAssignment(ra)

	// Reader can read.
	if err := p.EvaluateRBAC("reader-oid", "Microsoft.Storage/storageAccounts/read",
		"/subscriptions/local-sub"); err != nil {
		t.Fatalf("read should be allowed: %v", err)
	}
	// Reader cannot write.
	if err := p.EvaluateRBAC("reader-oid", "Microsoft.Storage/storageAccounts/write",
		"/subscriptions/local-sub"); err == nil {
		t.Fatal("write should be denied")
	}
}

func TestPolicyAllowsMatchingLocation(t *testing.T) {
	p := newProvider(t)
	// Assign allowed-locations with westeurope.
	pa := PolicyAssignment{
		Name: "allow-west",
		Type: "Microsoft.Authorization/policyAssignments",
		Properties: PolicyAssignmentProps{
			PolicyDefinitionID: "/providers/Microsoft.Authorization/policyDefinitions/" + PolicyAllowedLocationsGUID,
			Scope:              "/subscriptions/local-sub",
			Parameters: map[string]any{
				"listOfAllowedLocations": map[string]any{
					"value": []any{"westeurope"},
				},
			},
		},
	}
	_ = p.store.WritePolicyAssignment(pa)

	err := p.EvaluatePolicy("local-sub", "demo", "Microsoft.Storage", "storageAccounts",
		"acct", "westeurope", nil)
	if err != nil {
		t.Fatalf("westeurope should be allowed: %v", err)
	}
}

func TestPolicyDeniesDisallowedLocation(t *testing.T) {
	p := newProvider(t)
	pa := PolicyAssignment{
		Name: "allow-west",
		Type: "Microsoft.Authorization/policyAssignments",
		Properties: PolicyAssignmentProps{
			PolicyDefinitionID: "/providers/Microsoft.Authorization/policyDefinitions/" + PolicyAllowedLocationsGUID,
			Scope:              "/subscriptions/local-sub",
			Parameters: map[string]any{
				"listOfAllowedLocations": map[string]any{
					"value": []any{"westeurope"},
				},
			},
		},
	}
	_ = p.store.WritePolicyAssignment(pa)

	err := p.EvaluatePolicy("local-sub", "demo", "Microsoft.Storage", "storageAccounts",
		"acct", "eastus", nil)
	if err == nil {
		t.Fatal("eastus should be denied")
	}
}

func TestPolicyRequireTag(t *testing.T) {
	p := newProvider(t)
	pa := PolicyAssignment{
		Name: "require-env",
		Type: "Microsoft.Authorization/policyAssignments",
		Properties: PolicyAssignmentProps{
			PolicyDefinitionID: "/providers/Microsoft.Authorization/policyDefinitions/" + PolicyRequireTagGUID,
			Scope:              "/subscriptions/local-sub",
			Parameters: map[string]any{
				"tagName": map[string]any{"value": "env"},
			},
		},
	}
	_ = p.store.WritePolicyAssignment(pa)

	// No env tag -> deny.
	err := p.EvaluatePolicy("local-sub", "demo", "Microsoft.Storage", "storageAccounts",
		"acct", "westeurope", nil)
	if err == nil {
		t.Fatal("missing env tag should be denied")
	}
	// With env tag -> allow.
	err = p.EvaluatePolicy("local-sub", "demo", "Microsoft.Storage", "storageAccounts",
		"acct", "westeurope", map[string]string{"env": "prod"})
	if err != nil {
		t.Fatalf("with env tag should be allowed: %v", err)
	}
}

func TestScopeChain(t *testing.T) {
	got := scopeChain("/subscriptions/s/resourceGroups/rg/providers/NS/T/N")
	want := []string{
		"/subscriptions/s/resourceGroups/rg/providers/NS/T/N",
		"/subscriptions/s/resourceGroups/rg/providers/NS/T",
		"/subscriptions/s/resourceGroups/rg/providers/NS",
		"/subscriptions/s/resourceGroups/rg",
		"/subscriptions/s",
		"/",
	}
	if len(got) != len(want) {
		t.Fatalf("len mismatch: got %v", got)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("at %d: got %s want %s", i, got[i], want[i])
		}
	}
}

func TestActionMatching(t *testing.T) {
	cases := []struct {
		patterns []string
		action   string
		want     bool
	}{
		{[]string{"*"}, "Microsoft.Storage/storageAccounts/write", true},
		{[]string{"*/read"}, "Microsoft.Storage/storageAccounts/read", true},
		{[]string{"*/read"}, "Microsoft.Storage/storageAccounts/write", false},
		{[]string{"Microsoft.Storage/*"}, "Microsoft.Storage/storageAccounts/write", true},
		{[]string{"Microsoft.Storage/storageAccounts/write"}, "Microsoft.Storage/storageAccounts/write", true},
	}
	for _, tc := range cases {
		if got := actionMatches(tc.patterns, tc.action); got != tc.want {
			t.Fatalf("actionMatches(%v, %s) = %v, want %v", tc.patterns, tc.action, got, tc.want)
		}
	}
}
