package mcp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/user/go-agent-sdk/core/tool"
	llmschema "github.com/user/go-agent-sdk/llm/schema"
)

func TestHTTPClientHeaders(t *testing.T) {
	var apiKey string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiKey = r.Header.Get("X-Api-Key")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	client := clientWithHeaders(map[string]string{"X-Api-Key": "k"})
	response, err := client.Get(server.URL)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	response.Body.Close()
	if apiKey != "k" {
		t.Fatalf("api key = %q", apiKey)
	}
}

func TestMCPInputSchemaNormalization(t *testing.T) {
	def := &sdk.Tool{
		Name:        "t",
		InputSchema: map[string]any{"properties": map[string]any{"a": map[string]any{"type": "string"}}},
	}
	schema := MCPInputSchema(def)
	if schema["type"] != "object" {
		t.Fatalf("type = %v", schema["type"])
	}
	if schema["additionalProperties"] != false {
		t.Fatalf("additionalProperties = %v", schema["additionalProperties"])
	}
	if _, ok := schema["properties"].(map[string]any); !ok {
		t.Fatalf("properties = %v", schema["properties"])
	}
	empty := MCPInputSchema(&sdk.Tool{Name: "t"})
	if _, ok := empty["properties"].(map[string]any); !ok {
		t.Fatalf("empty properties = %v", empty["properties"])
	}
}

func TestClassifyError(t *testing.T) {
	cases := []struct {
		message string
		status  string
	}{
		{"OAuth authorization required", "needs_auth"},
		{"dynamic client registration failed", "needs_client_registration"},
		{"connection refused", "failed"},
	}
	for _, tc := range cases {
		if got := classifyError(errors.New(tc.message)); got.Status != tc.status {
			t.Fatalf("classify(%q) = %q, want %q", tc.message, got.Status, tc.status)
		}
	}
}

func TestClientPromptsAccessor(t *testing.T) {
	ctx := context.Background()
	server := sdk.NewServer(&sdk.Implementation{Name: "test-server", Version: "1.0.0"}, nil)
	server.AddPrompt(&sdk.Prompt{Name: "greet", Description: "greeting"}, func(context.Context, *sdk.GetPromptRequest) (*sdk.GetPromptResult, error) {
		return &sdk.GetPromptResult{}, nil
	})
	serverTransport, clientTransport := sdk.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer serverSession.Close()

	client := NewClient(t.TempDir(), "test")
	defer client.Close()
	if status := client.ConnectTransport(ctx, "srv", clientTransport, 5*time.Second); status.Status != "connected" {
		t.Fatalf("status = %+v", status)
	}
	prompts, err := client.Prompts(ctx)
	if err != nil {
		t.Fatalf("prompts: %v", err)
	}
	if got := prompts["srv:greet"]; got.Description != "greeting" || got.Client != "srv" {
		t.Fatalf("prompt = %+v", got)
	}
}

func TestSanitizeAndToolName(t *testing.T) {
	if got := Sanitize("my server/tools"); got != "my_server_tools" {
		t.Fatalf("sanitize = %q", got)
	}
	if got := ToolName("my server", "read.file"); got != "my_server_read_file" {
		t.Fatalf("tool name = %q", got)
	}
}

type echoInput struct {
	Text string `json:"text"`
}

func TestAdaptMCPToolEndToEnd(t *testing.T) {
	ctx := context.Background()
	server := sdk.NewServer(&sdk.Implementation{Name: "test-server", Version: "1.0.0"}, nil)
	sdk.AddTool(server, &sdk.Tool{Name: "echo", Description: "echo text"},
		func(_ context.Context, _ *sdk.CallToolRequest, input echoInput) (*sdk.CallToolResult, any, error) {
			return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: input.Text}}}, nil, nil
		})
	serverTransport, clientTransport := sdk.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer serverSession.Close()

	client := NewClient(t.TempDir(), "test")
	defer client.Close()
	status := client.ConnectTransport(ctx, "srv", clientTransport, 5*time.Second)
	if status.Status != "connected" {
		t.Fatalf("status = %+v", status)
	}

	tools, err := client.Tools(ctx)
	if err != nil {
		t.Fatalf("tools: %v", err)
	}
	adapted, ok := tools["srv_echo"]
	if !ok {
		t.Fatalf("tools = %+v", tools)
	}

	coreTool := adapted.CoreTool()
	output, err := tool.Settle(coreTool, ctx, llmschema.ToolCallPart{
		Type: "tool-call", ID: "c1", Name: "srv_echo",
		Input: map[string]any{"text": "hello mcp"},
	}, tool.Context{SessionID: "ses"})
	if err != nil {
		t.Fatalf("settle: %v", err)
	}
	if len(output.Content) == 0 {
		t.Fatalf("no content: %+v", output)
	}
	text, ok := output.Content[0].(llmschema.ToolTextContent)
	if !ok || !strings.Contains(text.Text, "hello mcp") {
		t.Fatalf("content = %+v", output.Content)
	}
}
