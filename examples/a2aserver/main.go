// Command a2aserver exposes a go-agent-sdk agent over A2A (inbound) and then
// calls it with the SDK's own A2A client, so the whole round trip stays inside
// the SDK. It shows what the chat example only shows from the client side:
//
//   - the SDK as an A2A *server*: a2a/server + NewA2AFactory + Server.Handler,
//     with durable task storage on the core turn table;
//   - multi-turn context: the caller and the server keep one A2A context, so
//     the exposed agent sees the earlier turns;
//   - human-in-the-loop: the agent asks a question, the caller answers, and the
//     turn resumes (A2A input-required).
//
// The agent uses a deterministic, credential-free stub model, so the example
// runs without any provider key.
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"iter"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	agent "github.com/jiaweisky/go-agent-sdk"
	"github.com/jiaweisky/go-agent-sdk/a2a/server"
	llmschema "github.com/jiaweisky/go-agent-sdk/llm/schema"
	"github.com/jiaweisky/go-agent-sdk/schema"
)

func main() {
	addr := flag.String("http", "127.0.0.1:0", "address to serve A2A on")
	flag.Parse()

	// A fresh state root per run keeps each run deterministic and re-runnable;
	// the server's durability (turn table) still applies within the run. The
	// workspace is also temporary, so the example neither needs nor touches a repo.
	root, err := os.MkdirTemp("", "a2aserver-*")
	if err != nil {
		log.Fatalf("a2aserver: %v", err)
	}
	defer os.RemoveAll(root)

	serverClient, err := newServerClient(root)
	if err != nil {
		log.Fatalf("a2aserver: %v", err)
	}
	defer serverClient.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	base, shutdown, err := serve(*addr, serverClient)
	if err != nil {
		log.Fatalf("a2aserver: %v", err)
	}
	defer shutdown()
	log.Printf("go-agent-sdk A2A server listening at %s", base)
	log.Printf("agent card: %s/.well-known/agent-card.json", base)

	scanner := bufio.NewScanner(os.Stdin)
	lines := readLines(scanner)
	session, closeCaller, err := dial(ctx, filepath.Join(root, "caller"), base, askOnConsole(lines))
	if err != nil {
		log.Fatalf("a2aserver: %v", err)
	}
	defer closeCaller()

	fmt.Println("Type a prompt and press Enter (say \"ask\" to trigger a question). Ctrl+D to exit.")
	for {
		fmt.Print("\n> ")
		select {
		case <-ctx.Done():
			log.Print("interrupted; shutting down")
			return
		case line, ok := <-lines:
			if !ok {
				log.Print("stdin closed; shutting down")
				return
			}
			prompt := strings.TrimSpace(line)
			if prompt == "" {
				continue
			}
			reply, err := send(ctx, session, prompt)
			if err != nil {
				log.Printf("turn: %v", err)
				continue
			}
			fmt.Printf("< %s\n", reply)
		}
	}
}

// readLines feeds stdin lines to a channel so the loop can also observe
// cancellation while waiting for input.
func readLines(scanner *bufio.Scanner) <-chan string {
	lines := make(chan string)
	go func() {
		defer close(lines)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
	}()
	return lines
}

// dial opens a caller session that drives the exposed agent over A2A, answering
// any question through the given handler.
func dial(ctx context.Context, root, base string, question agent.QuestionHandlerFunc) (*agent.Session, func(), error) {
	options := clientOptions(root)
	options.Question = question
	caller, err := agent.NewClient(options)
	if err != nil {
		return nil, nil, err
	}
	if err := caller.RegisterAgent("assistant", agent.AgentConfig{
		Backend: &schema.AgentBackend{
			Type: schema.AgentBackendA2A,
			A2A:  &schema.AgentA2ABackend{Endpoint: base},
		},
	}); err != nil {
		caller.Close()
		return nil, nil, err
	}
	session, err := caller.CreateSession(ctx, &agent.SessionConfig{Agent: "assistant"})
	if err != nil {
		caller.Close()
		return nil, nil, err
	}
	return session, func() { session.Close(); caller.Close() }, nil
}

