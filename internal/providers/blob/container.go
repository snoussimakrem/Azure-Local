package blob

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/azure-local/azure-local/internal/kernel"
)

func (p *Provider) accountDir(account string) string {
	return filepath.Join(p.root, "accounts", account, "containers")
}

func (p *Provider) containerDir(account, container string) string {
	return filepath.Join(p.accountDir(account), container)
}

func (p *Provider) metaPath(account, container string) string {
	return filepath.Join(p.containerDir(account, container), ".metadata.json")
}

// ---- create ---------------------------------------------------------------

func (p *Provider) createContainer(w http.ResponseWriter, req *http.Request, account, container string) {
	if err := validateContainerName(container); err != nil {
		writeBlobError(w, http.StatusBadRequest, "InvalidResourceName", err.Error())
		return
	}

	parent := p.accountDir(account)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		writeBlobError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	dir := p.containerDir(account, container)
	// Mkdir (not MkdirAll) is atomic: it fails if the dir already exists.
	if err := os.Mkdir(dir, 0o755); err != nil {
		if os.IsExist(err) {
			writeBlobError(w, http.StatusConflict, "ContainerAlreadyExists",
				"The specified container already exists.")
			return
		}
		writeBlobError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	meta := StoredContainer{
		Name:         container,
		Created:      time.Now().UTC(),
		ETag:         newETag(),
		Metadata:     extractMetadata(req),
		PublicAccess: req.Header.Get("x-ms-blob-public-access"),
	}
	if err := writeJSON(p.metaPath(account, container), meta); err != nil {
		_ = os.RemoveAll(dir)
		writeBlobError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	writeBlobSuccessHeaders(w, meta.ETag, meta.Created)
	w.WriteHeader(http.StatusCreated)

	p.bus.Publish(kernel.Event{
		Type: "ResourceCreated",
		Data: map[string]any{
			"provider":  "Microsoft.Storage",
			"type":      "containers",
			"account":   account,
			"name":      container,
			"container": container,
		},
	})
}

// ---- list -----------------------------------------------------------------

func (p *Provider) listContainers(w http.ResponseWriter, req *http.Request, account string) {
	q := req.URL.Query()
	prefix := q.Get("prefix")
	includeMetadata := strings.Contains(q.Get("include"), "metadata")

	entries, err := os.ReadDir(p.accountDir(account))
	if err != nil && !os.IsNotExist(err) {
		writeBlobError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	var infos []ContainerInfo
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if prefix != "" && !strings.HasPrefix(name, prefix) {
			continue
		}
		raw, err := os.ReadFile(p.metaPath(account, name))
		if err != nil {
			continue
		}
		var m StoredContainer
		if err := json.Unmarshal(raw, &m); err != nil {
			continue
		}
		info := ContainerInfo{
			Name: name,
			Properties: ContainerProperties{
				LastModified:          m.Created.UTC().Format(http.TimeFormat),
				Etag:                  m.ETag,
				LeaseStatus:           "unlocked",
				LeaseState:            "available",
				HasImmutabilityPolicy: false,
				HasLegalHold:          false,
			},
		}
		if includeMetadata && len(m.Metadata) > 0 {
			md := Metadata(m.Metadata)
			info.Metadata = &md
		}
		infos = append(infos, info)
	}

	sort.Slice(infos, func(i, j int) bool { return infos[i].Name < infos[j].Name })

	resp := EnumerationResults{
		ServiceEndpoint: fmt.Sprintf("http://%s/%s/", req.Host, account),
		AccountName:     account,
		Prefix:          prefix,
		Containers:      ContainerList{Containers: infos},
		NextMarker:      "",
	}

	body, err := xml.MarshalIndent(resp, "", "  ")
	if err != nil {
		writeBlobError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/xml")
	writeBlobSuccessHeaders(w, "", time.Time{})
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(xml.Header))
	_, _ = w.Write(body)
}

// ---- delete ---------------------------------------------------------------

func (p *Provider) deleteContainer(w http.ResponseWriter, req *http.Request, account, container string) {
	dir := p.containerDir(account, container)
	if _, err := os.Stat(dir); err != nil {
		if os.IsNotExist(err) {
			writeBlobError(w, http.StatusNotFound, "ContainerNotFound",
				"The specified container does not exist.")
			return
		}
		writeBlobError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if err := os.RemoveAll(dir); err != nil {
		writeBlobError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	writeBlobSuccessHeaders(w, "", time.Time{})
	w.WriteHeader(http.StatusAccepted)

	p.bus.Publish(kernel.Event{
		Type: "ResourceDeleted",
		Data: map[string]any{
			"provider":  "Microsoft.Storage",
			"type":      "containers",
			"account":   account,
			"container": container,
		},
	})
}

// ---- head -----------------------------------------------------------------

func (p *Provider) headContainer(w http.ResponseWriter, req *http.Request, account, container string) {
	m, ok := p.readContainerMeta(account, container)
	if !ok {
		writeBlobError(w, http.StatusNotFound, "ContainerNotFound",
			"The specified container does not exist.")
		return
	}
	writeBlobSuccessHeaders(w, m.ETag, m.Created)
	w.Header().Set("x-ms-lease-status", "unlocked")
	w.Header().Set("x-ms-lease-state", "available")
	w.Header().Set("x-ms-has-immutability-policy", "false")
	w.Header().Set("x-ms-has-legal-hold", "false")
	for k, v := range m.Metadata {
		w.Header().Set("x-ms-meta-"+k, v)
	}
	w.WriteHeader(http.StatusOK)
}

// ---- get metadata ---------------------------------------------------------

func (p *Provider) getContainerMetadata(w http.ResponseWriter, req *http.Request, account, container string) {
	m, ok := p.readContainerMeta(account, container)
	if !ok {
		writeBlobError(w, http.StatusNotFound, "ContainerNotFound",
			"The specified container does not exist.")
		return
	}
	writeBlobSuccessHeaders(w, m.ETag, m.Created)
	for k, v := range m.Metadata {
		w.Header().Set("x-ms-meta-"+k, v)
	}
	w.WriteHeader(http.StatusOK)
}

// ---- helpers --------------------------------------------------------------

func (p *Provider) readContainerMeta(account, container string) (StoredContainer, bool) {
	raw, err := os.ReadFile(p.metaPath(account, container))
	if err != nil {
		return StoredContainer{}, false
	}
	var m StoredContainer
	if err := json.Unmarshal(raw, &m); err != nil {
		return StoredContainer{}, false
	}
	return m, true
}

func extractMetadata(req *http.Request) map[string]string {
	out := map[string]string{}
	for k, vals := range req.Header {
		lower := strings.ToLower(k)
		if strings.HasPrefix(lower, "x-ms-meta-") && len(vals) > 0 {
			out[strings.TrimPrefix(lower, "x-ms-meta-")] = vals[0]
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func writeJSON(path string, v any) error {
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

func validateContainerName(name string) error {
	if len(name) < 3 || len(name) > 63 {
		return fmt.Errorf("container name must be 3-63 characters")
	}
	if name[0] == '-' || name[len(name)-1] == '-' {
		return fmt.Errorf("container name cannot begin or end with a hyphen")
	}
	prevHyphen := false
	for _, c := range name {
		isLower := c >= 'a' && c <= 'z'
		isDigit := c >= '0' && c <= '9'
		isHyphen := c == '-'
		if !isLower && !isDigit && !isHyphen {
			return fmt.Errorf("container name may only contain lowercase letters, numbers, and hyphens")
		}
		if isHyphen && prevHyphen {
			return fmt.Errorf("container name cannot contain consecutive hyphens")
		}
		prevHyphen = isHyphen
	}
	return nil
}
