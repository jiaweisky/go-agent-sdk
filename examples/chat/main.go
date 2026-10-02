// Command chat is an interactive chat example for go-agent-sdk.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/user/go-agent-sdk"
	"github.com/user/go-agent-sdk/core"
	"github.com/user/go-agent-sdk/examples/internal/util"
	"github.com/user/go-agent-sdk/mcp"
	"github.com/user/go-agent-sdk/schema"
)

// agentName is the primary agent the chat session runs. util.Client registers it
// with a system prompt; the ask rule added below extends the same agent.
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
		if err := addBashAskRule(client); err != nil {
			return err
		}
	}

	writeBanner(console, yolo)

	session, err := client.CreateSession(ctx, &agent.SessionConfig{
		Agent: agentName,
		Hooks: newSessionHooks(console),
	})
	if err != nil {
		return err
	}
	defer session.Close()

	// Client.On sees every session, so subagent (task) sessions are rendered
	// nested under their task.
	renderer := newChat(console, client, session.ID(), os.Getenv("AGENT_MODEL"))
	renderer.session = session
	client.On(renderer.handle)

	return chatLoop(ctx, console, renderer)
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

// addBashAskRule appends an "ask" rule for bash to the build agent, which
// otherwise allows every tool, so interactive approval has something to ask
// about.
func addBashAskRule(client *agent.Client) error {
	return client.Plugin().Add("chat-permission", func(pluginCtx *core.PluginContext) (func(), error) {
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
	console.write("Chat with go-agent-sdk (Ctrl+C to exit; Esc interrupts; type during a turn to steer it)\n")
	console.write(dim + "SessionHooks: lifecycle logged as [hook]; \"?<text>\" asks for a concise answer; rm -rf is denied" + reset + "\n")
	console.write(dim + "Commands: /help  /new  /sessions [<#n|id>]  /delete <#n|id>  /skills  /agents  /mcps  /task [<#n>]  /compact  /tool  /reference  /persona  /history" + reset + "\n\n")
}

// chatLoop prompts until stdin closes or the user interrupts a turn. The active
// session lives on the renderer, so /new, /sessions and /persona can replace it.
func chatLoop(ctx context.Context, console *console, renderer *chat) error {
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
		if runTurn(ctx, console, renderer, input) {
			return nil
		}
	}
}

// runTurn sends one prompt and watches for an interrupt while it runs. Esc
// (pressed twice) aborts the turn and keeps the chat alive; Ctrl+C aborts and
// exits. It returns whether the chat should exit.
func runTurn(ctx context.Context, console *console, renderer *chat, input string) bool {
	renderer.beginTurn()
	turnDone := make(chan struct{})
	go func() {
		defer close(turnDone)
		sendTurn(ctx, console, renderer.session, input)
	}()
	// While the turn runs, the bottom status line animates a spinner and prompts
	// for the second Esc after the first.
	// Typing during the turn steers it (Enter sends); Esc/Ctrl+C interrupt.
	action := console.watchInterrupt(turnDone, renderer.session.Abort, renderer.steer, renderer.updateStatus, renderer.setInterruptArmed)
	<-turnDone
	if action == interruptNone {
		console.setStatus("")
	} else {
		renderer.clearStatus()
	}
	console.write("\n")
	return action == interruptExit
}

// handleCommand runs a slash command from the prompt, and reports whether the
// input was one: the caller must not send it to the model. The input is trimmed
// here so that "/skills " (or a leading space) is still a command.
func handleCommand(c *chat, input string) bool {
	switch command := strings.TrimSpace(input); {
	case command == "/new" || command == "/clear":
		c.newCommand()
	case command == "/sessions" || strings.HasPrefix(command, "/sessions ") ||
		command == "/resume" || strings.HasPrefix(command, "/resume ") ||
		command == "/continue" || strings.HasPrefix(command, "/continue "):
		c.sessionsCommand(command)
	case command == "/delete" || strings.HasPrefix(command, "/delete "):
		c.deleteCommand(command)
	case command == "/skills":
		c.skillsCommand()
	case command == "/agents":
		c.agentsCommand()
	case command == "/mcps":
		c.mcpsCommand()
	case command == "/task" || strings.HasPrefix(command, "/task "):
		c.taskCommand(command)
	case command == "/compact":
		compactCommand(c)
	case command == "/tool" || strings.HasPrefix(command, "/tool "):
		c.toolsCommand(command)
	case command == "/reference" || strings.HasPrefix(command, "/reference "):
		c.referenceCommand(command)
	case command == "/persona" || strings.HasPrefix(command, "/persona "):
		c.personaCommand(command)
	case command == "/history":
		c.historyCommand()
	case command == "/help" || strings.HasPrefix(command, "/help "):
		c.helpCommand()
	case strings.HasPrefix(command, "/"):
		// A slash-prefixed input is a command attempt, not a prompt: report it
		// instead of sending it to the model.
		c.console.write("unknown command: " + commandName(command) + "\n")
	default:
		return false
	}
	c.console.write("\n")
	return true
}

// commandName returns the command token of a slash command, dropping any
// arguments, so "unknown command: /help" names the command rather than the
// whole line.
func commandName(command string) string {
	if fields := strings.Fields(command); len(fields) > 0 {
		return fields[0]
	}
	return command
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

// steer feeds text into the running turn as steering input instead of starting a
// new turn. The turn watcher calls it when the user types during a turn and
// presses Enter; the active turn picks the text up at its next step boundary.
func (c *chat) steer(text string) {
	if c.session == nil {
		return
	}
	if err := c.session.Steer(text); err != nil {
		c.console.write(fmt.Sprintf("%s%v%s\n", yellow, err, reset))
		return
	}
	c.console.write(fmt.Sprintf("%s[steer] %s%s\n", dim, text, reset))
}

// clientOptions builds the chat's client options. With yolo the interactive
// permission handler is omitted, so util.Client falls back to agent.ApproveAll
// and every "ask" is approved without a prompt. Questions stay interactive.
// Model limits come from the example's model catalog, which is what lets
// automatic compaction run.
func clientOptions(console *console, yolo bool) []util.Option {
	// A local reference to the SDK docs, advertised in the model's system
	// context so it can read them when relevant.
	docsDir := filepath.Join(filepath.Dir(filepath.Dir(util.ExampleDir())), "docs")
	docsDescription := "go-agent-sdk design and porting documentation"
	titleEnabled := true
	options := []util.Option{
		util.WithQuestion(interactiveQuestion(console)),
		util.WithMCP(weatherMCP()...),
		util.WithModelLimits(modelLimits),
		// Generate a session title from the first user message, so /sessions
		// lists something more useful than the placeholder.
		util.WithTitle(agent.TitleConfig{Auto: &titleEnabled}),
		// Keep only a small tail of history verbatim so /compact has something
		// to summarize in a short session.
		util.WithCompaction(agent.CompactionConfig{PreserveRecentTokens: 2000}),
		util.WithReference("docs", core.ReferenceLocalSource{
			Type: "local", Path: schema.AbsolutePath(docsDir), Description: &docsDescription,
		}),
		// Allow a subagent to start another subagent, so nesting is visible in
		// the /task list.
		util.WithSubagentDepth(2),
	}
	if !yolo {
		options = append(options, util.WithPermission(interactivePermission(console)))
	}
	return options
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
	server := expandPath(os.Getenv("AGENT_WEATHER_MCP"))
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

// expandPath expands $VAR references and a leading ~ in a configured path, so a
// .env can write $HOME/... or ~/... (the dotenv loader stores the value
// verbatim; the shell is not involved).
func expandPath(path string) string {
	if path == "" {
		return ""
	}
	path = os.ExpandEnv(path)
	if path == "~" || strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			path = filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(path, "~"), "/"))
		}
	}
	return path
}
