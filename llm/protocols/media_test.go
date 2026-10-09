package protocols

import (
	"testing"

	"github.com/jiaweisky/go-agent-sdk/llm/schema"
)

func TestMediaAllowedNarrowsByProtocolThenModel(t *testing.T) {
	cases := []struct {
		name    string
		proto   schema.ProtocolID
		input   []string
		mime    string
		allowed bool
	}{
		{"protocol and model allow an image", OpenAIChatHTTPRoute.Protocol, nil, "image/png", true},
		{"protocol rejects a document", OpenAIChatHTTPRoute.Protocol, nil, "application/pdf", false},
		{"bedrock carries a document", BedrockConverseProtocol.ID, nil, "application/pdf", true},
		{"gemini does not lower a document", GeminiHTTPRoute.Protocol, nil, "application/pdf", false},
		{"anthropic does not lower a document", AnthropicMessagesHTTPRoute.Protocol, nil, "application/pdf", false},
		{"text-only model rejects an image", OpenAIChatHTTPRoute.Protocol, []string{"text"}, "image/png", false},
		{"vision model allows an image", OpenAIChatHTTPRoute.Protocol, []string{"text", "image"}, "image/png", true},
		{"gemini carries audio", GeminiHTTPRoute.Protocol, nil, "audio/wav", true},
		{"anthropic carries jpeg", AnthropicMessagesHTTPRoute.Protocol, nil, "image/jpeg", true},
		{"unknown protocol allows nothing", schema.ProtocolID("nope"), nil, "image/png", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := MediaAllowed(tc.proto, tc.input, tc.mime); got != tc.allowed {
				t.Fatalf("MediaAllowed(%s, %v, %s) = %v, want %v", tc.proto, tc.input, tc.mime, got, tc.allowed)
			}
		})
	}
}

func TestMediaFeasibleDescriptor(t *testing.T) {
	support := ProtocolMediaSupport{
		MIMEs:   imageMIMESet,
		Sources: []MediaSource{MediaSourceInline, MediaSourceURL},
		Roles:   []string{"user"},
	}
	if !MediaFeasible(support, nil, "image/png") {
		t.Fatal("image should be feasible")
	}
	if MediaFeasible(support, nil, "audio/wav") {
		t.Fatal("audio is not in the MIME set")
	}
	if MediaFeasible(support, []string{"text"}, "image/png") {
		t.Fatal("a text-only model rejects images")
	}
	if !support.AllowsSource(MediaSourceInline) || support.AllowsSource(MediaSourceFile) {
		t.Fatalf("sources = %v", support.Sources)
	}
	if !support.AllowsRole("user") || support.AllowsRole("tool") {
		t.Fatalf("roles = %v", support.Roles)
	}
	// Empty Sources/Roles mean unconstrained.
	unconstrained := ProtocolMediaSupport{MIMEs: imageMIMESet}
	if !unconstrained.AllowsSource(MediaSourceFile) || !unconstrained.AllowsRole("tool") {
		t.Fatal("empty sources/roles should be unconstrained")
	}
}
