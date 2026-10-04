package blob

import (
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/xml"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/azure-local/azure-local/internal/kernel"
)

func (p *Provider) blockStagingDir(account, container, blob string) string {
	return filepath.Join(p.containerDir(account, container), ".blobs", "blocks", filepath.FromSlash(blob))
}

// PUT /{account}/{container}/{blob}?comp=block&blockid={base64-id}
func (p *Provider) putBlock(w http.ResponseWriter, req *http.Request, account, container, blob string) {
	if err := validateBlobName(blob); err != nil {
		writeBlobError(w, http.StatusBadRequest, "InvalidBlobName", err.Error())
		return
	}
	if _, ok := p.readContainerMeta(account, container); !ok {
		writeBlobError(w, http.StatusNotFound, "ContainerNotFound",
			"The specified container does not exist.")
		return
	}

	blockID := req.URL.Query().Get("blockid")
	if blockID == "" {
		writeBlobError(w, http.StatusBadRequest, "InvalidQueryParameterValue",
			"blockid is required.")
		return
	}
	idBytes, err := base64.StdEncoding.DecodeString(blockID)
	if err != nil {
		writeBlobError(w, http.StatusBadRequest, "InvalidQueryParameterValue",
			"blockid must be base64-encoded.")
		return
	}

	dir := p.blockStagingDir(account, container, blob)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		writeBlobError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	blockFile := filepath.Join(dir, hex.EncodeToString(idBytes))
	tmp := blockFile + ".tmp"

	f, err := os.Create(tmp)
	if err != nil {
		writeBlobError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if _, err := io.Copy(f, req.Body); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		writeBlobError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if err := f.Close(); err != nil {
		writeBlobError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if err := os.Rename(tmp, blockFile); err != nil {
		writeBlobError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	writeBlobSuccessHeaders(w, "", time.Time{})
	w.WriteHeader(http.StatusCreated)
}

// PUT /{account}/{container}/{blob}?comp=blocklist
func (p *Provider) putBlockList(w http.ResponseWriter, req *http.Request, account, container, blob string) {
	if err := validateBlobName(blob); err != nil {
		writeBlobError(w, http.StatusBadRequest, "InvalidBlobName", err.Error())
		return
	}
	if _, ok := p.readContainerMeta(account, container); !ok {
		writeBlobError(w, http.StatusNotFound, "ContainerNotFound",
			"The specified container does not exist.")
		return
	}

	lock := p.blobLock(account + "/" + container + "/" + blob)
	lock.Lock()
	defer lock.Unlock()

	body, err := io.ReadAll(req.Body)
	if err != nil {
		writeBlobError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	var list BlockListRequest
	if err := xml.Unmarshal(body, &list); err != nil {
		writeBlobError(w, http.StatusBadRequest, "InvalidXmlDocument",
			"The specified XML is not well-formed.")
		return
	}

	blocks := append([]BlockEntry{}, list.Latest...)
	blocks = append(blocks, list.Uncommitted...)

	blockDir := p.blockStagingDir(account, container, blob)

	dataPath := p.blobDataPath(account, container, blob)
	if err := os.MkdirAll(filepath.Dir(dataPath), 0o755); err != nil {
		writeBlobError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	tmp, err := os.CreateTemp(filepath.Dir(dataPath), ".commit-*")
	if err != nil {
		writeBlobError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	hasher := md5.New()
	writer := io.MultiWriter(tmp, hasher)

	var total int64
	for _, be := range blocks {
		idBytes, err := base64.StdEncoding.DecodeString(be.Value)
		if err != nil {
			_ = tmp.Close()
			writeBlobError(w, http.StatusBadRequest, "InvalidBlockList",
				"Block ID is not valid base64.")
			return
		}
		blockFile := filepath.Join(blockDir, hex.EncodeToString(idBytes))
		bf, err := os.Open(blockFile)
		if err != nil {
			_ = tmp.Close()
			writeBlobError(w, http.StatusBadRequest, "InvalidBlockList",
				"The specified block list is invalid.")
			return
		}
		n, copyErr := io.Copy(writer, bf)
		_ = bf.Close()
		if copyErr != nil {
			_ = tmp.Close()
			writeBlobError(w, http.StatusInternalServerError, "InternalError", copyErr.Error())
			return
		}
		total += n
	}
	if err := tmp.Close(); err != nil {
		writeBlobError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	md5Base64 := base64.StdEncoding.EncodeToString(hasher.Sum(nil))
	now := time.Now().UTC()

	existing, exists := p.readBlobMeta(account, container, blob)
	meta := BlobMetadata{
		Name:               blob,
		ContentType:        firstNonEmpty(req.Header.Get("x-ms-blob-content-type"), "application/octet-stream"),
		ContentLength:      total,
		ETag:               newETag(),
		Created:            now,
		LastModified:       now,
		ContentMD5:         md5Base64,
		ContentEncoding:    req.Header.Get("x-ms-blob-content-encoding"),
		ContentLanguage:    req.Header.Get("x-ms-blob-content-language"),
		CacheControl:       req.Header.Get("x-ms-blob-cache-control"),
		ContentDisposition: req.Header.Get("x-ms-blob-content-disposition"),
		Metadata:           extractMetadata(req),
		BlobType:           "BlockBlob",
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

	_ = os.RemoveAll(blockDir)

	writeBlobSuccessHeaders(w, meta.ETag, meta.LastModified)
	w.WriteHeader(http.StatusCreated)

	p.bus.Publish(kernel.Event{
		Type: "BlobCreated",
		Data: map[string]any{
			"account":   account,
			"container": container,
			"blob":      blob,
			"size":      total,
		},
	})
}
