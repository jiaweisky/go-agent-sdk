package route

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"

	jsonschema6 "github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/user/go-agent-sdk/jsonschema"
	"github.com/user/go-agent-sdk/llm/schema"
)

// RouteBody is how a route turns a request into a wire body, and how it validates
// the result.
type RouteBody struct {
	Schema    *jsonschema.Schema
	Validator *jsonschema6.Schema
	From      func(request *schema.LLMRequest) (any, error)
}

// RouteDefaults is what a route applies to every request.
type RouteDefaults struct {
	Headers         map[string]string
	Limits          *schema.ModelLimits
	Generation      *schema.GenerationOptions
	ProviderOptions schema.ProviderOptions
	Http            *schema.HttpOptions
}

// RouteDefaultsInput is the input form of RouteDefaults.
type RouteDefaultsInput struct {
	Headers         map[string]string
	Limits          *schema.ModelLimits
	Generation      *schema.GenerationOptions
	ProviderOptions schema.ProviderOptions
	Http            *schema.HttpOptions
}

// RouteModelInput describes the model to bind to a route.
type RouteModelInput struct {
	ID            string
	Provider      string
	Defaults      *schema.ModelDefaults
	Compatibility *schema.ModelCompatibility
}

// RoutePatch overrides parts of a route; a nil or empty field keeps the current
// value.
type RoutePatch struct {
	ID        string
	Provider  string
	Auth      *Auth
	Transport Transport
	Endpoint  *EndpointPatch
	Defaults  *RouteDefaultsInput
}

// Route reaches one provider's API: its endpoint, auth, transport and protocol,
// plus the model factory built on top of them.
type Route struct {
	ID               string
	Provider         schema.ProviderID
	Protocol         schema.ProtocolID
	Endpoint         Endpoint
	Auth             *Auth
	Defaults         RouteDefaults
	Body             RouteBody
	Transport        Transport
	With             func(patch RoutePatch) Route
	Model            func(input RouteModelInput) Model
	PrepareTransport func(body any, request *schema.LLMRequest) (any, error)
	StreamPrepared   func(ctx context.Context, prepared any, request *schema.LLMRequest, runtime TransportRuntime) iter.Seq2[any, error]
}

// MakeTransportInput describes a route to build.
type MakeTransportInput struct {
	ID        string
	Provider  string
	Protocol  Protocol
	Endpoint  Endpoint
	Auth      *Auth
	Headers   func(request *schema.LLMRequest) map[string]string
	Transport Transport
	Defaults  *RouteDefaultsInput
}

// MakeFromTransport builds a route from a protocol, endpoint, auth and transport.
func MakeFromTransport(input MakeTransportInput) Route {
	var build func(makeTransportInput) Route
	build = func(ri makeTransportInput) Route {
		r := Route{
			ID:        ri.ID,
			Provider:  schema.ProviderID(ri.Provider),
			Protocol:  ri.Protocol.ID,
			Endpoint:  ri.Endpoint,
			Auth:      ri.Auth,
			Transport: ri.Transport,
			Defaults:  ri.Defaults,
			Body: RouteBody{
				Schema:    ri.Protocol.Body.Schema,
				Validator: CompileBodySchema(ri.Protocol.Body.Schema),
				From:      ri.Protocol.Body.From,
			},
		}
		r.With = func(patch RoutePatch) Route { return routeWith(ri, patch, build) }
		r.Model = func(input RouteModelInput) Model { return routeModel(ri, r, input) }
		r.PrepareTransport = func(body any, request *schema.LLMRequest) (any, error) {
			return routePrepareTransport(ri, body, request)
		}
		r.StreamPrepared = func(
			ctx context.Context,
			prepared any,
			request *schema.LLMRequest,
			runtime TransportRuntime,
		) iter.Seq2[any, error] {
			return routeStreamPrepared(ri, ctx, prepared, request, runtime)
		}
		return r
	}

	var defaults RouteDefaults
	if input.Defaults != nil {
		defaults = mergeRouteDefaults(RouteDefaults{}, *input.Defaults)
	}
	return build(makeTransportInput{
		ID:        input.ID,
		Provider:  input.Provider,
		Protocol:  input.Protocol,
		Endpoint:  input.Endpoint,
		Auth:      input.Auth,
		Transport: input.Transport,
		Defaults:  defaults,
		Headers:   input.Headers,
	})
}

// routeWith applies a patch to the route's inputs and rebuilds the route.
func routeWith(ri makeTransportInput, patch RoutePatch, build func(makeTransportInput) Route) Route {
	next := ri
	if patch.ID != "" {
		next.ID = patch.ID
	}
	if patch.Provider != "" {
		next.Provider = patch.Provider
	}
	if patch.Auth != nil {
		next.Auth = patch.Auth
	}
	if patch.Transport != nil {
		next.Transport = patch.Transport
	}
	if patch.Endpoint != nil {
		next.Endpoint = MergeEndpoint(ri.Endpoint, *patch.Endpoint)
	}
	if patch.Defaults != nil {
		next.Defaults = mergeRouteDefaults(ri.Defaults, *patch.Defaults)
	}
	return build(next)
}

