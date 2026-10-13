package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	config "github.com/jiaweisky/go-agent-sdk/cmd/agentd/config"
)

func TestLoadSupervisorConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sup.yaml")
	body := `agentd:
  baseURL: http://127.0.0.1:9090
  token: ${AGENTD_SUPERVISOR_TOKEN:-dev}
project:
  kind: a2a
  rootBase: /var/lib/agentd/projects
  workingDirectory: /srv/source
  agents: [build]
  isolation:
    mode: overlay
    baseDir: /var/lib/agentd/views
    baseReadOnly: true
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	var cfg supervisorConfig
	if err := config.Load(path, &cfg); err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Agentd.BaseURL != "http://127.0.0.1:9090" || cfg.Agentd.Token != "dev" {
		t.Fatalf("agentd = %+v (env default not applied?)", cfg.Agentd)
	}
	if cfg.Project.Kind != "a2a" || cfg.Project.Isolation == nil || cfg.Project.Isolation.Mode != "overlay" {
		t.Fatalf("project = %+v", cfg.Project)
	}
}

// fakeAgentd is a stub of agentd's control API. It records the created project
// body and the released id, and fails requests whose Authorization is wrong.
func fakeAgentd(t *testing.T, created *config.ProjectRequest, released *string, token string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if token != "" && r.Header.Get("Authorization") != "Bearer "+token {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/admin/projects":
			if err := json.NewDecoder(r.Body).Decode(created); err != nil {
				t.Errorf("decode create body: %v", err)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
				"project_id":        created.Name,
				"server_base":       "http://127.0.0.1:9999",
				"transport":         created.Kind,
				"working_directory": "/var/lib/agentd/views/" + created.Name,
			}})
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/admin/projects/"):
			*released = strings.TrimPrefix(r.URL.Path, "/admin/projects/")
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func testConfig(baseURL, token string) supervisorConfig {
	return supervisorConfig{
		Agentd: endpoint{BaseURL: baseURL, Token: token},
		Project: projectTemplate{
			Kind:             "http",
			RootBase:         "/var/lib/agentd/projects",
			WorkingDirectory: "/srv/source",
			Models:           []string{"deepseek-flash"},
			DefaultModel:     "deepseek-flash",
			Agents:           []string{"build"},
			Isolation:        &config.IsolationConfig{Mode: "worktree", BaseDir: "/var/lib/agentd/views"},
			Auth:             &config.AuthConfig{Bearer: "instance-token"},
		},
	}
}

func TestAcquireCreatesProjectAndReturnsLease(t *testing.T) {
	var created config.ProjectRequest
	released := ""
	server := fakeAgentd(t, &created, &released, "ctl")
	defer server.Close()

	sup := newSupervisor(testConfig(server.URL, "ctl"))
	got, err := sup.acquire(request{Action: "acquire", ProjectID: "wecom/group/42", Transport: "http"})
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if got.ServerBase != "http://127.0.0.1:9999" || got.ProjectID != "wecom/group/42" || !got.Ephemeral {
		t.Fatalf("lease = %+v", got)
	}
	if got.WorkingDirectory != "/var/lib/agentd/views/wecom/group/42" {
		t.Fatalf("working directory = %q, want the view reported by agentd", got.WorkingDirectory)
	}
	if created.Name != "wecom/group/42" || created.Kind != "http" {
		t.Fatalf("posted name/kind = %q/%q", created.Name, created.Kind)
	}
	// The project id is sanitized into a single path element under rootBase.
	if created.Root != "/var/lib/agentd/projects/wecom_group_42" {
		t.Fatalf("root = %q", created.Root)
	}
	if created.Isolation == nil || created.Isolation.Mode != "worktree" {
		t.Fatalf("writable project must request the isolated view: %+v", created.Isolation)
	}
	if created.Auth == nil || created.Auth.Bearer != "instance-token" {
		t.Fatalf("auth = %+v", created.Auth)
	}
}

func TestAcquireReadonlySharesSource(t *testing.T) {
	var created config.ProjectRequest
	released := ""
	server := fakeAgentd(t, &created, &released, "")
	defer server.Close()

	sup := newSupervisor(testConfig(server.URL, ""))
	if _, err := sup.acquire(request{Action: "acquire", ProjectID: "p1", Readonly: true}); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if created.Isolation != nil {
		t.Fatalf("read-only project must not request an isolated view: %+v", created.Isolation)
	}
}

func TestAcquireRejectsUnknownAgent(t *testing.T) {
	sup := newSupervisor(testConfig("http://example.invalid", ""))
	if _, err := sup.acquire(request{Action: "acquire", ProjectID: "p1", Agent: "review"}); err == nil {
		t.Fatal("expected an agent outside the template's agents to be rejected")
	}
}

func TestReleaseDeletesProject(t *testing.T) {
	var created config.ProjectRequest
	released := ""
	server := fakeAgentd(t, &created, &released, "")
	defer server.Close()

	sup := newSupervisor(testConfig(server.URL, ""))
	if err := sup.release(request{Action: "release", ProjectID: "p1"}); err != nil {
		t.Fatalf("release: %v", err)
	}
	if released != "p1" {
		t.Fatalf("released = %q, want p1", released)
	}
}

func TestRequiresProjectID(t *testing.T) {
	sup := newSupervisor(testConfig("http://example.invalid", ""))
	if _, err := sup.acquire(request{Action: "acquire"}); err == nil {
		t.Fatal("expected acquire without project_id to fail")
	}
	if err := sup.release(request{Action: "release"}); err == nil {
		t.Fatal("expected release without project_id to fail")
	}
}

func TestAcquireRequiresBaseURL(t *testing.T) {
	sup := newSupervisor(supervisorConfig{})
	if _, err := sup.acquire(request{Action: "acquire", ProjectID: "p1"}); err == nil {
		t.Fatal("expected a missing agentd.baseURL to fail")
	}
}

func TestControlErrorPropagates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":"lease limit reached (1)"}`, http.StatusBadRequest)
	}))
	defer server.Close()

	sup := newSupervisor(testConfig(server.URL, ""))
	if _, err := sup.acquire(request{Action: "acquire", ProjectID: "p1"}); err == nil {
		t.Fatal("expected a control API error to propagate")
	}
}

func TestUnknownActionFails(t *testing.T) {
	sup := newSupervisor(testConfig("http://example.invalid", ""))
	if _, err := sup.handle(request{Action: "nope"}); err == nil {
		t.Fatal("expected an unknown action to fail")
	}
}
