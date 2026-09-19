package route

import "github.com/user/go-agent-sdk/llm/schema"

type Protocol struct {
	ID     schema.ProtocolID
	Body   ProtocolBody
	Stream ProtocolStream
}

type ProtocolBody struct {
	Schema schema.JsonSchema
	From   func(request *schema.LLMRequest) (any, error)
}

type ProtocolStream struct {
	DecodeEvent func(frame string) (any, error)
	Initial     func(request *schema.LLMRequest) any
	Step        func(state any, event any) (any, []schema.LLMEvent, error)
	Terminal    func(event any) bool
	OnHalt      func(state any) []schema.LLMEvent
}

func MakeProtocol(input Protocol) Protocol {
	return input
}
