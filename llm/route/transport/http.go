package transport

import (
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"net/http"
	"net/url"
	"strings"

	"github.com/user/go-agent-sdk/llm/route"
	"github.com/user/go-agent-sdk/llm/schema"
)

type JsonRequestParts struct {
	URL      string
	JSONBody any
	BodyText string
	Headers  map[string]string
}

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
	if m, ok := body.(map[string]any); ok {
		merged := schema.MergeJsonRecords(m, overlay)
		if merged == nil {
			merged = map[string]any{}
		}
		b, _ := json.Marshal(merged)
		return merged, string(b), nil
	}
	return nil, "", fmt.Errorf("http.body can only overlay JSON object request bodies")
}

func jsonRequestParts(input route.TransportPrepareInput) (JsonRequestParts, error) {
	u := input.Endpoint.BaseURL + input.Endpoint.Path(route.EndpointInput{Request: input.Request})
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
	if _, exists := headers["content-type"]; !exists {
		headers["content-type"] = "application/json"
	}
	return JsonRequestParts{
		URL:      u,
		JSONBody: jsonBody,
		BodyText: bodyText,
		Headers:  headers,
	}, nil
}

type HttpJsonInput struct {
	Framing route.Framing[string]
}

type HttpJsonTransport struct {
	id      string
	withFn  func(patch HttpJsonInput) *HttpJsonTransport
	prepare func(input route.TransportPrepareInput) (HttpPrepared, error)
	frames  func(prepared HttpPrepared, request *schema.LLMRequest, runtime route.TransportRuntime) iter.Seq2[any, error]
}

func (t *HttpJsonTransport) ID() string { return t.id }

func (t *HttpJsonTransport) Prepare(input route.TransportPrepareInput) (any, error) {
	return t.prepare(input)
}

func (t *HttpJsonTransport) Frames(prepared any, request *schema.LLMRequest, runtime route.TransportRuntime) iter.Seq2[any, error] {
	p := prepared.(HttpPrepared)
	return t.frames(p, request, runtime)
}

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
		frames: func(prepared HttpPrepared, request *schema.LLMRequest, runtime route.TransportRuntime) iter.Seq2[any, error] {
			return func(yield func(any, error) bool) {
				req, err := http.NewRequest("POST", prepared.URL, strings.NewReader(prepared.Body))
				if err != nil {
					yield(nil, err)
					return
				}
				for k, v := range prepared.Headers {
					req.Header.Set(k, v)
				}
				resp, llmErr := runtime.HTTP.DoWithRetries(req)
				if llmErr != nil {
					yield(nil, llmErr)
					return
				}
				body, err := io.ReadAll(resp.Body)
				resp.Body.Close()
				if err != nil {
					yield(nil, err)
					return
				}
				reader := io.NopCloser(strings.NewReader(string(body)))
				for frame, err := range prepared.Framing.Frame(reader) {
					if !yield(frame, err) {
						return
					}
				}
			}
		},
	}
	return base
}

func SSEJson() *HttpJsonTransport {
	return NewHttpJsonTransport(HttpJsonInput{Framing: route.SSE})
}
