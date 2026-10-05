package arm

import (
	"net/http"
	"strings"
)

// handleMetadata implements the Terraform azurerm provider's `metadata_host`
// contract: GET /metadata/endpoints?api-version=2022-09-01
//
// NOTE: The azurerm provider currently hardcodes https:// for the metadata
// request. Our server is HTTP-only through Milestone 4; TLS lands later.
// This endpoint exists so the contract shape is in place.
func (p *Provider) handleMetadata(w http.ResponseWriter, req *http.Request) {
	base := "http://" + req.Host
	if strings.HasPrefix(req.Host, "localhost") || strings.HasPrefix(req.Host, "127.0.0.1") {
		base = "http://" + req.Host
	}

	resp := map[string]any{
		"name":                     "AzureLocal",
		"resourceManager":          base + "/",
		"portal":                   base + "/",
		"graph":                    base + "/",
		"graphAudience":            base + "/",
		"gallery":                  base + "/",
		"datalake":                 "",
		"batch":                    "",
		"media":                    "",
		"sqlManagement":            base + "/",
		"microsoftGraphResourceId": base + "/",
		"vmImageAliasDoc":          "",
		"authentication": map[string]any{
			"loginEndpoint":    base + "/",
			"audiences":        []string{base + "/"},
			"tenant":           DefaultTenantID,
			"identityProvider": "AAD",
		},
		"suffixes": map[string]string{
			"storage":                              "localhost",
			"keyvaultDns":                          ".localhost",
			"sqlServerHostname":                    ".localhost",
			"azureFrontDoorEndpointSuffix":         "localhost",
			"azureDatalakeAnalyticsCatalogAndJob":  "localhost",
			"azureDatalakeStoreFileSystem":         "localhost",
		},
	}
	writeARMJSON(w, http.StatusOK, resp)
}
