// Command notesmcp runs a small MCP server for persistent notes. With no flags
// it speaks MCP over stdio; with --http it serves streamable HTTP. It backs the
// chat example's MCP demo: the agent reads and writes notes through the tools,
// while prompts and resources expose the same store to the client.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	addr := flag.String("http", "", "serve streamable HTTP at this address (default: stdio)")
	data := flag.String("data", "notes.json", "path to the notes JSON file")
	flag.Parse()

	store := newStore(*data)
	if err := store.load(); err != nil {
		log.Fatalf("notes: %v", err)
	}
	server := newServer(store)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if *addr == "" {
		if err := server.Run(ctx, &sdk.StdioTransport{}); err != nil && !errors.Is(err, context.Canceled) {
			log.Fatalf("notes: %v", err)
		}
		return
	}
	serveHTTP(ctx, server, *addr)
}

// serveHTTP serves the server over streamable HTTP until ctx is canceled.
func serveHTTP(ctx context.Context, server *sdk.Server, addr string) {
	handler := sdk.NewStreamableHTTPHandler(func(*http.Request) *sdk.Server { return server }, nil)
	httpServer := &http.Server{Addr: addr, Handler: handler}
	go func() {
		<-ctx.Done()
		_ = httpServer.Shutdown(context.Background())
	}()
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("notes: %v", err)
	}
}

// newServer exposes the store as MCP tools, prompts and resources.
func newServer(s *store) *sdk.Server {
	server := sdk.NewServer(
		&sdk.Implementation{Name: "notes", Version: "0.1.0", Description: "Persistent notes for the go-agent-sdk chat example."},
		&sdk.ServerOptions{Instructions: "Persistent notes shared across sessions. Use the note_* tools to read and " +
			"write. Each note is also readable as notes://note/<id>, with an index at notes://index."},
	)
	registerTools(server, s)
	registerPrompts(server, s)
	registerResources(server, s)
	return server
}

// registerTools exposes the store as the note_* tools the model calls.
func registerTools(server *sdk.Server, s *store) {
	sdk.AddTool[noteSaveInput, any](server, &sdk.Tool{
		Name: "note_save", Description: "Create or overwrite a note.",
	}, func(_ context.Context, _ *sdk.CallToolRequest, in noteSaveInput) (*sdk.CallToolResult, any, error) {
		if err := s.set(in.ID, in.Text); err != nil {
			return nil, nil, err
		}
		return textResult("saved note %q", in.ID), nil, nil
	})

	sdk.AddTool[noteIDInput, any](server, &sdk.Tool{
		Name: "note_get", Description: "Read a note by id.",
	}, func(_ context.Context, _ *sdk.CallToolRequest, in noteIDInput) (*sdk.CallToolResult, any, error) {
		text, ok := s.get(in.ID)
		if !ok {
			return nil, nil, fmt.Errorf("unknown note: %s", in.ID)
		}
		return textResult("%s", text), nil, nil
	})

	sdk.AddTool[noteIDInput, any](server, &sdk.Tool{
		Name: "note_delete", Description: "Delete a note by id.",
	}, func(_ context.Context, _ *sdk.CallToolRequest, in noteIDInput) (*sdk.CallToolResult, any, error) {
		deleted, err := s.delete(in.ID)
		if err != nil {
			return nil, nil, err
		}
		if !deleted {
			return nil, nil, fmt.Errorf("unknown note: %s", in.ID)
		}
		return textResult("deleted note %q", in.ID), nil, nil
	})

	sdk.AddTool[emptyInput, any](server, &sdk.Tool{
		Name: "note_list", Description: "List every note id with its first line.",
	}, func(_ context.Context, _ *sdk.CallToolRequest, _ emptyInput) (*sdk.CallToolResult, any, error) {
		return textResult("%s", renderIndex(s)), nil, nil
	})
}

// registerPrompts exposes the store as prompt templates the client can fetch.
func registerPrompts(server *sdk.Server, s *store) {
	server.AddPrompt(&sdk.Prompt{
		Name: "notes_digest", Description: "Summarize the current notes into a short digest.",
	}, func(_ context.Context, _ *sdk.GetPromptRequest) (*sdk.GetPromptResult, error) {
		return userPrompt("Summarize these notes into a short digest:\n\n" + renderAll(s)), nil
	})

	server.AddPrompt(&sdk.Prompt{
		Name: "action_items", Description: "Extract action items from the current notes.",
	}, func(_ context.Context, _ *sdk.GetPromptRequest) (*sdk.GetPromptResult, error) {
		return userPrompt("List the action items in these notes, most important first:\n\n" + renderAll(s)), nil
	})
}

