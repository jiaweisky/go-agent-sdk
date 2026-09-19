package schema

// ProviderID mirrors the branded ProviderV2.ID string.
type ProviderID string

func (id ProviderID) String() string {
	return string(id)
}

// ProviderAISDK mirrors Provider.AISDK.
type ProviderAISDK struct {
	Type     string         `json:"type"` // "aisdk"
	Package  string         `json:"package"`
	URL      *string        `json:"url,omitempty"`
	Settings map[string]any `json:"settings,omitempty"`
}

// ProviderNative mirrors Provider.Native.
type ProviderNative struct {
	Type     string         `json:"type"` // "native"
	URL      *string        `json:"url,omitempty"`
	Settings map[string]any `json:"settings"`
}

// ProviderAPI mirrors Provider.Api (the tagged AISDK | Native union).
type ProviderAPI struct {
	Type     string         `json:"type"`
	Package  string         `json:"package,omitempty"`
	URL      *string        `json:"url,omitempty"`
	Settings map[string]any `json:"settings,omitempty"`
}

// ProviderRequest mirrors Provider.Request.
type ProviderRequest struct {
	Headers map[string]string `json:"headers"`
	Body    map[string]any    `json:"body"`
}

// ProviderInfo mirrors ProviderV2.Info.
type ProviderInfo struct {
	ID            ProviderID      `json:"id"`
	IntegrationID *string         `json:"integrationID,omitempty"`
	Name          string          `json:"name"`
	Disabled      *bool           `json:"disabled,omitempty"`
	API           ProviderAPI     `json:"api"`
	Request       ProviderRequest `json:"request"`
}

// ProviderInfoEmpty mirrors Provider.Info.empty.
func ProviderInfoEmpty(id ProviderID) ProviderInfo {
	name := string(id)
	return ProviderInfo{
		ID:      id,
		Name:    name,
		API:     ProviderAPI{Type: "native", Settings: map[string]any{}},
		Request: ProviderRequest{Headers: map[string]string{}, Body: map[string]any{}},
	}
}
