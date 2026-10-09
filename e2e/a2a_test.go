//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"image/color"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"

	"iter"

	agent "github.com/jiaweisky/go-agent-sdk"
	a2aserver "github.com/jiaweisky/go-agent-sdk/pkg/a2a/server"
	llmschema "github.com/jiaweisky/go-agent-sdk/pkg/llm/schema"
	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

// registerExposed registers a media-capable "build" agent and marks it exposed.
func registerExposed(t *testing.T, client *agent.Client, name string) {
	t.Helper()
	if err := client.RegisterAgent("build", agent.AgentConfig{
		System: "You are a test.", Expose: &schema.AgentExpose{Name: name},
	}); err != nil {
		t.Fatalf("register agent: %v", err)
	}
}

// newA2AMediaServer serves client's "build" agent over A2A and returns a
// connected a2a-go client plus the httptest server.
func newA2AMediaServer(t *testing.T, client *agent.Client, inboundMedia string) (*httptest.Server, *a2aclient.Client) {
	t.Helper()
	var handler http.Handler
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(ts.Close)
	srv, err := a2aserver.New(a2aserver.Options{
		Factory: agent.NewA2AFactory(client,
			agent.WithInboundMedia(inboundMedia),
			agent.WithInboundPermissions(denyAllTools()),
		),
		Agents:  client.Agents().All(),
		BaseURL: ts.URL,
		Turns:   client.TurnStore(),
	})
	if err != nil {
		t.Fatalf("new a2a server: %v", err)
	}
	handler = srv.Handler()

	card := fetchA2ACard(t, ts.URL)
	c, err := a2aclient.NewFromCard(context.Background(), card)
	if err != nil {
		t.Fatalf("new a2a client: %v", err)
	}
	t.Cleanup(func() { _ = c.Destroy() })
	return ts, c
}

// a2aResult is the outcome of one A2A turn.
type a2aResult struct {
	artifact strings.Builder
	state    a2a.TaskState
	notices  []string // status-update messages (EventNotice surfaces here)
}

func (r a2aResult) text() string { return r.artifact.String() }

// sendA2ARaw sends one raw-bytes part over A2A and returns the streamed result.
func sendA2ARaw(t *testing.T, c *a2aclient.Client, mime string, data []byte, prompt string) a2aResult {
	t.Helper()
	part := &a2a.Part{Content: a2a.Raw(data), MediaType: mime}
	return sendA2AMessage(t, c, prompt, part)
}

func sendA2AMessage(t *testing.T, c *a2aclient.Client, prompt string, parts ...*a2a.Part) a2aResult {
	t.Helper()
	message := a2a.NewMessage(a2a.MessageRoleUser, append([]*a2a.Part{a2a.NewTextPart(prompt)}, parts...)...)
	message.ContextID = a2a.NewContextID()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	var result a2aResult
	for event, err := range c.SendStreamingMessage(ctx, &a2a.SendMessageRequest{Message: message}) {
		if err != nil {
			t.Fatalf("stream: %v", err)
		}
		switch e := event.(type) {
		case *a2a.Task:
			result.state = e.Status.State
		case *a2a.TaskArtifactUpdateEvent:
			for _, part := range e.Artifact.Parts {
				result.artifact.WriteString(part.Text())
			}
		case *a2a.TaskStatusUpdateEvent:
			result.state = e.Status.State
			if e.Status.Message != nil {
				for _, part := range e.Status.Message.Parts {
					if text := part.Text(); text != "" {
						result.notices = append(result.notices, text)
					}
				}
			}
		}
	}
	return result
}

// TestA2AInboundInlineMedia drives inbound inline media of every modality
// through the real HTTP/JSON-RPC A2A server to a real model.
//
//	export VLLM_BASE_URL=http://host:8000/v1 VLLM_MODEL=...
func TestA2AInboundInlineMedia(t *testing.T) {
	baseURL, model := vllmEnv(t)
	client := newVLLMMediaClient(t, baseURL, model)
	defer client.Close()
	registerExposed(t, client, "Media")
	_, c := newA2AMediaServer(t, client, "inline")

	cases := []struct {
		name    string
		mime    string
		fixture func(*testing.T) []byte
		red     bool
	}{
		{"image", "image/png", func(t *testing.T) []byte { return solidPNG(t, color.RGBA{R: 255, A: 255}) }, true},
		{"audio", "audio/wav", toneWAV, false},
		{"video", "video/jpeg", videoJPEG, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// produce runs one A2A turn, asserting the task completes.
			produce := func() (string, bool) {
				result := sendA2ARaw(t, c, tc.mime, tc.fixture(t), "Describe the attached media briefly.")
				if result.state != a2a.TaskStateCompleted {
					t.Fatalf("state = %s", result.state)
				}
				return result.text(), false
			}
			if tc.red {
				replyRed(t, produce)
				return
			}
			text, _ := produce()
			if strings.TrimSpace(text) == "" {
				t.Fatal("empty reply: the attachment did not reach the model")
			}
		})
	}
}

