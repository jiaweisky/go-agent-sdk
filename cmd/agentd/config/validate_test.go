package config

import "testing"

func testModel() ModelConfig {
	return ModelConfig{
		ID:          "test-model",
		Model:       "test-model",
		Protocol:    "openai-responses",
		Credentials: Credentials{APIKey: "test"},
	}
}

func TestValidateExampleConfig(t *testing.T) {
	cfg := Config{}
	if err := Load("../agentd.example.yaml", &cfg); err != nil {
		t.Fatalf("load example: %v", err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate example: %v", err)
	}
}

func testCards() map[string]CardConfig {
	return map[string]CardConfig{"build": {Name: "Build Agent", Description: "A build agent."}}
}

func TestValidateRejectsDuplicateListen(t *testing.T) {
	cfg := Config{
		Models: []ModelConfig{testModel()},
		Servers: []Server{
			{Name: "a", Kind: "http", Listen: "127.0.0.1:1", Root: "/a", Agents: []string{"build"}},
			{Name: "b", Kind: "a2a", Listen: "127.0.0.1:1", Root: "/b", Agents: []string{"build"}, Cards: testCards()},
		},
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected a duplicate listen to be rejected")
	}
}

func TestValidateRejectsA2AAgentWithoutCard(t *testing.T) {
	cfg := Config{
		Models: []ModelConfig{testModel()},
		Servers: []Server{
			{Name: "a", Kind: "a2a", Listen: "127.0.0.1:1", Root: "/a", Agents: []string{"build"}},
		},
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected an exposed a2a agent without a card to be rejected")
	}
}

func TestValidateRejectsCardWithoutNameOrDescription(t *testing.T) {
	base := func(card CardConfig) Config {
		return Config{
			Models: []ModelConfig{testModel()},
			Servers: []Server{
				{Name: "a", Kind: "a2a", Listen: "127.0.0.1:1", Root: "/a", Agents: []string{"build"},
					Cards: map[string]CardConfig{"build": card}},
			},
		}
	}
	if err := base(CardConfig{Description: "desc"}).Validate(); err == nil {
		t.Fatal("expected a card without a name to be rejected")
	}
	if err := base(CardConfig{Name: "Build"}).Validate(); err == nil {
		t.Fatal("expected a card without a description to be rejected")
	}
	if err := base(CardConfig{Name: "Build", Description: "desc"}).Validate(); err != nil {
		t.Fatalf("a card with name and description should pass: %v", err)
	}
}

func TestValidateRejectsUnknownModelSubset(t *testing.T) {
	cfg := Config{
		Models: []ModelConfig{testModel()},
		Servers: []Server{
			{Name: "a", Kind: "http", Listen: "127.0.0.1:1", Root: "/a", Agents: []string{"build"}, Models: []string{"nope"}},
		},
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected an unknown model id to be rejected")
	}
}

func TestValidateRejectsDefaultOutsideSubset(t *testing.T) {
	cfg := Config{
		Models: []ModelConfig{testModel(), {ID: "other", Model: "other", Protocol: "openai-responses", Credentials: Credentials{APIKey: "k"}}},
		Servers: []Server{
			{Name: "a", Kind: "http", Listen: "127.0.0.1:1", Root: "/a", Agents: []string{"build"},
				Models: []string{"test-model"}, DefaultModel: "other"},
		},
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected a defaultModel outside the subset to be rejected")
	}
}

func TestValidateRejectsNonLoopbackWithoutAuth(t *testing.T) {
	cfg := Config{
		Models: []ModelConfig{testModel()},
		Servers: []Server{
			{Name: "a", Kind: "http", Listen: "0.0.0.0:1", Root: "/a", Agents: []string{"build"}},
		},
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected a non-loopback bind without auth to be rejected")
	}
}

func TestValidateRejectsUnknownDefaultModelWholeCatalog(t *testing.T) {
	cfg := Config{
		Models: []ModelConfig{testModel()},
		Servers: []Server{
			{Name: "a", Kind: "http", Listen: "127.0.0.1:1", Root: "/a", Agents: []string{"build"},
				DefaultModel: "nope"},
		},
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected an unknown defaultModel to be rejected even with the whole catalog")
	}
}

func TestValidateRejectsUnknownQuestionPolicy(t *testing.T) {
	cfg := Config{
		Models: []ModelConfig{testModel()},
		Servers: []Server{
			{Name: "a", Kind: "a2a", Listen: "127.0.0.1:1", Root: "/a", Agents: []string{"build"},
				Cards: testCards(), Questions: "maybe"},
		},
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected an unknown questions policy to be rejected")
	}
}

func TestValidateRejectsKindMismatchedFields(t *testing.T) {
	base := func(kind string) Config {
		return Config{
			Models: []ModelConfig{testModel()},
			Servers: []Server{
				{Name: "a", Kind: kind, Listen: "127.0.0.1:1", Root: "/a", Agents: []string{"build"}},
			},
		}
	}
	httpCfg := base("http")
	httpCfg.Servers[0].BaseURL = "https://x"
	if err := httpCfg.Validate(); err == nil {
		t.Fatal("expected an a2a-only field on an http server to be rejected")
	}
	a2aCfg := base("a2a")
	a2aCfg.Servers[0].CORS = []string{"https://x"}
	if err := a2aCfg.Validate(); err == nil {
		t.Fatal("expected an http-only field on an a2a server to be rejected")
	}
}

func TestValidateAllowsSharedListen(t *testing.T) {
	cfg := Config{
		Models: []ModelConfig{testModel()},
		Servers: []Server{
			{Name: "root", Kind: "http", Listen: "127.0.0.1:1", Root: "/a", Agents: []string{"build"}},
			{Name: "api2", Kind: "http", Listen: "127.0.0.1:1", Root: "/b", Agents: []string{"build"}, Mount: "/api2"},
			{Name: "hosted", Kind: "http", Listen: "127.0.0.1:1", Root: "/c", Agents: []string{"build"}, Host: "h.example.com"},
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate shared listen: %v", err)
	}
}

func TestValidateRejectsTwoRootsOnOneListen(t *testing.T) {
	cfg := Config{
		Models: []ModelConfig{testModel()},
		Servers: []Server{
			{Name: "a", Kind: "http", Listen: "127.0.0.1:1", Root: "/a", Agents: []string{"build"}},
			{Name: "b", Kind: "http", Listen: "127.0.0.1:1", Root: "/b", Agents: []string{"build"}},
		},
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected two root servers on one listen to be rejected")
	}
}

func TestValidateRejectsDuplicateMount(t *testing.T) {
	cfg := Config{
		Models: []ModelConfig{testModel()},
		Servers: []Server{
			{Name: "root", Kind: "http", Listen: "127.0.0.1:1", Root: "/a", Agents: []string{"build"}},
			{Name: "a", Kind: "http", Listen: "127.0.0.1:1", Root: "/b", Agents: []string{"build"}, Mount: "/x"},
			{Name: "b", Kind: "http", Listen: "127.0.0.1:1", Root: "/c", Agents: []string{"build"}, Mount: "/x/"},
		},
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected a duplicate mount to be rejected")
	}
}

func TestValidateRejectsDefaultOnHTTP(t *testing.T) {
	cfg := Config{
		Models: []ModelConfig{testModel()},
		Servers: []Server{
			{Name: "a", Kind: "http", Listen: "127.0.0.1:1", Root: "/a", Agents: []string{"build"}, Default: true},
		},
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected default on an http server to be rejected")
	}
}

func TestValidateSharesListenTLS(t *testing.T) {
	tls := &TLS{CertFile: "c.pem", KeyFile: "k.pem"}
	shared := Config{
		Models: []ModelConfig{testModel()},
		Servers: []Server{
			{Name: "root", Kind: "http", Listen: "127.0.0.1:1", Root: "/a", Agents: []string{"build"}, TLS: tls},
			{Name: "api2", Kind: "http", Listen: "127.0.0.1:1", Root: "/b", Agents: []string{"build"}, Mount: "/api2", TLS: tls},
		},
	}
	if err := shared.Validate(); err != nil {
		t.Fatalf("same tls on a shared listen should pass: %v", err)
	}

	mismatched := shared
	mismatched.Servers = append([]Server(nil), shared.Servers...)
	mismatched.Servers[1].TLS = nil
	if err := mismatched.Validate(); err == nil {
		t.Fatal("expected mismatched tls on a shared listen to be rejected")
	}
}
