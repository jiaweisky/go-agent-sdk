package server

import (
	"testing"

	"github.com/jiaweisky/go-agent-sdk/schema"
)

func TestBuildCardDeclaresMediaModes(t *testing.T) {
	description := "an agent"
	card := buildCard(schema.AgentInfo{ID: schema.AgentID("build"), Description: &description}, "http://example.com/")

	if !containsMode(card.DefaultInputModes, "text/plain") ||
		!containsMode(card.DefaultInputModes, "image/png") {
		t.Fatalf("defaultInputModes = %v, want text and image support", card.DefaultInputModes)
	}
	if !containsMode(card.DefaultOutputModes, "text/plain") {
		t.Fatalf("defaultOutputModes = %v, want text", card.DefaultOutputModes)
	}
}

func containsMode(modes []string, want string) bool {
	for _, mode := range modes {
		if mode == want {
			return true
		}
	}
	return false
}
