package protocols

import (
	"github.com/jiaweisky/go-agent-sdk/pkg/llm/route"
	"github.com/jiaweisky/go-agent-sdk/pkg/llm/route/transport"
)

// DeepSeekChatProtocol is the OpenAI-compatible Chat wire with DeepSeek's `file`
// block (a Files API file_id), parameterized by the DeepSeek profile.
var DeepSeekChatProtocol = openAIChatProtocolWith(deepSeekProfile)

// DeepSeekChatHTTPRoute is DeepSeek's OpenAI-compatible Chat Completions route.
// It is distinct from `openai-compatible-chat` (vLLM) so that only DeepSeek
// models advertise the `file` block.
var DeepSeekChatHTTPRoute = route.MakeFromTransport(route.MakeTransportInput{
	ID:        "deepseek-chat",
	Protocol:  DeepSeekChatProtocol,
	Endpoint:  route.NewStaticPath(OpenAIChatPath),
	Transport: transport.SSEJson(),
})
