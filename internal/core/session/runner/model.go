package runner

import (
	"github.com/jiaweisky/go-agent-sdk/internal/llm/protocols/utils"
	"github.com/jiaweisky/go-agent-sdk/pkg/llm/protocols"
	"github.com/jiaweisky/go-agent-sdk/pkg/llm/route"
	llmschema "github.com/jiaweisky/go-agent-sdk/pkg/llm/schema"
	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

// ModelNotSelectedError reports a session without a model.
type ModelNotSelectedError struct {
	SessionID schema.SessionID
}

func (e *ModelNotSelectedError) Error() string {
	return "No model is available for session " + string(e.SessionID)
}

// ModelUnavailableError reports a model the provider does not offer.
type ModelUnavailableError struct {
	ProviderID schema.ProviderID
	ModelID    schema.ModelID
}

func (e *ModelUnavailableError) Error() string {
	return "Model unavailable: " + string(e.ProviderID) + "/" + string(e.ModelID)
}

// VariantUnavailableError reports a variant the model does not have.
type VariantUnavailableError struct {
	ProviderID schema.ProviderID
	ModelID    schema.ModelID
	Variant    schema.ModelVariantID
}

func (e *VariantUnavailableError) Error() string {
	return "Variant unavailable for " + string(e.ProviderID) + "/" + string(e.ModelID) + ": " + string(e.Variant)
}

// UnsupportedApiError reports a model whose API the SDK does not speak.
type UnsupportedApiError struct {
	ProviderID schema.ProviderID
	ModelID    schema.ModelID
	API        string
}

func (e *UnsupportedApiError) Error() string {
	return "Unsupported API for " + string(e.ProviderID) + "/" + string(e.ModelID) + ": " + e.API
}

// ModelResolver resolves a session's model to a route model.
type ModelResolver func(session schema.SessionInfo) (route.Model, error)

// runnerModelWithDefaults applies the model's defaults to a route.
func runnerModelWithDefaults(model schema.ModelInfo, r route.Route) route.Route {
	httpBody := model.Request.Body
	if _, has := httpBody["apiKey"]; has {
		filtered := make(map[string]any, len(httpBody))
		for key, value := range httpBody {
			if key != "apiKey" {
				filtered[key] = value
			}
		}
		httpBody = filtered
	}
	var endpoint *route.EndpointPatch
	if model.API.URL != nil {
		endpoint = &route.EndpointPatch{BaseURL: model.API.URL}
	}
	return r.With(route.RoutePatch{
		Provider: string(model.ProviderID),
		Endpoint: endpoint,
		Defaults: &route.RouteDefaultsInput{
			Headers: model.Request.Headers,
			Limits:  &llmschema.ModelLimits{Context: model.Limit.Context, Output: model.Limit.Output},
			Http:    &llmschema.HttpOptions{Body: httpBody},
		},
	})
}

// runnerModelWithVariant applies a model variant's overrides.
func runnerModelWithVariant(model schema.ModelInfo, variantID *schema.ModelVariantID) (schema.ModelInfo, error) {
	var id *string
	if variantID == nil || *variantID == "default" {
		id = model.Request.Variant
	} else {
		value := string(*variantID)
		id = &value
	}
	var variant *schema.ModelVariant
	if id != nil {
		for i := range model.Variants {
			if string(model.Variants[i].ID) == *id {
				variant = &model.Variants[i]
				break
			}
		}
	}
	if variant == nil && variantID != nil && *variantID != "default" {
		return schema.ModelInfo{}, &VariantUnavailableError{
			ProviderID: model.ProviderID,
			ModelID:    model.ID,
			Variant:    *variantID,
		}
	}
	if variant == nil {
		return model, nil
	}
	updated := model
	updated.Request.Headers = make(map[string]string, len(model.Request.Headers)+len(variant.Headers))
	for key, value := range model.Request.Headers {
		updated.Request.Headers[key] = value
	}
	for key, value := range variant.Headers {
		updated.Request.Headers[key] = value
	}
	updated.Request.Body = make(map[string]any, len(model.Request.Body)+len(variant.Body))
	for key, value := range model.Request.Body {
		updated.Request.Body[key] = value
	}
	for key, value := range variant.Body {
		updated.Request.Body[key] = value
	}
	return updated, nil
}

func runnerModelAPIID(model schema.ModelInfo) string {
	if model.API.ID == nil {
		return ""
	}
	return string(*model.API.ID)
}

// FromCatalogModel resolves a model to a route.Model by looking
// its protocol up in the registry and applying the credential with the
// protocol's auth scheme.
func FromCatalogModel(model schema.ModelInfo, credential schema.Credential) (route.Model, error) {
	entry, ok := protocols.LookupProtocol(llmschema.ProtocolID(model.API.Protocol))
	if !ok {
		return route.Model{}, &UnsupportedApiError{
			ProviderID: model.ProviderID,
			ModelID:    model.ID,
			API:        string(model.API.Protocol),
		}
	}
	return runnerModelWithDefaults(model, entry.Route).
		With(route.RoutePatch{Auth: runnerProtocolAuth(entry, credential)}).
		Model(route.RouteModelInput{
			ID:           runnerModelAPIID(model),
			Capabilities: runnerModelCapabilities(model),
		})
}

// runnerModelCapabilities converts a catalog model's capabilities to the llm
// schema form, or nil when none are declared.
func runnerModelCapabilities(model schema.ModelInfo) *llmschema.ModelCapabilities {
	caps := model.Capabilities
	var sources []llmschema.MediaSource
	if caps.Media != nil {
		for _, source := range caps.Media.Sources {
			sources = append(sources, llmschema.MediaSource(source))
		}
	}
	if len(caps.Input) == 0 && len(caps.Output) == 0 && len(sources) == 0 {
		return nil
	}
	return &llmschema.ModelCapabilities{Input: caps.Input, Output: caps.Output, Sources: sources}
}

// runnerProtocolAuth applies the credential using the protocol's auth scheme.
func runnerProtocolAuth(entry protocols.ProtocolEntry, credential schema.Credential) *route.Auth {
	switch entry.Scheme {
	case protocols.AuthBearer:
		if credential.APIKey != "" {
			return route.Bearer(credential.APIKey)
		}
	case protocols.AuthHeader:
		if credential.APIKey != "" {
			return route.Header(entry.HeaderName, credential.APIKey)
		}
	case protocols.AuthSigV4:
		// Bedrock accepts either a bearer API key or AWS SigV4 credentials.
		if credential.APIKey != "" {
			return route.Bearer(credential.APIKey)
		}
		if credential.AWS != nil {
			return bedrockSigV4(credential.AWS)
		}
	}
	return route.None()
}

// bedrockSigV4 signs Bedrock requests with AWS SigV4 credentials.
func bedrockSigV4(aws *schema.AWSCredentials) *route.Auth {
	signer := utils.SigV4(aws)
	return route.Custom(func(input route.AuthInput) (map[string]string, error) {
		return signer(utils.AuthInput{URL: input.URL, Body: input.Body, Headers: input.Headers})
	})
}

// ResolveModel resolves a session's model, applying its credential and variant.
func ResolveModel(
	session schema.SessionInfo,
	model schema.ModelInfo,
	credential schema.Credential,
) (route.Model, error) {
	var variant *schema.ModelVariantID
	if session.Model != nil {
		variant = session.Model.Variant
	}
	selected, err := runnerModelWithVariant(model, variant)
	if err != nil {
		return route.Model{}, err
	}
	return FromCatalogModel(selected, credential)
}
