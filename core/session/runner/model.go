package runner

import (
	"github.com/user/go-agent-sdk/llm/protocols"
	"github.com/user/go-agent-sdk/llm/route"
	llmschema "github.com/user/go-agent-sdk/llm/schema"
	"github.com/user/go-agent-sdk/schema"
)

// ModelNotSelectedError mirrors SessionRunnerModel.ModelNotSelectedError.
type ModelNotSelectedError struct {
	SessionID schema.SessionID
}

func (e *ModelNotSelectedError) Error() string {
	return "No model is available for session " + string(e.SessionID)
}

// ModelUnavailableError mirrors SessionRunnerModel.ModelUnavailableError.
type ModelUnavailableError struct {
	ProviderID schema.ProviderID
	ModelID    schema.ModelID
}

func (e *ModelUnavailableError) Error() string {
	return "Model unavailable: " + string(e.ProviderID) + "/" + string(e.ModelID)
}

// VariantUnavailableError mirrors SessionRunnerModel.VariantUnavailableError.
type VariantUnavailableError struct {
	ProviderID schema.ProviderID
	ModelID    schema.ModelID
	Variant    schema.ModelVariantID
}

func (e *VariantUnavailableError) Error() string {
	return "Variant unavailable for " + string(e.ProviderID) + "/" + string(e.ModelID) + ": " + string(e.Variant)
}

// UnsupportedApiError mirrors SessionRunnerModel.UnsupportedApiError.
type UnsupportedApiError struct {
	ProviderID schema.ProviderID
	ModelID    schema.ModelID
	API        string
}

func (e *UnsupportedApiError) Error() string {
	return "Unsupported API for " + string(e.ProviderID) + "/" + string(e.ModelID) + ": " + e.API
}

// ModelResolver mirrors the SessionRunnerModel.Interface.resolve seam.
type ModelResolver func(session schema.SessionInfo) (route.Model, error)

// runnerModelAPIKey mirrors `apiKey`. Credentials are narrowed to an optional
// API key (the credential service is not ported).
func runnerModelAPIKey(model schema.ModelInfo, credential *string) (string, bool) {
	if credential != nil {
		return *credential, true
	}
	if value, ok := model.Request.Body["apiKey"]; ok && value != nil {
		if text, ok := value.(string); ok {
			return text, true
		}
		return "", false
	}
	if value, ok := model.API.Settings["apiKey"]; ok {
		if text, ok := value.(string); ok {
			return text, true
		}
	}
	return "", false
}

// runnerModelWithDefaults mirrors `withDefaults`.
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

// runnerModelWithVariant mirrors `withVariant`.
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

func runnerModelAPIName(model schema.ModelInfo) string {
	if model.API.Type == "aisdk" {
		return "aisdk:" + model.API.Package
	}
	return model.API.Type
}

func runnerModelAPIID(model schema.ModelInfo) string {
	if model.API.ID == nil {
		return ""
	}
	return string(*model.API.ID)
}

// FromCatalogModel mirrors `fromCatalogModel`. The credential is narrowed to an
// optional API key (the credential service is not ported).
func FromCatalogModel(model schema.ModelInfo, credential *string) (route.Model, error) {
	key, hasKey := runnerModelAPIKey(model, credential)
	modelID := runnerModelAPIID(model)
	switch {
	case model.API.Type == "aisdk" && model.API.Package == "@ai-sdk/openai":
		auth := route.None()
		if hasKey {
			auth = route.Bearer(key)
		}
		return runnerModelWithDefaults(model, protocols.OpenAIResponsesHTTPRoute).
			With(route.RoutePatch{Auth: auth}).
			Model(route.RouteModelInput{ID: modelID}), nil
	case model.API.Type == "aisdk" && model.API.Package == "@ai-sdk/anthropic":
		auth := route.None()
		if hasKey {
			auth = route.Header("x-api-key", key)
		}
		return runnerModelWithDefaults(model, protocols.AnthropicMessagesHTTPRoute).
			With(route.RoutePatch{Auth: auth}).
			Model(route.RouteModelInput{ID: modelID}), nil
	case model.API.Type == "aisdk" && model.API.Package == "@ai-sdk/openai-compatible" && model.API.URL != nil:
		auth := route.None()
		if hasKey {
			auth = route.Bearer(key)
		}
		return runnerModelWithDefaults(model, protocols.OpenAICompatibleChatHTTPRoute).
			With(route.RoutePatch{Auth: auth}).
			Model(route.RouteModelInput{ID: modelID}), nil
	}
	return route.Model{}, &UnsupportedApiError{
		ProviderID: model.ProviderID,
		ModelID:    model.ID,
		API:        runnerModelAPIName(model),
	}
}

// ResolveModel mirrors `resolve`.
func ResolveModel(session schema.SessionInfo, model schema.ModelInfo, credential *string) (route.Model, error) {
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

// Supported mirrors `supported`.
func Supported(model schema.ModelInfo) bool {
	return model.API.Type == "aisdk" &&
		(model.API.Package == "@ai-sdk/openai" ||
			model.API.Package == "@ai-sdk/anthropic" ||
			(model.API.Package == "@ai-sdk/openai-compatible" && model.API.URL != nil))
}
