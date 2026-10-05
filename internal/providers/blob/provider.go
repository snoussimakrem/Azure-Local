package blob

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/azure-local/azure-local/internal/kernel"
)

type Provider struct {
	logger  *slog.Logger
	persist *kernel.PersistenceManager
	bus     *kernel.EventBus
	root    string

	locksMu sync.Mutex
	locks   map[string]*sync.Mutex
}

func New(persist *kernel.PersistenceManager, bus *kernel.EventBus, logger *slog.Logger) (*Provider, error) {
	root, err := persist.ServiceDir("blob")
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(root, "accounts"), 0o755); err != nil {
		return nil, err
	}
	return &Provider{
		logger:  logger,
		persist: persist,
		bus:     bus,
		root:    root,
		locks:   make(map[string]*sync.Mutex),
	}, nil
}

func (p *Provider) Name() string    { return "blob" }
func (p *Provider) Version() string { return "2023-11-03" }

func (p *Provider) Init(context.Context) error  { return nil }
func (p *Provider) Start(context.Context) error { return nil }
func (p *Provider) Stop(context.Context) error  { return nil }

func (p *Provider) Health(context.Context) kernel.HealthStatus { return kernel.Healthy() }

func (p *Provider) blobLock(key string) *sync.Mutex {
	p.locksMu.Lock()
	defer p.locksMu.Unlock()
	m, ok := p.locks[key]
	if !ok {
		m = &sync.Mutex{}
		p.locks[key] = m
	}
	return m
}

func (p *Provider) Handle(w http.ResponseWriter, req *http.Request) bool {
	path := strings.TrimPrefix(req.URL.Path, "/")
	if path == "" {
		return false
	}

	for _, reserved := range []string{"subscriptions/", "providers/", "tenants/", "metadata/"} {
		if strings.HasPrefix(path, reserved) {
			return false
		}
	}
	switch path {
	case "health", "healthz", "metadata", "_azlocal":
		return false
	}

	parts := strings.SplitN(path, "/", 3)
	account := parts[0]
	if !validAccountName(account) {
		return false
	}
	var container, blobName string
	if len(parts) >= 2 {
		container = parts[1]
	}
	if len(parts) >= 3 {
		blobName = parts[2]
	}

	q := req.URL.Query()
	restype := q.Get("restype")
	comp := q.Get("comp")

	// Account-level: list containers.
	if container == "" && comp == "list" && req.Method == http.MethodGet {
		p.listContainers(w, req, account)
		return true
	}

	// Container-level.
	if container != "" && blobName == "" && restype == "container" {
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
			switch comp {
			case "metadata":
				p.getContainerMetadata(w, req, account, container)
				return true
			case "list":
				p.listBlobs(w, req, account, container)
				return true
			case "acl":
				writeBlobError(w, http.StatusNotImplemented, "NotImplemented",
					"ACL operations are not implemented in this build.")
				return true
			}
		}
	}

	// Blob-level.
	if container != "" && blobName != "" {
		switch req.Method {
		case http.MethodPut:
			switch comp {
			case "":
				p.uploadBlob(w, req, account, container, blobName)
				return true
			case "block":
				p.putBlock(w, req, account, container, blobName)
				return true
			case "blocklist":
				p.putBlockList(w, req, account, container, blobName)
				return true
			}
		case http.MethodGet:
			switch comp {
			case "":
				p.downloadBlob(w, req, account, container, blobName)
				return true
			case "blocklist":
				writeBlobError(w, http.StatusNotImplemented, "NotImplemented",
					"Get Block List is not implemented in this build.")
				return true
			}
		case http.MethodHead:
			p.headBlob(w, req, account, container, blobName)
			return true
		case http.MethodDelete:
			p.deleteBlob(w, req, account, container, blobName)
			return true
		}
	}

	return false
}

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
