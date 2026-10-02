package blob

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/azure-local/azure-local/internal/kernel"
)

// Provider implements the Azure Blob Storage data plane for the subset of
// container operations in Milestone 1. Path shape:
//
//	/{account}/                   — account-level (list containers)
//	/{account}/{container}        — container-level
//	/{account}/{container}/{blob} — blob-level (not yet implemented)
type Provider struct {
	logger  *slog.Logger
	persist *kernel.PersistenceManager
	bus     *kernel.EventBus
	root    string
}

func New(persist *kernel.PersistenceManager, bus *kernel.EventBus, logger *slog.Logger) (*Provider, error) {
	root, err := persist.ServiceDir("blob")
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(root, "accounts"), 0o755); err != nil {
		return nil, err
	}
	return &Provider{logger: logger, persist: persist, bus: bus, root: root}, nil
}

func (p *Provider) Name() string    { return "blob" }
func (p *Provider) Version() string { return "2023-11-03" }

func (p *Provider) Init(context.Context) error  { return nil }
func (p *Provider) Start(context.Context) error { return nil }
func (p *Provider) Stop(context.Context) error  { return nil }

func (p *Provider) Health(context.Context) kernel.HealthStatus { return kernel.Healthy() }

// Handle claims only paths that look like Blob data-plane paths. It returns
// false for everything else, including all future ARM paths, so the gateway
// can route elsewhere without us ever inventing a fake 200.
func (p *Provider) Handle(w http.ResponseWriter, req *http.Request) bool {
	path := strings.TrimPrefix(req.URL.Path, "/")
	if path == "" {
		return false
	}

	// Reserve control-plane prefixes for later milestones.
	for _, reserved := range []string{"subscriptions/", "providers/", "tenants/"} {
		if strings.HasPrefix(path, reserved) {
			return false
		}
	}
	switch path {
	case "health", "healthz", "metadata", "_azlocal":
		return false
	}

	parts := strings.Split(path, "/")
	account := parts[0]
	if !validAccountName(account) {
		return false
	}

	var container string
	if len(parts) >= 2 {
		container = parts[1]
	}
	if len(parts) > 2 {
		// Blob-level ops are not yet implemented. Return 501 honestly
		// instead of pretending the object exists.
		if container != "" && parts[2] != "" {
			writeBlobError(w, http.StatusNotImplemented, "NotImplemented",
				"Blob-level operations are not implemented in this build.")
			return true
		}
	}

	q := req.URL.Query()
	restype := q.Get("restype")
	comp := q.Get("comp")

	// Account-level: GET /{account}/?comp=list
	if container == "" && comp == "list" && req.Method == http.MethodGet {
		p.listContainers(w, req, account)
		return true
	}

	// Container-level ops require restype=container.
	if container != "" && restype == "container" {
		switch req.Method {
		case http.MethodPut:
			p.createContainer(w, req, account, container)
			return true
		case http.MethodDelete:
			p.deleteContainer(w, req, account, container)
			return true
		case http.MethodHead:
			p.headContainer(w, req, account, container)
			return true
		case http.MethodGet:
			if comp == "metadata" {
				p.getContainerMetadata(w, req, account, container)
				return true
			}
			if comp == "acl" {
				writeBlobError(w, http.StatusNotImplemented, "NotImplemented",
					"ACL operations are not implemented in this build.")
				return true
			}
		}
	}

	return false
}

// validAccountName matches Azure storage account naming for the data plane.
func validAccountName(name string) bool {
	if len(name) < 3 || len(name) > 24 {
		return false
	}
	for _, c := range name {
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9')) {
			return false
		}
	}
	return true
}
