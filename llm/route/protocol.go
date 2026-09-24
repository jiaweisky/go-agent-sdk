package route

import (
	jsonschema6 "github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/user/go-agent-sdk/jsonschema"
	"github.com/user/go-agent-sdk/llm/schema"
)

// Protocol is a wire protocol: how to build its body and how to decode its
// stream.
type Protocol struct {
	ID     schema.ProtocolID
	Body   ProtocolBody
	Stream ProtocolStream
}

// ProtocolBody lowers a request into the protocol's body and describes its
// schema.
type ProtocolBody struct {
	// Schema is the provider-native body contract. Route construction compiles
	// it and compile() validates every built body against it.
	Schema *jsonschema.Schema
	From   func(request *schema.LLMRequest) (any, error)
}

// CompileBodySchema compiles a body schema's validation projection, panicking
// when a non-nil schema cannot be compiled. A route that silently skipped body
// validation would let a malformed request reach the provider.
func CompileBodySchema(sch *jsonschema.Schema) *jsonschema6.Schema {
	if sch == nil {
		return nil
	}
	compiled, err := jsonschema.CompileValidation(sch.Validation())
	if err != nil {
		panic("route: body schema failed to compile: " + err.Error())
	}
	return compiled
}

// ProtocolStream decodes a protocol's frames into LLM events through a state
// machine.
type ProtocolStream struct {
	DecodeEvent func(frame string) (any, error)
	Initial     func(request *schema.LLMRequest) any
	Step        func(state any, event any) (any, []schema.LLMEvent, error)
	Terminal    func(event any) bool
	OnHalt      func(state any) []schema.LLMEvent
}

// MakeProtocol fills in a protocol's defaults.
func MakeProtocol(input Protocol) Protocol {
	return input
}
