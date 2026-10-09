package schema

// ProviderID identifies a provider.
type ProviderID string

func (id ProviderID) String() string {
	return string(id)
}

// ProviderAISDK is a provider served through the AI SDK.
type ProviderAISDK struct {
	Type     string         `json:"type"` // "aisdk"
	Package  string         `json:"package"`
	URL      *string        `json:"url,omitempty"`
	Settings map[string]any `json:"settings,omitempty"`
}

// ProviderNative is a provider the SDK talks to directly.
type ProviderNative struct {
	Type     string         `json:"type"` // "native"
	URL      *string        `json:"url,omitempty"`
	Settings map[string]any `json:"settings"`
}

// ProviderAPI is how a provider is reached: through the AI SDK or natively.
type ProviderAPI struct {
	Type     string         `json:"type"`
	Package  string         `json:"package,omitempty"`
	URL      *string        `json:"url,omitempty"`
	Settings map[string]any `json:"settings,omitempty"`
}

// ProviderRequest is the headers and body a provider's requests carry.
type ProviderRequest struct {
	Headers map[string]string `json:"headers"`
	Body    map[string]any    `json:"body"`
}

// ProviderInfo is a provider's catalog metadata.
type ProviderInfo struct {
	ID            ProviderID      `json:"id"`
	IntegrationID *string         `json:"integrationID,omitempty"`
	Name          string          `json:"name"`
	Disabled      *bool           `json:"disabled,omitempty"`
	API           ProviderAPI     `json:"api"`
	Request       ProviderRequest `json:"request"`
}

// ProviderInfoEmpty is an empty provider info.
func ProviderInfoEmpty(id ProviderID) ProviderInfo {
	name := string(id)
	return ProviderInfo{
		ID:      id,
		Name:    name,
		API:     ProviderAPI{Type: "native", Settings: map[string]any{}},
		Request: ProviderRequest{Headers: map[string]string{}, Body: map[string]any{}},
	}
}
