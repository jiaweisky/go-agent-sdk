// Command chat is an interactive chat example for go-agent-sdk.
package main

import (
	"context"
	"flag"
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

// agentName is the primary agent the chat session runs. util.Client registers it
// with a system prompt; the permission demo below extends the same agent.
const agentName = "build"

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "chat: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	dir, yolo := parseFlags()
	if dir != "" {
		if err := os.Chdir(dir); err != nil {
			return err
		}
	}
	// Load .env from the working directory, after -C so the file comes from the
	// intended directory.
	util.LoadDotEnv()

	ctx := context.Background()
	console, restore, err := newConsole()
	if err != nil {
		return err
	}
	defer restore()

	client, err := util.Client(util.ExampleDir(), clientOptions(console, yolo)...)
	if err != nil {
		return err
	}
	defer client.Close()

	if !yolo {
		if err := addPermissionDemo(client); err != nil {
			return err
		}
	}

	session, err := client.CreateSession(ctx, &agent.SessionConfig{Agent: agentName})
	if err != nil {
		return err
	}
	defer session.Close()

	// Client.On sees every session, so subagent (task) sessions are rendered
	// nested under their task.
	renderer := newChat(console, client, session.ID(), os.Getenv("AGENT_MODEL"))
	client.On(renderer.handle)
	writeBanner(console, yolo)

	return chatLoop(ctx, console, session, renderer)
}

// parseFlags reads the command line. -C runs as if started in the given
// directory (like git -C): the workspace and the project's .config follow it,
// while the example's own state and sample config stay next to the example (see
// util.ExampleDir).
func parseFlags() (dir string, yolo bool) {
	flagDir := flag.String("C", "", "run as if started in this directory")
	flagYolo := flag.Bool("yolo", false, "skip all permission prompts (approve every tool call)")
	flag.Parse()
	return *flagDir, *flagYolo
}

// addPermissionDemo appends an "ask" rule for bash to the build agent, which
// otherwise allows every tool, so interactive approval has something to ask
// about.
func addPermissionDemo(client *agent.Client) error {
	return client.Plugin().Add("chat-permission-demo", func(pluginCtx *core.PluginContext) (func(), error) {
		_, err := pluginCtx.Agents.Transform(func(draft core.AgentDraft) error {
			draft.Update(schema.AgentID(agentName), func(info *schema.AgentInfo) {
				info.Permissions = append(info.Permissions, schema.PermissionRule{
					Action: "bash", Resource: "*", Effect: schema.PermissionEffectAsk,
				})
			})
			return nil
		})
		return func() {}, err
	})
}

// writeBanner prints the startup banner.
func writeBanner(console *console, yolo bool) {
	if wd, err := os.Getwd(); err == nil {
		console.write(dim + "(directory: " + wd + ")" + reset + "\n")
	}
	if yolo {
		console.write(yellow + "warning: --yolo skips all permission prompts" + reset + "\n")
	}
	console.write("Chat with go-agent-sdk (Esc or Ctrl+C to exit; interrupts a running turn)\n")
	console.write(dim + "Commands: /skills  /agents  /mcps  /task [<#n>]" + reset + "\n\n")
}

// chatLoop prompts until stdin closes or the user interrupts a turn.
func chatLoop(ctx context.Context, console *console, session *agent.Session, renderer *chat) error {
	for {
		input, err := console.prompt("You: ")
		if input == "" {
			if err != nil {
				return nil
			}
			continue
		}
		console.write("\n")
		if handleCommand(renderer, input) {
			continue
		}
		if runTurn(ctx, console, session, renderer, input) {
			return nil
		}
	}
}

// runTurn sends one prompt and watches for an interrupt while it runs, returning
// whether the user interrupted it (which exits the chat).
func runTurn(ctx context.Context, console *console, session *agent.Session, renderer *chat, input string) bool {
	renderer.beginTurn()
	turnDone := make(chan struct{})
	go func() {
		defer close(turnDone)
		sendTurn(ctx, console, session, input)
	}()
	// While the turn runs, Esc/Ctrl+C aborts it and exits the chat; the bottom
	// status line animates a spinner for busy sessions.
	if !console.watchInterrupt(turnDone, session.Abort, renderer.updateStatus) {
		renderer.clearStatus()
		console.write("\n")
		return false
	}
	<-turnDone
	// Leave the terminal on a clean line before exiting.
	console.setStatus("")
	console.write("\n")
	return true
}

