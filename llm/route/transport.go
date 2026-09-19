package route

import (
	"iter"

	"github.com/user/go-agent-sdk/llm/schema"
)

type TransportRuntime struct {
	HTTP      *Executor
	WebSocket any
}

type Transport interface {
	ID() string
	Prepare(input TransportPrepareInput) (any, error)
	Frames(prepared any, request *schema.LLMRequest, runtime TransportRuntime) iter.Seq2[any, error]
}

type TransportPrepareInput struct {
	Body       any
	Request    *schema.LLMRequest
	Endpoint   Endpoint
	Auth       *Auth
	EncodeBody func(body any) string
	Headers    func(input *schema.LLMRequest) map[string]string
}
