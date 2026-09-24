package protocols

import (
	"github.com/user/go-agent-sdk/llm/route"
	"github.com/user/go-agent-sdk/llm/schema"
)

// AuthScheme is the credential convention a protocol expects on the wire.
type AuthScheme string

const (
	AuthNone   AuthScheme = "none"
	AuthBearer AuthScheme = "bearer"
	AuthHeader AuthScheme = "header"
	AuthSigV4  AuthScheme = "sigv4"
)

// Entry describes how to reach a protocol: the route that speaks it and the
// auth scheme that route expects.
type Entry struct {
	Route route.Route
	// Scheme is how the credential is applied.
	Scheme AuthScheme
	// HeaderName is the header carrying the key when Scheme == AuthHeader.
	HeaderName string
}

// registry is the table of supported protocols, keyed by route id (each protocol
// has exactly one HTTP route). It is populated once at init and read-only after.
var registry = map[schema.ProtocolID]Entry{
	schema.ProtocolID(OpenAIResponsesHTTPRoute.ID):      {Route: OpenAIResponsesHTTPRoute, Scheme: AuthBearer},
	schema.ProtocolID(OpenAIChatHTTPRoute.ID):           {Route: OpenAIChatHTTPRoute, Scheme: AuthBearer},
	schema.ProtocolID(OpenAICompatibleChatHTTPRoute.ID): {Route: OpenAICompatibleChatHTTPRoute, Scheme: AuthBearer},
	schema.ProtocolID(AnthropicMessagesHTTPRoute.ID):    {Route: AnthropicMessagesHTTPRoute, Scheme: AuthHeader, HeaderName: "x-api-key"},
	schema.ProtocolID(GeminiHTTPRoute.ID):               {Route: GeminiHTTPRoute, Scheme: AuthHeader, HeaderName: "x-goog-api-key"},
	schema.ProtocolID(BedrockConverseProtocol.ID):       {Route: BedrockConverseRoute(BedrockConverseProtocol), Scheme: AuthSigV4},
}

// Lookup returns the entry for a protocol id.
func Lookup(id schema.ProtocolID) (Entry, bool) {
	entry, ok := registry[id]
	return entry, ok
}
