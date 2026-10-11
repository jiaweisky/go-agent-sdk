package main

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"

	config "github.com/jiaweisky/go-agent-sdk/cmd/agentd/config"
)

func stubServer(name, host, mount, kind string, cards func(*http.Request) []*a2a.AgentCard) *builtServer {
	return &builtServer{
		cfg: config.Server{Name: name, Kind: kind, Host: host, Mount: mount},
		handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(name))
		}),
		cards: cards,
	}
}

func cardSet(names ...string) func(*http.Request) []*a2a.AgentCard {
	return func(*http.Request) []*a2a.AgentCard {
		cards := make([]*a2a.AgentCard, 0, len(names))
		for _, name := range names {
			cards = append(cards, &a2a.AgentCard{Name: name})
		}
		return cards
	}
}

func routeBody(t *testing.T, h http.Handler, host, path string) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "http://"+host+path, nil)
	req.Host = host
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return strings.TrimSpace(rec.Body.String())
}

func TestRouterDispatch(t *testing.T) {
	h := newRouter([]*builtServer{
		stubServer("root", "", "", "http", nil),
		stubServer("mounted", "", "/api2", "http", nil),
		stubServer("hosted", "h.example.com", "", "http", nil),
	})
	cases := []struct{ host, path, want string }{
		{"x", "/foo", "root"},
		{"x", "/api2/bar", "mounted"},
		{"x", "/api3", "root"},                   // no mount match → root
		{"h.example.com", "/foo", "hosted"},      // host match
		{"H.EXAMPLE.COM", "/foo", "hosted"},      // host match is case-insensitive
		{"h.example.com:8080", "/foo", "hosted"}, // host match ignores the port
	}
	for _, c := range cases {
		if got := routeBody(t, h, c.host, c.path); got != c.want {
			t.Errorf("host %q path %q = %q, want %q", c.host, c.path, got, c.want)
		}
	}
}

func TestRouterPrefersForwardedHost(t *testing.T) {
	h := newRouter([]*builtServer{
		stubServer("root", "", "", "http", nil),
		stubServer("hosted", "h.example.com", "", "http", nil),
	})
	req := httptest.NewRequest(http.MethodGet, "http://internal/foo", nil)
	req.Host = "internal"
	req.Header.Set("X-Forwarded-Host", "h.example.com")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if got := strings.TrimSpace(rec.Body.String()); got != "hosted" {
		t.Fatalf("forwarded host = %q, want hosted", got)
	}
}

func TestRouterDiscovery(t *testing.T) {
	defaulted := stubServer("a2a-b", "b.example.com", "", "a2a", cardSet("B"))
	defaulted.cfg.Default = true
	h := newRouter([]*builtServer{
		stubServer("root", "", "", "http", nil),
		stubServer("a2a-a", "", "/agents-a", "a2a", cardSet("A")),
		defaulted,
	})
	// Origin-root single card = the default A2A entry.
	if got := routeBody(t, h, "x", "/.well-known/agent-card.json"); !strings.Contains(got, `"B"`) {
		t.Errorf("root card = %q, want B", got)
	}
	// Aggregate across the listener's A2A entries.
	var cards []a2a.AgentCard
	if err := json.Unmarshal([]byte(routeBody(t, h, "x", "/.well-known/agents.json")), &cards); err != nil {
		t.Fatalf("decode aggregate: %v", err)
	}
	if len(cards) != 2 {
		t.Fatalf("aggregate = %d cards, want 2", len(cards))
	}
}

func TestPublicBaseURL(t *testing.T) {
	addr := &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 8788}
	cases := []struct {
		name string
		s    config.Server
		want string
	}{
		{"listen", config.Server{Listen: "127.0.0.1:8788"}, "http://127.0.0.1:8788"},
		{"mount", config.Server{Listen: "127.0.0.1:8788", Mount: "/api"}, "http://127.0.0.1:8788/api"},
		{"host", config.Server{Host: "h.example.com"}, "http://h.example.com"},
		{"tls", config.Server{Listen: "127.0.0.1:8788", TLS: &config.TLS{CertFile: "c", KeyFile: "k"}}, "https://127.0.0.1:8788"},
		{"baseURL", config.Server{BaseURL: "https://pub.example.com/prefix/", Mount: "/api"}, "https://pub.example.com/prefix"},
	}
	for _, c := range cases {
		if got := publicBaseURL(c.s, addr); got != c.want {
			t.Errorf("%s: publicBaseURL = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestDiscoveryBaseURL(t *testing.T) {
	addr := &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 8788}
	cases := []struct {
		name string
		s    config.Server
		want string
	}{
		{"listen", config.Server{Listen: "127.0.0.1:8788"}, "http://127.0.0.1:8788"},
		{"host", config.Server{Host: "h.example.com"}, "http://h.example.com"},
		{"tls", config.Server{Listen: "127.0.0.1:8788", TLS: &config.TLS{CertFile: "c", KeyFile: "k"}}, "https://127.0.0.1:8788"},
		{"baseURL drops path", config.Server{BaseURL: "https://pub.example.com/prefix"}, "https://pub.example.com"},
	}
	for _, c := range cases {
		if got := discoveryBaseURL(c.s, addr); got != c.want {
			t.Errorf("%s: discoveryBaseURL = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestDefaultA2AEntry(t *testing.T) {
	httpEntry := stubServer("http", "", "", "http", nil)
	a2aA := stubServer("a", "", "", "a2a", cardSet("A"))
	a2aB := stubServer("b", "", "", "a2a", cardSet("B"))
	a2aB.cfg.Default = true
	if got := defaultA2AEntry([]*builtServer{httpEntry, a2aA, a2aB}); got != a2aB {
		t.Errorf("defaultA2AEntry = %v, want the Default entry", got)
	}
	if got := defaultA2AEntry([]*builtServer{httpEntry, a2aA}); got != a2aA {
		t.Errorf("defaultA2AEntry = %v, want the first A2A entry", got)
	}
	if got := defaultA2AEntry([]*builtServer{httpEntry}); got != nil {
		t.Errorf("defaultA2AEntry = %v, want nil", got)
	}
}
