package blob

import (
	"encoding/xml"
	"time"
)

// BlobMetadata is the on-disk sidecar for each blob.
type BlobMetadata struct {
	Name               string            `json:"name"`
	ContentType        string            `json:"contentType"`
	ContentLength      int64             `json:"contentLength"`
	ETag               string            `json:"etag"`
	Created            time.Time         `json:"created"`
	LastModified       time.Time         `json:"lastModified"`
	ContentMD5         string            `json:"contentMD5,omitempty"` // base64
	ContentEncoding    string            `json:"contentEncoding,omitempty"`
	ContentLanguage    string            `json:"contentLanguage,omitempty"`
	CacheControl       string            `json:"cacheControl,omitempty"`
	ContentDisposition string            `json:"contentDisposition,omitempty"`
	Metadata           map[string]string `json:"metadata,omitempty"`
	BlobType           string            `json:"blobType"` // "BlockBlob"
	AccessTier         string            `json:"accessTier,omitempty"`
}

// XML response shapes for List Blobs --------------------------------------

type ListBlobsResult struct {
	XMLName         xml.Name `xml:"EnumerationResults"`
	ServiceEndpoint string   `xml:"ServiceEndpoint,attr"`
	ContainerName   string   `xml:"ContainerName,attr"`
	Prefix          string   `xml:"Prefix,omitempty"`
	Marker          string   `xml:"Marker,omitempty"`
	MaxResults      int      `xml:"MaxResults,omitempty"`
	Delimiter       string   `xml:"Delimiter,omitempty"`
	Blobs           BlobList `xml:"Blobs"`
	NextMarker      string   `xml:"NextMarker"`
}

type BlobList struct {
	Blobs []BlobInfo `xml:"Blob"`
}

type BlobInfo struct {
	Name       string         `xml:"Name"`
	Properties BlobProperties `xml:"Properties"`
	Metadata   *Metadata      `xml:"Metadata,omitempty"`
}

type BlobProperties struct {
	CreationTime       string `xml:"Creation-Time,omitempty"`
	LastModified       string `xml:"Last-Modified"`
	Etag               string `xml:"Etag"`
	ContentLength      int64  `xml:"Content-Length"`
	ContentType        string `xml:"Content-Type,omitempty"`
	ContentEncoding    string `xml:"Content-Encoding,omitempty"`
	ContentLanguage    string `xml:"Content-Language,omitempty"`
	ContentMD5         string `xml:"Content-MD5,omitempty"`
	CacheControl       string `xml:"Cache-Control,omitempty"`
	ContentDisposition string `xml:"Content-Disposition,omitempty"`
	BlobType           string `xml:"BlobType"`
	AccessTier         string `xml:"AccessTier,omitempty"`
	AccessTierInferred bool   `xml:"AccessTierInferred,omitempty"`
	LeaseStatus        string `xml:"LeaseStatus"`
	LeaseState         string `xml:"LeaseState"`
	ServerEncrypted    bool   `xml:"ServerEncrypted"`
}

// BlockListRequest is the XML body sent by clients when committing blocks.
type BlockListRequest struct {
	XMLName     xml.Name     `xml:"BlockList"`
	Latest      []BlockEntry `xml:"Latest"`
	Committed   []BlockEntry `xml:"Committed"`
	Uncommitted []BlockEntry `xml:"Uncommitted"`
}

type BlockEntry struct {
	Value string `xml:",chardata"`
}
