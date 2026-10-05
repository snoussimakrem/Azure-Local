package arm

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
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
	p, err := New(persist, kernel.NewEventBus(), logger)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func do(t *testing.T, p *Provider, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, bytes.NewBufferString(body))
		r.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	claimed := p.Handle(w, r)
	if !claimed {
		t.Fatalf("%s %s: not claimed", method, path)
	}
	return w
}

func TestListSubscriptions(t *testing.T) {
	p := newProvider(t)
	w := do(t, p, http.MethodGet, "/subscriptions?api-version=2022-12-01", "")
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	var out SubscriptionList
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Value) != 1 || out.Value[0].SubscriptionID != DefaultSubscriptionID {
		t.Fatalf("unexpected subs: %+v", out.Value)
	}
}

func TestResourceGroupCRUD(t *testing.T) {
	p := newProvider(t)
	base := "/subscriptions/" + DefaultSubscriptionID + "/resourceGroups/demo"

	// Create
	w := do(t, p, http.MethodPut, base+"?api-version=2022-09-01", `{"location":"westeurope"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var rg ResourceGroup
	_ = json.Unmarshal(w.Body.Bytes(), &rg)
	if rg.Name != "demo" || rg.Location != "westeurope" {
		t.Fatalf("bad rg: %+v", rg)
	}

	// Get
	w = do(t, p, http.MethodGet, base+"?api-version=2022-09-01", "")
	if w.Code != http.StatusOK {
		t.Fatalf("get: %d", w.Code)
	}

	// List
	w = do(t, p, http.MethodGet, "/subscriptions/"+DefaultSubscriptionID+"/resourceGroups?api-version=2022-09-01", "")
	var list ResourceGroupList
	_ = json.Unmarshal(w.Body.Bytes(), &list)
	if len(list.Value) != 1 || list.Value[0].Name != "demo" {
		t.Fatalf("list: %+v", list.Value)
	}

	// Delete
	w = do(t, p, http.MethodDelete, base+"?api-version=2022-09-01", "")
	if w.Code != http.StatusOK {
		t.Fatalf("delete: %d", w.Code)
	}

	// Get after delete
	w = do(t, p, http.MethodGet, base+"?api-version=2022-09-01", "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("get-after-delete: %d", w.Code)
	}
}

func TestStorageAccountCRUD(t *testing.T) {
	p := newProvider(t)
	sub := DefaultSubscriptionID
	rgURL := "/subscriptions/" + sub + "/resourceGroups/strg"
	do(t, p, http.MethodPut, rgURL+"?api-version=2022-09-01", `{"location":"westeurope"}`)

	saURL := rgURL + "/providers/Microsoft.Storage/storageAccounts/myacct?api-version=2023-01-01"
	body := `{"location":"westeurope","sku":{"name":"Standard_LRS"},"kind":"StorageV2"}`

	w := do(t, p, http.MethodPut, saURL, body)
	if w.Code != http.StatusOK && w.Code != http.StatusCreated {
		t.Fatalf("put sa: %d %s", w.Code, w.Body.String())
	}
	var sa GenericResource
	_ = json.Unmarshal(w.Body.Bytes(), &sa)
	if sa.Name != "myacct" || sa.Type != "Microsoft.Storage/storageAccounts" {
		t.Fatalf("bad sa: %+v", sa)
	}
	if sa.Properties["provisioningState"] != "Succeeded" {
		t.Fatalf("missing provisioningState")
	}
	ep, ok := sa.Properties["primaryEndpoints"].(map[string]any)
	if !ok || ep["blob"] == "" {
		t.Fatalf("missing primaryEndpoints: %+v", sa.Properties)
	}

	// Get
	w = do(t, p, http.MethodGet, saURL, "")
	if w.Code != http.StatusOK {
		t.Fatalf("get sa: %d", w.Code)
	}

	// List in RG
	w = do(t, p, http.MethodGet,
		rgURL+"/resources?api-version=2022-09-01", "")
	var list ResourceList
	_ = json.Unmarshal(w.Body.Bytes(), &list)
	if len(list.Value) != 1 {
		t.Fatalf("resources: %+v", list.Value)
	}

	// Delete
	w = do(t, p, http.MethodDelete, saURL, "")
	if w.Code != http.StatusOK {
		t.Fatalf("del sa: %d", w.Code)
	}
}

func TestMetadataEndpoint(t *testing.T) {
	p := newProvider(t)
	w := do(t, p, http.MethodGet,
		"/metadata/endpoints?api-version=2022-09-01", "")
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"resourceManager"`) {
		t.Fatalf("missing resourceManager in metadata: %s", w.Body.String())
	}
}

func TestDeclinesNonARMPaths(t *testing.T) {
	p := newProvider(t)
	for _, path := range []string{
		"/",
		"/health",
		"/devstoreaccount1/invoices",
		"/something/else",
	} {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		if p.Handle(w, r) {
			t.Fatalf("claimed %q, should decline", path)
		}
	}
}

func TestPutResourceRequiresExistingRG(t *testing.T) {
	p := newProvider(t)
	w := do(t, p, http.MethodPut,
		"/subscriptions/local-sub/resourceGroups/nope/providers/Microsoft.Storage/storageAccounts/x?api-version=2023-01-01",
		`{"location":"westeurope"}`)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}
