//go:build e2e

package e2e

import (
	"bufio"
	"context"
	"encoding/json"
	"image/color"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	agent "github.com/jiaweisky/go-agent-sdk"
	"github.com/jiaweisky/go-agent-sdk/schema"
	"github.com/jiaweisky/go-agent-sdk/server"
)

// TestServerPromptWithMedia drives the HTTP/SSE surface end to end against a
// real model for every modality: create a session over HTTP, POST a prompt
// carrying a media attachment, then read the streamed assistant text. It proves
// the attachment survives the host boundary rather than only the in-process SDK
// path.
//
//	export VLLM_BASE_URL=http://host:8000/v1 VLLM_MODEL=...
func TestServerPromptWithMedia(t *testing.T) {
	baseURL, model := vllmEnv(t)

	cases := []struct {
		name    string
		mime    string
		fixture func(*testing.T) string
		red     bool
	}{
		{"image", "image/png", func(t *testing.T) string {
			return dataURL("image/png", solidPNG(t, color.RGBA{R: 255, A: 255}))
		}, true},
		{"audio", "audio/wav", func(t *testing.T) string {
			return dataURL("audio/wav", toneWAV(t))
		}, false},
		{"video", "video/jpeg", videoDataURL, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := newVLLMMediaClient(t, baseURL, model)
			defer client.Close()
			if err := client.RegisterAgent("build", agent.AgentConfig{System: "You are a test."}); err != nil {
				t.Fatalf("register agent: %v", err)
			}
			omitted := false
			unsubscribe := client.On(func(event agent.Event) {
				if _, ok := event.(agent.AttachmentOmitted); ok {
					omitted = true
				}
			})
			defer unsubscribe()

			srv, err := server.New(context.Background(), client, server.Options{
				InboundPermissions: denyAllTools(),
			})
			if err != nil {
				t.Fatalf("new server: %v", err)
			}
			defer srv.Close()
			ts := httptest.NewServer(srv.Handler())
			defer ts.Close()

			// produce runs one turn over HTTP, returning the streamed text and
			// whether the attachment was omitted.
			produce := func() (string, bool) {
				id := createSession(t, ts, "build")
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
				defer cancel()
				req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/api/session/"+id+"/event", nil)
				resp, err := http.DefaultClient.Do(req)
				if err != nil {
					t.Fatalf("sse connect: %v", err)
				}
				defer resp.Body.Close()

				body, _ := json.Marshal(map[string]any{
					"prompt": "Describe the attached media briefly; if it is a solid color, name it in one word.",
					"files":  []schema.PromptFileAttachment{{URI: tc.fixture(t), Mime: tc.mime}},
				})
				post, err := http.Post(ts.URL+"/api/session/"+id+"/prompt", "application/json", strings.NewReader(string(body)))
				if err != nil {
					t.Fatalf("prompt: %v", err)
				}
				post.Body.Close()
				if post.StatusCode != http.StatusAccepted {
					t.Fatalf("prompt status = %d", post.StatusCode)
				}
				return readStreamText(t, resp), omitted
			}

			if tc.red {
				replyRed(t, produce)
				return
			}
			text, omitted := produce()
			if omitted {
				t.Fatalf("the %s attachment was omitted over the HTTP path", tc.name)
			}
			if strings.TrimSpace(text) == "" {
				t.Fatal("empty reply: the attachment did not reach the model")
			}
		})
	}
}

func vllmEnv(t *testing.T) (baseURL, model string) {
	t.Helper()
	baseURL = os.Getenv("VLLM_BASE_URL")
	model = os.Getenv("VLLM_MODEL")
	if baseURL == "" || model == "" {
		t.Skip("set VLLM_BASE_URL and VLLM_MODEL to run the vLLM E2E")
	}
	return baseURL, model
}

// newVLLMMediaClient builds a client that reaches a real OpenAI-compatible
// (vLLM) model capable of image/audio/video input.
func newVLLMMediaClient(t *testing.T, baseURL, model string) *agent.Client {
	t.Helper()
	return newVLLMMediaClientInput(t, baseURL, model, []string{"text", "image", "audio", "video"})
}

// newVLLMMediaClientInput is newVLLMMediaClient with an explicit input-modality
// declaration (empty means unconstrained).
func newVLLMMediaClientInput(t *testing.T, baseURL, model string, input []string) *agent.Client {
	t.Helper()
	client, err := agent.NewClient(&agent.ClientOptions{
		Root:        t.TempDir(),
		Model:       model,
		Permission:  agent.ApproveAll,
		Credentials: agent.APIKey("EMPTY"),
		ModelInfo: func(id string) (schema.ModelInfo, error) {
			info, err := agent.NewModelInfo("openai-compatible-chat", id, baseURL, schema.ModelLimit{Output: 1024})
			if err != nil {
				return schema.ModelInfo{}, err
			}
			info.Capabilities.Input = input
			return info, nil
		},
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	return client
}

func createSession(t *testing.T, ts *httptest.Server, agentName string) string {
	t.Helper()
	resp, err := http.Post(ts.URL+"/api/session", "application/json", strings.NewReader(`{"agent":"`+agentName+`"}`))
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer resp.Body.Close()
	var out struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode session: %v", err)
	}
	if out.Data.ID == "" {
		t.Fatal("empty session id")
	}
	return out.Data.ID
}

// readStreamText reads SSE frames until the assistant text ends, returning the
// accumulated text.
func readStreamText(t *testing.T, resp *http.Response) string {
	t.Helper()
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	var text strings.Builder
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var ev struct {
			Type string `json:"type"`
			Data struct {
				Delta string `json:"delta"`
				Text  string `json:"text"`
			} `json:"data"`
		}
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &ev); err != nil {
			continue
		}
		switch ev.Type {
		case "session.next.text.delta":
			text.WriteString(ev.Data.Delta)
		case "session.next.text.ended":
			if ev.Data.Text != "" {
				return ev.Data.Text
			}
			return text.String()
		}
	}
	return text.String()
}
