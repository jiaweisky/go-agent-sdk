package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
)

// runA2A drives one A2A turn. url may be the agentd A2A base URL (the card is
// fetched from /.well-known/agent-card.json) or a full card URL.
func runA2A(ctx context.Context, url, prompt string) error {
	card, err := fetchCard(ctx, cardURL(url))
	if err != nil {
		return err
	}
	client, err := a2aclient.NewFromCard(ctx, card)
	if err != nil {
		return err
	}
	defer client.Destroy()

	message := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart(prompt))
	message.ContextID = a2a.NewContextID()

	var text strings.Builder
	for event, err := range client.SendStreamingMessage(ctx, &a2a.SendMessageRequest{Message: message}) {
		if err != nil {
			return err
		}
		if update, ok := event.(*a2a.TaskArtifactUpdateEvent); ok {
			for _, part := range update.Artifact.Parts {
				text.WriteString(part.Text())
			}
		}
	}
	fmt.Println(text.String())
	return nil
}

func cardURL(url string) string {
	if strings.HasSuffix(url, ".json") {
		return url
	}
	return strings.TrimRight(url, "/") + "/.well-known/agent-card.json"
}

func fetchCard(ctx context.Context, url string) (*a2a.AgentCard, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("card %s: status %d", url, resp.StatusCode)
	}
	var card a2a.AgentCard
	if err := json.NewDecoder(resp.Body).Decode(&card); err != nil {
		return nil, err
	}
	return &card, nil
}
