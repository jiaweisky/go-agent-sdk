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

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"

	config "github.com/jiaweisky/go-agent-sdk/cmd/agentd/config"
	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

const (
	// httpSessionTimeout bounds one HTTP/SSE turn in the e2e tests.
	httpSessionTimeout = 3 * time.Minute
	// a2aTurnTimeout bounds one A2A streaming turn in the e2e tests.
	a2aTurnTimeout = 2 * time.Minute
)

// These end-to-end tests start the real agentd command against a live model and
// exercise the media surface of each transport:
//
//	export VLLM_BASE_URL=http://host:8000/v1 VLLM_MODEL=...
//	# optional: restrict to the modalities the model supports (default: all)
//	export VLLM_MODALITIES=text,image
//
// A media case whose modality is not enabled is skipped.

func mediaModel(baseURL, model string, modalities []string) config.ModelConfig {
	return config.ModelConfig{
		ID:              model,
		Model:           model,
		Protocol:        "openai-compatible-chat",
		ModelBaseURL:    baseURL,
		InputModalities: modalities,
		Credentials:     config.Credentials{APIKey: "EMPTY"},
	}
}

// mediaDenyAll denies every tool: the test model has no tool calling enabled.
func mediaDenyAll() []config.PermissionRule {
	return []config.PermissionRule{{Action: "*", Resource: "*", Effect: "deny"}}
}

func envModel(t *testing.T) (string, string, []string) {
	t.Helper()
	baseURL, model := os.Getenv("VLLM_BASE_URL"), os.Getenv("VLLM_MODEL")
	if baseURL == "" || model == "" {
		t.Skip("set VLLM_BASE_URL and VLLM_MODEL to run the media E2E")
	}
	return baseURL, model, envModalities(t)
}

// envModalities reads VLLM_MODALITIES (comma-separated); empty means all.
func envModalities(t *testing.T) []string {
	t.Helper()
	raw := os.Getenv("VLLM_MODALITIES")
	if raw == "" {
		return []string{"text", "image", "audio", "video"}
	}
	var out []string
	for _, part := range strings.Split(raw, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

// mediaCase is one media turn keyed by the modality it exercises.
type mediaCase struct {
	name     string
	modality string
	mime     string
	httpURI  func(*testing.T) string
	fixture  func(*testing.T) []byte
	red      bool
}

func mediaCases() []mediaCase {
	return []mediaCase{
		{"image", "image", "image/png", func(t *testing.T) string { return dataURL("image/png", redPNG(t)) }, redPNG, true},
		{"audio", "audio", "audio/wav", func(t *testing.T) string { return dataURL("audio/wav", toneWAV(t)) }, toneWAV, false},
		{"video", "video", "video/jpeg", func(t *testing.T) string { return dataURL("video/jpeg", redJPEG(t)) }, redJPEG, true},
	}
}

// startAgentd runs agentd until the returned stop function is called.
func startAgentd(t *testing.T, cfg config.Config) func() {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- run(ctx, cfg, nil) }()
	return func() {
		cancel()
		if err := <-done; err != nil {
			t.Fatalf("run: %v", err)
		}
	}
}

func TestRunServesMediaHTTP(t *testing.T) {
	baseURL, model, modalities := envModel(t)
	addr := freeAddr(t)
	stop := startAgentd(t, config.Config{
		Models: []config.ModelConfig{mediaModel(baseURL, model, modalities)},
		Servers: []config.Server{{
			Name: "http", Kind: "http", Listen: addr, Root: t.TempDir(),
			Agents: []string{"build"}, InboundPermissions: mediaDenyAll(),
		}},
	})
	defer stop()

	base := "http://" + addr
	waitForHealth(t, base)
	inputs := fetchModelInputs(t, base, model)
	for _, modality := range modalities {
		if !contains(inputs, modality) {
			t.Fatalf("model input = %v, want to include %q", inputs, modality)
		}
	}

	for _, tc := range mediaCases() {
		if !contains(modalities, tc.modality) {
			t.Run(tc.name, func(t *testing.T) { t.Skipf("modality %q not enabled", tc.modality) })
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			prompt := "Describe the attached media briefly; if it is a solid color, name it in one word."
			assertMediaReply(t, tc.red, func() string {
				files := []schema.PromptFileAttachment{{URI: tc.httpURI(t), Mime: tc.mime}}
				return runHTTPSession(t, base, "build", prompt, files)
			})
		})
	}
}

// runHTTPSession creates a session on the given agent, sends one prompt (with
// optional attachments) over the session's event stream, and returns the
// assistant text.
func runHTTPSession(t *testing.T, base, agent, prompt string, files []schema.PromptFileAttachment) string {
	t.Helper()
	id := postJSON(t, base+"/api/session", `{"agent":"`+agent+`"}`, http.StatusOK)
	var created struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(id), &created); err != nil || created.Data.ID == "" {
		t.Fatalf("create session: %v (%s)", err, id)
	}
	ctx, cancel := context.WithTimeout(context.Background(), httpSessionTimeout)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/session/"+created.Data.ID+"/event", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("sse connect: %v", err)
	}
	defer resp.Body.Close()
	body, _ := json.Marshal(map[string]any{"prompt": prompt, "files": files})
	if status := postJSONStatus(t, base+"/api/session/"+created.Data.ID+"/prompt", string(body)); status != http.StatusAccepted {
		t.Fatalf("prompt status = %d", status)
	}
	return readEventText(t, resp)
}

