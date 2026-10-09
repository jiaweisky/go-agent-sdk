package server

import (
	"testing"

	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
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

// TestBuildCardDeclaresAudioVideoModes covers the media-capability advertisement
// added for audio/video models: the exposed agent's InputModes (derived from the
// model) become the card's defaultInputModes.
func TestBuildCardDeclaresAudioVideoModes(t *testing.T) {
	info := schema.AgentInfo{ID: schema.AgentID("build"), Expose: &schema.AgentExpose{
		InputModes: []string{"text/plain", "image/png", "audio/wav", "video/mp4"},
	}}
	card := buildCard(info, "", nil)
	for _, mode := range []string{"audio/wav", "video/mp4"} {
		if !containsMode(card.DefaultInputModes, mode) {
			t.Fatalf("defaultInputModes = %v, want %s", card.DefaultInputModes, mode)
		}
	}
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

func TestBuildCardRendersExposeMetadata(t *testing.T) {
	info := schema.AgentInfo{ID: schema.AgentID("build"), Expose: &schema.AgentExpose{
		Name:             "Build Agent",
		Version:          "2.0.0",
		DocumentationURL: "https://example.com/doc",
		IconURL:          "https://example.com/icon.png",
		Provider:         &schema.AgentProvider{Organization: "Acme", URL: "https://acme.dev"},
		Skills: []schema.AgentSkill{{
			ID: "read", Name: "Read", Description: "Read files", Tags: []string{"files"}, Examples: []string{"read a file"},
		}},
	}}
	card := buildCard(info, "", nil)
	if card.Version != "2.0.0" || card.DocumentationURL != "https://example.com/doc" || card.IconURL != "https://example.com/icon.png" {
		t.Fatalf("card = %+v", card)
	}
	if card.Provider == nil || card.Provider.Org != "Acme" || card.Provider.URL != "https://acme.dev" {
		t.Fatalf("provider = %+v", card.Provider)
	}
	if len(card.Skills) != 1 || card.Skills[0].ID != "read" || card.Skills[0].Name != "Read" || card.Skills[0].Tags[0] != "files" {
		t.Fatalf("skills = %+v", card.Skills)
	}
}
