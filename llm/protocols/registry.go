package protocols

import (
	"slices"
	"strings"

	"github.com/jiaweisky/go-agent-sdk/llm/protocols/utils"
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

// Entry describes how to reach a protocol: the route that speaks it, the auth
// scheme that route expects, and the media types its wire format can carry.
type ProtocolEntry struct {
	Route route.Route
	// Scheme is how the credential is applied.
	Scheme ProtocolAuthScheme
	// HeaderName is the header carrying the key when Scheme == AuthHeader.
	HeaderName string
	// MediaMIMEs is the set of MIME types the protocol can send on the wire.
	MediaMIMEs map[string]struct{}
}

// registry is the table of supported protocols, keyed by route id. It is
// populated once at init and read-only after.
var registry = map[schema.ProtocolID]ProtocolEntry{
	schema.ProtocolID(OpenAIResponsesHTTPRoute.ID): {
		Route: OpenAIResponsesHTTPRoute, Scheme: AuthBearer, MediaMIMEs: imageMIMESet,
	},
	schema.ProtocolID(OpenAIChatHTTPRoute.ID): {
		Route: OpenAIChatHTTPRoute, Scheme: AuthBearer, MediaMIMEs: imageMIMESet,
	},
	schema.ProtocolID(OpenAICompatibleChatHTTPRoute.ID): {
		Route: OpenAICompatibleChatHTTPRoute, Scheme: AuthBearer, MediaMIMEs: imageMIMESet,
	},
	schema.ProtocolID(AnthropicMessagesHTTPRoute.ID): {
		Route:      AnthropicMessagesHTTPRoute,
		Scheme:     AuthHeader,
		HeaderName: "x-api-key",
		MediaMIMEs: imageMIMESet,
	},
	schema.ProtocolID(GeminiHTTPRoute.ID): {
		Route:      GeminiHTTPRoute,
		Scheme:     AuthHeader,
		HeaderName: "x-goog-api-key",
		MediaMIMEs: geminiMediaMIMEs,
	},
	schema.ProtocolID(BedrockConverseProtocol.ID): {
		Route:      BedrockConverseRoute(BedrockConverseProtocol),
		Scheme:     AuthSigV4,
		MediaMIMEs: mediaMIMESet(utils.BedrockMediaMIMEs()),
	},
}

// LookupProtocol returns the entry for a protocol id.
func LookupProtocol(id schema.ProtocolID) (ProtocolEntry, bool) {
	entry, ok := registry[id]
	return entry, ok
}

// MediaAllowed reports whether a protocol can put a media part of the given MIME
// type on the wire and the model accepts its modality. An empty modelInput means
// the model declares no input modalities, so only the protocol's wire support
// constrains it.
func MediaAllowed(id schema.ProtocolID, modelInput []string, mime string) bool {
	entry, ok := LookupProtocol(id)
	if !ok {
		return false
	}
	mime = strings.ToLower(mime)
	if _, ok := entry.MediaMIMEs[mime]; !ok {
		return false
	}
	if len(modelInput) == 0 {
		return true
	}
	return slices.Contains(modelInput, utils.MediaModality(mime))
}
