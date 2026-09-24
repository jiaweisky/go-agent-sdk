package protocols

import (
	"github.com/user/go-agent-sdk/llm/route"
	"github.com/user/go-agent-sdk/llm/route/transport"
)

// OpenAICompatibleChatHTTPRoute is the OpenAI Chat protocol under a distinct
// route id, for providers that expose a Chat-compatible /chat/completions
// endpoint.
var OpenAICompatibleChatHTTPRoute = route.MakeFromTransport(route.MakeTransportInput{
	ID:        "openai-compatible-chat",
	Protocol:  OpenAIChatProtocol,
	Endpoint:  route.NewStaticPath(OpenAIChatPath),
	Transport: transport.SSEJson(),
})
