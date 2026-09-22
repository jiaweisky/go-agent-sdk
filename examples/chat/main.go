// Command chat is an interactive chat example for go-agent-sdk.
package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/user/go-agent-sdk/agent"
	"github.com/user/go-agent-sdk/examples/internal/util"
)

const blue = "\033[34m"
const reset = "\033[0m"

func main() {
	ctx := context.Background()
	client, err := util.Client()
	if err != nil {
		panic(err)
	}
	defer client.Close()

	session, err := client.CreateSession(ctx, &agent.SessionConfig{Agent: "build"})
	if err != nil {
		panic(err)
	}
	defer session.Close()

	session.On(func(event agent.Event) {
		var output string
		switch e := event.(type) {
		case agent.ReasoningDelta:
			output = fmt.Sprintf("[reasoning: %s]", e.Delta)
		case agent.ToolCalled:
			output = fmt.Sprintf("[tool: %s]", e.Name)
		}
		if output != "" {
			fmt.Printf("%s%s%s\n", blue, output, reset)
		}
	})

	fmt.Println("Chat with go-agent-sdk (Ctrl+C to exit)")
	fmt.Println()
	scanner := bufio.NewScanner(os.Stdin)

	for {
		fmt.Print("You: ")
		if !scanner.Scan() {
			break
		}
		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			continue
		}
		fmt.Println()

		reply := send(ctx, session, input)
		fmt.Printf("\nAssistant: %s\n\n", reply)
	}
}

func send(ctx context.Context, session *agent.Session, prompt string) string {
	var parts []string
	var deltas strings.Builder
	for event, err := range session.Send(ctx, agent.MessageOptions{Prompt: prompt}) {
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			break
		}
		switch e := event.(type) {
		case agent.TextDelta:
			deltas.WriteString(e.Delta)
		case agent.TextEnded:
			parts = append(parts, e.Text)
		}
	}
	if len(parts) > 0 {
		return strings.Join(parts, "\n\n")
	}
	return deltas.String()
}
