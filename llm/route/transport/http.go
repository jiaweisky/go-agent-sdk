package transport

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"net/http"
	"strings"

	"github.com/jiaweisky/go-agent-sdk/llm/route"
	"github.com/jiaweisky/go-agent-sdk/llm/schema"
)

// jsonParts is the pieces a JSON request is assembled from.
type jsonParts struct {
	URL      string
	BodyText string
	Headers  map[string]string
}

// HttpPrepared is a request ready to send: its URL, method, headers and body.
type HttpPrepared struct {
	URL     string
	Method  string
	Body    string
	Headers map[string]string
	Framing route.Framing[string]
}

var protocolBodyOverlayDenylist = map[string]struct{}{
	"content": {}, "contents": {}, "frequencyPenalty": {}, "frequency_penalty": {},
	"generationConfig": {}, "inferenceConfig": {}, "input": {},
	"maxTokens": {}, "max_tokens": {}, "messages": {}, "model": {},
	"presencePenalty": {}, "presence_penalty": {}, "responseFormat": {}, "response_format": {},
	"seed": {}, "stop": {}, "stopSequences": {}, "stop_sequences": {}, "stream": {},
	"streamOptions": {}, "stream_options": {}, "system": {}, "systemInstruction": {}, "system_instruction": {},
	"temperature": {}, "thinking": {}, "toolChoice": {}, "toolConfig": {}, "tool_choice": {}, "tool_config": {},
	"tools": {}, "topK": {}, "topP": {}, "top_k": {}, "top_p": {},
}

func forbiddenBodyOverlayKeys(body map[string]any) []string {
	var forbidden []string
	for k := range body {
		if _, ok := protocolBodyOverlayDenylist[k]; ok {
			forbidden = append(forbidden, k)
		}
	}
	return forbidden
}

func bodyWithOverlay(
	body any,
	request *schema.LLMRequest,
	encodeBody func(any) (string, error),
) (bodyText string, err error) {
	if request.Http == nil || request.Http.Body == nil {
		return encodeBody(body)
	}
	overlay := request.Http.Body
	if forbidden := forbiddenBodyOverlayKeys(overlay); len(forbidden) > 0 {
		return "", fmt.Errorf("http.body cannot overlay protocol-owned field(s): %s", strings.Join(forbidden, ", "))
	}
	base, ok := jsonRecord(body)
	if !ok {
		return "", fmt.Errorf("http.body can only overlay JSON object request bodies")
	}
	merged := schema.MergeJsonRecords(base, overlay)
	if merged == nil {
		merged = map[string]any{}
	}
	b, err := json.Marshal(merged)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// jsonRecord returns a JSON object body as a map, whether it is already one or a
// typed struct, so it can be overlaid.
func jsonRecord(body any) (map[string]any, bool) {
	if m, ok := body.(map[string]any); ok {
		return m, true
	}
	data, err := json.Marshal(body)
	if err != nil {
		return nil, false
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, false
	}
	return m, true
}

func jsonRequestParts(input route.TransportPrepareInput) (jsonParts, error) {
	rendered, err := route.RenderEndpoint(input.Endpoint, route.EndpointInput{Request: input.Request, Body: input.Body})
	if err != nil {
		return jsonParts{}, err
	}
	u := rendered.String()
	if input.Request.Http != nil {
		u = route.ApplyQuery(u, input.Request.Http.Query)
	}
	bodyText, err := bodyWithOverlay(input.Body, input.Request, input.EncodeBody)
	if err != nil {
		return jsonParts{}, err
	}
	var routeHeaders map[string]string
	if input.Headers != nil {
		routeHeaders = input.Headers(input.Request)
	}
	var requestHeaders map[string]string
	if input.Request.Http != nil {
		requestHeaders = input.Request.Http.Headers
	}
	headers := schema.MergeStringRecords(routeHeaders, requestHeaders)
	if headers == nil {
		headers = make(map[string]string)
	}
	if input.Auth != nil {
		authHeaders, err := input.Auth.Apply(route.AuthInput{
			Request: input.Request,
			URL:     u,
			Body:    bodyText,
			Headers: headers,
		})
		if err != nil {
			return jsonParts{}, err
		}
		headers = authHeaders
	}
	if headers == nil {
		headers = make(map[string]string)
	}
	headers["content-type"] = "application/json"
	return jsonParts{
		URL:      u,
		BodyText: bodyText,
		Headers:  headers,
	}, nil
}

// HttpJsonInput describes a JSON-over-HTTP transport.
type HttpJsonInput struct {
	Framing route.Framing[string]
}

// HttpJsonTransport sends a JSON body over HTTP and frames the response.
type HttpJsonTransport struct {
	id      string
	prepare func(input route.TransportPrepareInput) (HttpPrepared, error)
	frames  func(
		ctx context.Context,
		prepared HttpPrepared,
		request *schema.LLMRequest,
		runtime route.TransportRuntime,
	) iter.Seq2[any, error]
}

// ID identifies the transport.
func (t *HttpJsonTransport) ID() string { return t.id }

// Prepare builds the HTTP request for one call.
func (t *HttpJsonTransport) Prepare(input route.TransportPrepareInput) (any, error) {
	return t.prepare(input)
}

// Frames sends the prepared request and yields its frames.
func (t *HttpJsonTransport) Frames(
	ctx context.Context,
	prepared any,
	request *schema.LLMRequest,
	runtime route.TransportRuntime,
) iter.Seq2[any, error] {
	p := prepared.(HttpPrepared)
	return t.frames(ctx, p, request, runtime)
}

// NewHttpJsonTransport builds a JSON-over-HTTP transport.
func NewHttpJsonTransport(input HttpJsonInput) *HttpJsonTransport {
	base := &HttpJsonTransport{
		id: "http-json",
		prepare: func(pi route.TransportPrepareInput) (HttpPrepared, error) {
			parts, err := jsonRequestParts(pi)
			if err != nil {
				return HttpPrepared{}, err
			}
			return HttpPrepared{
				URL:     parts.URL,
				Method:  "POST",
				Body:    parts.BodyText,
				Headers: parts.Headers,
				Framing: input.Framing,
			}, nil
		},
		frames: func(
			ctx context.Context,
			prepared HttpPrepared,
			request *schema.LLMRequest,
			runtime route.TransportRuntime,
		) iter.Seq2[any, error] {
			return func(yield func(any, error) bool) {
				req, err := http.NewRequestWithContext(ctx, prepared.Method, prepared.URL, strings.NewReader(prepared.Body))
				if err != nil {
					yield(nil, err)
					return
				}
				for k, v := range prepared.Headers {
					req.Header.Set(k, v)
				}
				resp, llmErr := runtime.HTTP.DoWithRetries(req)
				if llmErr != nil {
					if ctxErr := ctx.Err(); ctxErr != nil {
						yield(nil, ctxErr)
						return
					}
					yield(nil, llmErr)
					return
				}
				defer resp.Body.Close()
				for frame, err := range prepared.Framing.Frame(resp.Body) {
					if !yield(frame, err) {
						return
					}
				}
			}
		},
	}
	return base
}

// SSEJson is the standard JSON-over-HTTP transport: a JSON body in, server-sent
// events out.
func SSEJson() *HttpJsonTransport {
	return NewHttpJsonTransport(HttpJsonInput{Framing: route.SSE})
}