// TestA2AInboundOmissionNotice sends audio to a model that only accepts images,
// so the SDK omits it and surfaces an EventNotice, which the executor must map
// to a status message that reaches the A2A client.
func TestA2AInboundOmissionNotice(t *testing.T) {
	baseURL, model := vllmEnv(t)
	client := newVLLMMediaClientInput(t, baseURL, model, []string{"text", "image"})
	defer client.Close()
	registerExposed(t, client, "ImageOnly")
	_, c := newA2AMediaServer(t, client, "inline")

	result := sendA2ARaw(t, c, "audio/wav", toneWAV(t), "Describe this audio.")
	if result.state != a2a.TaskStateCompleted {
		t.Fatalf("state = %s", result.state)
	}
	joined := strings.Join(result.notices, "\n")
	if !strings.Contains(joined, "omitted") && !strings.Contains(joined, "audio/wav") {
		t.Fatalf("no omission notice reached the client: notices = %v", result.notices)
	}
}

// TestA2AInboundSpool verifies spool mode end to end: the raw part is written
// into the workspace (not inlined) and the turn still completes. The read-tool
// loop is not exercised because the available vLLM has no tool calling enabled.
func TestA2AInboundSpool(t *testing.T) {
	baseURL, model := vllmEnv(t)
	workspace := t.TempDir()
	client := newVLLMMediaSpoolClient(t, baseURL, model, workspace)
	defer client.Close()
	registerExposed(t, client, "Spool")
	_, c := newA2AMediaServer(t, client, "spool")

	png := solidPNG(t, color.RGBA{R: 255, A: 255})
	result := sendA2ARaw(t, c, "image/png", png, "An image is attached.")
	if result.state != a2a.TaskStateCompleted {
		t.Fatalf("state = %s", result.state)
	}
	if !spooledFileExists(workspace, png) {
		t.Fatal("the inbound attachment was not written into the workspace")
	}
}

// noopLLM satisfies ClientOptions.LLM for a caller whose agent delegates to a
// remote A2A endpoint, so the local model is never invoked.
type noopLLM struct{}

func (noopLLM) Prepare(context.Context, *llmschema.LLMRequest) (*llmschema.PreparedRequest, error) {
	return nil, nil
}
func (noopLLM) Generate(context.Context, *llmschema.LLMRequest) (*llmschema.LLMResponse, error) {
	return nil, nil
}
func (noopLLM) Stream(context.Context, *llmschema.LLMRequest) (iter.Seq2[llmschema.LLMEvent, error], error) {
	return func(yield func(llmschema.LLMEvent, error) bool) {}, nil
}

