package route

import (
	"context"
	"iter"

	"github.com/user/go-agent-sdk/llm/schema"
)

type TransportRuntime struct {
	HTTP *Executor
}

type Transport interface {
	ID() string
	Prepare(input TransportPrepareInput) (any, error)
	Frames(ctx context.Context, prepared any, request *schema.LLMRequest, runtime TransportRuntime) iter.Seq2[any, error]
}

type TransportPrepareInput struct {
	Body       any
	Request    *schema.LLMRequest
	Endpoint   Endpoint
	Auth       *Auth
	EncodeBody func(body any) string
	Headers    func(input *schema.LLMRequest) map[string]string
}
