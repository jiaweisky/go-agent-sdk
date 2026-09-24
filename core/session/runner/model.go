package runner

import (
	"github.com/user/go-agent-sdk/llm/protocols"
	"github.com/user/go-agent-sdk/llm/protocols/utils"
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
	if model.API.Protocol != "" {
		return string(model.API.Protocol)
	}
	return model.API.Type
}

func runnerModelAPIID(model schema.ModelInfo) string {
	if model.API.ID == nil {
		return ""
	}
	return string(*model.API.ID)
}

// FromCatalogModel resolves a native-protocol model to a route.Model by looking
// its protocol up in the registry and applying the credential with the
// protocol's auth scheme.
func FromCatalogModel(model schema.ModelInfo, credential schema.Credential) (route.Model, error) {
	entry, ok := protocols.Lookup(llmschema.ProtocolID(model.API.Protocol))
	if !ok {
		return route.Model{}, &UnsupportedApiError{
			ProviderID: model.ProviderID,
			ModelID:    model.ID,
			API:        runnerModelAPIName(model),
		}
	}
	return runnerModelWithDefaults(model, entry.Route).
		With(route.RoutePatch{Auth: runnerProtocolAuth(entry, credential)}).
		Model(route.RouteModelInput{ID: runnerModelAPIID(model)}), nil
}

// runnerProtocolAuth applies the credential using the protocol's auth scheme.
func runnerProtocolAuth(entry protocols.Entry, credential schema.Credential) *route.Auth {
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
	signer := utils.SigV4(&utils.Credentials{
		Region:          aws.Region,
		AccessKeyID:     aws.AccessKeyID,
		SecretAccessKey: aws.SecretAccessKey,
		SessionToken:    aws.SessionToken,
	})
	return route.Custom(func(input route.AuthInput) (map[string]string, error) {
		return signer(utils.AuthInput{URL: input.URL, Body: input.Body, Headers: input.Headers})
	})
}

// ResolveModel mirrors `resolve`.
func ResolveModel(session schema.SessionInfo, model schema.ModelInfo, credential schema.Credential) (route.Model, error) {
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
