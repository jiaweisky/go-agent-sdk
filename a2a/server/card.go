package server

import (
	"github.com/a2aproject/a2a-go/v2/a2a"

	"github.com/jiaweisky/go-agent-sdk/schema"
)

// buildCard builds the AgentCard for one exposed agent. A card describes a
// single agent (one card = one agent); its URL is where the JSON-RPC endpoint
// is mounted.
func buildCard(info schema.AgentInfo, endpoint string) *a2a.AgentCard {
	name := string(info.ID)
	description := ""
	if info.Description != nil {
		description = *info.Description
	}
	if info.Expose != nil {
		if info.Expose.Name != "" {
			name = info.Expose.Name
		}
		if info.Expose.Description != "" {
			description = info.Expose.Description
		}
	}
	card := &a2a.AgentCard{
		Name:        name,
		Description: description,
		Version:     "1.0.0",
		Capabilities: a2a.AgentCapabilities{
			Streaming: true,
		},
		// Input: text prompts, structured data parts, and the image types the read
		// tool can ingest. Output: assistant text and structured (data) parts.
		DefaultInputModes:  []string{"text/plain", "application/json", "image/png", "image/jpeg", "image/gif", "image/webp"},
		DefaultOutputModes: []string{"text/plain", "application/json"},
		Skills: []a2a.AgentSkill{{
			ID:          string(info.ID),
			Name:        name,
			Description: description,
			Tags:        []string{"agent"},
		}},
	}
	if endpoint != "" {
		card.SupportedInterfaces = []*a2a.AgentInterface{
			a2a.NewAgentInterface(endpoint, a2a.TransportProtocolJSONRPC),
		}
	}
	return card
}
