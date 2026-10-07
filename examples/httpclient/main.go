// Command httpclient is a minimal Go client for cmd/agentserve. It creates a
// session, sends a prompt, streams the turn's events over SSE and auto-approves
// permission requests — a worked example of the wire contract that a client in
// another language (e.g. a Python/Node bot) mirrors.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

func main() {
	base := flag.String("url", "http://127.0.0.1:8787", "agentserve base URL")
	agentID := flag.String("agent", "build", "agent to run")
	prompt := flag.String("prompt", "Say hello, then read the repository README.", "prompt to send")
	token := flag.String("token", "", "bearer token (when the server requires auth)")
	timeout := flag.Duration("timeout", 2*time.Minute, "overall timeout")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	c := &client{base: strings.TrimRight(*base, "/"), token: *token}
	if err := c.run(ctx, *agentID, *prompt); err != nil {
		fmt.Fprintln(os.Stderr, "httpclient:", err)
		os.Exit(1)
	}
}

type client struct {
	base  string
	token string
}

func (c *client) run(ctx context.Context, agentID, prompt string) error {
	sessionID, err := c.createSession(ctx, agentID)
	if err != nil {
		return err
	}
	fmt.Printf("session %s\n\n", sessionID)

	stream, err := c.openEvents(ctx, sessionID)
	if err != nil {
		return err
	}
	defer stream.Body.Close()

	if err := c.prompt(ctx, sessionID, prompt); err != nil {
		return err
	}
	return c.consume(ctx, sessionID, stream)
}

func (c *client) createSession(ctx context.Context, agentID string) (string, error) {
	body := map[string]any{"agent": agentID}
	var out struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := c.do(ctx, http.MethodPost, "/api/session", body, &out); err != nil {
		return "", err
	}
	if out.Data.ID == "" {
		return "", fmt.Errorf("server returned no session id")
	}
	return out.Data.ID, nil
}

func (c *client) prompt(ctx context.Context, sessionID, prompt string) error {
	return c.do(ctx, http.MethodPost, "/api/session/"+sessionID+"/prompt", map[string]any{"prompt": prompt}, nil)
}

func (c *client) openEvents(ctx context.Context, sessionID string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/api/session/"+sessionID+"/event", nil)
	if err != nil {
		return nil, err
	}
	c.authorize(req)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("event stream status %d", resp.StatusCode)
	}
	return resp, nil
}

// consume reads SSE frames until the session goes idle.
func (c *client) consume(ctx context.Context, sessionID string, stream *http.Response) error {
	scanner := bufio.NewScanner(stream.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var frame strings.Builder
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case line == "":
			if idle := c.handleFrame(ctx, sessionID, frame.String()); idle {
				return nil
			}
			frame.Reset()
		case strings.HasPrefix(line, "data:"):
			frame.WriteString(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	return scanner.Err()
}

func (c *client) handleFrame(ctx context.Context, sessionID, data string) (idle bool) {
	if data == "" {
		return false
	}
	var event struct {
		Type string          `json:"type"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal([]byte(data), &event); err != nil {
		return false
	}
	switch event.Type {
	case "session.next.text.delta":
		var payload struct {
			Delta string `json:"delta"`
		}
		_ = json.Unmarshal(event.Data, &payload)
		fmt.Print(payload.Delta)
	case "permission.v2.asked":
		var payload struct {
			ID     string `json:"id"`
			Action string `json:"action"`
		}
		_ = json.Unmarshal(event.Data, &payload)
		fmt.Printf("\n[permission] %s -> approving once\n", payload.Action)
		if err := c.replyPermission(ctx, sessionID, payload.ID); err != nil {
			fmt.Fprintf(os.Stderr, "reply: %v\n", err)
		}
	case "session.status":
		var payload struct {
			Status struct {
				Type string `json:"type"`
			} `json:"status"`
		}
		_ = json.Unmarshal(event.Data, &payload)
		if payload.Status.Type == "idle" {
			fmt.Println()
			return true
		}
	}
	return false
}

func (c *client) replyPermission(ctx context.Context, sessionID, requestID string) error {
	path := "/api/session/" + sessionID + "/permission/" + requestID + "/reply"
	return c.do(ctx, http.MethodPost, path, map[string]any{"reply": "once"}, nil)
}

func (c *client) do(ctx context.Context, method, path string, body, out any) error {
	var reader *bytes.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(encoded)
	} else {
		reader = bytes.NewReader(nil)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	c.authorize(req)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		var apiErr struct {
			Error   string `json:"error"`
			Message string `json:"message"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&apiErr)
		return fmt.Errorf("%s %s: %d %s", method, path, resp.StatusCode, apiErr.Message)
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

func (c *client) authorize(req *http.Request) {
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
}
