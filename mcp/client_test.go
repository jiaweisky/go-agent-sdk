package mcp

import (
	"context"
	"strings"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/user/go-agent-sdk/core/tool"
	llmschema "github.com/user/go-agent-sdk/llm/schema"
)

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
