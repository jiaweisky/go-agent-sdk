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
