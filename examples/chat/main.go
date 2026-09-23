// Command chat is an interactive chat example for go-agent-sdk.
package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/user/go-agent-sdk/agent"
	"github.com/user/go-agent-sdk/core"
	"github.com/user/go-agent-sdk/examples/internal/util"
	"github.com/user/go-agent-sdk/mcp"
	"github.com/user/go-agent-sdk/schema"
)

const (
	blue   = "\033[34m"
	yellow = "\033[33m"
	dim    = "\033[2m"
	reset  = "\033[0m"
)

func main() {
	ctx := context.Background()
	console, restore, err := newConsole()
	if err != nil {
		panic(err)
	}
	defer restore()

	client, err := util.Client(
		util.WithPermission(interactivePermission(console)),
		util.WithQuestion(interactiveQuestion(console)),
		util.WithMCP(weatherMCP()...),
	)
	if err != nil {
		panic(err)
	}
	defer client.Close()

	// The built-in "build" agent allows every tool, so nothing would ever ask.
	// Append an "ask" rule for bash to demonstrate interactive approval.
	if err := client.Plugin().Add("chat-permission-demo", func(pluginCtx *core.PluginContext) (func(), error) {
		_, err := pluginCtx.Agents.Transform(func(draft core.AgentDraft) error {
			draft.Update(schema.AgentID("build"), func(info *schema.AgentInfo) {
				info.Permissions = append(info.Permissions, schema.PermissionRule{
					Action: "bash", Resource: "*", Effect: schema.PermissionEffectAsk,
				})
			})
			return nil
		})
		return func() {}, err
	}); err != nil {
		panic(err)
	}

	session, err := client.CreateSession(ctx, &agent.SessionConfig{Agent: "build"})
	if err != nil {
		panic(err)
	}
	defer session.Close()

	// Client.On sees every session, so subagent (task) sessions are rendered
	// nested under their task.
	renderer := newChat(console, client, session.ID(), os.Getenv("OPENCODE_MODEL"))
	client.On(renderer.handle)

	console.write("Chat with go-agent-sdk (Esc or Ctrl+C to exit; interrupts a running turn)\n\n")
	for {
		input, err := console.prompt("You: ")
		if input == "" {
			if err != nil {
				break
			}
			continue
		}
		console.write("\n")
		if input == "/task" || strings.HasPrefix(input, "/task ") {
			renderer.taskCommand(input)
			console.write("\n")
			continue
		}

		renderer.beginTurn()
		turnDone := make(chan struct{})
		go func() {
			defer close(turnDone)
			send(ctx, console, session, input)
		}()
		// While the turn runs, Esc/Ctrl+C aborts it and exits the chat; the
		// bottom status line animates a spinner for busy sessions.
		interrupted := console.watchInterrupt(turnDone, session.Abort, renderer.updateStatus)
		if interrupted {
			<-turnDone
			// Leave the terminal on a clean line before exiting.
			console.setStatus("")
			console.write("\n")
			break
		}
		renderer.clearStatus()
		console.write("\n")
	}
}

// send drives one turn; rendering happens in the client event handler.
func send(ctx context.Context, console *console, session *agent.Session, prompt string) {
	for _, err := range session.Send(ctx, agent.MessageOptions{Prompt: prompt}) {
		if err != nil {
			console.write(fmt.Sprintf("%s%v%s\n", yellow, err, reset))
			break
		}
	}
}

// interactivePermission prompts for each "ask" permission request.
func interactivePermission(console *console) agent.PermissionHandlerFunc {
	return func(request agent.PermissionRequest) (agent.PermissionDecision, error) {
		console.write(fmt.Sprintf("\n%s[permission] %s: %s%s\n",
			yellow, request.Action, strings.Join(request.Resources, ", "), reset))
		answer, _ := console.prompt("Allow? [y/N] ")
		switch strings.ToLower(answer) {
		case "y", "yes":
			return agent.PermissionDecision{Allow: true}, nil
		default:
			return agent.PermissionDecision{Allow: false, Message: "User denied this action."}, nil
		}
	}
}

// interactiveQuestion prompts for each "question" the model asks.
func interactiveQuestion(console *console) agent.QuestionHandlerFunc {
	return func(request agent.QuestionRequest) ([]schema.QuestionAnswer, error) {
		answers := make([]schema.QuestionAnswer, 0, len(request.Questions))
		for _, question := range request.Questions {
			var message strings.Builder
			fmt.Fprintf(&message, "\n%s[question] %s%s\n", yellow, question.Question, reset)
			for i, option := range question.Options {
				fmt.Fprintf(&message, "  %d) %s - %s\n", i+1, option.Label, option.Description)
			}
			console.write(message.String())
			answer, _ := console.prompt("Answer (number or text, empty to skip): ")
			answers = append(answers, parseAnswer(answer, question.Options))
		}
		return answers, nil
	}
}

func parseAnswer(input string, options []schema.QuestionOption) []string {
	if input == "" {
		return []string{}
	}
	if index, err := strconv.Atoi(input); err == nil && index >= 1 && index <= len(options) {
		return []string{options[index-1].Label}
	}
	return []string{input}
}

// weatherMCP configures an optional local weather MCP server (stdio). It is
// enabled only when OPENCODE_WEATHER_MCP points at the server entry script;
// AMAP_API_KEY is passed through to the server. No path or key is hardcoded.
func weatherMCP() []agent.MCPConfig {
	server := os.Getenv("OPENCODE_WEATHER_MCP")
	if server == "" {
		return nil
	}
	return []agent.MCPConfig{{
		Name: "weather",
		Server: mcp.ServerConfig{
			Type:        "local",
			Command:     []string{"node", server},
			Environment: map[string]string{"AMAP_API_KEY": os.Getenv("AMAP_API_KEY")},
		},
	}}
}
