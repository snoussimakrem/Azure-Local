package blob

import (
	"crypto/md5"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/azure-local/azure-local/internal/kernel"
)

// ---- paths ----------------------------------------------------------------

func (p *Provider) blobDataPath(account, container, blob string) string {
	return filepath.Join(p.containerDir(account, container), ".blobs", "data", filepath.FromSlash(blob))
}

func (p *Provider) blobMetaPath(account, container, blob string) string {
	return filepath.Join(p.containerDir(account, container), ".blobs", "meta", filepath.FromSlash(blob)+".json")
}

// ---- upload (single PUT) --------------------------------------------------

func (p *Provider) uploadBlob(w http.ResponseWriter, req *http.Request, account, container, blob string) {
	if err := validateBlobName(blob); err != nil {
		writeBlobError(w, http.StatusBadRequest, "InvalidBlobName", err.Error())
		return
	}
	if _, ok := p.readContainerMeta(account, container); !ok {
		writeBlobError(w, http.StatusNotFound, "ContainerNotFound",
			"The specified container does not exist.")
		return
	}

	blobType := req.Header.Get("x-ms-blob-type")
	if blobType == "" {
		blobType = "BlockBlob"
	}
	if blobType != "BlockBlob" {
		writeBlobError(w, http.StatusNotImplemented, "NotImplemented",
			"Only BlockBlob is implemented in this build.")
		return
	}

	lock := p.blobLock(account + "/" + container + "/" + blob)
	lock.Lock()
	defer lock.Unlock()

	existing, exists := p.readBlobMeta(account, container, blob)
	if req.Header.Get("If-None-Match") == "*" && exists {
		writeBlobError(w, http.StatusPreconditionFailed, "ConditionNotMet",
			"The condition specified using HTTP conditional header(s) is not met.")
		return
	}
	if match := req.Header.Get("If-Match"); match != "" && match != "*" {
		if !exists || match != existing.ETag {
			writeBlobError(w, http.StatusPreconditionFailed, "ConditionNotMet",
				"The condition specified using HTTP conditional header(s) is not met.")
			return
		}
	}

	dataPath := p.blobDataPath(account, container, blob)
	if err := os.MkdirAll(filepath.Dir(dataPath), 0o755); err != nil {
		writeBlobError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	tmp, err := os.CreateTemp(filepath.Dir(dataPath), ".upload-*")
	if err != nil {
		writeBlobError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	hasher := md5.New()
	writer := io.MultiWriter(tmp, hasher)

	size, err := io.Copy(writer, req.Body)
	if err != nil {
		_ = tmp.Close()
		writeBlobError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if err := tmp.Close(); err != nil {
		writeBlobError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	md5Base64 := base64.StdEncoding.EncodeToString(hasher.Sum(nil))

	if clientMD5 := req.Header.Get("Content-MD5"); clientMD5 != "" && clientMD5 != md5Base64 {
		writeBlobError(w, http.StatusBadRequest, "Md5Mismatch",
			"The MD5 value specified in the request did not match with the MD5 value calculated by the server.")
		return
	}

	now := time.Now().UTC()
	meta := BlobMetadata{
		Name:               blob,
		ContentType:        firstNonEmpty(req.Header.Get("x-ms-blob-content-type"), req.Header.Get("Content-Type"), "application/octet-stream"),
		ContentLength:      size,
		ETag:               newETag(),
		Created:            now,
		LastModified:       now,
		ContentMD5:         md5Base64,
		ContentEncoding:    firstNonEmpty(req.Header.Get("x-ms-blob-content-encoding"), req.Header.Get("Content-Encoding")),
		ContentLanguage:    firstNonEmpty(req.Header.Get("x-ms-blob-content-language"), req.Header.Get("Content-Language")),
		CacheControl:       firstNonEmpty(req.Header.Get("x-ms-blob-cache-control"), req.Header.Get("Cache-Control")),
		ContentDisposition: firstNonEmpty(req.Header.Get("x-ms-blob-content-disposition"), req.Header.Get("Content-Disposition")),
		Metadata:           extractMetadata(req),
		BlobType:           blobType,
		AccessTier:         "Hot",
	}
	if exists {
		meta.Created = existing.Created
	}

	if err := os.Rename(tmpPath, dataPath); err != nil {
		writeBlobError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if err := writeJSON(p.blobMetaPath(account, container, blob), meta); err != nil {
		writeBlobError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	writeBlobSuccessHeaders(w, meta.ETag, meta.LastModified)
	w.Header().Set("Content-MD5", meta.ContentMD5)
	w.WriteHeader(http.StatusCreated)

	p.bus.Publish(kernel.Event{
		Type: "BlobCreated",
		Data: map[string]any{
			"account":   account,
			"container": container,
			"blob":      blob,
			"size":      size,
		},
	})
}

// ---- download -------------------------------------------------------------

func (p *Provider) downloadBlob(w http.ResponseWriter, req *http.Request, account, container, blob string) {
	meta, ok := p.readBlobMeta(account, container, blob)
	if !ok {
		writeBlobError(w, http.StatusNotFound, "BlobNotFound",
			"The specified blob does not exist.")
		return
	}
	if !p.checkReadConditions(w, req, meta) {
		return
	}

	f, err := os.Open(p.blobDataPath(account, container, blob))
	if err != nil {
		writeBlobError(w, http.StatusNotFound, "BlobNotFound",
			"The specified blob does not exist.")
		return
	}
	defer f.Close()

	setBlobHeaders(w, meta)

	if rng := req.Header.Get("Range"); rng != "" {
		start, end, ok := parseRange(rng, meta.ContentLength)
		if !ok {
			w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", meta.ContentLength))
			writeBlobError(w, http.StatusRequestedRangeNotSatisfiable, "InvalidRange",
				"The range specified is invalid for the current size of the resource.")
			return
		}
		length := end - start + 1
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, meta.ContentLength))
		w.Header().Set("Content-Length", strconv.FormatInt(length, 10))
		w.WriteHeader(http.StatusPartialContent)
		if _, err := f.Seek(start, io.SeekStart); err != nil {
			return
		}
		_, _ = io.CopyN(w, f, length)
		return
	}

	w.Header().Set("Content-Length", strconv.FormatInt(meta.ContentLength, 10))
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, f)
}

// ---- head -----------------------------------------------------------------

func (p *Provider) headBlob(w http.ResponseWriter, req *http.Request, account, container, blob string) {
	meta, ok := p.readBlobMeta(account, container, blob)
	if !ok {
		writeBlobError(w, http.StatusNotFound, "BlobNotFound",
			"The specified blob does not exist.")
		return
	}
	if !p.checkReadConditions(w, req, meta) {
		return
	}
	setBlobHeaders(w, meta)
	w.Header().Set("Content-Length", strconv.FormatInt(meta.ContentLength, 10))
	w.WriteHeader(http.StatusOK)
}

// ---- delete ---------------------------------------------------------------

func (p *Provider) deleteBlob(w http.ResponseWriter, req *http.Request, account, container, blob string) {
	lock := p.blobLock(account + "/" + container + "/" + blob)
	lock.Lock()
	defer lock.Unlock()

	meta, ok := p.readBlobMeta(account, container, blob)
	if !ok {
		writeBlobError(w, http.StatusNotFound, "BlobNotFound",
			"The specified blob does not exist.")
		return
	}
	if match := req.Header.Get("If-Match"); match != "" && match != "*" && match != meta.ETag {
		writeBlobError(w, http.StatusPreconditionFailed, "ConditionNotMet",
			"The condition specified using HTTP conditional header(s) is not met.")
		return
	}

	_ = os.Remove(p.blobDataPath(account, container, blob))
	_ = os.Remove(p.blobMetaPath(account, container, blob))

	writeBlobSuccessHeaders(w, "", time.Time{})
	w.WriteHeader(http.StatusAccepted)

	p.bus.Publish(kernel.Event{
		Type: "BlobDeleted",
		Data: map[string]any{
			"account":   account,
			"container": container,
			"blob":      blob,
		},
	})
}

// ---- list blobs -----------------------------------------------------------

func (p *Provider) listBlobs(w http.ResponseWriter, req *http.Request, account, container string) {
	if _, ok := p.readContainerMeta(account, container); !ok {
		writeBlobError(w, http.StatusNotFound, "ContainerNotFound",
			"The specified container does not exist.")
		return
	}

	q := req.URL.Query()
	prefix := q.Get("prefix")
	includeMetadata := strings.Contains(q.Get("include"), "metadata")

	dataRoot := filepath.Join(p.containerDir(account, container), ".blobs", "data")

	var infos []BlobInfo
	walkErr := filepath.Walk(dataRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dataRoot, path)
		if err != nil {
			return nil
		}
		blobName := filepath.ToSlash(rel)
		if prefix != "" && !strings.HasPrefix(blobName, prefix) {
			return nil
		}
		meta, ok := p.readBlobMeta(account, container, blobName)
		if !ok {
			return nil
		}
		bInfo := BlobInfo{
			Name: blobName,
			Properties: BlobProperties{
				CreationTime:       meta.Created.UTC().Format(http.TimeFormat),
				LastModified:       meta.LastModified.UTC().Format(http.TimeFormat),
				Etag:               meta.ETag,
				ContentLength:      meta.ContentLength,
				ContentType:        meta.ContentType,
				ContentEncoding:    meta.ContentEncoding,
				ContentLanguage:    meta.ContentLanguage,
				ContentMD5:         meta.ContentMD5,
				CacheControl:       meta.CacheControl,
				ContentDisposition: meta.ContentDisposition,
				BlobType:           meta.BlobType,
				AccessTier:         meta.AccessTier,
				LeaseStatus:        "unlocked",
				LeaseState:         "available",
				ServerEncrypted:    true,
			},
		}
		if includeMetadata && len(meta.Metadata) > 0 {
			md := Metadata(meta.Metadata)
			bInfo.Metadata = &md
		}
		infos = append(infos, bInfo)
		return nil
	})
	if walkErr != nil && !os.IsNotExist(walkErr) {
		writeBlobError(w, http.StatusInternalServerError, "InternalError", walkErr.Error())
		return
	}

	sort.Slice(infos, func(i, j int) bool { return infos[i].Name < infos[j].Name })

	resp := ListBlobsResult{
		ServiceEndpoint: fmt.Sprintf("http://%s/%s/", req.Host, account),
		ContainerName:   container,
		Prefix:          prefix,
		Blobs:           BlobList{Blobs: infos},
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

// ---- helpers --------------------------------------------------------------

func (p *Provider) readBlobMeta(account, container, blob string) (BlobMetadata, bool) {
	raw, err := os.ReadFile(p.blobMetaPath(account, container, blob))
	if err != nil {
		return BlobMetadata{}, false
	}
	var m BlobMetadata
	if err := json.Unmarshal(raw, &m); err != nil {
		return BlobMetadata{}, false
	}
	return m, true
}

func (p *Provider) checkReadConditions(w http.ResponseWriter, req *http.Request, meta BlobMetadata) bool {
	if match := req.Header.Get("If-Match"); match != "" && match != "*" && match != meta.ETag {
		writeBlobError(w, http.StatusPreconditionFailed, "ConditionNotMet",
			"The condition specified using HTTP conditional header(s) is not met.")
		return false
	}
	if none := req.Header.Get("If-None-Match"); none != "" {
		if none == "*" || none == meta.ETag {
			w.WriteHeader(http.StatusNotModified)
			return false
		}
	}
	return true
}

func setBlobHeaders(w http.ResponseWriter, m BlobMetadata) {
	writeBlobSuccessHeaders(w, m.ETag, m.LastModified)
	w.Header().Set("x-ms-blob-type", m.BlobType)
	w.Header().Set("x-ms-creation-time", m.Created.UTC().Format(http.TimeFormat))
	w.Header().Set("x-ms-lease-status", "unlocked")
	w.Header().Set("x-ms-lease-state", "available")
	w.Header().Set("x-ms-server-encrypted", "true")
	w.Header().Set("Accept-Ranges", "bytes")
	if m.ContentType != "" {
		w.Header().Set("Content-Type", m.ContentType)
	}
	if m.ContentEncoding != "" {
		w.Header().Set("Content-Encoding", m.ContentEncoding)
	}
	if m.ContentLanguage != "" {
		w.Header().Set("Content-Language", m.ContentLanguage)
	}
	if m.CacheControl != "" {
		w.Header().Set("Cache-Control", m.CacheControl)
	}
	if m.ContentDisposition != "" {
		w.Header().Set("Content-Disposition", m.ContentDisposition)
	}
	if m.ContentMD5 != "" {
		w.Header().Set("Content-MD5", m.ContentMD5)
	}
	if m.AccessTier != "" {
		w.Header().Set("x-ms-access-tier", m.AccessTier)
	}
	for k, v := range m.Metadata {
		w.Header().Set("x-ms-meta-"+k, v)
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func validateBlobName(name string) error {
	if name == "" {
		return fmt.Errorf("blob name must not be empty")
	}
	if len(name) > 1024 {
		return fmt.Errorf("blob name must be 1-1024 characters")
	}
	for _, part := range strings.Split(name, "/") {
		if part == "" || part == "." || part == ".." {
			return fmt.Errorf("blob name contains invalid path segment %q", part)
		}
	}
	return nil
}
