package main

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"

	agent "github.com/jiaweisky/go-agent-sdk"
	"github.com/jiaweisky/go-agent-sdk/schema"
)

func newTestClient(t *testing.T) *agent.Client {
	t.Helper()
	client, err := agent.NewClient(&agent.ClientOptions{
		Root:        t.TempDir(),
		Model:       "test-model",
		Protocol:    "openai-responses",
		Credentials: agent.APIKey("test"),
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func TestExposeAgentsUnknown(t *testing.T) {
	client := newTestClient(t)
	if _, err := exposeAgents(client, []string{"does-not-exist"}, nil); err == nil {
		t.Fatal("expected an error for an unknown agent")
	}
}

func TestExposeAgentsBuild(t *testing.T) {
	client := newTestClient(t)
	exposed, err := exposeAgents(client, []string{"build"}, nil)
	if err != nil {
		t.Fatalf("exposeAgents: %v", err)
	}
	if len(exposed) != 1 || exposed[0].Expose == nil {
		t.Fatalf("exposed = %+v, want build with Expose set", exposed)
	}
}

func TestExposeAgentsCardOverride(t *testing.T) {
	client := newTestClient(t)
	exposed, err := exposeAgents(client, []string{"build"}, map[string]cardConfig{
		"build": {Name: "Custom", Description: "custom card"},
	})
	if err != nil {
		t.Fatalf("exposeAgents: %v", err)
	}
	if exposed[0].Expose == nil || exposed[0].Expose.Name != "Custom" || exposed[0].Expose.Description != "custom card" {
		t.Fatalf("expose = %+v, want the card override", exposed[0].Expose)
	}
}

func TestRunServesCard(t *testing.T) {
	addr := freeAddr(t)
	cfg := defaultConfig()
	cfg.Listen = addr
	cfg.Root = t.TempDir()
	cfg.Model = "test-model"
	cfg.Protocol = "openai-responses"
	cfg.Credentials = credentialsConfig{APIKey: "test"}
	cfg.KeepAlive = ""

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- run(ctx, cfg) }()

	url := "http://" + addr + "/.well-known/agent-card.json"
	var lastErr error
	for i := 0; i < 200; i++ {
		response, err := http.Get(url)
		if err == nil {
			response.Body.Close()
			if response.StatusCode == http.StatusOK {
				lastErr = nil
				break
			}
			lastErr = err
		} else {
			lastErr = err
		}
		time.Sleep(20 * time.Millisecond)
	}

	cancel()
	if err := <-done; err != nil {
		t.Fatalf("run: %v", err)
	}
	if lastErr != nil {
		t.Fatalf("card not served: %v", lastErr)
	}
}

func TestCredentialProviderAWS(t *testing.T) {
	cfg := defaultConfig()
	cfg.Credentials = credentialsConfig{AWS: &awsCredentials{
		Region: "us-east-1", AccessKeyID: "AK", SecretAccessKey: "SK", SessionToken: "ST",
	}}
	credential, err := credentialProvider(cfg)(schema.ProviderID("bedrock-converse"))
	if err != nil {
		t.Fatalf("credentialProvider: %v", err)
	}
	if credential.AWS == nil || credential.AWS.Region != "us-east-1" ||
		credential.AWS.AccessKeyID != "AK" || credential.AWS.SecretAccessKey != "SK" || credential.AWS.SessionToken != "ST" {
		t.Fatalf("credential = %+v", credential)
	}
}

func TestModelInfoProviderBedrockEndpoint(t *testing.T) {
	cfg := defaultConfig()
	cfg.Protocol = "bedrock-converse"
	cfg.Credentials = credentialsConfig{AWS: &awsCredentials{Region: "eu-west-1", AccessKeyID: "AK", SecretAccessKey: "SK"}}
	info, err := modelInfoProvider(cfg)("anthropic.claude-3-5-sonnet")
	if err != nil {
		t.Fatalf("modelInfoProvider: %v", err)
	}
	if info.API.URL == nil || *info.API.URL != "https://bedrock-runtime.eu-west-1.amazonaws.com" {
		t.Fatalf("endpoint = %v, want the region-derived Bedrock endpoint", info.API.URL)
	}
}

func freeAddr(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()
	return listener.Addr().String()
}
