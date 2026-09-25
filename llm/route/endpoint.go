package route

import (
	"net/url"
	"strings"

	"github.com/user/go-agent-sdk/llm/schema"
)

func trimBaseURL(value string) string {
	return strings.TrimRight(value, "/")
}

// EndpointInput is what an endpoint renders from.
type EndpointInput struct {
	Request *schema.LLMRequest
	Body    any
}

// Endpoint is a route's base URL, path and query.
type Endpoint struct {
	BaseURL string
	Path    func(input EndpointInput) string
	Query   map[string]string
}

// EndpointPatch overrides an endpoint; a nil field keeps the current value.
type EndpointPatch struct {
	BaseURL *string
	Path    func(input EndpointInput) string
	Query   map[string]string
}

// NewPath builds an endpoint whose path is computed per request.
func NewPath(path func(EndpointInput) string, opts ...EndpointOption) Endpoint {
	e := Endpoint{Path: path}
	for _, opt := range opts {
		opt(&e)
	}
	return e
}

// NewStaticPath builds an endpoint with a fixed path.
func NewStaticPath(path string, opts ...EndpointOption) Endpoint {
	e := Endpoint{Path: func(EndpointInput) string { return path }}
	for _, opt := range opts {
		opt(&e)
	}
	return e
}

// EndpointOption customizes an endpoint.
type EndpointOption func(*Endpoint)

// WithBaseURL sets the endpoint's base URL.
func WithBaseURL(baseURL string) EndpointOption {
	return func(e *Endpoint) { e.BaseURL = baseURL }
}

// MergeEndpoint applies a patch to an endpoint.
func MergeEndpoint(base Endpoint, patch EndpointPatch) Endpoint {
	e := base
	if patch.BaseURL != nil {
		e.BaseURL = *patch.BaseURL
	}
	if patch.Path != nil {
		e.Path = patch.Path
	}
	if patch.Query != nil {
		merged := make(map[string]string, len(e.Query)+len(patch.Query))
		for k, v := range e.Query {
			merged[k] = v
		}
		for k, v := range patch.Query {
			merged[k] = v
		}
		e.Query = merged
	}
	return e
}

// RenderEndpoint builds the request URL for one call.
func RenderEndpoint(endpoint Endpoint, input EndpointInput) (*url.URL, error) {
	path := endpoint.Path(input)
	baseURL := trimBaseURL(endpoint.BaseURL)
	u, err := url.Parse(baseURL + path)
	if err != nil {
		return nil, err
	}
	setQuery(u, endpoint.Query)
	return u, nil
}

// setQuery sets query values on a URL.
func setQuery(u *url.URL, query map[string]string) {
	if len(query) == 0 {
		return
	}
	q := u.Query()
	for k, v := range query {
		q.Set(k, v)
	}
	u.RawQuery = q.Encode()
}

// ApplyQuery sets query values on a URL string, returning it unchanged when it
// cannot be parsed.
func ApplyQuery(baseURL string, query map[string]string) string {
	if len(query) == 0 {
		return baseURL
	}
	u, err := url.Parse(baseURL)
	if err != nil {
		return baseURL
	}
	setQuery(u, query)
	return u.String()
}
