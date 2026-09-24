package transport

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"net/http"
	"net/url"
	"strings"

	"github.com/user/go-agent-sdk/llm/route"
	"github.com/user/go-agent-sdk/llm/schema"
)

// JsonRequestParts is the pieces a JSON request is assembled from.
type JsonRequestParts struct {
	URL      string
	JSONBody any
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

func applyQuery(baseURL string, query map[string]string) string {
	if len(query) == 0 {
		return baseURL
	}
	u, err := url.Parse(baseURL)
	if err != nil {
		return baseURL
	}
	q := u.Query()
	for k, v := range query {
		q.Set(k, v)
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func bodyWithOverlay(body any, request *schema.LLMRequest, encodeBody func(any) string) (jsonBody any, bodyText string, err error) {
	if request.Http == nil || request.Http.Body == nil {
		text := encodeBody(body)
		return body, text, nil
	}
	overlay := request.Http.Body
	if forbidden := forbiddenBodyOverlayKeys(overlay); len(forbidden) > 0 {
		return nil, "", fmt.Errorf("http.body cannot overlay protocol-owned field(s): %s", strings.Join(forbidden, ", "))
	}
	base, ok := jsonRecord(body)
	if !ok {
		return nil, "", fmt.Errorf("http.body can only overlay JSON object request bodies")
	}
	merged := schema.MergeJsonRecords(base, overlay)
	if merged == nil {
		merged = map[string]any{}
	}
	b, err := json.Marshal(merged)
	if err != nil {
		return nil, "", err
	}
	return merged, string(b), nil
}

// jsonRecord mirrors ProviderShared.isRecord: any JSON object body, whether it
// is already a map or a typed struct, becomes a map so it can be overlaid.
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

func jsonRequestParts(input route.TransportPrepareInput) (JsonRequestParts, error) {
	rendered, err := route.RenderEndpoint(input.Endpoint, route.EndpointInput{Request: input.Request, Body: input.Body})
	if err != nil {
		return JsonRequestParts{}, err
	}
	u := rendered.String()
	if input.Request.Http != nil {
		u = applyQuery(u, input.Request.Http.Query)
	}
	jsonBody, bodyText, err := bodyWithOverlay(input.Body, input.Request, input.EncodeBody)
	if err != nil {
		return JsonRequestParts{}, err
	}
	headers := make(map[string]string)
	if input.Headers != nil {
		for k, v := range input.Headers(input.Request) {
			headers[k] = v
		}
	}
	if input.Request.Http != nil && input.Request.Http.Headers != nil {
		for k, v := range input.Request.Http.Headers {
			headers[k] = v
		}
	}
	if input.Auth != nil {
		authHeaders, err := input.Auth.Apply(route.AuthInput{
			Request: input.Request,
			Method:  "POST",
			URL:     u,
			Body:    bodyText,
			Headers: headers,
		})
		if err != nil {
			return JsonRequestParts{}, err
		}
		headers = authHeaders
	}
	headers["content-type"] = "application/json"
	return JsonRequestParts{
		URL:      u,
		JSONBody: jsonBody,
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
	withFn  func(patch HttpJsonInput) *HttpJsonTransport
	prepare func(input route.TransportPrepareInput) (HttpPrepared, error)
	frames  func(ctx context.Context, prepared HttpPrepared, request *schema.LLMRequest, runtime route.TransportRuntime) iter.Seq2[any, error]
}

// ID identifies the transport.
func (t *HttpJsonTransport) ID() string { return t.id }

// Prepare builds the HTTP request for one call.
func (t *HttpJsonTransport) Prepare(input route.TransportPrepareInput) (any, error) {
	return t.prepare(input)
}

// Frames sends the prepared request and yields its frames.
func (t *HttpJsonTransport) Frames(ctx context.Context, prepared any, request *schema.LLMRequest, runtime route.TransportRuntime) iter.Seq2[any, error] {
	p := prepared.(HttpPrepared)
	return t.frames(ctx, p, request, runtime)
}

// NewHttpJsonTransport builds a JSON-over-HTTP transport.
func NewHttpJsonTransport(input HttpJsonInput) *HttpJsonTransport {
	base := &HttpJsonTransport{
		id: "http-json",
		withFn: func(patch HttpJsonInput) *HttpJsonTransport {
			merged := input
			if patch.Framing.ID != "" {
				merged.Framing = patch.Framing
			}
			return NewHttpJsonTransport(merged)
		},
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
		frames: func(ctx context.Context, prepared HttpPrepared, request *schema.LLMRequest, runtime route.TransportRuntime) iter.Seq2[any, error] {
			return func(yield func(any, error) bool) {
				req, err := http.NewRequestWithContext(ctx, "POST", prepared.URL, strings.NewReader(prepared.Body))
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
