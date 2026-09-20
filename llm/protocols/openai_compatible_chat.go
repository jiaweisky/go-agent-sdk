package protocols

import (
	"github.com/user/go-agent-sdk/llm/route"
	"github.com/user/go-agent-sdk/llm/route/transport"
)

// OpenAICompatibleChatHTTPRoute mirrors OpenAICompatibleChat.route: the OpenAI
// Chat protocol with a distinct route id for non-OpenAI providers exposing a
// Chat-compatible /chat/completions endpoint.
var OpenAICompatibleChatHTTPRoute = route.MakeFromTransport(route.MakeTransportInput{
	ID:        "openai-compatible-chat",
	Protocol:  OpenAIChatProtocol,
	Endpoint:  route.NewStaticPath(OpenAIChatPath),
	Transport: transport.SSEJson(),
})