// TestA2AOutboundBackendMedia exercises the outbound A2A backend end to end: a
// caller client delegates to a remote agent served over A2A by a second client
// backed by a real model. The inline image must survive caller → A2A wire →
// remote → model and the reply must come back.
//
//	export VLLM_BASE_URL=http://host:8000/v1 VLLM_MODEL=...
func TestA2AOutboundBackendMedia(t *testing.T) {
	baseURL, model := vllmEnv(t)

	remote := newVLLMMediaClient(t, baseURL, model)
	defer remote.Close()
	registerExposed(t, remote, "Remote")
	remoteTS, _ := newA2AMediaServer(t, remote, "inline")

	caller, err := agent.NewClient(&agent.ClientOptions{
		Root:        t.TempDir(),
		Model:       model,
		Protocol:    "openai-compatible-chat",
		Credentials: agent.APIKey("EMPTY"),
		LLM:         noopLLM{},
		ModelInfo: func(id string) (schema.ModelInfo, error) {
			info, err := agent.NewModelInfo("openai-compatible-chat", id, baseURL, schema.ModelLimit{Output: 1024})
			if err != nil {
				return schema.ModelInfo{}, err
			}
			info.Capabilities.Input = []string{"text", "image", "audio", "video"}
			return info, nil
		},
	})
	if err != nil {
		t.Fatalf("new caller: %v", err)
	}
	defer caller.Close()
	if err := caller.RegisterAgent("remote", agent.AgentConfig{
		Backend: &schema.AgentBackend{
			Type: schema.AgentBackendA2A,
			A2A:  &schema.AgentA2ABackend{Endpoint: remoteTS.URL},
		},
	}); err != nil {
		t.Fatalf("register remote agent: %v", err)
	}

	omitted := false
	unsubscribe := caller.On(func(event agent.Event) {
		if _, ok := event.(agent.AttachmentOmitted); ok {
			omitted = true
		}
	})
	defer unsubscribe()

	cases := []struct {
		name    string
		mime    string
		fixture func(*testing.T) string
		red     bool
	}{
		{"image", "image/png", func(t *testing.T) string {
			return dataURL("image/png", solidPNG(t, color.RGBA{R: 255, A: 255}))
		}, true},
		{"audio", "audio/wav", func(t *testing.T) string { return dataURL("audio/wav", toneWAV(t)) }, false},
		{"video", "video/jpeg", videoDataURL, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// produce runs one outbound turn, returning the streamed reply and
			// whether the attachment was omitted.
			produce := func() (string, bool) {
				session, err := caller.CreateSession(context.Background(), &agent.SessionConfig{Agent: "remote", Permissions: denyAllTools()})
				if err != nil {
					t.Fatalf("create session: %v", err)
				}
				defer session.Close()

				var reply strings.Builder
				for event, err := range session.Send(context.Background(), agent.MessageOptions{
					Prompt: "Describe the attached media briefly; if it is a solid color, name it in one word.",
					Files:  []schema.PromptFileAttachment{{URI: tc.fixture(t), Mime: tc.mime}},
				}) {
					if err != nil {
						t.Fatalf("send: %v", err)
					}
					switch e := event.(type) {
					case agent.TextDelta:
						reply.WriteString(e.Delta)
					case agent.TextEnded:
						reply.WriteString(e.Text)
					}
				}
				return reply.String(), omitted
			}
			if tc.red {
				replyRed(t, produce)
				return
			}
			text, _ := produce()
			if strings.TrimSpace(text) == "" {
				t.Fatal("empty reply: the attachment did not reach the remote model")
			}
		})
	}
}

func fetchA2ACard(t *testing.T, baseURL string) *a2a.AgentCard {
	t.Helper()
	resp, err := http.Get(baseURL + "/.well-known/agent-card.json")
	if err != nil {
		t.Fatalf("get card: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("card status: %d", resp.StatusCode)
	}
	var card a2a.AgentCard
	if err := json.NewDecoder(resp.Body).Decode(&card); err != nil {
		t.Fatalf("decode card: %v", err)
	}
	return &card
}

// newVLLMMediaSpoolClient builds a client whose workspace is explicit, so a
// spool-mode test can inspect where inbound attachments land.
func newVLLMMediaSpoolClient(t *testing.T, baseURL, model, workspace string) *agent.Client {
	t.Helper()
	client, err := agent.NewClient(&agent.ClientOptions{
		Root:             workspace,
		WorkingDirectory: workspace,
		Model:            model,
		Permission:       agent.ApproveAll,
		Credentials:      agent.APIKey("EMPTY"),
		ModelInfo: func(id string) (schema.ModelInfo, error) {
			info, err := agent.NewModelInfo("openai-compatible-chat", id, baseURL, schema.ModelLimit{Output: 1024})
			if err != nil {
				return schema.ModelInfo{}, err
			}
			info.Capabilities.Input = []string{"text", "image"}
			return info, nil
		},
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	return client
}

// spooledFileExists reports whether a file under the workspace's .a2a directory
// has exactly the given bytes.
func spooledFileExists(workspace string, want []byte) bool {
	found := false
	_ = filepath.WalkDir(workspace, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.Contains(path, ".a2a") {
			return nil
		}
		if data, readErr := os.ReadFile(path); readErr == nil && bytes.Equal(data, want) {
			found = true
		}
		return nil
	})
	return found
}
