package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func testModel() ModelConfig {
	return ModelConfig{
		ID:          "test-model",
		Model:       "test-model",
		Protocol:    "openai-responses",
		Credentials: Credentials{APIKey: "test"},
	}
}

func testCards() map[string]CardConfig {
	return map[string]CardConfig{"build": {Name: "Build Agent", Description: "A build agent."}}
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

func TestValidateRejectsSharedRoot(t *testing.T) {
	cfg := Config{
		Models: []ModelConfig{testModel()},
		Servers: []Server{
			{Name: "a", Kind: "http", Listen: "127.0.0.1:1", Root: "/same", Agents: []string{"build"}},
			{Name: "b", Kind: "http", Listen: "127.0.0.1:2", Root: "/same", Agents: []string{"build"}},
		},
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected two servers sharing a state root to be rejected")
	}
}

func TestValidateAllowsDistinctRoots(t *testing.T) {
	cfg := Config{
		Models: []ModelConfig{testModel()},
		Servers: []Server{
			{Name: "a", Kind: "http", Listen: "127.0.0.1:1", Root: "/a", Agents: []string{"build"}},
			{Name: "b", Kind: "http", Listen: "127.0.0.1:2", Root: "/b", Agents: []string{"build"}},
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("distinct roots must validate: %v", err)
	}
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

func TestValidateAuthRequiresExplicitBearerToken(t *testing.T) {
	base := func(auth *AuthConfig) Config {
		return Config{
			Models: []ModelConfig{testModel()},
			Servers: []Server{
				{Name: "a", Kind: "http", Listen: "127.0.0.1:1", Root: "/a", Agents: []string{"build"}, Auth: auth},
			},
		}
	}
	cases := []struct {
		name string
		auth *AuthConfig
		ok   bool
	}{
		{"nil", nil, true},
		{"bearer", &AuthConfig{Type: "bearer", Token: "t"}, true},
		{"missing type", &AuthConfig{Token: "t"}, false},
		{"unknown type", &AuthConfig{Type: "basic", Token: "t"}, false},
		{"empty token", &AuthConfig{Type: "bearer"}, false},
	}
	for _, tc := range cases {
		err := base(tc.auth).Validate()
		if tc.ok && err != nil {
			t.Errorf("%s: unexpected error: %v", tc.name, err)
		}
		if !tc.ok && err == nil {
			t.Errorf("%s: expected an error", tc.name)
		}
	}
}

func TestValidateRejectsUnknownDefaultModelWholeRegistry(t *testing.T) {
	cfg := Config{
		Models: []ModelConfig{testModel()},
		Servers: []Server{
			{Name: "a", Kind: "http", Listen: "127.0.0.1:1", Root: "/a", Agents: []string{"build"},
				DefaultModel: "nope"},
		},
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected an unknown defaultModel to be rejected even with the whole registry")
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
	base := func(kind Kind) Config {
		return Config{
			Models: []ModelConfig{testModel()},
			Servers: []Server{
				{Name: "a", Kind: kind, Listen: "127.0.0.1:1", Root: "/a", Agents: []string{"build"}},
			},
		}
	}
	httpCfg := base(KindHTTP)
	httpCfg.Servers[0].BaseURL = "https://x"
	if err := httpCfg.Validate(); err == nil {
		t.Fatal("expected an a2a-only field on an http server to be rejected")
	}
	a2aCfg := base(KindA2A)
	a2aCfg.Servers[0].CORS = []string{"https://x"}
	if err := a2aCfg.Validate(); err == nil {
		t.Fatal("expected an http-only field on an a2a server to be rejected")
	}
}

func TestValidateRejectsUnknownKind(t *testing.T) {
	cfg := Config{
		Models: []ModelConfig{testModel()},
		Servers: []Server{
			{Name: "a", Kind: "grpc", Listen: "127.0.0.1:1", Root: "/a", Agents: []string{"build"}},
		},
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected an unknown server kind to be rejected")
	}
}

func TestValidateRejectsInvalidListen(t *testing.T) {
	cfg := Config{
		Models: []ModelConfig{testModel()},
		Servers: []Server{
			{Name: "a", Kind: KindHTTP, Listen: "not-an-address", Root: "/a", Agents: []string{"build"},
				AllowInsecure: true},
		},
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected an invalid listen address to be rejected")
	}
}

func TestValidateRejectsRootPathPrefix(t *testing.T) {
	cfg := Config{
		Models: []ModelConfig{testModel()},
		Servers: []Server{
			{Name: "a", Kind: KindHTTP, Listen: "127.0.0.1:1", Root: "/a", Agents: []string{"build"},
				PathPrefix: "/"},
		},
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected a root pathPrefix to be rejected")
	}
}

func TestValidateRejectsNegativeLimits(t *testing.T) {
	cases := []struct {
		name  string
		apply func(*Server)
		want  string
	}{
		{"subagentDepth", func(s *Server) { s.SubagentDepth = -1 }, "subagentDepth"},
		{"subscriberCapacity", func(s *Server) { s.SubscriberCapacity = -1 }, "subscriberCapacity"},
		{"maxBodyBytes", func(s *Server) { s.MaxBodyBytes = -1 }, "maxBodyBytes"},
	}
	for _, tc := range cases {
		server := Server{Name: "a", Kind: KindHTTP, Listen: "127.0.0.1:1", Root: "/a", Agents: []string{"build"}}
		tc.apply(&server)
		err := (Config{Models: []ModelConfig{testModel()}, Servers: []Server{server}}).Validate()
		if err == nil {
			t.Errorf("%s: expected an error", tc.name)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: error %q should name the field", tc.name, err)
		}
	}
}

func TestIsLoopback(t *testing.T) {
	cases := []struct {
		listen string
		want   bool
	}{
		{"127.0.0.1:1", true},
		{"127.0.0.2:1", true},
		{"[::1]:1", true},
		{"localhost:1", true},
		{"LocalHost:1", true},
		{"0.0.0.0:1", false},
		{"[::]:1", false},
		{"127.0.0.1", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := IsLoopback(tc.listen); got != tc.want {
			t.Errorf("IsLoopback(%q) = %v, want %v", tc.listen, got, tc.want)
		}
	}
}

func TestValidateRejectsUnknownInboundMedia(t *testing.T) {
	cfg := Config{
		Models: []ModelConfig{testModel()},
		Servers: []Server{
			{Name: "a", Kind: KindA2A, Listen: "127.0.0.1:1", Root: "/a", Agents: []string{"build"},
				Cards: testCards(), InboundMedia: "bogus"},
		},
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected an unknown inboundMedia policy to be rejected")
	}
}

func TestValidateAllowsSharedListen(t *testing.T) {
	cfg := Config{
		Models: []ModelConfig{testModel()},
		Servers: []Server{
			{Name: "root", Kind: "http", Listen: "127.0.0.1:1", Root: "/a", Agents: []string{"build"}},
			{Name: "api2", Kind: "http", Listen: "127.0.0.1:1", Root: "/b", Agents: []string{"build"}, PathPrefix: "/api2"},
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

func TestValidateRejectsDuplicatePathPrefix(t *testing.T) {
	cfg := Config{
		Models: []ModelConfig{testModel()},
		Servers: []Server{
			{Name: "root", Kind: "http", Listen: "127.0.0.1:1", Root: "/a", Agents: []string{"build"}},
			{Name: "a", Kind: "http", Listen: "127.0.0.1:1", Root: "/b", Agents: []string{"build"}, PathPrefix: "/x"},
			{Name: "b", Kind: "http", Listen: "127.0.0.1:1", Root: "/c", Agents: []string{"build"}, PathPrefix: "/x/"},
		},
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected a duplicate pathPrefix to be rejected")
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
			{Name: "api2", Kind: "http", Listen: "127.0.0.1:1", Root: "/b", Agents: []string{"build"}, PathPrefix: "/api2", TLS: tls},
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

func TestValidateRejectsEmptyModels(t *testing.T) {
	if err := (Config{}).Validate(); err == nil || err.Error() != "models is required" {
		t.Fatalf("empty models error = %v", err)
	}
}

func TestValidateRejectsCaseVariantRoot(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("case-insensitive paths are a Windows concern")
	}
	cfg := Config{
		Models: []ModelConfig{testModel()},
		Servers: []Server{
			{Name: "a", Kind: "http", Listen: "127.0.0.1:1", Root: `C:\state`, Agents: []string{"build"}},
			{Name: "b", Kind: "http", Listen: "127.0.0.1:2", Root: `c:\state`, Agents: []string{"build"}},
		},
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected case-variant roots to be rejected on Windows")
	}
}

func TestValidateRejectsSymlinkRoot(t *testing.T) {
	root := t.TempDir()
	linkParent := t.TempDir()
	link := filepath.Join(linkParent, "state")
	if err := os.Symlink(root, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	cfg := Config{
		Models: []ModelConfig{testModel()},
		Servers: []Server{
			{Name: "a", Kind: "http", Listen: "127.0.0.1:1", Root: root, Agents: []string{"build"}},
			{Name: "b", Kind: "http", Listen: "127.0.0.1:2", Root: link, Agents: []string{"build"}},
		},
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected symlinked roots to be rejected")
	}
}

func TestValidateRejectsControlListenOverlap(t *testing.T) {
	cfg := Config{
		Models:  []ModelConfig{testModel()},
		Control: &ControlConfig{Listen: "127.0.0.1:1"},
		Servers: []Server{
			{Name: "a", Kind: "http", Listen: "127.0.0.1:1", Root: "/a", Agents: []string{"build"}},
		},
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected a control listen overlapping a server listen to be rejected")
	}
}

func TestValidateRejectsEmptyNormalizedHost(t *testing.T) {
	cfg := Config{
		Models: []ModelConfig{testModel()},
		Servers: []Server{
			{Name: "a", Kind: "http", Listen: "127.0.0.1:1", Root: "/a", Agents: []string{"build"}, Host: ":8080"},
		},
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected a host that normalizes to empty to be rejected")
	}
}

func TestValidateGroupsEquivalentListenAddresses(t *testing.T) {
	cfg := Config{
		Models: []ModelConfig{testModel()},
		Servers: []Server{
			{Name: "a", Kind: "http", Listen: "127.0.0.1:1", Root: "/a", Agents: []string{"build"}},
			{Name: "b", Kind: "http", Listen: "127.0.0.1:01", Root: "/b", Agents: []string{"build"}},
		},
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected equivalent listen addresses to share a listener group")
	}
}

func TestValidateRejectsUnroutableHost(t *testing.T) {
	for _, host := range []string{" ", ":8080", "example.com/path", "example.com:bad"} {
		cfg := Config{
			Models: []ModelConfig{testModel()},
			Servers: []Server{
				{Name: "a", Kind: "http", Listen: "127.0.0.1:1", Root: "/a", Agents: []string{"build"}, Host: host},
			},
		}
		if err := cfg.Validate(); err == nil {
			t.Errorf("host %q: expected an error", host)
		}
	}
}

func TestValidateRejectsUnroutablePathPrefix(t *testing.T) {
	for _, prefix := range []string{"/x?y", "/x y", "/x/../y", "/x//y"} {
		cfg := Config{
			Models: []ModelConfig{testModel()},
			Servers: []Server{
				{Name: "a", Kind: "http", Listen: "127.0.0.1:1", Root: "/a", Agents: []string{"build"}, PathPrefix: prefix},
			},
		}
		if err := cfg.Validate(); err == nil {
			t.Errorf("pathPrefix %q: expected an error", prefix)
		}
	}
}

func TestValidateRejectsControlListenOverlapWithPortFormatting(t *testing.T) {
	cfg := Config{
		Models:  []ModelConfig{testModel()},
		Control: &ControlConfig{Listen: "127.0.0.1:01"},
		Servers: []Server{
			{Name: "a", Kind: "http", Listen: "127.0.0.1:1", Root: "/a", Agents: []string{"build"}},
		},
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected normalized control/server ports to conflict")
	}
}

func TestValidateRejectsControlProjectListenOverlap(t *testing.T) {
	cfg := Config{
		Models:  []ModelConfig{testModel()},
		Control: &ControlConfig{Listen: "127.0.0.1:1", ProjectListen: "127.0.0.1:1"},
		Servers: []Server{
			{Name: "a", Kind: "http", Listen: "127.0.0.1:2", Root: "/a", Agents: []string{"build"}},
		},
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected control.projectListen to conflict with control.listen")
	}
}

func TestValidateRejectsEquivalentIPv6Listen(t *testing.T) {
	cfg := Config{
		Models:  []ModelConfig{testModel()},
		Control: &ControlConfig{Listen: "[0:0:0:0:0:0:0:1]:1"},
		Servers: []Server{
			{Name: "a", Kind: "http", Listen: "[::1]:1", Root: "/a", Agents: []string{"build"}},
		},
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected equivalent IPv6 listen addresses to conflict")
	}
}

func TestValidateRejectsPercentEncodedPathPrefix(t *testing.T) {
	cfg := Config{
		Models: []ModelConfig{testModel()},
		Servers: []Server{
			{Name: "a", Kind: "http", Listen: "127.0.0.1:1", Root: "/a", Agents: []string{"build"}, PathPrefix: "/x%20y"},
		},
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected a percent-encoded pathPrefix to be rejected")
	}
}

func TestServerValidateRejectsEmptyName(t *testing.T) {
	registry, err := (Config{Models: []ModelConfig{testModel()}}).ModelRegistry()
	if err != nil {
		t.Fatal(err)
	}
	server := Server{Kind: KindHTTP, Listen: "127.0.0.1:1", Root: "/a", Agents: []string{"build"}}
	if err := server.Validate(registry); err == nil {
		t.Fatal("expected an empty server name to be rejected")
	}
}
