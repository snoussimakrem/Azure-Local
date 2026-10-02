package blob

import (
	"encoding/xml"
	"time"
)

// StoredContainer is the on-disk JSON representation.
type StoredContainer struct {
	Name         string            `json:"name"`
	Created      time.Time         `json:"created"`
	ETag         string            `json:"etag"`
	Metadata     map[string]string `json:"metadata,omitempty"`
	PublicAccess string            `json:"publicAccess,omitempty"`
}

// XML response shapes ------------------------------------------------------

type EnumerationResults struct {
	XMLName         xml.Name      `xml:"EnumerationResults"`
	ServiceEndpoint string        `xml:"ServiceEndpoint,attr"`
	AccountName     string        `xml:"AccountName,attr,omitempty"`
	Prefix          string        `xml:"Prefix,omitempty"`
	Marker          string        `xml:"Marker,omitempty"`
	MaxResults      int           `xml:"MaxResults,omitempty"`
	Containers      ContainerList `xml:"Containers"`
	NextMarker      string        `xml:"NextMarker"`
}

type ContainerList struct {
	Containers []ContainerInfo `xml:"Container"`
}

type ContainerInfo struct {
	Name       string              `xml:"Name"`
	Properties ContainerProperties `xml:"Properties"`
	Metadata   *Metadata           `xml:"Metadata,omitempty"`
}

type ContainerProperties struct {
	LastModified          string `xml:"Last-Modified"`
	Etag                  string `xml:"Etag"`
	LeaseStatus           string `xml:"LeaseStatus"`
	LeaseState            string `xml:"LeaseState"`
	HasImmutabilityPolicy bool   `xml:"HasImmutabilityPolicy"`
	HasLegalHold          bool   `xml:"HasLegalHold"`
}

// Metadata marshals as <Metadata><key>value</key>...</Metadata>.
type Metadata map[string]string

func (m Metadata) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
	start.Name = xml.Name{Local: "Metadata"}
	if err := e.EncodeToken(start); err != nil {
		return err
	}
	for k, v := range m {
		if err := e.EncodeElement(v, xml.StartElement{Name: xml.Name{Local: k}}); err != nil {
			return err
		}
	}
	return e.EncodeToken(start.End())
}
