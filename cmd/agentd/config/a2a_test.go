package config

import "testing"

func a2aConfig(card CardConfig) Config {
	return Config{
		Models: []ModelConfig{testModel()},
		Servers: []Server{{
			Name: "a", Kind: "a2a", Listen: "127.0.0.1:1", Root: "/a",
			Agents: []string{"build"},
			Cards:  map[string]CardConfig{"build": card},
		}},
	}
}

func TestValidateCardExtendedFields(t *testing.T) {
	cfg := a2aConfig(CardConfig{
		Name: "Build", Description: "builds",
		CardPath:           "/agents/build/card.json",
		DefaultInputModes:  []string{"text/plain", "image/png"},
		DefaultOutputModes: []string{"text/plain"},
		Provider:           &ProviderConfig{Organization: "Acme", URL: "https://acme.example"},
		Extended: &ExtendedCardConfig{
			Description:       "authenticated details",
			DefaultInputModes: []string{"text/plain", "audio/wav"},
			Skills: []SkillConfig{{
				ID: "review", Name: "Review", Description: "reviews code", Tags: []string{"code"},
			}},
		},
	})
	if err := cfg.Validate(); err != nil {
		t.Fatalf("valid card rejected: %v", err)
	}
}

func TestValidateCardRejectsBadProvider(t *testing.T) {
	cfg := a2aConfig(CardConfig{
		Name: "Build", Description: "builds",
		Provider: &ProviderConfig{Organization: "Acme"},
	})
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected a provider without url to be rejected")
	}
}

func TestValidateCardRejectsBadModes(t *testing.T) {
	for _, modes := range [][]string{{""}, {"not-a-mime"}} {
		cfg := a2aConfig(CardConfig{Name: "Build", Description: "builds", DefaultInputModes: modes})
		if err := cfg.Validate(); err == nil {
			t.Fatalf("expected modes %v to be rejected", modes)
		}
	}
}

func TestValidateCardRejectsBadCardPath(t *testing.T) {
	for _, path := range []string{"agents/build/card.json", "/", "/agents/build/", "/agents/../x", "/agents/{id}", "/agents/%20", "/.well-known/agents.json"} {
		cfg := a2aConfig(CardConfig{Name: "Build", Description: "builds", CardPath: path})
		if err := cfg.Validate(); err == nil {
			t.Fatalf("expected cardPath %q to be rejected", path)
		}
	}
}

func TestValidateCardRejectsDuplicatePaths(t *testing.T) {
	cfg := Config{
		Models: []ModelConfig{testModel()},
		Servers: []Server{{
			Name: "a", Kind: "a2a", Listen: "127.0.0.1:1", Root: "/a",
			Agents: []string{"build", "review"},
			Cards: map[string]CardConfig{
				"build":  {Name: "Build", Description: "builds", CardPath: "/shared/card.json"},
				"review": {Name: "Review", Description: "reviews", CardPath: "/shared/card.json"},
			},
		}},
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected duplicate cardPath values to be rejected")
	}
}

func TestValidateCardRejectsEndpointCollision(t *testing.T) {
	cfg := a2aConfig(CardConfig{Name: "Build", Description: "builds", CardPath: "/agents/build/"})
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected a cardPath equal to an endpoint to be rejected")
	}
}

func TestValidateA2ARejectsHostWithPathPrefix(t *testing.T) {
	cfg := a2aConfig(CardConfig{Name: "Build", Description: "builds"})
	cfg.Servers[0].Host = "a.example.com"
	cfg.Servers[0].PathPrefix = "/api"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected an a2a server combining host and pathPrefix to be rejected")
	}
}

func TestValidateCardRejectsRootIntrospectionCollision(t *testing.T) {
	cfg := a2aConfig(CardConfig{Name: "Build", Description: "builds", CardPath: "/api/config"})
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected a cardPath colliding with introspection to be rejected")
	}
}

func TestValidateCardRejectsHostRoutedIntrospectionCollision(t *testing.T) {
	cfg := a2aConfig(CardConfig{Name: "Build", Description: "builds", CardPath: "/api/config"})
	cfg.Servers[0].Host = "a.example.com"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected a host-routed cardPath colliding with introspection to be rejected")
	}
}

func TestValidateCardAllowsExactRESTPrefix(t *testing.T) {
	cfg := a2aConfig(CardConfig{Name: "Build", Description: "builds", CardPath: "/agents/build/rest"})
	cfg.Servers[0].Transports = []A2ATransport{A2ATransportJSONRPC, A2ATransportREST}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("exact REST prefix should be allowed: %v", err)
	}
}

func TestValidateCardRejectsRESTCollision(t *testing.T) {
	cfg := a2aConfig(CardConfig{Name: "Build", Description: "builds", CardPath: "/agents/build/rest/message:send"})
	cfg.Servers[0].Transports = []A2ATransport{A2ATransportJSONRPC, A2ATransportREST}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected a cardPath under the REST binding to be rejected")
	}
}

func TestValidateRejectsDuplicateAgents(t *testing.T) {
	cfg := Config{
		Models: []ModelConfig{testModel()},
		Servers: []Server{{
			Name: "a", Kind: "a2a", Listen: "127.0.0.1:1", Root: "/a",
			Agents: []string{"build", "build"},
			Cards:  testCards(),
		}},
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected duplicate agents to be rejected")
	}
}

func TestValidateA2AOptions(t *testing.T) {
	base := func(server Server) Config {
		server.Name = "a"
		server.Kind = "a2a"
		server.Listen = "127.0.0.1:1"
		server.Root = "/a"
		server.Agents = []string{"build"}
		server.Cards = testCards()
		return Config{Models: []ModelConfig{testModel()}, Servers: []Server{server}}
	}
	if err := base(Server{PushNotifications: &PushNotificationsConfig{Enabled: true, Timeout: "soon"}}).Validate(); err == nil {
		t.Fatal("expected an invalid push timeout to be rejected")
	}
	if err := base(Server{PushNotifications: &PushNotificationsConfig{Timeout: "soon"}}).Validate(); err != nil {
		t.Fatalf("a disabled push block must ignore its timeout: %v", err)
	}
	if err := base(Server{Signing: &SigningConfig{}}).Validate(); err == nil {
		t.Fatal("expected signing without a keyFile to be rejected")
	}
	if err := base(Server{Transports: []A2ATransport{"grpc"}}).Validate(); err == nil {
		t.Fatal("expected an unknown transport to be rejected")
	}
	if err := base(Server{Transports: []A2ATransport{A2ATransportREST, A2ATransportREST}}).Validate(); err == nil {
		t.Fatal("expected a duplicate transport to be rejected")
	}
	if err := base(Server{Transports: []A2ATransport{A2ATransportJSONRPC, A2ATransportREST}}).Validate(); err != nil {
		t.Fatalf("valid transports rejected: %v", err)
	}
}
