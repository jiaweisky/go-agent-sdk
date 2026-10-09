package route

import (
	"context"
	"iter"

	"github.com/jiaweisky/go-agent-sdk/pkg/llm/schema"
)

// TransportRuntime is what a transport needs from the host: the HTTP client and
// any credentials.
type TransportRuntime struct {
	HTTP *Executor
}

// Transport sends a prepared body and yields the frames it receives.
type Transport interface {
	ID() string
	Prepare(input TransportPrepareInput) (any, error)
	Frames(ctx context.Context, prepared any, request *schema.LLMRequest, runtime TransportRuntime) iter.Seq2[any, error]
}

// TransportPrepareInput is what a transport prepares from.
type TransportPrepareInput struct {
	Body       any
	Request    *schema.LLMRequest
	Endpoint   Endpoint
	Auth       *Auth
	EncodeBody func(body any) (string, error)
	Headers    func(input *schema.LLMRequest) map[string]string
}