// askOnConsole prompts for a question's answer on the terminal, reading from the
// same line channel the loop uses.
func askOnConsole(lines <-chan string) agent.QuestionHandlerFunc {
	return func(request agent.QuestionRequest) ([]schema.QuestionAnswer, error) {
		answers := make([]schema.QuestionAnswer, 0, len(request.Questions))
		for _, question := range request.Questions {
			fmt.Printf("\n? %s\n", question.Question)
			for i, option := range question.Options {
				fmt.Printf("  %d) %s - %s\n", i+1, option.Label, option.Description)
			}
			fmt.Print("answer: ")
			answer, ok := <-lines
			if !ok {
				return answers, fmt.Errorf("no answer")
			}
			answers = append(answers, schema.QuestionAnswer{strings.TrimSpace(answer)})
		}
		return answers, nil
	}
}

// newServerClient builds the agent client that the A2A server exposes. Its model
// is a deterministic stub, so no credentials are needed.
func newServerClient(root string) (*agent.Client, error) {
	options := clientOptions(filepath.Join(root, "server"))
	options.WorkingDirectory = filepath.Join(root, "workspace")
	options.LLM = deterministicModel{}
	client, err := agent.NewClient(options)
	if err != nil {
		return nil, err
	}
	if err := client.RegisterAgent("assistant", agent.AgentConfig{
		Description: "A deterministic go-agent-sdk agent exposed over A2A.",
		Expose:      &schema.AgentExpose{Name: "assistant"},
		// Asking a question is not dangerous, so allow it without a permission
		// round trip; the default inbound rules still deny shell/file writes.
		Permissions: schema.PermissionRuleset{
			{Action: "question", Resource: "*", Effect: schema.PermissionEffectAllow},
		},
	}); err != nil {
		client.Close()
		return nil, err
	}
	return client, nil
}

// clientOptions is the client setup shared by the server and the caller: a
// deterministic model and no real credentials.
func clientOptions(root string) *agent.ClientOptions {
	return &agent.ClientOptions{
		Protocol: "openai-responses", Credentials: agent.APIKey("unused"),
		Model: "deterministic", Root: root,
	}
}

// serve starts the A2A server for the client's exposed agents.
func serve(addr string, client *agent.Client) (string, func(), error) {
	srv, err := server.New(server.Options{
		Factory: agent.NewA2AFactory(client),
		Agents:  client.Agents().All(),
		Turns:   client.TurnStore(),
	})
	if err != nil {
		return "", nil, err
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return "", nil, err
	}
	httpServer := &http.Server{Handler: srv.Handler()}
	go func() { _ = httpServer.Serve(listener) }()
	shutdown := func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
	}
	return "http://" + listener.Addr().String(), shutdown, nil
}

// converse drives the exposed agent through the SDK's own A2A client: successive
// prompts share a context (multi-turn), and a prompt mentioning "ask" parks at
// input-required and resumes with the caller's answer.
func converse(ctx context.Context, root, base string, prompts []string) ([]string, error) {
	session, closeCaller, err := dial(ctx, root, base, func(agent.QuestionRequest) ([]schema.QuestionAnswer, error) {
		return []schema.QuestionAnswer{{"blue"}}, nil
	})
	if err != nil {
		return nil, err
	}
	defer closeCaller()

	replies := make([]string, 0, len(prompts))
	for _, prompt := range prompts {
		reply, err := send(ctx, session, prompt)
		replies = append(replies, reply)
		if err != nil {
			return replies, err
		}
	}
	return replies, nil
}

