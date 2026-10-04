package blob

import (
	"bytes"
	"encoding/base64"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
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

// ---- container tests ------------------------------------------------------

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

// ---- blob tests -----------------------------------------------------------

func setupContainer(t *testing.T, p *Provider, account, container string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut,
		"/"+account+"/"+container+"?restype=container", nil)
	req.Header.Set("x-ms-version", "2023-11-03")
	w := httptest.NewRecorder()
	p.Handle(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("setup container: got %d", w.Code)
	}
}

func TestUploadAndDownloadBlob(t *testing.T) {
	p := newProvider(t)
	setupContainer(t, p, "devstoreaccount1", "invoices")

	payload := []byte("hello, azure-local\n")

	req := httptest.NewRequest(http.MethodPut,
		"/devstoreaccount1/invoices/invoice.txt", bytes.NewReader(payload))
	req.Header.Set("x-ms-blob-type", "BlockBlob")
	req.Header.Set("x-ms-version", "2023-11-03")
	req.Header.Set("Content-Type", "text/plain")
	w := httptest.NewRecorder()
	if !p.Handle(w, req) {
		t.Fatal("upload not claimed")
	}
	if w.Code != http.StatusCreated {
		t.Fatalf("upload: got %d, want 201. body=%s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet,
		"/devstoreaccount1/invoices/invoice.txt", nil)
	w = httptest.NewRecorder()
	p.Handle(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("download: got %d", w.Code)
	}
	if got := w.Body.Bytes(); !bytes.Equal(got, payload) {
		t.Fatalf("body mismatch: got %q want %q", got, payload)
	}
	if got := w.Header().Get("Content-Type"); got != "text/plain" {
		t.Fatalf("content-type: got %q", got)
	}
	if got := w.Header().Get("Content-Length"); got != "19" {
		t.Fatalf("content-length: got %q", got)
	}
}

func TestRangeRequest(t *testing.T) {
	p := newProvider(t)
	setupContainer(t, p, "devstoreaccount1", "range")

	payload := []byte("0123456789")
	req := httptest.NewRequest(http.MethodPut,
		"/devstoreaccount1/range/bytes.txt", bytes.NewReader(payload))
	req.Header.Set("x-ms-blob-type", "BlockBlob")
	w := httptest.NewRecorder()
	p.Handle(w, req)

	cases := []struct {
		header string
		want   string
		code   int
	}{
		{"bytes=0-3", "0123", http.StatusPartialContent},
		{"bytes=5-", "56789", http.StatusPartialContent},
		{"bytes=-3", "789", http.StatusPartialContent},
		{"bytes=0-999", "0123456789", http.StatusPartialContent},
		{"bytes=99-", "", http.StatusRequestedRangeNotSatisfiable},
	}

	for _, tc := range cases {
		req = httptest.NewRequest(http.MethodGet,
			"/devstoreaccount1/range/bytes.txt", nil)
		req.Header.Set("Range", tc.header)
		w = httptest.NewRecorder()
		p.Handle(w, req)
		if w.Code != tc.code {
			t.Fatalf("%s: got %d want %d", tc.header, w.Code, tc.code)
		}
		if tc.code == http.StatusPartialContent && w.Body.String() != tc.want {
			t.Fatalf("%s: got %q want %q", tc.header, w.Body.String(), tc.want)
		}
	}
}

func TestListBlobs(t *testing.T) {
	p := newProvider(t)
	setupContainer(t, p, "devstoreaccount1", "listing")

	for _, name := range []string{"a.txt", "b.txt", "logs/a.log", "logs/b.log"} {
		req := httptest.NewRequest(http.MethodPut,
			"/devstoreaccount1/listing/"+name, strings.NewReader("x"))
		req.Header.Set("x-ms-blob-type", "BlockBlob")
		w := httptest.NewRecorder()
		p.Handle(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("upload %s: %d", name, w.Code)
		}
	}

	req := httptest.NewRequest(http.MethodGet,
		"/devstoreaccount1/listing?restype=container&comp=list", nil)
	w := httptest.NewRecorder()
	p.Handle(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list: %d", w.Code)
	}
	body := w.Body.String()
	for _, want := range []string{"a.txt", "b.txt", "logs/a.log", "logs/b.log"} {
		if !strings.Contains(body, "<Name>"+want+"</Name>") {
			t.Fatalf("missing %q in list:\n%s", want, body)
		}
	}

	req = httptest.NewRequest(http.MethodGet,
		"/devstoreaccount1/listing?restype=container&comp=list&prefix=logs/", nil)
	w = httptest.NewRecorder()
	p.Handle(w, req)
	body = w.Body.String()
	if !strings.Contains(body, "logs/a.log") || strings.Contains(body, "a.txt") {
		t.Fatalf("prefix filter failed:\n%s", body)
	}
}

func TestDeleteBlob(t *testing.T) {
	p := newProvider(t)
	setupContainer(t, p, "devstoreaccount1", "del")

	req := httptest.NewRequest(http.MethodPut,
		"/devstoreaccount1/del/x.txt", strings.NewReader("hi"))
	req.Header.Set("x-ms-blob-type", "BlockBlob")
	w := httptest.NewRecorder()
	p.Handle(w, req)

	req = httptest.NewRequest(http.MethodDelete,
		"/devstoreaccount1/del/x.txt", nil)
	w = httptest.NewRecorder()
	p.Handle(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("delete: %d", w.Code)
	}

	req = httptest.NewRequest(http.MethodGet,
		"/devstoreaccount1/del/x.txt", nil)
	w = httptest.NewRecorder()
	p.Handle(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("get after delete: %d", w.Code)
	}
}

func TestBlockUpload(t *testing.T) {
	p := newProvider(t)
	setupContainer(t, p, "devstoreaccount1", "blocks")

	block1 := base64.StdEncoding.EncodeToString([]byte("block-000001"))
	block2 := base64.StdEncoding.EncodeToString([]byte("block-000002"))

	for id, data := range map[string]string{block1: "hello, ", block2: "world!\n"} {
		req := httptest.NewRequest(http.MethodPut,
			"/devstoreaccount1/blocks/assembled.txt?comp=block&blockid="+url.QueryEscape(id),
			strings.NewReader(data))
		w := httptest.NewRecorder()
		p.Handle(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("put block: %d", w.Code)
		}
	}

	body := "<BlockList><Latest>" + block1 + "</Latest><Latest>" + block2 + "</Latest></BlockList>"
	req := httptest.NewRequest(http.MethodPut,
		"/devstoreaccount1/blocks/assembled.txt?comp=blocklist",
		strings.NewReader(body))
	req.Header.Set("x-ms-blob-content-type", "text/plain")
	w := httptest.NewRecorder()
	p.Handle(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("blocklist: %d body=%s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet,
		"/devstoreaccount1/blocks/assembled.txt", nil)
	w = httptest.NewRecorder()
	p.Handle(w, req)
	if got := w.Body.String(); got != "hello, world!\n" {
		t.Fatalf("assembled: got %q", got)
	}
}