func TestRunServesMediaA2A(t *testing.T) {
	baseURL, model, modalities := envModel(t)
	addr := freeAddr(t)
	stop := startAgentd(t, config.Config{
		Models: []config.ModelConfig{mediaModel(baseURL, model, modalities)},
		Servers: []config.Server{{
			Name: "a2a", Kind: "a2a", Listen: addr, Root: t.TempDir(),
			Agents: []string{"build"}, InboundMedia: "inline", InboundPermissions: mediaDenyAll(),
			Cards: map[string]config.CardConfig{"build": {Name: "Build", Description: "media e2e"}},
		}},
	})
	defer stop()

	card := waitForCard(t, "http://"+addr+"/.well-known/agent-card.json")
	wantModes := map[string]string{"image": "image/png", "audio": "audio/wav", "video": "video/mp4"}
	for _, modality := range modalities {
		if mode, ok := wantModes[modality]; ok && !cardHasMode(card, mode) {
			t.Fatalf("card input modes = %v, want %s", card.DefaultInputModes, mode)
		}
	}
	client, err := a2aclient.NewFromCard(context.Background(), card)
	if err != nil {
		t.Fatalf("new a2a client: %v", err)
	}
	defer client.Destroy()

	for _, tc := range mediaCases() {
		if !contains(modalities, tc.modality) {
			t.Run(tc.name, func(t *testing.T) { t.Skipf("modality %q not enabled", tc.modality) })
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			assertMediaReply(t, tc.red, func() string { return sendTurn(t, client, tc.mime, tc.fixture(t)) })
		})
	}
}

// assertMediaReply checks a produced reply: a solid-red fixture must be named,
// otherwise the reply must simply be non-empty (the media reached the model).
func assertMediaReply(t *testing.T, red bool, produce func() string) {
	t.Helper()
	if red {
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
}

func waitForHealth(t *testing.T, base string) {
	t.Helper()
	var lastErr error
	for i := 0; i < 300; i++ {
		if resp, err := http.Get(base + "/api/health"); err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
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

// sendTurn drives one A2A turn with a raw part and returns the artifact text.
func sendTurn(t *testing.T, client *a2aclient.Client, mime string, data []byte) string {
	t.Helper()
	message := a2a.NewMessage(a2a.MessageRoleUser,
		a2a.NewTextPart("Describe the attached media briefly; if it is a solid color, name it in one word."),
		&a2a.Part{Content: a2a.Raw(data), MediaType: mime})
	message.ContextID = a2a.NewContextID()

	ctx, cancel := context.WithTimeout(context.Background(), a2aTurnTimeout)
	defer cancel()
	var (
		artifact strings.Builder
		state    a2a.TaskState
	)
	for event, err := range client.SendStreamingMessage(ctx, &a2a.SendMessageRequest{Message: message}) {
		if err != nil {
			t.Fatalf("stream: %v", err)
		}
		switch e := event.(type) {
		case *a2a.Task:
			state = e.Status.State
		case *a2a.TaskArtifactUpdateEvent:
			for _, part := range e.Artifact.Parts {
				artifact.WriteString(part.Text())
			}
		case *a2a.TaskStatusUpdateEvent:
			state = e.Status.State
		}
	}
	if state != a2a.TaskStateCompleted {
		t.Fatalf("state = %s", state)
	}
	return artifact.String()
}

func waitForCard(t *testing.T, url string) *a2a.AgentCard {
	t.Helper()
	var lastErr error
	for i := 0; i < 300; i++ {
		resp, err := http.Get(url)
		if err == nil {
			var card a2a.AgentCard
			decodeErr := json.NewDecoder(resp.Body).Decode(&card)
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK && decodeErr == nil {
				return &card
			}
			lastErr = decodeErr
		} else {
			lastErr = err
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("card not served: %v", lastErr)
	return nil
}

func cardHasMode(card *a2a.AgentCard, mode string) bool {
	for _, m := range card.DefaultInputModes {
		if m == mode {
			return true
		}
	}
	return false
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
	return solidImage(t, "png")
}

func redJPEG(t *testing.T) []byte {
	t.Helper()
	return solidImage(t, "jpeg")
}

func solidImage(t *testing.T, format string) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			img.Set(x, y, color.RGBA{R: 255, A: 255})
		}
	}
	var buf bytes.Buffer
	var err error
	if format == "png" {
		err = png.Encode(&buf, img)
	} else {
		err = jpeg.Encode(&buf, img, nil)
	}
	if err != nil {
		t.Fatalf("encode %s: %v", format, err)
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
		write(int16(30000 * math.Sin(2*math.Pi*440*float64(i)/rate)))
	}
	return buf.Bytes()
}