// send runs one turn and returns the agent's text. It accumulates streamed
// deltas and falls back to the completed text when a provider sent no deltas.
func send(ctx context.Context, session *agent.Session, prompt string) (string, error) {
	var reply strings.Builder
	completed := ""
	for event, err := range session.Send(ctx, agent.MessageOptions{Prompt: prompt}) {
		if err != nil {
			return reply.String(), err
		}
		switch e := event.(type) {
		case agent.TextDelta:
			reply.WriteString(e.Delta)
		case agent.TextEnded:
			completed = e.Text
		}
	}
	if reply.Len() == 0 {
		return completed, nil
	}
	return reply.String(), nil
}

// deterministicModel gives the server something to show: it counts turns
// (multi-turn context), echoes, and asks a question when the prompt mentions
// "ask" (input-required).
type deterministicModel struct{}

func (deterministicModel) Prepare(context.Context, *llmschema.LLMRequest) (*llmschema.PreparedRequest, error) {
	return nil, nil
}

func (deterministicModel) Generate(context.Context, *llmschema.LLMRequest) (*llmschema.LLMResponse, error) {
	return nil, nil
}

func (deterministicModel) Stream(_ context.Context, req *llmschema.LLMRequest) (iter.Seq2[llmschema.LLMEvent, error], error) {
	events := modelEvents(req)
	return func(yield func(llmschema.LLMEvent, error) bool) {
		for _, event := range events {
			if !yield(event, nil) {
				return
			}
		}
	}, nil
}

func modelEvents(req *llmschema.LLMRequest) []llmschema.LLMEvent {
	if len(req.Messages) == 0 {
		return textEvents("(no input)")
	}
	last := req.Messages[len(req.Messages)-1]
	if last.Role == llmschema.RoleTool {
		return textEvents("Thanks, you answered: " + toolAnswer(last))
	}
	prompt := lastUserText(req)
	if strings.Contains(strings.ToLower(prompt), "ask") {
		return questionEvents()
	}
	return textEvents(fmt.Sprintf("Turn %d — you said %q.", userTurns(req), prompt))
}

func lastUserText(req *llmschema.LLMRequest) string {
	for i := len(req.Messages) - 1; i >= 0; i-- {
		message := req.Messages[i]
		if message.Role != llmschema.RoleUser {
			continue
		}
		var builder strings.Builder
		for _, part := range message.Content {
			if text, ok := part.(llmschema.TextPart); ok && text.Text != "" {
				if builder.Len() > 0 {
					builder.WriteByte('\n')
				}
				builder.WriteString(text.Text)
			}
		}
		return builder.String()
	}
	return ""
}

func userTurns(req *llmschema.LLMRequest) int {
	count := 0
	for _, message := range req.Messages {
		if message.Role == llmschema.RoleUser {
			count++
		}
	}
	return count
}

func toolAnswer(message llmschema.Message) string {
	for _, part := range message.Content {
		if result, ok := part.(llmschema.ToolResultPart); ok {
			return fmt.Sprint(result.Result.Value)
		}
	}
	return ""
}

func textEvents(text string) []llmschema.LLMEvent {
	return []llmschema.LLMEvent{
		llmschema.NewStepStart(0),
		llmschema.NewTextStart("t1"),
		llmschema.NewTextDelta("t1", text),
		llmschema.NewTextEnd("t1"),
		llmschema.NewStepFinish(0, llmschema.ReasonStop),
		llmschema.NewFinish(llmschema.ReasonStop),
	}
}

func questionEvents() []llmschema.LLMEvent {
	return []llmschema.LLMEvent{
		llmschema.NewStepStart(0),
		llmschema.NewToolCall("call-q", "question", map[string]any{
			"questions": []any{map[string]any{
				"question": "Pick a color",
				"header":   "color",
				"options": []any{
					map[string]any{"label": "red", "description": "warm"},
					map[string]any{"label": "blue", "description": "cool"},
				},
			}},
		}),
		llmschema.NewStepFinish(0, llmschema.ReasonToolCalls),
		llmschema.NewFinish(llmschema.ReasonToolCalls),
	}
}
