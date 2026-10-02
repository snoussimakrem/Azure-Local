package blob

import (
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

func TestCreateAndListContainer(t *testing.T) {
	p := newProvider(t)

	req := httptest.NewRequest(http.MethodPut,
		"/devstoreaccount1/invoices?restype=container", nil)
	req.Header.Set("x-ms-version", "2023-11-03")
	w := httptest.NewRecorder()
	if !p.Handle(w, req) {
		t.Fatal("expected Handle to claim the request")
	}
	if w.Code != http.StatusCreated {
		t.Fatalf("create: got %d, want 201", w.Code)
	}
	if w.Header().Get("ETag") == "" {
		t.Fatal("create: missing ETag header")
	}

	req = httptest.NewRequest(http.MethodGet,
		"/devstoreaccount1/?comp=list", nil)
	w = httptest.NewRecorder()
	if !p.Handle(w, req) {
		t.Fatal("expected Handle to claim the list request")
	}
	if w.Code != http.StatusOK {
		t.Fatalf("list: got %d, want 200", w.Code)
	}
	if !strings.Contains(w.Body.String(), "<Name>invoices</Name>") {
		t.Fatalf("list: body missing container name:\n%s", w.Body.String())
	}
}

func TestCreateExistingContainerConflicts(t *testing.T) {
	p := newProvider(t)

	makeReq := func() *http.Request {
		r := httptest.NewRequest(http.MethodPut,
			"/devstoreaccount1/dup?restype=container", nil)
		r.Header.Set("x-ms-version", "2023-11-03")
		return r
	}

	w := httptest.NewRecorder()
	p.Handle(w, makeReq())
	if w.Code != http.StatusCreated {
		t.Fatalf("first create: %d", w.Code)
	}

	w = httptest.NewRecorder()
	p.Handle(w, makeReq())
	if w.Code != http.StatusConflict {
		t.Fatalf("second create: got %d, want 409", w.Code)
	}
	if !strings.Contains(w.Body.String(), "ContainerAlreadyExists") {
		t.Fatalf("expected ContainerAlreadyExists, got:\n%s", w.Body.String())
	}
}

func TestDeleteContainer(t *testing.T) {
	p := newProvider(t)

	req := httptest.NewRequest(http.MethodPut,
		"/devstoreaccount1/todelete?restype=container", nil)
	w := httptest.NewRecorder()
	p.Handle(w, req)

	req = httptest.NewRequest(http.MethodDelete,
		"/devstoreaccount1/todelete?restype=container", nil)
	w = httptest.NewRecorder()
	if !p.Handle(w, req) {
		t.Fatal("Handle did not claim delete")
	}
	if w.Code != http.StatusAccepted {
		t.Fatalf("delete: got %d, want 202", w.Code)
	}

	req = httptest.NewRequest(http.MethodHead,
		"/devstoreaccount1/todelete?restype=container", nil)
	w = httptest.NewRecorder()
	p.Handle(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("head after delete: got %d, want 404", w.Code)
	}
}

func TestRejectsNonBlobPaths(t *testing.T) {
	p := newProvider(t)
	for _, path := range []string{
		"/health",
		"/subscriptions/abc/resourceGroups/rg",
		"/providers/Microsoft.Storage/storageAccounts/x",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		if p.Handle(w, req) {
			t.Fatalf("Handle claimed %q, should not", path)
		}
	}
}

func TestInvalidContainerNameRejected(t *testing.T) {
	p := newProvider(t)
	for _, name := range []string{"ab", "UPPER", "with_underscore", "-lead", "trail-", "a--b"} {
		req := httptest.NewRequest(http.MethodPut,
			"/devstoreaccount1/"+name+"?restype=container", nil)
		w := httptest.NewRecorder()
		p.Handle(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("name %q: got %d, want 400", name, w.Code)
		}
	}
}