// routeModel binds a model to the route, rejecting the calls that cannot produce
// a request.
func routeModel(ri makeTransportInput, r Route, input RouteModelInput) Model {
	prov := input.Provider
	if prov == "" {
		prov = ri.Provider
	}
	if prov == "" {
		panic(fmt.Sprintf("Route.Model(%s) requires a provider", ri.ID))
	}
	if ri.Endpoint.BaseURL == "" {
		panic(fmt.Sprintf("Route.Model(%s) requires an endpoint baseURL — configure it on the route first", ri.ID))
	}
	return Model{
		Model: schema.NewModel(schema.ModelInput{
			ID:            input.ID,
			Provider:      prov,
			Defaults:      input.Defaults,
			Compatibility: input.Compatibility,
		}),
		Route: r,
	}
}

// routePrepareTransport runs the transport's preparation step for a body.
func routePrepareTransport(ri makeTransportInput, body any, request *schema.LLMRequest) (any, error) {
	return ri.Transport.Prepare(TransportPrepareInput{
		Body:     body,
		Request:  request,
		Endpoint: ri.Endpoint,
		Auth:     ri.Auth,
		EncodeBody: func(b any) string {
			bytes, err := json.Marshal(b)
			if err != nil {
				return ""
			}
			return string(bytes)
		},
		Headers: ri.Headers,
	})
}

// routeStreamPrepared decodes the transport's frames into LLM events, stepping
// the protocol's state machine until it reports a terminal event.
func routeStreamPrepared(
	ri makeTransportInput,
	ctx context.Context,
	prepared any,
	request *schema.LLMRequest,
	runtime TransportRuntime,
) iter.Seq2[any, error] {
	stream := ri.Protocol.Stream
	if stream.DecodeEvent == nil {
		return ri.Transport.Frames(ctx, prepared, request, runtime)
	}
	return func(yield func(any, error) bool) {
		state := stream.Initial(request)
		for frame, err := range ri.Transport.Frames(ctx, prepared, request, runtime) {
			if err != nil {
				yield(nil, err)
				return
			}
			frameStr, ok := frame.(string)
			if !ok {
				yield(nil, fmt.Errorf("route %s: expected string frame, got %T", ri.ID, frame))
				return
			}
			event, decodeErr := stream.DecodeEvent(frameStr)
			if decodeErr != nil {
				yield(nil, decodeErr)
				return
			}
			next, events, stepErr := stream.Step(state, event)
			if stepErr != nil {
				yield(nil, stepErr)
				return
			}
			state = next
			if !emitEvents(events, yield) {
				return
			}
			if stream.Terminal != nil && stream.Terminal(event) {
				if stream.OnHalt != nil {
					emitEvents(stream.OnHalt(state), yield)
				}
				return
			}
		}
		if stream.OnHalt != nil {
			emitEvents(stream.OnHalt(state), yield)
		}
	}
}

// emitEvents yields every event, reporting whether the consumer kept iterating.
func emitEvents(events []schema.LLMEvent, yield func(any, error) bool) bool {
	for _, event := range events {
		if !yield(schema.LLMEvent(event), nil) {
			return false
		}
	}
	return true
}

type makeTransportInput struct {
	ID        string
	Provider  string
	Protocol  Protocol
	Endpoint  Endpoint
	Auth      *Auth
	Transport Transport
	Defaults  RouteDefaults
	Headers   func(request *schema.LLMRequest) map[string]string
}

func mergeRouteDefaults(base RouteDefaults, input RouteDefaultsInput) RouteDefaults {
	r := base
	if input.Limits != nil {
		r.Limits = input.Limits
	}
	if input.Generation != nil {
		r.Generation = schema.MergeGenerationOptions(base.Generation, input.Generation)
	} else if base.Generation != nil {
		r.Generation = base.Generation
	}
	if input.ProviderOptions != nil {
		r.ProviderOptions = schema.MergeProviderOptions(base.ProviderOptions, input.ProviderOptions)
	}
	headers := mergeHeaders(base.Headers, input.Headers)
	r.Headers = headers
	httpInput := input.Http
	if headers != nil {
		httpWithHeaders := &schema.HttpOptions{Headers: headers}
		if httpInput != nil {
			r.Http = schema.MergeHttpOptions(base.Http, httpInput, httpWithHeaders)
		} else {
			r.Http = schema.MergeHttpOptions(base.Http, httpWithHeaders)
		}
	} else if httpInput != nil {
		r.Http = schema.MergeHttpOptions(base.Http, httpInput)
	}
	return r
}

func mergeHeaders(base, patch map[string]string) map[string]string {
	if len(base) == 0 && len(patch) == 0 {
		return nil
	}
	r := make(map[string]string, len(base)+len(patch))
	for k, v := range base {
		r[k] = v
	}
	for k, v := range patch {
		r[k] = v
	}
	return r
}

// LLMClient runs requests. Client implements it over a route.
type LLMClient interface {
	Prepare(ctx context.Context, req *schema.LLMRequest) (*schema.PreparedRequest, error)
	Stream(ctx context.Context, req *schema.LLMRequest) (iter.Seq2[schema.LLMEvent, error], error)
	Generate(ctx context.Context, req *schema.LLMRequest) (*schema.LLMResponse, error)
}
