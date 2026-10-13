package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	config "github.com/jiaweisky/go-agent-sdk/cmd/agentd/config"
)

func testCatalog(t *testing.T) config.Catalog {
	t.Helper()
	cfg := config.Config{Models: []config.ModelConfig{{
		ID: "test-model", Model: "test-model", Protocol: "openai-responses",
		Credentials: config.Credentials{APIKey: "test"},
	}}}
	catalog, err := cfg.Catalog()
	if err != nil {
		t.Fatalf("catalog: %v", err)
	}
	return catalog
}

func testControlRequest(t *testing.T) controlRequest {
	return controlRequest{
		Name:             "grp-a",
		Kind:             "http",
		Root:             t.TempDir(),
		WorkingDirectory: t.TempDir(),
		Models:           []string{"test-model"},
		Agents:           []string{"build"},
	}
}

func TestManagerStartServeStop(t *testing.T) {
	manager := newProjectManager(context.Background(), testCatalog(t), &config.ControlConfig{})
	t.Cleanup(manager.Close)

	instance, err := manager.Start(testControlRequest(t))
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	resp, err := http.Get(instance.serverBase + "/api/health")
	if err != nil {
		t.Fatalf("health: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("health status = %d", resp.StatusCode)
	}

	if !manager.Stop("grp-a") {
		t.Fatal("stop reported no such project")
	}
	if len(manager.List()) != 0 {
		t.Fatal("instance was not removed")
	}
	if resp, err := http.Get(instance.serverBase + "/api/health"); err == nil {
		_ = resp.Body.Close()
		t.Fatal("expected the listener to be closed after stop")
	}
}

func TestManagerHonorsIsolation(t *testing.T) {
	manager := newProjectManager(context.Background(), testCatalog(t), &config.ControlConfig{})
	t.Cleanup(manager.Close)

	req := testControlRequest(t)
	req.Isolation = &config.IsolationConfig{Mode: "copy", BaseDir: t.TempDir()}
	instance, err := manager.Start(req)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if instance.workingDirectory == req.WorkingDirectory {
		t.Fatal("copy isolation did not produce a distinct view")
	}
	if _, err := os.Stat(instance.workingDirectory); err != nil {
		t.Fatalf("view missing: %v", err)
	}
	view := instance.workingDirectory
	manager.Stop("grp-a")
	if _, err := os.Stat(view); !os.IsNotExist(err) {
		t.Fatal("teardown did not remove the view")
	}
}

func TestManagerRejectsExternalMode(t *testing.T) {
	manager := newProjectManager(context.Background(), testCatalog(t), &config.ControlConfig{})
	t.Cleanup(manager.Close)
	req := testControlRequest(t)
	req.Isolation = &config.IsolationConfig{Mode: "container"}
	if _, err := manager.Start(req); err == nil {
		t.Fatal("expected container mode to be rejected by the local manager")
	}
}

func TestManagerRejectsDuplicateNameAndRoot(t *testing.T) {
	manager := newProjectManager(context.Background(), testCatalog(t), &config.ControlConfig{})
	t.Cleanup(manager.Close)

	root := t.TempDir()
	first := testControlRequest(t)
	first.Root = root
	if _, err := manager.Start(first); err != nil {
		t.Fatalf("first start: %v", err)
	}

	// Same name (even with a fresh root) is one instance per project.
	if _, err := manager.Start(testControlRequest(t)); err == nil {
		t.Fatal("expected a duplicate name to be rejected")
	}

	// A second project with a different name but the same root must be refused,
	// so two instances cannot share one SDK state directory.
	second := testControlRequest(t)
	second.Name = "grp-b"
	second.Root = root
	if _, err := manager.Start(second); err == nil {
		t.Fatal("expected a duplicate root to be rejected")
	}
}

func TestManagerEnforcesLeaseCap(t *testing.T) {
	manager := newProjectManager(context.Background(), testCatalog(t), &config.ControlConfig{MaxLeases: 1})
	t.Cleanup(manager.Close)

	if _, err := manager.Start(testControlRequest(t)); err != nil {
		t.Fatalf("first start: %v", err)
	}
	second := testControlRequest(t)
	second.Name = "grp-b"
	second.Root = t.TempDir()
	if _, err := manager.Start(second); err == nil {
		t.Fatal("expected the lease cap to reject the second project")
	}
}

func TestControlHandlerCRUDAndAuth(t *testing.T) {
	manager := newProjectManager(context.Background(), testCatalog(t), &config.ControlConfig{})
	t.Cleanup(manager.Close)
	server := httptest.NewServer(newControlHandler(manager, &config.AuthConfig{Bearer: "s3cret"}))
	defer server.Close()

	body, err := json.Marshal(testControlRequest(t))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	if code := doControl(t, http.MethodPost, server.URL+"/admin/projects", string(body), ""); code != http.StatusUnauthorized {
		t.Fatalf("unauthorized create = %d, want 401", code)
	}
	if code := doControl(t, http.MethodPost, server.URL+"/admin/projects", string(body), "s3cret"); code != http.StatusCreated {
		t.Fatalf("create = %d, want 201", code)
	}
	listBody := doControlBody(t, http.MethodGet, server.URL+"/admin/projects", "", "s3cret")
	if !strings.Contains(listBody, "grp-a") {
		t.Fatalf("list did not include the project: %s", listBody)
	}
	if code := doControl(t, http.MethodDelete, server.URL+"/admin/projects/grp-a", "", "s3cret"); code != http.StatusNoContent {
		t.Fatalf("delete = %d, want 204", code)
	}
	if code := doControl(t, http.MethodDelete, server.URL+"/admin/projects/grp-a", "", "s3cret"); code != http.StatusNotFound {
		t.Fatalf("second delete = %d, want 404", code)
	}
}

func TestControlHandlerDeleteSlashedID(t *testing.T) {
	manager := newProjectManager(context.Background(), testCatalog(t), &config.ControlConfig{})
	t.Cleanup(manager.Close)
	server := httptest.NewServer(newControlHandler(manager, nil))
	defer server.Close()

	// A project id may contain slashes (a conversation key); the delete route
	// must capture the whole remainder, not a single path segment.
	req := testControlRequest(t)
	req.Name = "wecom/group/42"
	if _, err := manager.Start(req); err != nil {
		t.Fatalf("start: %v", err)
	}
	if code := doControl(t, http.MethodDelete, server.URL+"/admin/projects/"+url.PathEscape(req.Name), "", ""); code != http.StatusNoContent {
		t.Fatalf("delete slashed id = %d, want 204", code)
	}
	if len(manager.List()) != 0 {
		t.Fatal("the project was not removed")
	}
}

func doControl(t *testing.T, method, url, body, token string) int {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

func doControlBody(t *testing.T, method, url, body, token string) string {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return string(data)
}
