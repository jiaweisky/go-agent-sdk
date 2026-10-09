package mcp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jiaweisky/go-agent-sdk/pkg/core/tool"
	llmschema "github.com/jiaweisky/go-agent-sdk/pkg/llm/schema"
	"github.com/modelcontextprotocol/go-sdk/auth"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
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
		name   string
		err    error
		status string
	}{
		{"auth message", errors.New("OAuth authorization required"), StatusNeedsAuth},
		{"registration message", errors.New("dynamic client registration failed"), StatusNeedsClientRegistration},
		{"connection refused", errors.New("connection refused"), StatusFailed},
		{
			"typed registration",
			&oauthex.ClientRegistrationError{ErrorCode: "invalid_redirect_uri"},
			StatusNeedsClientRegistration,
		},
		{"typed oauth", fmt.Errorf("connect: %w", auth.ErrOAuth), StatusNeedsAuth},
	}
	for _, tc := range cases {
		if got := classifyError(tc.err); got.Status != tc.status {
			t.Fatalf("classify(%s) = %q, want %q", tc.name, got.Status, tc.status)
		}
	}
}

func TestClientCatalogAccessors(t *testing.T) {
	ctx := context.Background()
	server := sdk.NewServer(&sdk.Implementation{Name: "test-server", Version: "1.0.0"},
		&sdk.ServerOptions{Instructions: "use carefully"})
	server.AddPrompt(&sdk.Prompt{Name: "greet", Description: "greeting"}, func(context.Context, *sdk.GetPromptRequest) (*sdk.GetPromptResult, error) {
		return &sdk.GetPromptResult{Description: "hi"}, nil
	})
	server.AddResource(&sdk.Resource{Name: "readme", URI: "file:///readme", MIMEType: "text/plain"},
		func(context.Context, *sdk.ReadResourceRequest) (*sdk.ReadResourceResult, error) {
			return &sdk.ReadResourceResult{Contents: []*sdk.ResourceContents{
				{URI: "file:///readme", MIMEType: "text/plain", Text: "hello"},
			}}, nil
		})
	serverTransport, clientTransport := sdk.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer serverSession.Close()

	client := NewClient(t.TempDir(), "test-client", "test")
	defer client.Close()
	if status := client.ConnectTransport(ctx, "srv", clientTransport, 5*time.Second); status.Status != "connected" {
		t.Fatalf("status = %+v", status)
	}

	prompts := client.Prompts(ctx)
	if got := prompts["srv:greet"]; got.Description != "greeting" || got.Client != "srv" {
		t.Fatalf("prompt = %+v", got)
	}
	prompt, err := client.GetPrompt(ctx, "srv", "greet", nil)
	if err != nil || prompt == nil || prompt.Description != "hi" {
		t.Fatalf("get prompt = %+v, %v", prompt, err)
	}
	if filtered := client.Prompts(ctx, "srv"); len(filtered) != 1 {
		t.Fatalf("filtered prompts = %+v", filtered)
	}
	if none := client.Prompts(ctx, "other"); len(none) != 0 {
		t.Fatalf("unmatched prompts = %+v", none)
	}

	resources := client.Resources(ctx)
	if got := resources["srv:file:///readme"]; got.URI != "file:///readme" || got.Name != "readme" {
		t.Fatalf("resource = %+v", got)
	}
	resource, err := client.ReadResource(ctx, "srv", "file:///readme")
	if err != nil || len(resource.Contents) == 0 || resource.Contents[0].Text != "hello" {
		t.Fatalf("read resource = %+v, %v", resource, err)
	}

	instructions := client.Instructions()
	if len(instructions) != 1 || instructions[0].Name != "srv" || instructions[0].Instructions != "use carefully" {
		t.Fatalf("instructions = %+v", instructions)
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

	client := NewClient(t.TempDir(), "test-client", "test")
	defer client.Close()
	status := client.ConnectTransport(ctx, "srv", clientTransport, 5*time.Second)
	if status.Status != "connected" {
		t.Fatalf("status = %+v", status)
	}

	tools := client.Tools(ctx)
	adapted, ok := tools["srv_echo"]
	if !ok {
		t.Fatalf("tools = %+v", tools)
	}
	if adapted.Timeout != 5*time.Second {
		t.Fatalf("adapted.Timeout = %v, want the connect timeout 5s", adapted.Timeout)
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

func TestDisconnectClearsCachedTools(t *testing.T) {
	ctx := context.Background()
	client := NewClient(t.TempDir(), "test-client", "test")
	defer client.Close()

	connect := func(toolName string) {
		t.Helper()
		server := sdk.NewServer(&sdk.Implementation{Name: "srv", Version: "1.0.0"}, nil)
		sdk.AddTool(server, &sdk.Tool{Name: toolName},
			func(_ context.Context, _ *sdk.CallToolRequest, _ echoInput) (*sdk.CallToolResult, any, error) {
				return &sdk.CallToolResult{}, nil, nil
			})
		serverTransport, clientTransport := sdk.NewInMemoryTransports()
		serverSession, err := server.Connect(ctx, serverTransport, nil)
		if err != nil {
			t.Fatalf("server connect: %v", err)
		}
		t.Cleanup(func() { _ = serverSession.Close() })
		if status := client.ConnectTransport(ctx, "srv", clientTransport, 5*time.Second); status.Status != StatusConnected {
			t.Fatalf("status = %+v", status)
		}
	}

	connect("old")
	tools := client.Tools(ctx)
	if _, ok := tools["srv_old"]; !ok {
		t.Fatalf("tools = %+v, want srv_old", tools)
	}
	client.Disconnect("srv")

	connect("new")
	tools = client.Tools(ctx)
	if _, ok := tools["srv_old"]; ok {
		t.Fatalf("stale tool after reconnect: %+v", tools)
	}
	if _, ok := tools["srv_new"]; !ok {
		t.Fatalf("tools = %+v, want srv_new", tools)
	}
}

func TestServerInfoFromInitialize(t *testing.T) {
	ctx := context.Background()
	server := sdk.NewServer(&sdk.Implementation{
		Name: "weather", Title: "Weather", Description: "A demo weather server", Version: "1.0.0",
	}, nil)
	clientTransport, serverTransport := sdk.NewInMemoryTransports()
	go func() { _ = server.Run(ctx, serverTransport) }()

	client := NewClient("", "test-client", "test")
	defer client.Close()
	if status := client.ConnectTransport(ctx, "weather", clientTransport, 5*time.Second); status.Status != "connected" {
		t.Fatalf("connect status = %+v", status)
	}

	info, ok := client.ServerInfo("weather")
	if !ok {
		t.Fatal("ServerInfo(weather) not found")
	}
	if info.Name != "weather" || info.Title != "Weather" || info.Description != "A demo weather server" || info.Version != "1.0.0" {
		t.Fatalf("info = %+v", info)
	}
	if _, ok := client.ServerInfo("missing"); ok {
		t.Fatal("ServerInfo(missing) should be false")
	}
}