// registerResources exposes the store as an index resource and a per-note
// resource template.
func registerResources(server *sdk.Server, s *store) {
	server.AddResource(&sdk.Resource{
		URI: "notes://index", Name: "index", MIMEType: "text/markdown",
		Description: "Every note id with its first line.",
	}, func(_ context.Context, req *sdk.ReadResourceRequest) (*sdk.ReadResourceResult, error) {
		return textResource(req.Params.URI, renderIndex(s)), nil
	})

	server.AddResourceTemplate(&sdk.ResourceTemplate{
		URITemplate: "notes://note/{id}", Name: "note", MIMEType: "text/markdown",
		Description: "A single note by id.",
	}, func(_ context.Context, req *sdk.ReadResourceRequest) (*sdk.ReadResourceResult, error) {
		id := strings.TrimPrefix(req.Params.URI, "notes://note/")
		text, ok := s.get(id)
		if !ok {
			return nil, fmt.Errorf("unknown note: %s", id)
		}
		return textResource(req.Params.URI, text), nil
	})
}

// Tool inputs.
type (
	noteSaveInput struct {
		ID   string `json:"id" jsonschema:"Note id, e.g. todo"`
		Text string `json:"text" jsonschema:"Note body (markdown)"`
	}
	noteIDInput struct {
		ID string `json:"id" jsonschema:"Note id"`
	}
	emptyInput struct{}
)

func textResult(format string, args ...any) *sdk.CallToolResult {
	return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: fmt.Sprintf(format, args...)}}}
}

func textResource(uri, text string) *sdk.ReadResourceResult {
	return &sdk.ReadResourceResult{
		Contents: []*sdk.ResourceContents{{URI: uri, MIMEType: "text/markdown", Text: text}},
	}
}

func userPrompt(text string) *sdk.GetPromptResult {
	return &sdk.GetPromptResult{Messages: []*sdk.PromptMessage{{
		Role:    sdk.Role("user"),
		Content: &sdk.TextContent{Text: text},
	}}}
}

// renderIndex lists every note id with its first line.
func renderIndex(s *store) string {
	notes := s.snapshot()
	if len(notes) == 0 {
		return "(no notes)"
	}
	var b strings.Builder
	for _, id := range sortedIDs(notes) {
		fmt.Fprintf(&b, "- %s: %s\n", id, firstLine(notes[id]))
	}
	return b.String()
}

// renderAll prints every note's full body.
func renderAll(s *store) string {
	notes := s.snapshot()
	if len(notes) == 0 {
		return "(no notes)"
	}
	var b strings.Builder
	for _, id := range sortedIDs(notes) {
		fmt.Fprintf(&b, "## %s\n\n%s\n\n", id, notes[id])
	}
	return b.String()
}

func sortedIDs(notes map[string]string) []string {
	ids := make([]string, 0, len(notes))
	for id := range notes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func firstLine(text string) string {
	if i := strings.IndexByte(text, '\n'); i >= 0 {
		return text[:i]
	}
	return text
}

// store is a mutex-guarded set of notes persisted to a JSON file.
type store struct {
	mu    sync.Mutex
	path  string
	notes map[string]string
}

func newStore(path string) *store {
	return &store{path: path, notes: map[string]string{}}
}

func (s *store) load() error {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, &s.notes); err != nil {
		return err
	}
	if s.notes == nil { // a JSON "null" would leave the map nil
		s.notes = map[string]string{}
	}
	return nil
}

func (s *store) saveLocked() error {
	data, err := json.MarshalIndent(s.notes, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0o644)
}

func (s *store) set(id, text string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.notes[id] = text
	return s.saveLocked()
}

func (s *store) get(id string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	text, ok := s.notes[id]
	return text, ok
}

func (s *store) delete(id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.notes[id]; !ok {
		return false, nil
	}
	delete(s.notes, id)
	return true, s.saveLocked()
}

func (s *store) snapshot() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]string, len(s.notes))
	for id, text := range s.notes {
		out[id] = text
	}
	return out
}
