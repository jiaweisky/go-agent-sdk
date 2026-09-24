package route

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"

	"github.com/user/go-agent-sdk/llm/schema"
)

type RouteBody struct {
	Schema schema.JsonSchema
	From   func(request *schema.LLMRequest) (any, error)
}

type RouteDefaults struct {
	Headers         map[string]string
	Limits          *schema.ModelLimits
	Generation      *schema.GenerationOptions
	ProviderOptions schema.ProviderOptions
	Http            *schema.HttpOptions
}

type RouteDefaultsInput struct {
	Headers         map[string]string
	Limits          *schema.ModelLimits
	Generation      *schema.GenerationOptions
	ProviderOptions schema.ProviderOptions
	Http            *schema.HttpOptions
}

type RouteModelInput struct {
	ID            string
	Provider      string
	Defaults      *schema.ModelDefaults
	Compatibility *schema.ModelCompatibility
}

type RoutePatch struct {
	ID        string
	Provider  string
	Auth      *Auth
	Transport Transport
	Endpoint  *EndpointPatch
	Defaults  *RouteDefaultsInput
}

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

func MakeFromTransport(input MakeTransportInput) Route {
	var build func(makeTransportInput) Route
	build = func(ri makeTransportInput) Route {
		var r Route
		r = Route{
			ID:        ri.ID,
			Provider:  schema.ProviderID(ri.Provider),
			Protocol:  ri.Protocol.ID,
			Endpoint:  ri.Endpoint,
			Auth:      ri.Auth,
			Transport: ri.Transport,
			Defaults:  ri.Defaults,
			Body: RouteBody{
				Schema: ri.Protocol.Body.Schema,
				From:   ri.Protocol.Body.From,
			},
			With: func(patch RoutePatch) Route {
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
			},
			Model: func(input RouteModelInput) Model {
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
			},
			PrepareTransport: func(body any, request *schema.LLMRequest) (any, error) {
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
			},
			StreamPrepared: func(ctx context.Context, prepared any, request *schema.LLMRequest, runtime TransportRuntime) iter.Seq2[any, error] {
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
						if stream.Terminal != nil && stream.Terminal(event) {
							newState, events, stepErr := stream.Step(state, event)
							if stepErr != nil {
								yield(nil, stepErr)
								return
							}
							state = newState
							for _, evt := range events {
								if !yield(schema.LLMEvent(evt), nil) {
									return
								}
							}
							if stream.OnHalt != nil {
								for _, evt := range stream.OnHalt(state) {
									if !yield(schema.LLMEvent(evt), nil) {
										return
									}
								}
							}
							return
						}
						newState, events, stepErr := stream.Step(state, event)
						if stepErr != nil {
							yield(nil, stepErr)
							return
						}
						state = newState
						for _, evt := range events {
							if !yield(schema.LLMEvent(evt), nil) {
								return
							}
						}
					}
					if stream.OnHalt != nil {
						for _, evt := range stream.OnHalt(state) {
							if !yield(schema.LLMEvent(evt), nil) {
								return
							}
						}
					}
				}
			},
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

type LLMClient interface {
	Prepare(ctx context.Context, req *schema.LLMRequest) (*schema.PreparedRequest, error)
	Stream(ctx context.Context, req *schema.LLMRequest) (iter.Seq2[schema.LLMEvent, error], error)
	Generate(ctx context.Context, req *schema.LLMRequest) (*schema.LLMResponse, error)
}
