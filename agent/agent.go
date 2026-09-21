// Package agent is the high-level, embeddable API for go-agent-sdk.
//
// It wires the lower-level packages (llm, core/session, core/tool, ...) into a
// small surface modelled on github/copilot-sdk/go:
//
//	client := agent.NewClient(&agent.ClientOptions{
//	    Provider: agent.ProviderConfig{Type: "openai", APIKey: os.Getenv("OPENAI_API_KEY")},
//	    Model:    "gpt-4o",
//	})
//	client.RegisterAgent("build", agent.AgentConfig{System: "You are a helpful assistant."})
//	session, _ := client.CreateSession(ctx, &agent.SessionConfig{Agent: "build"})
//	for event, err := range session.Send(ctx, agent.MessageOptions{Prompt: "hi"}) {
//	    ...
//	}
//
// Unlike copilot-sdk, the agent loop runs in-process: there is no external
// runtime process to start or connect to.
package agent
