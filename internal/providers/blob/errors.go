package blob

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/xml"
	"net/http"
	"time"
)

type BlobError struct {
	XMLName xml.Name `xml:"Error"`
	Code    string   `xml:"Code"`
	Message string   `xml:"Message"`
}

func writeBlobError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/xml")
	w.Header().Set("x-ms-request-id", newRequestID())
	w.Header().Set("x-ms-version", "2023-11-03")
	w.Header().Set("Date", time.Now().UTC().Format(http.TimeFormat))
	w.WriteHeader(status)
	_, _ = w.Write([]byte(xml.Header))
	body, _ := xml.Marshal(BlobError{Code: code, Message: message})
	_, _ = w.Write(body)
}

func writeBlobSuccessHeaders(w http.ResponseWriter, etag string, lastModified time.Time) {
	w.Header().Set("x-ms-request-id", newRequestID())
	w.Header().Set("x-ms-version", "2023-11-03")
	w.Header().Set("Date", time.Now().UTC().Format(http.TimeFormat))
	if etag != "" {
		w.Header().Set("ETag", etag)
	}
	if !lastModified.IsZero() {
		w.Header().Set("Last-Modified", lastModified.UTC().Format(http.TimeFormat))
	}
}

func newRequestID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func newETag() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return `"0x8D` + hex.EncodeToString(b[:])[:14] + `"`
}
