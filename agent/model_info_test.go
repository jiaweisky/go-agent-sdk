package agent

import (
	"testing"

	"github.com/user/go-agent-sdk/llm/route"
	"github.com/user/go-agent-sdk/schema"
)

func TestModelInfoProvider(t *testing.T) {
	resolve, err := buildModelResolver(&ClientOptions{
		Protocol: "openai-responses", Credentials: APIKey("k"),
		Model: "gpt-4o",
		ModelInfo: func(id string) (schema.ModelInfo, error) {
			return NewModelInfo("openai-responses", id, "", schema.ModelLimit{Context: 128_000, Output: 8_192})
		},
	})
	if err != nil {
		t.Fatalf("resolver: %v", err)
	}
	model, err := resolve(schema.SessionInfo{})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	limits := model.Route.Defaults.Limits
	if limits == nil || limits.Context != 128_000 || limits.Output != 8_192 {
		t.Fatalf("limits = %+v", limits)
	}
}

func TestModelInfoProviderUsesSessionModel(t *testing.T) {
	var seen string
	resolve, err := buildModelResolver(&ClientOptions{
		Protocol: "openai-responses", Credentials: APIKey("k"),
		Model: "default",
		ModelInfo: func(id string) (schema.ModelInfo, error) {
			seen = id
			return NewModelInfo("anthropic-messages", id, "", schema.ModelLimit{Context: 200_000, Output: 4_096})
		},
	})
	if err != nil {
		t.Fatalf("resolver: %v", err)
	}
	if _, err := resolve(schema.SessionInfo{Model: &schema.ModelRef{ID: "claude-sonnet"}}); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if seen != "claude-sonnet" {
		t.Fatalf("resolved id = %q, want claude-sonnet", seen)
	}
}

func TestNewModelInfoRejectsUnknownProtocol(t *testing.T) {
	if _, err := NewModelInfo("bogus", "m", "", schema.ModelLimit{}); err == nil {
		t.Fatal("expected an error for an unknown protocol")
	}
	if _, err := NewModelInfo("gemini", "m", "", schema.ModelLimit{}); err != nil {
		t.Fatalf("gemini should be registered: %v", err)
	}
}

func TestModelInfoProviderAppliesCredential(t *testing.T) {
	resolve, err := buildModelResolver(&ClientOptions{
		Model: "gemini-2.0-flash",
		ModelInfo: func(id string) (schema.ModelInfo, error) {
			return NewModelInfo("gemini", id, "", schema.ModelLimit{})
		},
		Credentials: func(providerID schema.ProviderID) (schema.Credential, error) {
			return schema.Credential{APIKey: "gemini-key"}, nil
		},
	})
	if err != nil {
		t.Fatalf("resolver: %v", err)
	}
	model, err := resolve(schema.SessionInfo{})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	headers, err := model.Route.Auth.Apply(route.AuthInput{Headers: map[string]string{}})
	if err != nil {
		t.Fatalf("auth: %v", err)
	}
	if headers["x-goog-api-key"] != "gemini-key" {
		t.Fatalf("headers = %+v", headers)
	}
}

func TestProtocolResolvesNativeRoutes(t *testing.T) {
	cases := []struct {
		protocol schema.ProtocolID
		routeID  string
		header   string
	}{
		{"openai-responses", "openai-responses", "authorization"},
		{"anthropic-messages", "anthropic-messages", "x-api-key"},
		{"gemini", "gemini", "x-goog-api-key"},
	}
	for _, tc := range cases {
		resolve, err := buildModelResolver(&ClientOptions{
			Model: "m", Protocol: tc.protocol, Credentials: APIKey("k"),
		})
		if err != nil {
			t.Fatalf("%s: resolver: %v", tc.protocol, err)
		}
		model, err := resolve(schema.SessionInfo{})
		if err != nil {
			t.Fatalf("%s: resolve: %v", tc.protocol, err)
		}
		if model.Route.ID != tc.routeID {
			t.Fatalf("%s: route = %s, want %s", tc.protocol, model.Route.ID, tc.routeID)
		}
		headers, err := model.Route.Auth.Apply(route.AuthInput{Headers: map[string]string{}})
		if err != nil {
			t.Fatalf("%s: auth: %v", tc.protocol, err)
		}
		if headers[tc.header] == "" {
			t.Fatalf("%s: headers = %+v, want %s", tc.protocol, headers, tc.header)
		}
	}
}

func TestBedrockSigV4Credential(t *testing.T) {
	resolve, err := buildModelResolver(&ClientOptions{
		Model:    "claude-3",
		Protocol: "bedrock-converse",
		Credentials: func(schema.ProviderID) (schema.Credential, error) {
			return schema.Credential{AWS: &schema.AWSCredentials{
				Region: "us-east-1", AccessKeyID: "AKIA", SecretAccessKey: "secret",
			}}, nil
		},
	})
	if err != nil {
		t.Fatalf("resolver: %v", err)
	}
	model, err := resolve(schema.SessionInfo{})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	headers, err := model.Route.Auth.Apply(route.AuthInput{
		URL:     "https://bedrock-runtime.us-east-1.amazonaws.com/model/x/converse",
		Body:    "{}",
		Headers: map[string]string{},
	})
	if err != nil {
		t.Fatalf("auth: %v", err)
	}
	if headers["Authorization"] == "" && headers["authorization"] == "" {
		t.Fatalf("expected a SigV4 authorization header, got %+v", headers)
	}
}
