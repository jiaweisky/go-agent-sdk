package protocols

import (
	"slices"
	"strings"

	"github.com/jiaweisky/go-agent-sdk/internal/llm/protocols/utils"
	"github.com/jiaweisky/go-agent-sdk/pkg/llm/route"
	"github.com/jiaweisky/go-agent-sdk/pkg/llm/schema"
)

// AuthScheme is the credential convention a protocol expects on the wire.
type ProtocolAuthScheme string

const (
	AuthBearer ProtocolAuthScheme = "bearer"
	AuthHeader ProtocolAuthScheme = "header"
	AuthSigV4  ProtocolAuthScheme = "sigv4"
)

// MediaSource is where a media part's bytes come from (canonical to llm/schema).
type MediaSource = schema.MediaSource

const (
	MediaSourceInline = schema.MediaSourceInline
	MediaSourceURL    = schema.MediaSourceURL
	MediaSourceFile   = schema.MediaSourceFile
)

// ProtocolMediaSupport is a protocol's media descriptor: the MIME types its wire
// can carry and the sources/roles it accepts. It is the **single source of
// truth** the planner reads (empty Sources/Roles mean unconstrained). Lowering
// validates against the same descriptor, so the gate and lowering cannot drift.
type ProtocolMediaSupport struct {
	MIMEs   map[string]struct{}
	Sources []MediaSource
	Roles   []string
}

// AllowsMIME reports whether the wire can carry a MIME type.
func (s ProtocolMediaSupport) AllowsMIME(mime string) bool {
	_, ok := s.MIMEs[mime]
	return ok
}

// AllowsSource reports whether a source is accepted (empty = all).
func (s ProtocolMediaSupport) AllowsSource(source MediaSource) bool {
	return len(s.Sources) == 0 || slices.Contains(s.Sources, source)
}

// AllowsRole reports whether a message role may carry media (empty = all).
func (s ProtocolMediaSupport) AllowsRole(role string) bool {
	return len(s.Roles) == 0 || slices.Contains(s.Roles, role)
}

// Entry describes how to reach a protocol: the route that speaks it, the auth
// scheme that route expects, and the media its wire format can carry.
type ProtocolEntry struct {
	Route route.Route
	// Scheme is how the credential is applied.
	Scheme ProtocolAuthScheme
	// HeaderName is the header carrying the key when Scheme == AuthHeader.
	HeaderName string
	// Media is the protocol's media descriptor (single source of truth).
	Media ProtocolMediaSupport
}

// registry is the table of supported protocols, keyed by route id. It is
// populated once at init and read-only after.
var registry = map[schema.ProtocolID]ProtocolEntry{
	schema.ProtocolID(OpenAIResponsesHTTPRoute.ID): {
		Route: OpenAIResponsesHTTPRoute, Scheme: AuthBearer,
		Media: ProtocolMediaSupport{MIMEs: imageMIMESet, Sources: []MediaSource{MediaSourceInline, MediaSourceURL, MediaSourceFile}},
	},
	schema.ProtocolID(OpenAIChatHTTPRoute.ID): {
		Route: OpenAIChatHTTPRoute, Scheme: AuthBearer,
		Media: ProtocolMediaSupport{MIMEs: openAIChatProfile.MIMEs(), Sources: []MediaSource{MediaSourceInline, MediaSourceURL}},
	},
	schema.ProtocolID(OpenAICompatibleChatHTTPRoute.ID): {
		Route: OpenAICompatibleChatHTTPRoute, Scheme: AuthBearer,
		Media: ProtocolMediaSupport{MIMEs: openAICompatibleProfile.MIMEs(), Sources: []MediaSource{MediaSourceInline, MediaSourceURL}},
	},
	schema.ProtocolID(AnthropicMessagesHTTPRoute.ID): {
		Route:      AnthropicMessagesHTTPRoute,
		Scheme:     AuthHeader,
		HeaderName: "x-api-key",
		// Anthropic image blocks accept base64 or a URL only; provider file
		// references belong to the document block (not lowered yet), so a `file`
		// source is omitted rather than emitting an invalid image block.
		Media: ProtocolMediaSupport{MIMEs: imageMIMESet, Sources: []MediaSource{MediaSourceInline, MediaSourceURL}},
	},
	schema.ProtocolID(GeminiHTTPRoute.ID): {
		Route:      GeminiHTTPRoute,
		Scheme:     AuthHeader,
		HeaderName: "x-goog-api-key",
		Media:      ProtocolMediaSupport{MIMEs: geminiMediaMIMEs, Sources: []MediaSource{MediaSourceInline, MediaSourceURL, MediaSourceFile}},
	},
	schema.ProtocolID(BedrockConverseProtocol.ID): {
		Route:  BedrockConverseRoute(BedrockConverseProtocol),
		Scheme: AuthSigV4,
		Media:  ProtocolMediaSupport{MIMEs: mediaMIMESet(utils.BedrockMediaMIMEs()), Sources: []MediaSource{MediaSourceInline, MediaSourceFile}},
	},
}

// LookupProtocol returns the entry for a protocol id.
func LookupProtocol(id schema.ProtocolID) (ProtocolEntry, bool) {
	entry, ok := registry[id]
	return entry, ok
}

// MediaFeasible reports whether a media part of the given MIME is feasible for a
// protocol descriptor and a model's declared input modalities. An empty
// modelInput means the model declares no modalities, so only the wire constrains
// it. This is the planner primitive: the runner and the gate read the same
// descriptor.
func MediaFeasible(support ProtocolMediaSupport, modelInput []string, mime string) bool {
	mime = strings.ToLower(mime)
	if !support.AllowsMIME(mime) {
		return false
	}
	if len(modelInput) == 0 {
		return true
	}
	return slices.Contains(modelInput, utils.MediaModality(mime))
}

// MediaAllowed reports whether a protocol can put a media part of the given MIME
// type on the wire and the model accepts its modality.
func MediaAllowed(id schema.ProtocolID, modelInput []string, mime string) bool {
	entry, ok := LookupProtocol(id)
	if !ok {
		return false
	}
	return MediaFeasible(entry.Media, modelInput, mime)
}