// handleCommand runs a slash command from the prompt, and reports whether the
// input was one: the caller must not send it to the model. The input is trimmed
// here so that "/skills " (or a leading space) is still a command.
func handleCommand(c *chat, input string) bool {
	switch command := strings.TrimSpace(input); {
	case command == "/skills":
		c.skillsCommand()
	case command == "/agents":
		c.agentsCommand()
	case command == "/mcps":
		c.mcpsCommand()
	case command == "/task" || strings.HasPrefix(command, "/task "):
		c.taskCommand(command)
	default:
		return false
	}
	c.console.write("\n")
	return true
}

// sendTurn drives one turn; rendering happens in the client event handler.
func sendTurn(ctx context.Context, console *console, session *agent.Session, prompt string) {
	for _, err := range session.Send(ctx, agent.MessageOptions{Prompt: prompt}) {
		if err != nil {
			console.write(fmt.Sprintf("%s%v%s\n", yellow, err, reset))
			break
		}
	}
}

// clientOptions builds the chat's client options. With yolo the interactive
// permission handler is omitted, so util.Client falls back to agent.ApproveAll
// and every "ask" is approved without a prompt. Questions stay interactive.
func clientOptions(console *console, yolo bool) []util.Option {
	options := []util.Option{
		util.WithQuestion(interactiveQuestion(console)),
		util.WithMCP(weatherMCP()...),
	}
	if !yolo {
		options = append(options, util.WithPermission(interactivePermission(console)))
	}
	return options
}

// interactivePermission prompts for each "ask" permission request.
func interactivePermission(console *console) agent.PermissionHandlerFunc {
	return func(request agent.PermissionRequest) (agent.PermissionDecision, error) {
		console.write(fmt.Sprintf("\n%s[permission] %s: %s%s\n",
			yellow, request.Action, strings.Join(request.Resources, ", "), reset))
		answer, err := console.prompt("Allow? [Y/n] ")
		if err != nil {
			// EOF (e.g. Ctrl+D): fail safe rather than allow.
			return agent.PermissionDecision{Allow: false, Message: "User denied this action."}, nil
		}
		switch strings.ToLower(strings.TrimSpace(answer)) {
		case "n", "no":
			return agent.PermissionDecision{Allow: false, Message: "User denied this action."}, nil
		default:
			return agent.PermissionDecision{Allow: true}, nil
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
			answer, err := console.prompt(questionPrompt(question))
			if err != nil {
				// EOF (e.g. Ctrl+D) or a closed stdin: the remaining answers are
				// skipped rather than prompted into a dead terminal, and every
				// question keeps its slot so the answers stay aligned.
				answers = append(answers, schema.QuestionAnswer{})
				continue
			}
			answers = append(answers, parseAnswer(answer, question.Options))
		}
		return answers, nil
	}
}

func questionPrompt(question schema.QuestionInfo) string {
	if question.Multiple != nil && *question.Multiple {
		return "Answer (numbers separated by comma, empty to skip): "
	}
	return "Answer (number or text, empty to skip): "
}

// parseAnswer maps a prompt answer to the labels the model expects: numbers
// select options (several, comma separated, when the question allows it), and
// anything else is free text.
func parseAnswer(input string, options []schema.QuestionOption) []string {
	if input == "" {
		return []string{}
	}
	selected := []string{}
	for _, part := range strings.Split(input, ",") {
		index, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || index < 1 || index > len(options) {
			// Not a selection list: treat the whole answer as text.
			return []string{input}
		}
		selected = append(selected, options[index-1].Label)
	}
	return selected
}

// weatherMCP configures an optional local weather MCP server (stdio). It is
// enabled only when AGENT_WEATHER_MCP points at the server entry script;
// AMAP_API_KEY is passed through to the server. No path or key is hardcoded.
func weatherMCP() []agent.MCPConfig {
	server := os.Getenv("AGENT_WEATHER_MCP")
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
