package server

import (
	"testing"

	"github.com/jiaweisky/go-agent-sdk/schema"
)

func TestBuildCardDeclaresMediaModes(t *testing.T) {
	description := "an agent"
	card := buildCard(schema.AgentInfo{ID: schema.AgentID("build"), Description: &description}, "http://example.com/", nil)

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

func TestBuildCardDeclaresSecurityScheme(t *testing.T) {
	card := buildCard(schema.AgentInfo{ID: schema.AgentID("build")}, "http://example.com/", &Security{Bearer: "s3cret"})
	if _, ok := card.SecuritySchemes["bearer"]; !ok {
		t.Fatalf("securitySchemes = %v, want a bearer scheme", card.SecuritySchemes)
	}
	if len(card.SecurityRequirements) != 1 {
		t.Fatalf("securityRequirements = %v, want one requirement", card.SecurityRequirements)
	}
	card = buildCard(schema.AgentInfo{ID: schema.AgentID("build")}, "http://example.com/", nil)
	if len(card.SecuritySchemes) != 0 {
		t.Fatalf("securitySchemes = %v, want none without security", card.SecuritySchemes)
	}
}
