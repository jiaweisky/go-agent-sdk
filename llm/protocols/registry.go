package protocols

import (
	"github.com/jiaweisky/go-agent-sdk/llm/route"
	"github.com/jiaweisky/go-agent-sdk/llm/schema"
)

// AuthScheme is the credential convention a protocol expects on the wire.
type ProtocolAuthScheme string

const (
	AuthBearer ProtocolAuthScheme = "bearer"
	AuthHeader ProtocolAuthScheme = "header"
	AuthSigV4  ProtocolAuthScheme = "sigv4"
)

// Entry describes how to reach a protocol: the route that speaks it and the
// auth scheme that route expects.
type ProtocolEntry struct {
	Route route.Route
	// Scheme is how the credential is applied.
	Scheme ProtocolAuthScheme
	// HeaderName is the header carrying the key when Scheme == AuthHeader.
	HeaderName string
}

// registry is the table of supported protocols, keyed by route id. It is
// populated once at init and read-only after.
var registry = map[schema.ProtocolID]ProtocolEntry{
	schema.ProtocolID(OpenAIResponsesHTTPRoute.ID):      {Route: OpenAIResponsesHTTPRoute, Scheme: AuthBearer},
	schema.ProtocolID(OpenAIChatHTTPRoute.ID):           {Route: OpenAIChatHTTPRoute, Scheme: AuthBearer},
	schema.ProtocolID(OpenAICompatibleChatHTTPRoute.ID): {Route: OpenAICompatibleChatHTTPRoute, Scheme: AuthBearer},
	schema.ProtocolID(AnthropicMessagesHTTPRoute.ID):    {Route: AnthropicMessagesHTTPRoute, Scheme: AuthHeader, HeaderName: "x-api-key"},
	schema.ProtocolID(GeminiHTTPRoute.ID):               {Route: GeminiHTTPRoute, Scheme: AuthHeader, HeaderName: "x-goog-api-key"},
	schema.ProtocolID(BedrockConverseProtocol.ID):       {Route: BedrockConverseRoute(BedrockConverseProtocol), Scheme: AuthSigV4},
}

// LookupProtocol returns the entry for a protocol id.
func LookupProtocol(id schema.ProtocolID) (ProtocolEntry, bool) {
	entry, ok := registry[id]
	return entry, ok
}
