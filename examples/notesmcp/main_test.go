package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestServerSurfaces drives the server through an in-memory MCP client: a tool
// round-trip, a prompt, and both a static and a templated resource.
func TestServerSurfaces(t *testing.T) {
	ctx := context.Background()
	store := newStore(filepath.Join(t.TempDir(), "notes.json"))
	server := newServer(store)

	serverTransport, clientTransport := sdk.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer serverSession.Close()

	client := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "0"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer clientSession.Close()

	call, err := clientSession.CallTool(ctx, &sdk.CallToolParams{
		Name: "note_save", Arguments: map[string]any{"id": "todo", "text": "buy milk\nand eggs"},
	})
	if err != nil || call.IsError {
		t.Fatalf("note_save: %v %+v", err, call)
	}
	call, err = clientSession.CallTool(ctx, &sdk.CallToolParams{Name: "note_list"})
	if err != nil {
		t.Fatalf("note_list: %v", err)
	}
	if text := contentText(call); !strings.Contains(text, "todo: buy milk") {
		t.Fatalf("note_list = %q", text)
	}

	prompt, err := clientSession.GetPrompt(ctx, &sdk.GetPromptParams{Name: "notes_digest"})
	if err != nil || len(prompt.Messages) == 0 {
		t.Fatalf("notes_digest: %v %+v", err, prompt)
	}
	if text, ok := prompt.Messages[0].Content.(*sdk.TextContent); !ok || !strings.Contains(text.Text, "buy milk") {
		t.Fatalf("prompt content = %+v", prompt.Messages[0].Content)
	}

	index, err := clientSession.ReadResource(ctx, &sdk.ReadResourceParams{URI: "notes://index"})
	if err != nil || len(index.Contents) == 0 || !strings.Contains(index.Contents[0].Text, "todo") {
		t.Fatalf("index resource: %v %+v", err, index)
	}
	note, err := clientSession.ReadResource(ctx, &sdk.ReadResourceParams{URI: "notes://note/todo"})
	if err != nil || len(note.Contents) == 0 || !strings.Contains(note.Contents[0].Text, "buy milk") {
		t.Fatalf("note resource: %v %+v", err, note)
	}

	templates, err := clientSession.ListResourceTemplates(ctx, nil)
	if err != nil || len(templates.ResourceTemplates) == 0 {
		t.Fatalf("resource templates: %v %+v", err, templates)
	}
}

func contentText(result *sdk.CallToolResult) string {
	var b strings.Builder
	for _, content := range result.Content {
		if text, ok := content.(*sdk.TextContent); ok {
			b.WriteString(text.Text)
		}
	}
	return b.String()
}
