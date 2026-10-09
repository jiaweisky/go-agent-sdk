//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"image/color"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	agent "github.com/jiaweisky/go-agent-sdk"
	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

// openAIStub is an OpenAI-compatible /chat/completions upstream that records the
// last request and returns a fixed text completion. It stands in for vLLM so the
// shim test runs offline and deterministically.
type openAIStub struct {
	mu    sync.Mutex
	last  map[string]any
	reply string
}

func (s *openAIStub) handler(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	var decoded map[string]any
	_ = json.Unmarshal(body, &decoded)
	s.mu.Lock()
	s.last = decoded
	s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id": "1", "object": "chat.completion", "model": "m",
		"choices": []any{map[string]any{
			"index": 0, "finish_reason": "stop",
			"message": map[string]any{"role": "assistant", "content": s.reply},
		}},
		"usage": map[string]any{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
	})
}

// geminiShim accepts the SDK's Gemini streamGenerateContent requests, forwards
// them to an OpenAI-compatible upstream, and emits Gemini SSE back. Dev/test
// tooling only.
func geminiShim(t *testing.T, upstream string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Contents []struct {
				Role  string           `json:"role"`
				Parts []map[string]any `json:"parts"`
			} `json:"contents"`
			SystemInstruction *struct {
				Parts []map[string]any `json:"parts"`
			} `json:"systemInstruction"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		openai := geminiToOpenAI(body.Contents, body.SystemInstruction, modelFromPath(r.URL.Path))
		payload, _ := json.Marshal(openai)
		resp, err := http.Post(upstream+"/chat/completions", "application/json", bytes.NewReader(payload))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		var completion struct {
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&completion); err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		text := ""
		if len(completion.Choices) > 0 {
			text = completion.Choices[0].Message.Content
		}
		frame, _ := json.Marshal(map[string]any{
			"candidates": []any{map[string]any{
				"content":      map[string]any{"role": "model", "parts": []any{map[string]any{"text": text}}},
				"finishReason": "STOP",
			}},
		})
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: " + string(frame) + "\n\n"))
	}))
}

func geminiToOpenAI(contents []struct {
	Role  string           `json:"role"`
	Parts []map[string]any `json:"parts"`
}, system *struct {
	Parts []map[string]any `json:"parts"`
}, model string) map[string]any {
	messages := []any{}
	if system != nil {
		messages = append(messages, map[string]any{"role": "system", "content": partsText(system.Parts)})
	}
	for _, content := range contents {
		role := "user"
		if content.Role == "model" {
			role = "assistant"
		}
		parts := []any{}
		for _, part := range content.Parts {
			switch {
			case part["text"] != nil:
				parts = append(parts, map[string]any{"type": "text", "text": part["text"]})
			case part["inlineData"] != nil:
				inl, _ := part["inlineData"].(map[string]any)
				mime, _ := inl["mimeType"].(string)
				data, _ := inl["data"].(string)
				parts = append(parts, openAIMedia(mime, "data:"+mime+";base64,"+data))
			case part["fileData"] != nil:
				fd, _ := part["fileData"].(map[string]any)
				mime, _ := fd["mimeType"].(string)
				uri, _ := fd["fileUri"].(string)
				parts = append(parts, openAIMedia(mime, uri))
			}
		}
		messages = append(messages, map[string]any{"role": role, "content": parts})
	}
	return map[string]any{"model": model, "messages": messages, "stream": false}
}

func openAIMedia(mime, value string) map[string]any {
	switch {
	case strings.HasPrefix(mime, "audio/"):
		if strings.HasPrefix(value, "data:") {
			if comma := strings.Index(value, ","); comma >= 0 {
				return map[string]any{"type": "input_audio", "input_audio": map[string]any{
					"data": value[comma+1:], "format": strings.TrimPrefix(mime, "audio/"),
				}}
			}
		}
		return map[string]any{"type": "audio_url", "audio_url": map[string]any{"url": value}}
	case strings.HasPrefix(mime, "video/"):
		return map[string]any{"type": "video_url", "video_url": map[string]any{"url": value}}
	default:
		return map[string]any{"type": "image_url", "image_url": map[string]any{"url": value}}
	}
}

func partsText(parts []map[string]any) string {
	var builder strings.Builder
	for _, part := range parts {
		if text, ok := part["text"].(string); ok {
			builder.WriteString(text)
		}
	}
	return builder.String()
}

func modelFromPath(path string) string {
	if i := strings.LastIndex(path, "/models/"); i >= 0 {
		path = path[i+len("/models/"):]
	}
	if i := strings.Index(path, ":"); i >= 0 {
		path = path[:i]
	}
	return path
}

// sendGemini drives one attachment through the SDK's Gemini protocol to the shim
// and returns the assistant text and whether it was omitted.
func sendGemini(t *testing.T, shimURL, model, mime, uri, prompt string) (string, bool) {
	t.Helper()
	client, err := agent.NewClient(&agent.ClientOptions{
		Root: t.TempDir(), Model: model, Permission: agent.ApproveAll, Credentials: agent.APIKey("k"),
		ModelInfo: func(id string) (schema.ModelInfo, error) {
			info, err := agent.NewModelInfo("gemini", id, shimURL, schema.ModelLimit{})
			if err != nil {
				return schema.ModelInfo{}, err
			}
			info.Capabilities.Input = []string{"text", "image", "audio", "video"}
			return info, nil
		},
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	defer client.Close()
	omitted := false
	unsubscribe := client.On(func(event agent.Event) {
		if _, ok := event.(agent.AttachmentOmitted); ok {
			omitted = true
		}
	})
	defer unsubscribe()
	if err := client.RegisterAgent("build", agent.AgentConfig{System: "You are a test."}); err != nil {
		t.Fatalf("register agent: %v", err)
	}
	session, err := client.CreateSession(context.Background(), &agent.SessionConfig{Agent: "build", Permissions: denyAllTools()})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer session.Close()

	var reply strings.Builder
	for event, err := range session.Send(context.Background(), agent.MessageOptions{
		Prompt: prompt,
		Files:  []schema.PromptFileAttachment{{URI: uri, Mime: mime}},
	}) {
		if err != nil {
			t.Fatalf("send: %v", err)
		}
		if delta, ok := event.(agent.TextDelta); ok {
			reply.WriteString(delta.Delta)
		}
	}
	return reply.String(), omitted
}

// upstreamSawBlock reports whether the stub received an OpenAI content block of
// the given type.
func upstreamSawBlock(stub *openAIStub, block string) bool {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	if stub.last == nil {
		return false
	}
	for _, message := range asSlice(stub.last["messages"]) {
		msg, _ := message.(map[string]any)
		for _, part := range asSlice(msg["content"]) {
			p, _ := part.(map[string]any)
			if p["type"] == block {
				return true
			}
		}
	}
	return false
}

func asSlice(value any) []any {
	slice, _ := value.([]any)
	return slice
}

// TestGeminiMedia exercises the Gemini wire end to end for every modality: the
// SDK lowers media to the Gemini protocol, the shim forwards it to an upstream,
// and the reply returns as Gemini SSE. By default the upstream is an in-process
// stub (offline; it asserts the media block arrived); set VLLM_BASE_URL and
// VLLM_MODEL to forward to a real vLLM (with semantic assertions instead).
func TestGeminiMedia(t *testing.T) {
	realUpstream := os.Getenv("VLLM_BASE_URL")
	realModel := os.Getenv("VLLM_MODEL")
	real := realUpstream != "" && realModel != ""

	cases := []struct {
		name    string
		mime    string
		fixture func(*testing.T) string
		block   string
		want    string // semantic word required from a real model ("" = non-empty only)
	}{
		{"image", "image/png", func(t *testing.T) string {
			return dataURL("image/png", solidPNG(t, color.RGBA{R: 255, A: 255}))
		}, "image_url", "red"},
		{"audio", "audio/wav", func(t *testing.T) string {
			return dataURL("audio/wav", toneWAV(t))
		}, "input_audio", ""},
		{"video", "video/jpeg", videoDataURL, "video_url", "red"},
		{"image_url_source", "image/png", func(*testing.T) string { return publicImageURL }, "image_url", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			upstream, model := realUpstream, realModel
			var stub *openAIStub
			if real {
				upstream = strings.TrimSuffix(upstream, "/")
			} else {
				stub = &openAIStub{reply: "seen"}
				srv := httptest.NewServer(http.HandlerFunc(stub.handler))
				defer srv.Close()
				upstream, model = srv.URL, "Qwen3-Omni"
			}
			shim := geminiShim(t, upstream)
			defer shim.Close()

			produce := func() (string, bool) {
				return sendGemini(t, shim.URL, model, tc.mime, tc.fixture(t),
					"Describe this media and answer with the dominant color or a short description.")
			}

			// A real model can occasionally refuse or answer off-script for a
			// color case, so retry those a few times.
			if real && tc.want != "" {
				replyRed(t, produce)
				return
			}

			reply, omitted := produce()
			if omitted {
				t.Fatalf("%s was omitted", tc.name)
			}
			if !real {
				if !upstreamSawBlock(stub, tc.block) {
					t.Fatalf("the upstream did not receive a %s block", tc.block)
				}
				return
			}
			if strings.TrimSpace(reply) == "" {
				t.Fatalf("empty reply: the %s was not accepted", tc.name)
			}
		})
	}
}
