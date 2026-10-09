//go:build e2e

package main

import (
	"bytes"
	"context"
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

	"github.com/jiaweisky/go-agent-sdk/cmd/serveconfig"
)

// TestRunServesMediaTurn starts the real a2aserve command against a live model,
// fetches the card (asserting the model's media modes are advertised), and drives
// one image, audio and video turn each over A2A to completion.
//
//	export VLLM_BASE_URL=http://host:8000/v1 VLLM_MODEL=...
func TestRunServesMediaTurn(t *testing.T) {
	baseURL := os.Getenv("VLLM_BASE_URL")
	model := os.Getenv("VLLM_MODEL")
	if baseURL == "" || model == "" {
		t.Skip("set VLLM_BASE_URL and VLLM_MODEL to run the a2aserve E2E")
	}

	addr := freeAddr(t)
	cfg := defaultConfig()
	cfg.Listen = addr
	cfg.Root = t.TempDir()
	cfg.Model = model
	cfg.Protocol = "openai-compatible-chat"
	cfg.ModelBaseURL = baseURL
	cfg.InputModalities = []string{"text", "image", "audio", "video"}
	cfg.InboundMedia = "inline"
	// The available vLLM has no tool calling enabled; deny tools so none are
	// advertised.
	cfg.InboundPermissions = []serveconfig.PermissionRule{{Action: "*", Resource: "*", Effect: "deny"}}
	cfg.Credentials = serveconfig.Credentials{APIKey: "EMPTY"}
	cfg.KeepAlive = ""

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- run(ctx, cfg) }()

	cardURL := "http://" + addr + "/.well-known/agent-card.json"
	card := waitForCard(t, cardURL)
	for _, mode := range []string{"image/png", "audio/wav", "video/mp4"} {
		if !cardHasMode(card, mode) {
			t.Fatalf("card input modes = %v, want %s", card.DefaultInputModes, mode)
		}
	}

	c, err := a2aclient.NewFromCard(context.Background(), card)
	if err != nil {
		t.Fatalf("new a2a client: %v", err)
	}
	defer c.Destroy()

	cases := []struct {
		name    string
		mime    string
		fixture func(*testing.T) []byte
		red     bool
	}{
		{"image", "image/png", solidRedPNG, true},
		{"audio", "audio/wav", toneWAV, false},
		{"video", "video/jpeg", redJPEG, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.red {
				reply := ""
				for attempt := 0; attempt < 3; attempt++ {
					reply = sendTurn(t, c, tc.mime, tc.fixture(t))
					if mentionsRed(reply) {
						return
					}
				}
				t.Fatalf("reply = %q, want it to mention red", reply)
			}
			if text := sendTurn(t, c, tc.mime, tc.fixture(t)); strings.TrimSpace(text) == "" {
				t.Fatal("empty reply: the attachment did not reach the model")
			}
		})
	}

	cancel()
	if err := <-done; err != nil {
		t.Fatalf("run: %v", err)
	}
}

// sendTurn drives one A2A turn with a raw part and returns the artifact text.
func sendTurn(t *testing.T, c *a2aclient.Client, mime string, data []byte) string {
	t.Helper()
	part := &a2a.Part{Content: a2a.Raw(data), MediaType: mime}
	message := a2a.NewMessage(a2a.MessageRoleUser,
		a2a.NewTextPart("Describe the attached media briefly; if it is a solid color, name it in one word."), part)
	message.ContextID = a2a.NewContextID()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	var (
		artifact strings.Builder
		state    a2a.TaskState
	)
	for event, err := range c.SendStreamingMessage(ctx, &a2a.SendMessageRequest{Message: message}) {
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

func mentionsRed(reply string) bool {
	lower := strings.ToLower(reply)
	for _, word := range []string{"red", "crimson", "scarlet", "maroon", "reddish"} {
		if strings.Contains(lower, word) {
			return true
		}
	}
	return false
}

func solidRedPNG(t *testing.T) []byte {
	t.Helper()
	return encodeImage(t, "png")
}

func redJPEG(t *testing.T) []byte {
	t.Helper()
	return encodeImage(t, "jpeg")
}

func encodeImage(t *testing.T, format string) []byte {
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
