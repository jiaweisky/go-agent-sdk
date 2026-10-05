// Command tool registers a custom tool that requires a permission prompt.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/jiaweisky/go-agent-sdk"
	"github.com/jiaweisky/go-agent-sdk/examples/internal/util"
)

type statusInput struct {
	ID string `json:"id" jsonschema:"Identifier to look up"`
}

func main() {
	util.LoadDotEnv()
	ctx := context.Background()
	model, err := util.DefaultCatalog.DefaultModel()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	client, err := util.Client(util.ExampleDir(), util.DefaultCatalog.Option(), util.WithModel(model))
	if err != nil {
		panic(err)
	}
	defer client.Close()

	status := agent.DefineTool("status_lookup", "Look up a status value for an identifier",
		func(input statusInput, tc agent.ToolContext) (string, error) {
			return fmt.Sprintf("status for %s: READY", input.ID), nil
		})

	session, err := client.CreateSession(ctx, &agent.SessionConfig{
		Agent: "build",
		Tools: []agent.Tool{status},
		OnPermissionRequest: func(request agent.PermissionRequest) (agent.PermissionDecision, error) {
			fmt.Printf("[permission] %s %v -> allow\n", request.Action, request.Resources)
			return agent.PermissionDecision{Allow: true}, nil
		},
	})
	if err != nil {
		panic(err)
	}
	defer session.Close()

	for event, err := range session.Send(ctx, agent.MessageOptions{
		Prompt: "Use the status_lookup tool with id 'alpha' and tell me the result.",
	}) {
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			break
		}
		switch e := event.(type) {
		case agent.TextDelta:
			fmt.Print(e.Delta)
		case agent.ToolCalled:
			fmt.Printf("\n[tool: %s]\n", e.Name)
		}
	}
	fmt.Println()
}
