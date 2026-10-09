package route

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"

	"github.com/jiaweisky/go-agent-sdk/pkg/llm/schema"
	jsonschema6 "github.com/santhosh-tekuri/jsonschema/v6"
)

// RouteBody is how a route turns a request into a wire body, and how it validates
// the result.
type RouteBody struct {
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
type RouteDefaultsInput = RouteDefaults

// RouteModelInput describes the model to bind to a route.
type RouteModelInput struct {
	ID            string
	Provider      string
	Defaults      *schema.ModelDefaults
	Compatibility *schema.ModelCompatibility
	Capabilities  *schema.ModelCapabilities
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
	Model            func(input RouteModelInput) (Model, error)
	PrepareTransport func(body any, request *schema.LLMRequest) (any, error)
	StreamPrepared   func(
		ctx context.Context,
		prepared any,
		request *schema.LLMRequest,
		runtime TransportRuntime,
	) iter.Seq2[any, error]
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
				Validator: CompileBodySchema(ri.Protocol.Body.Schema),
				From:      ri.Protocol.Body.From,
			},
		}
		r.With = func(patch RoutePatch) Route { return routeWith(ri, patch, build) }
		r.Model = func(input RouteModelInput) (Model, error) { return routeModel(ri, r, input) }
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
func routeModel(ri makeTransportInput, r Route, input RouteModelInput) (Model, error) {
	prov := input.Provider
	if prov == "" {
		prov = ri.Provider
	}
	if prov == "" {
		return Model{}, fmt.Errorf("Route.Model(%s) requires a provider", ri.ID)
	}
	if ri.Endpoint.BaseURL == "" {
		return Model{}, fmt.Errorf("Route.Model(%s) requires an endpoint baseURL — configure it on the route first", ri.ID)
	}
	return Model{
		Model: schema.NewModel(schema.ModelInput{
			ID:            input.ID,
			Provider:      prov,
			Defaults:      input.Defaults,
			Compatibility: input.Compatibility,
			Capabilities:  input.Capabilities,
		}),
		Route: r,
	}, nil
}

// routePrepareTransport runs the transport's preparation step for a body.
func routePrepareTransport(ri makeTransportInput, body any, request *schema.LLMRequest) (any, error) {
	return ri.Transport.Prepare(TransportPrepareInput{
		Body:     body,
		Request:  request,
		Endpoint: ri.Endpoint,
		Auth:     ri.Auth,
		EncodeBody: func(b any) (string, error) {
			bytes, err := json.Marshal(b)
			if err != nil {
				return "", err
			}
			return string(bytes), nil
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
			next, events, terminal, err := stepFrame(ri, stream, state, frame)
			if err != nil {
				yield(nil, err)
				return
			}
			state = next
			if !emitEvents(events, yield) {
				return
			}
			if terminal {
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

// stepFrame decodes one frame, steps the state machine, and reports whether the
// frame was terminal.
func stepFrame(
	ri makeTransportInput,
	stream ProtocolStream,
	state any,
	frame any,
) (any, []schema.LLMEvent, bool, error) {
	frameStr, ok := frame.(string)
	if !ok {
		return state, nil, false, fmt.Errorf("route %s: expected string frame, got %T", ri.ID, frame)
	}
	event, err := stream.DecodeEvent(frameStr)
	if err != nil {
		return state, nil, false, err
	}
	next, events, err := stream.Step(state, event)
	if err != nil {
		return state, nil, false, err
	}
	return next, events, stream.Terminal != nil && stream.Terminal(event), nil
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
	}
	if input.ProviderOptions != nil {
		r.ProviderOptions = schema.MergeProviderOptions(base.ProviderOptions, input.ProviderOptions)
	}
	r.Headers = schema.MergeStringRecords(base.Headers, input.Headers)
	r.Http = mergeHttpDefaults(base.Http, r.Headers, input.Http)
	return r
}

// mergeHttpDefaults folds merged headers and a patch into the base HTTP options.
func mergeHttpDefaults(
	base *schema.HttpOptions,
	headers map[string]string,
	input *schema.HttpOptions,
) *schema.HttpOptions {
	if headers == nil {
		if input == nil {
			return base
		}
		return schema.MergeHttpOptions(base, input)
	}
	withHeaders := &schema.HttpOptions{Headers: headers}
	if input == nil {
		return schema.MergeHttpOptions(base, withHeaders)
	}
	return schema.MergeHttpOptions(base, input, withHeaders)
}

// LLMClient runs requests. Client implements it over a route.
type LLMClient interface {
	Prepare(ctx context.Context, req *schema.LLMRequest) (*schema.PreparedRequest, error)
	Stream(ctx context.Context, req *schema.LLMRequest) (iter.Seq2[schema.LLMEvent, error], error)
	Generate(ctx context.Context, req *schema.LLMRequest) (*schema.LLMResponse, error)
}
