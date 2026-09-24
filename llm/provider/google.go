package provider

import (
	"os"

	"github.com/user/go-agent-sdk/llm"
	"github.com/user/go-agent-sdk/llm/route"
	"github.com/user/go-agent-sdk/llm/schema"
)

// GoogleProviderID is the provider id models.dev uses for Google.
const GoogleProviderID = schema.ProviderID("google")

// GoogleConfig configures the Google facade.
type GoogleConfig struct {
	Auth            *route.Auth
	APIKey          *string
	BaseURL         string
	Headers         map[string]string
	Limits          *schema.ModelLimits
	Generation      *schema.GenerationOptions
	ProviderOptions schema.ProviderOptions
	Http            *schema.HttpOptions
}

// GoogleProvider is a configured Google facade: its id, its route, and its model
// factory.
type GoogleProvider struct {
	ID     schema.ProviderID
	Routes []route.Route
	Model  func(id string, opts ...llm.ModelOptions) route.Model
}

func googleAuth(input GoogleConfig) *route.Auth {
	if input.Auth != nil {
		return input.Auth
	}
	cred := route.NewValue("")
	if input.APIKey != nil {
		cred = route.NewValue(*input.APIKey)
	}
	envVal := os.Getenv("GOOGLE_GENERATIVE_AI_API_KEY")
	if envVal != "" {
		cred = cred.OrElse(route.NewValue(envVal))
	}
	return cred.Header("x-goog-api-key")
}

func configuredGoogleRoute(geminiRoute route.Route, input GoogleConfig) route.Route {
	defaults := route.RouteDefaultsInput{
		Headers:         input.Headers,
		Limits:          input.Limits,
		Generation:      input.Generation,
		ProviderOptions: input.ProviderOptions,
		Http:            input.Http,
	}
	var baseURL *string
	if input.BaseURL != "" {
		baseURL = &input.BaseURL
	}
	return geminiRoute.With(route.RoutePatch{
		Endpoint: &route.EndpointPatch{BaseURL: baseURL},
		Auth:     googleAuth(input),
		Defaults: &defaults,
	})
}

// NewGoogle builds the Google facade over the Gemini route.
func NewGoogle(geminiRoute route.Route, input GoogleConfig) GoogleProvider {
	r := configuredGoogleRoute(geminiRoute, input)
	return GoogleProvider{
		ID:     GoogleProviderID,
		Routes: []route.Route{r},
		Model: func(id string, opts ...llm.ModelOptions) route.Model {
			modelInput := route.RouteModelInput{ID: id}
			if len(opts) > 0 {
				opt := opts[0]
				modelInput.Defaults = opt.Defaults
				modelInput.Compatibility = opt.Compatibility
			}
			return r.Model(modelInput)
		},
	}
}
