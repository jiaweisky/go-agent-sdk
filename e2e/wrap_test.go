//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// TestAgentdWholeProcessWrap builds agentd, runs it under the platform
// whole-process sandbox (seatbelt on macOS, bwrap on Linux) and fetches the
// public A2A discovery card, proving the wrapped process still serves (keeps the
// network) from inside the sandbox. It needs no model call.
//
//	go test -tags e2e ./e2e/ -run TestAgentdWholeProcessWrap
func TestAgentdWholeProcessWrap(t *testing.T) {
	wrappers := map[string]string{"darwin": "seatbelt", "linux": "bwrap"}
	binaries := map[string]string{"seatbelt": "sandbox-exec", "bwrap": "bwrap"}
	wrapper, ok := wrappers[runtime.GOOS]
	if !ok {
		t.Skip("no whole-process wrapper for " + runtime.GOOS)
	}
	if _, err := exec.LookPath(binaries[wrapper]); err != nil {
		t.Skipf("%s is not available", binaries[wrapper])
	}

	_, thisFile, _, _ := runtime.Caller(0)
	moduleRoot := filepath.Dir(filepath.Dir(thisFile))
	work := t.TempDir()
	agentd := filepath.Join(work, "agentd")
	build := exec.Command("go", "build", "-o", agentd, "./cmd/agentd")
	build.Dir = moduleRoot
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build agentd: %v\n%s", err, out)
	}

	listen, port := freePort(t)
	root := filepath.Join(work, "state")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(work, "agentd.yaml")
	if err := os.WriteFile(configPath, []byte(wrapConfig(wrapper, root, listen)), 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(agentd, "-config", configPath)
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start agentd: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	url := fmt.Sprintf("http://127.0.0.1:%d/.well-known/agent-card.json", port)
	var card map[string]any
	deadline := time.Now().Add(10 * time.Second)
	for {
		resp, err := http.Get(url)
		if err == nil {
			if resp.StatusCode == http.StatusOK {
				_ = json.NewDecoder(resp.Body).Decode(&card)
			}
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK && card != nil {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("the sandboxed agentd never served %s", url)
		}
		time.Sleep(100 * time.Millisecond)
	}
	if card["name"] == nil {
		t.Fatalf("the agent card is missing a name: %v", card)
	}
}

// freePort reserves and releases a loopback port, returning it as an address and
// a number.
func freePort(t *testing.T) (string, int) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	defer ln.Close()
	addr := ln.Addr().(*net.TCPAddr)
	return addr.String(), addr.Port
}

// wrapConfig is a minimal A2A configuration (a dummy model, one exposed agent)
// sufficient to serve a discovery card without calling a model.
func wrapConfig(wrapper, root, listen string) string {
	return fmt.Sprintf(`wrap:
  wrapper: %s
models:
  - id: m
    model: m
    protocol: openai-compatible-chat
    modelBaseURL: http://127.0.0.1:1
    credentials: { apiKey: x }
    inputModalities: [text]
servers:
  - name: a2a
    kind: a2a
    listen: %s
    root: %s
    workingDirectory: %s
    defaultModel: m
    agents: [build]
    baseURL: http://%s
    cards:
      build:
        name: Build
        description: Build agent
`, wrapper, listen, root, root, listen)
}
