//go:build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// TestAgentdSupervisor runs both binaries end to end: a real agentd with its
// control API enabled, and agentd-supervisor satisfying the
// supervisor_command protocol against it. It acquires a writable lease (the
// instance must actually serve sessions), acquires a read-only lease (it must
// share the source directory), rejects an agent the template does not expose,
// and releases (the instance must go away; re-releasing stays ok).
//
//	go test -tags e2e ./e2e/ -run TestAgentdSupervisor
func TestAgentdSupervisor(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the control API and the isolation views are unix-only paths")
	}

	_, thisFile, _, _ := runtime.Caller(0)
	moduleRoot := filepath.Dir(filepath.Dir(thisFile))
	work := t.TempDir()
	build := func(target, pkg string) string {
		bin := filepath.Join(work, target)
		cmd := exec.Command("go", "build", "-o", bin, pkg)
		cmd.Dir = moduleRoot
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("build %s: %v\n%s", target, err, out)
		}
		return bin
	}
	agentd := build("agentd", "./cmd/agentd")
	supervisor := build("agentd-supervisor", "./cmd/agentd-supervisor")

	_, controlPort := freePort(t)
	_, staticPort := freePort(t)
	source := filepath.Join(work, "source")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	token := "ctl-token"

	agentdCfg := filepath.Join(work, "agentd.yaml")
	writeFile(t, agentdCfg, fmt.Sprintf(`models:
  - id: m
    model: m
    protocol: openai-compatible-chat
    modelBaseURL: http://127.0.0.1:1
    credentials: { apiKey: x }
    inputModalities: [text]
servers:
  - name: static
    kind: http
    listen: 127.0.0.1:%d
    root: %s
    workingDirectory: %s
    models: [m]
    defaultModel: m
    agents: [build]
control:
  listen: 127.0.0.1:%d
  auth:
    type: bearer
    token: %s
`, staticPort, filepath.Join(work, "static-root"), source, controlPort, token))
	agentdProc := startAgentd(t, agentd, agentdCfg)
	defer func() {
		_ = agentdProc.Process.Kill()
		_, _ = agentdProc.Process.Wait()
	}()
	waitForControl(t, controlPort, token)

	supCfg := filepath.Join(work, "supervisor.yaml")
	writeFile(t, supCfg, fmt.Sprintf(`agentd:
  baseURL: http://127.0.0.1:%d
  token: %s
project:
  kind: http
  rootBase: %s
  workingDirectory: %s
  models: [m]
  defaultModel: m
  agents: [build]
  isolation:
    mode: copy
    baseDir: %s
`, controlPort, token, filepath.Join(work, "projects"), source, filepath.Join(work, "views")))

	ask := func(t *testing.T, req string) (string, string) {
		t.Helper()
		var stdout, stderr bytes.Buffer
		cmd := exec.Command(supervisor, "-config", supCfg)
		cmd.Stdin = bytes.NewReader([]byte(req))
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		err := cmd.Run()
		return stdout.String(), stderr.String() + errSuffix(err)
	}

	// A writable acquire returns a lease whose instance really serves.
	out, diag := ask(t, `{"action":"acquire","project_id":"wecom/g1","transport":"http"}`)
	if diag != "" {
		t.Fatalf("acquire failed: %s", diag)
	}
	var lease struct {
		ProjectID        string `json:"project_id"`
		Transport        string `json:"transport"`
		ServerBase       string `json:"server_base"`
		WorkingDirectory string `json:"working_directory"`
		Ephemeral        bool   `json:"ephemeral"`
	}
	if err := json.Unmarshal([]byte(out), &lease); err != nil {
		t.Fatalf("acquire response %q: %v", out, err)
	}
	if lease.ProjectID != "wecom/g1" || lease.Transport != "http" || !lease.Ephemeral || lease.ServerBase == "" {
		t.Fatalf("lease = %+v", lease)
	}
	if lease.WorkingDirectory == source {
		t.Fatalf("a writable lease must be served from an isolated view, not %s", source)
	}
	sessionURL := lease.ServerBase + "/api/session"
	createSession := func() error {
		resp, err := http.Post(sessionURL, "application/json", bytes.NewReader([]byte(`{"agent":"build"}`)))
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("POST %s = %d", sessionURL, resp.StatusCode)
		}
		return nil
	}
	waitFor(t, "the leased instance to serve", createSession)

	// A read-only acquire shares the source directory.
	out, diag = ask(t, `{"action":"acquire","project_id":"ro1","readonly":true}`)
	if diag != "" {
		t.Fatalf("read-only acquire failed: %s", diag)
	}
	var roLease map[string]any
	if err := json.Unmarshal([]byte(out), &roLease); err != nil {
		t.Fatalf("read-only lease %q: %v", out, err)
	}
	if roLease["working_directory"] != source {
		t.Fatalf("read-only lease working_directory = %v, want the shared source %s", roLease["working_directory"], source)
	}

	// An agent outside the template is rejected before any control call.
	if out, diag = ask(t, `{"action":"acquire","project_id":"x","agent":"review"}`); diag == "" {
		t.Fatalf("an unknown agent must be rejected, got %q", out)
	}

	// Release stops the instance; a repeated release still succeeds.
	out, diag = ask(t, `{"action":"release","project_id":"wecom/g1"}`)
	if diag != "" || out == "" {
		t.Fatalf("release: out=%q diag=%s", out, diag)
	}
	var released map[string]bool
	if err := json.Unmarshal([]byte(out), &released); err != nil || !released["ok"] {
		t.Fatalf("release response = %q (%v)", out, err)
	}
	waitFor(t, "the released instance to go away", func() error {
		if err := createSession(); err == nil {
			return fmt.Errorf("%s still serves after release", sessionURL)
		}
		return nil
	})
	if out, diag = ask(t, `{"action":"release","project_id":"wecom/g1"}`); diag != "" {
		t.Fatalf("a repeated release must succeed: %s", diag)
	}
}

// waitFor polls cond until it returns nil or the deadline passes, reporting the
// last condition error (or the poll itself) in the failure message.
func waitFor(t *testing.T, what string, cond func() error) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	var last error
	for {
		last = cond()
		if last == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("waiting for %s: %v", what, last)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// startAgentd launches agentd and waits for its startup log to appear.
func startAgentd(t *testing.T, bin, configPath string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(bin, "-config", configPath)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start agentd: %v", err)
	}
	return cmd
}

// waitForControl polls the control API until agentd is listening.
func waitForControl(t *testing.T, port int, token string) {
	t.Helper()
	url := fmt.Sprintf("http://127.0.0.1:%d/admin/projects", port)
	waitFor(t, "the control API to come up", func() error {
		req, _ := http.NewRequest(http.MethodGet, url, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return err
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("GET %s = %d", url, resp.StatusCode)
		}
		return nil
	})
}

// errSuffix renders an exec error (including the exit status) for diagnostics.
func errSuffix(err error) string {
	if err == nil {
		return ""
	}
	return fmt.Sprintf(": %v", err)
}

// writeFile writes content with restrictive permissions.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
