//go:build e2e

package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jiaweisky/go-agent-sdk/cmd/serveconfig"
	"github.com/jiaweisky/go-agent-sdk/schema"
)

// TestRunServesMediaTurn starts the real agentserve command against a live model
// and exercises the HTTP surface end to end: GET /api/model advertises the
// model's media capability, then one prompt per modality reaches the model and
// streams a reply back.
//
//	export VLLM_BASE_URL=http://host:8000/v1 VLLM_MODEL=...
func TestRunServesMediaTurn(t *testing.T) {
	baseURL := os.Getenv("VLLM_BASE_URL")
	model := os.Getenv("VLLM_MODEL")
	if baseURL == "" || model == "" {
		t.Skip("set VLLM_BASE_URL and VLLM_MODEL to run the agentserve E2E")
	}

	addr := freeAddr(t)
	cfg := defaultConfig()
	cfg.Listen = addr
	cfg.Root = t.TempDir()
	cfg.Model = model
	cfg.Protocol = "openai-compatible-chat"
	cfg.ModelBaseURL = baseURL
	cfg.InputModalities = []string{"text", "image", "audio", "video"}
	// The available vLLM has no tool calling enabled; deny tools so none are
	// advertised.
	cfg.InboundPermissions = []serveconfig.PermissionRule{{Action: "*", Resource: "*", Effect: "deny"}}
	cfg.Credentials = serveconfig.Credentials{APIKey: "EMPTY"}
	cfg.KeepAlive = ""

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- run(ctx, cfg) }()

	base := "http://" + addr
	waitForHealth(t, base)

	// GET /api/model advertises the catalog and its media capability.
	modelModes := fetchModelInputs(t, base, model)
	if !contains(modelModes, "image") {
		t.Fatalf("model input = %v, want image", modelModes)
	}

	cases := []struct {
		name    string
		mime    string
		fixture func(*testing.T) string
		red     bool
	}{
		{"image", "image/png", func(t *testing.T) string {
			return dataURL("image/png", redPNG(t))
		}, true},
		{"audio", "audio/wav", func(t *testing.T) string {
			return dataURL("audio/wav", toneWAV(t))
		}, false},
		{"video", "video/jpeg", func(t *testing.T) string {
			return dataURL("video/jpeg", redJPEG(t))
		}, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// produce runs one turn over HTTP, returning the streamed text.
			produce := func() string {
				id := postJSON(t, base+"/api/session", `{"agent":"build"}`, http.StatusOK)
				var created struct {
					Data struct {
						ID string `json:"id"`
					} `json:"data"`
				}
				if err := json.Unmarshal([]byte(id), &created); err != nil || created.Data.ID == "" {
					t.Fatalf("create session: %v (%s)", err, id)
				}

				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
				defer cancel()
				req, _ := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/session/"+created.Data.ID+"/event", nil)
				resp, err := http.DefaultClient.Do(req)
				if err != nil {
					t.Fatalf("sse connect: %v", err)
				}
				defer resp.Body.Close()

				body, _ := json.Marshal(map[string]any{
					"prompt": "Describe the attached media briefly; if it is a solid color, name it in one word.",
					"files":  []schema.PromptFileAttachment{{URI: tc.fixture(t), Mime: tc.mime}},
				})
				status := postJSONStatus(t, base+"/api/session/"+created.Data.ID+"/prompt", string(body))
				if status != http.StatusAccepted {
					t.Fatalf("prompt status = %d", status)
				}
				return readEventText(t, resp)
			}

			if tc.red {
				// A real model can occasionally refuse or answer off-script.
				reply := ""
				for attempt := 0; attempt < 3; attempt++ {
					reply = produce()
					if mentionsRed(reply) {
						return
					}
				}
				t.Fatalf("reply = %q, want it to mention red", reply)
			}
			if text := produce(); strings.TrimSpace(text) == "" {
				t.Fatal("empty reply: the attachment did not reach the model")
			}
		})
	}

	cancel()
	if err := <-done; err != nil {
		t.Fatalf("run: %v", err)
	}
}

func waitForHealth(t *testing.T, base string) {
	t.Helper()
	var lastErr error
	for i := 0; i < 300; i++ {
		resp, err := http.Get(base + "/api/health")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
			lastErr = err
		} else {
			lastErr = err
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("server not healthy: %v", lastErr)
}

func fetchModelInputs(t *testing.T, base, model string) []string {
	t.Helper()
	resp, err := http.Get(base + "/api/model")
	if err != nil {
		t.Fatalf("get models: %v", err)
	}
	defer resp.Body.Close()
	var out struct {
		Data []struct {
			ID    string   `json:"id"`
			Input []string `json:"input"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode models: %v", err)
	}
	for _, m := range out.Data {
		if m.ID == model {
			return m.Input
		}
	}
	t.Fatalf("model %q not in GET /api/model: %+v", model, out.Data)
	return nil
}

func postJSON(t *testing.T, url, body string, want int) string {
	t.Helper()
	resp, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != want {
		t.Fatalf("POST %s status = %d, want %d", url, resp.StatusCode, want)
	}
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(resp.Body)
	return buf.String()
}

func postJSONStatus(t *testing.T, url, body string) int {
	t.Helper()
	resp, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

// readEventText reads SSE frames until the assistant text ends.
func readEventText(t *testing.T, resp *http.Response) string {
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

func contains(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}

func mentionsRed(reply string) bool {
	lower := strings.ToLower(reply)
	for _, word := range []string{"red", "crimson", "scarlet", "maroon", "reddish"} {
		if strings.Contains(lower, word) {
			return true
		}
	}
	return false
}

func dataURL(mime string, data []byte) string {
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data)
}

func redPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			img.Set(x, y, color.RGBA{R: 255, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

func redJPEG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			img.Set(x, y, color.RGBA{R: 255, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}
	return buf.Bytes()
}

func toneWAV(t *testing.T) []byte {
	t.Helper()
	const rate = 16000
	const samples = rate / 2
	var buf bytes.Buffer
	dataLen := samples * 2
	write := func(v any) {
		if err := binary.Write(&buf, binary.LittleEndian, v); err != nil {
			t.Fatalf("wav write: %v", err)
		}
	}
	buf.WriteString("RIFF")
	write(uint32(36 + dataLen))
	buf.WriteString("WAVE")
	buf.WriteString("fmt ")
	write(uint32(16))
	write(uint16(1))
	write(uint16(1))
	write(uint32(rate))
	write(uint32(rate * 2))
	write(uint16(2))
	write(uint16(16))
	buf.WriteString("data")
	write(uint32(dataLen))
	for i := 0; i < samples; i++ {
		sample := int16(30000 * math.Sin(2*math.Pi*440*float64(i)/rate))
		write(sample)
	}
	return buf.Bytes()
}
