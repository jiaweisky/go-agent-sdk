// Command adka2a runs a small A2A server backed by a Google ADK (adk-go) agent.
// It exists to prove cross-implementation interoperability: the go-agent-sdk
// chat example delegates to it over A2A, exercising the wire conventions
// against a different A2A implementation.
//
// It is a separate Go module so the SDK itself never depends on adk-go (see the
// dependency policy in the design doc). Its model is a deterministic stub, so
// the example needs no provider credentials.
package main

import (
	"context"
	"flag"
	"fmt"
	"iter"
	"log"
	"net"
	"net/http"
	"strings"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"google.golang.org/genai"

	adkagent "google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/model"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/server/adka2a/v2"
	"google.golang.org/adk/session"
)

func main() {
	addr := flag.String("http", "127.0.0.1:0", "address to serve the A2A endpoint on")
	flag.Parse()

	agent, err := newAgent()
	if err != nil {
		log.Fatalf("adka2a: build agent: %v", err)
	}
	listener, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatalf("adka2a: listen: %v", err)
	}
	base := "http://" + listener.Addr().String()
	log.Printf("adka2a: serving A2A at %s", base)
	if err := http.Serve(listener, newHandler(agent, base)); err != nil {
		log.Fatalf("adka2a: serve: %v", err)
	}
}

// newAgent builds the agent. Its model is a deterministic stub, so the example
// runs without credentials and returns predictable output the interop test can
// assert.
func newAgent() (adkagent.Agent, error) {
	return llmagent.New(llmagent.Config{
		Name:        "echo_agent",
		Description: "A deterministic agent that echoes the text it receives.",
		Instruction: "Reply with the text you were sent.",
		Model:       echoModel{},
	})
}

// newHandler wires the ADK agent into an A2A server: a well-known card plus a
// JSON-RPC endpoint, exactly as the ADK A2A example does.
func newHandler(agent adkagent.Agent, base string) http.Handler {
	const agentPath = "/invoke"
	card := &a2a.AgentCard{
		Name:        agent.Name(),
		Description: agent.Description(),
		SupportedInterfaces: []*a2a.AgentInterface{{
			URL:             base + agentPath,
			ProtocolBinding: a2a.TransportProtocolJSONRPC,
			ProtocolVersion: a2a.Version,
		}},
		Version:            "1.0.0",
		DefaultInputModes:  []string{"text/plain"},
		DefaultOutputModes: []string{"text/plain"},
		Skills:             adka2a.BuildAgentSkills(agent),
		Capabilities:       a2a.AgentCapabilities{Streaming: true},
	}
	executor := adka2a.NewExecutor(adka2a.ExecutorConfig{
		RunnerConfig: runner.Config{
			AppName:        agent.Name(),
			Agent:          agent,
			SessionService: session.InMemoryService(),
		},
	})
	mux := http.NewServeMux()
	mux.Handle(a2asrv.WellKnownAgentCardPath, a2asrv.NewStaticAgentCardHandler(card))
	mux.Handle(agentPath, a2asrv.NewJSONRPCHandler(a2asrv.NewHandler(executor)))
	return mux
}

// echoModel implements model.LLM with a fixed reply that quotes the caller's
// prompt, so a round trip proves the message reached the agent.
type echoModel struct{}

func (echoModel) Name() string { return "echo" }

func (echoModel) GenerateContent(_ context.Context, req *model.LLMRequest, _ bool) iter.Seq2[*model.LLMResponse, error] {
	prompt := lastUserText(req.Contents)
	reply := fmt.Sprintf("Echo: %q", prompt)
	return func(yield func(*model.LLMResponse, error) bool) {
		yield(&model.LLMResponse{
			Content:      genai.NewContentFromText(reply, genai.RoleModel),
			TurnComplete: true,
		}, nil)
	}
}

// lastUserText concatenates the text of the last user content.
func lastUserText(contents []*genai.Content) string {
	for i := len(contents) - 1; i >= 0; i-- {
		content := contents[i]
		if content == nil || content.Role != genai.RoleUser {
			continue
		}
		var builder strings.Builder
		for _, part := range content.Parts {
			if part == nil || part.Text == "" {
				continue
			}
			if builder.Len() > 0 {
				builder.WriteByte('\n')
			}
			builder.WriteString(part.Text)
		}
		return builder.String()
	}
	return ""
}
