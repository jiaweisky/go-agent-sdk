package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"

	jsonschema6 "github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/user/go-agent-sdk/llm/route"
	"github.com/user/go-agent-sdk/llm/schema"
)

// Client mirrors LLMClient.layer: it resolves request defaults, compiles the
// request against its model's route, and runs it through the transport.
type Client struct {
	runtime route.TransportRuntime
}

func NewClient(runtime route.TransportRuntime) *Client {
	return &Client{runtime: runtime}
}

var _ route.LLMClient = (*Client)(nil)

// Prepare mirrors LLMClient.prepare.
func (c *Client) Prepare(_ context.Context, request *schema.LLMRequest) (*schema.PreparedRequest, error) {
	resolved, r, body, _, err := c.compile(request)
	if err != nil {
		return nil, err
	}
	id := resolved.ID
	if id == "" {
		id = "request"
	}
	return &schema.PreparedRequest{
		ID:       id,
		Route:    schema.RouteID(r.ID),
		Protocol: r.Protocol,
		Model:    resolved.Model,
		Body:     body,
		Metadata: map[string]any{"transport": r.Transport.ID()},
	}, nil
}

// Stream mirrors LLMClient.stream.
func (c *Client) Stream(ctx context.Context, request *schema.LLMRequest) (iter.Seq2[schema.LLMEvent, error], error) {
	resolved, r, _, prepared, err := c.compile(request)
	if err != nil {
		return nil, err
	}
	raw := r.StreamPrepared(ctx, prepared, resolved, c.runtime)
	return func(yield func(schema.LLMEvent, error) bool) {
		for event, err := range raw {
			if err != nil {
				if !yield(schema.LLMEvent{}, err) {
					return
				}
				continue
			}
			llmEvent, ok := event.(schema.LLMEvent)
			if !ok {
				continue
			}
			if !yield(llmEvent, nil) {
				return
			}
		}
	}, nil
}

// Generate mirrors LLMClient.generate.
func (c *Client) Generate(ctx context.Context, request *schema.LLMRequest) (*schema.LLMResponse, error) {
	stream, err := c.Stream(ctx, request)
	if err != nil {
		return nil, err
	}
	state := schema.EmptyResponseState()
	for event, err := range stream {
		if err != nil {
			return nil, err
		}
		schema.ReduceResponseState(&state, event)
	}
	if response := schema.LLMResponseComplete(&state); response != nil {
		return response, nil
	}
	return nil, streamError(request, "Provider stream ended without a terminal finish event")
}

func (c *Client) compile(request *schema.LLMRequest) (*schema.LLMRequest, route.Route, any, any, error) {
	resolved := resolveRequestOptions(request)
	model, ok := resolved.Model.(route.Model)
	if !ok {
		return nil, route.Route{}, nil, nil, streamError(request, "Model has no route")
	}
	resolved = ApplyCachePolicy(resolved, string(model.Route.Protocol))
	r := model.Route
	body, err := r.Body.From(resolved)
	if err != nil {
		return nil, r, nil, nil, err
	}
	if err := validateBody(body, r.Body.Validator); err != nil {
		return nil, r, nil, nil, err
	}
	prepared, err := r.PrepareTransport(body, resolved)
	if err != nil {
		return nil, r, nil, nil, err
	}
	return resolved, r, body, prepared, nil
}

// resolveRequestOptions mirrors route/client.ts resolveRequestOptions: route
// defaults, then model overrides, then request values.
func resolveRequestOptions(request *schema.LLMRequest) *schema.LLMRequest {
	var routeDefaults schema.ModelDefaults
	if model, ok := request.Model.(route.Model); ok {
		d := model.Route.Defaults
		routeDefaults = schema.ModelDefaults{
			Limits:          d.Limits,
			Generation:      d.Generation,
			ProviderOptions: d.ProviderOptions,
			Http:            d.Http,
		}
	}
	var modelDefaults schema.ModelDefaults
	if request.Model != nil {
		if d := request.Model.GetDefaults(); d != nil {
			modelDefaults = *d
		}
	}
	updated := *request
	updated.Generation = schema.MergeGenerationOptions(routeDefaults.Generation, modelDefaults.Generation, request.Generation)
	if updated.Generation == nil {
		updated.Generation = &schema.GenerationOptions{}
	}
	updated.ProviderOptions = schema.MergeProviderOptions(routeDefaults.ProviderOptions, modelDefaults.ProviderOptions, request.ProviderOptions)
	updated.Http = schema.MergeHttpOptions(routeDefaults.Http, modelDefaults.Http, request.Http)
	return &updated
}

// validateBody mirrors ProviderShared.validateWith: a body that does not
// satisfy the protocol schema is rejected as an invalid request.
func validateBody(body any, validator *jsonschema6.Schema) error {
	if validator == nil {
		return nil
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return err
	}
	var document any
	if err := json.Unmarshal(encoded, &document); err != nil {
		return err
	}
	if err := validator.Validate(document); err != nil {
		return &schema.LLMError{
			Module: "ProviderShared",
			Method: "request",
			Reason: schema.NewInvalidRequestReason(err.Error()),
		}
	}
	return nil
}

func streamError(request *schema.LLMRequest, message string) error {
	routeID := ""
	if request != nil {
		if model, ok := request.Model.(route.Model); ok {
			routeID = model.Route.ID
		}
	}
	return &schema.LLMError{
		Module: "LLM",
		Method: "stream",
		Reason: schema.NewInvalidProviderOutputReason(fmt.Sprintf("%s (%s)", message, routeID)),
	}
}
