//go:build e2e

// Package e2e holds end-to-end tests that run against a real provider. They are
// gated behind the `e2e` build tag and skip unless the relevant environment is
// configured, so `go test ./...` stays offline.
package e2e

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math"
	"os"
	"strings"
	"testing"

	agent "github.com/jiaweisky/go-agent-sdk/pkg/agent"
	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

// denyAllTools makes the runner advertise no tools, so a raw vLLM server started
// without tool-call flags accepts the request.
func denyAllTools() schema.PermissionRuleset {
	return schema.PermissionRuleset{{Action: "*", Resource: "*", Effect: schema.PermissionEffectDeny}}
}

// mediaFile is one attachment for the E2E helpers.
type mediaFile struct {
	Mime   string
	Source string
	URI    string
}

// sendMedia drives one media attachment to a model and returns the assistant
// text and whether the attachment was omitted (no omission means it reached the
// model). protocol selects the wire protocol; apiKey authenticates.
func sendMedia(t *testing.T, protocol, baseURL, model, apiKey, mime, source, uri, prompt string) (string, bool) {
	return sendMediaFiles(t, protocol, baseURL, model, apiKey, []mediaFile{{Mime: mime, Source: source, URI: uri}}, prompt)
}

// sendMediaFiles drives several attachments in one message.
func sendMediaFiles(t *testing.T, protocol, baseURL, model, apiKey string, files []mediaFile, prompt string) (string, bool) {
	t.Helper()
	client, err := agent.NewClient(&agent.ClientOptions{
		Root:        t.TempDir(),
		Model:       model,
		Permission:  agent.ApproveAll,
		Credentials: agent.APIKey(apiKey),
		ModelInfo: func(id string) (schema.ModelInfo, error) {
			info, err := agent.NewModelInfo(schema.ProtocolID(protocol), id, baseURL, schema.ModelLimit{Output: 1024})
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

	attachments := make([]schema.PromptFileAttachment, len(files))
	for i, file := range files {
		attachments[i] = schema.PromptFileAttachment{URI: file.URI, Mime: file.Mime, MediaSource: file.Source}
	}
	var reply strings.Builder
	for event, err := range session.Send(context.Background(), agent.MessageOptions{
		Prompt: prompt,
		Files:  attachments,
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

func dataURL(mime string, data []byte) string {
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data)
}

// publicImageURL is a stable, publicly fetchable image the upstream can download,
// used to exercise the url source (the provider fetches it; the SDK does not).
const publicImageURL = "https://vllm-public-assets.s3.us-west-2.amazonaws.com/vision_model_images/2560px-Gfp-wisconsin-madison-the-nature-boardwalk.jpg"

// solidPNG builds a solid-color PNG fixture.
func solidPNG(t *testing.T, c color.RGBA) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

// toneWAV builds a 0.5s 440Hz tone WAV fixture (16 kHz mono 16-bit).
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
	write(uint16(1)) // PCM
	write(uint16(1)) // mono
	write(uint32(rate))
	write(uint32(rate * 2)) // byte rate
	write(uint16(2))        // block align
	write(uint16(16))       // bits per sample
	buf.WriteString("data")
	write(uint32(dataLen))
	for i := 0; i < samples; i++ {
		write(int16(30000 * math.Sin(2*math.Pi*440*float64(i)/rate)))
	}
	return buf.Bytes()
}

// videoJPEG builds a single solid-red JPEG frame.
func videoJPEG(t *testing.T) []byte {
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

// videoDataURL builds a single-frame video/jpeg data URL (vLLM's frame form).
// A single frame keeps the payload canonical base64, which the SDK validates.
func videoDataURL(t *testing.T) string {
	t.Helper()
	return "data:video/jpeg;base64," + base64.StdEncoding.EncodeToString(videoJPEG(t))
}

// TestVLLMImageURL exercises the url source: the attachment is an
// http(s) reference the provider fetches (the SDK never fetches). Set
// VLLM_BASE_URL and VLLM_MODEL to run it.
func TestVLLMImageURL(t *testing.T) {
	baseURL, model := os.Getenv("VLLM_BASE_URL"), os.Getenv("VLLM_MODEL")
	if baseURL == "" || model == "" {
		t.Skip("set VLLM_BASE_URL and VLLM_MODEL to run the vLLM url-source E2E")
	}
	reply, omitted := sendMedia(t, "openai-compatible-chat", baseURL, model, "EMPTY", "image/png", "url", publicImageURL,
		"What is in this image? Answer briefly.")
	if omitted {
		t.Fatal("the url-source image was omitted")
	}
	if strings.TrimSpace(reply) == "" {
		t.Fatal("empty reply: the url-source image was not fetched")
	}
}

// TestVLLMImage checks a local vLLM model consumes an inline image. Set
// VLLM_BASE_URL and VLLM_MODEL to run it.
func TestVLLMImage(t *testing.T) {
	baseURL, model := os.Getenv("VLLM_BASE_URL"), os.Getenv("VLLM_MODEL")
	if baseURL == "" || model == "" {
		t.Skip("set VLLM_BASE_URL and VLLM_MODEL to run the vLLM image E2E")
	}
	replyRed(t, func() (string, bool) {
		return sendMedia(t, "openai-compatible-chat", baseURL, model, "EMPTY", "image/png", "", dataURL("image/png", solidPNG(t, color.RGBA{R: 255, A: 255})),
			"What is the dominant color in this image? Answer with one word.")
	})
}

// mentionsRed accepts the synonym set a model may use for the solid-red fixture.
func mentionsRed(reply string) bool {
	lower := strings.ToLower(reply)
	for _, word := range []string{"red", "crimson", "scarlet", "maroon", "reddish"} {
		if strings.Contains(lower, word) {
			return true
		}
	}
	return false
}

// replyRed runs a media turn, retrying a few times because a real model
// occasionally refuses or answers off-script even when the attachment arrived.
// It fails if the attachment is ever omitted or if no attempt mentions red.
func replyRed(t *testing.T, produce func() (string, bool)) string {
	t.Helper()
	reply := ""
	for attempt := 0; attempt < 3; attempt++ {
		var omitted bool
		reply, omitted = produce()
		if omitted {
			t.Fatal("the attachment was omitted")
		}
		if mentionsRed(reply) {
			return reply
		}
	}
	t.Fatalf("reply = %q, want it to mention red", reply)
	return ""
}

// TestVLLMAudio checks a local vLLM audio model accepts audio. Set
// VLLM_AUDIO_BASE_URL and VLLM_AUDIO_MODEL (e.g. fixie-ai/ultravox-...) to run it.
func TestVLLMAudio(t *testing.T) {
	baseURL, model := os.Getenv("VLLM_AUDIO_BASE_URL"), os.Getenv("VLLM_AUDIO_MODEL")
	if baseURL == "" || model == "" {
		t.Skip("set VLLM_AUDIO_BASE_URL and VLLM_AUDIO_MODEL to run the vLLM audio E2E")
	}
	reply, omitted := sendMedia(t, "openai-compatible-chat", baseURL, model, "EMPTY", "audio/wav", "", dataURL("audio/wav", toneWAV(t)),
		"Describe this audio in one or two words.")
	if omitted {
		t.Fatal("the audio was omitted")
	}
	if strings.TrimSpace(reply) == "" {
		t.Fatal("empty reply: the audio was not accepted")
	}
}

// TestVLLMVideo checks a local vLLM video model consumes video. Set
// VLLM_VIDEO_BASE_URL and VLLM_VIDEO_MODEL (e.g. Qwen/Qwen3-VL-8B-Instruct) to
// run it.
func TestVLLMVideo(t *testing.T) {
	baseURL, model := os.Getenv("VLLM_VIDEO_BASE_URL"), os.Getenv("VLLM_VIDEO_MODEL")
	if baseURL == "" || model == "" {
		t.Skip("set VLLM_VIDEO_BASE_URL and VLLM_VIDEO_MODEL to run the vLLM video E2E")
	}
	replyRed(t, func() (string, bool) {
		return sendMedia(t, "openai-compatible-chat", baseURL, model, "EMPTY", "video/jpeg", "", videoDataURL(t),
			"What is the dominant color in this video? Answer with one word.")
	})
}

// TestVLLMMultiAttachment sends several media kinds in one message, so the
// planner and wire builder handle a mixed batch (not just one part). Set
// VLLM_BASE_URL and VLLM_MODEL to run it.
func TestVLLMMultiAttachment(t *testing.T) {
	baseURL, model := os.Getenv("VLLM_BASE_URL"), os.Getenv("VLLM_MODEL")
	if baseURL == "" || model == "" {
		t.Skip("set VLLM_BASE_URL and VLLM_MODEL to run the multi-attachment E2E")
	}
	files := []mediaFile{
		{Mime: "image/png", URI: dataURL("image/png", solidPNG(t, color.RGBA{R: 255, A: 255}))},
		{Mime: "audio/wav", URI: dataURL("audio/wav", toneWAV(t))},
		{Mime: "video/jpeg", URI: videoDataURL(t)},
	}
	reply, omitted := sendMediaFiles(t, "openai-compatible-chat", baseURL, model, "EMPTY", files,
		"Describe the attached image, audio and video briefly.")
	if omitted {
		t.Fatal("an attachment was omitted from the mixed batch")
	}
	if strings.TrimSpace(reply) == "" {
		t.Fatal("empty reply: the mixed batch was not accepted")
	}
}

// TestVLLMLocalMedia exercises the url source with server-local files: the
// provider reads a file:// reference (start vLLM with
// --allowed-local-media-path <dir>). Set VLLM_BASE_URL, VLLM_MODEL and
// VLLM_LOCAL_MEDIA_DIR (e.g. /home/poe/Downloads) to run it.
func TestVLLMLocalMedia(t *testing.T) {
	baseURL, model := os.Getenv("VLLM_BASE_URL"), os.Getenv("VLLM_MODEL")
	dir := strings.TrimSuffix(os.Getenv("VLLM_LOCAL_MEDIA_DIR"), "/")
	if baseURL == "" || model == "" || dir == "" {
		t.Skip("set VLLM_BASE_URL, VLLM_MODEL and VLLM_LOCAL_MEDIA_DIR to run the local-file E2E")
	}
	cases := []struct{ name, file, mime string }{
		{"image", "s-l960.jpg", "image/jpeg"},
		{"audio", "sample-3s.mp3", "audio/mp3"},
		{"video", "file_example_AVI_480_750kB.avi", "video/x-msvideo"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			uri := "file://" + dir + "/" + tc.file
			reply, omitted := sendMedia(t, "openai-compatible-chat", baseURL, model, "EMPTY", tc.mime, "url", uri, "Describe this media briefly.")
			if omitted {
				t.Fatalf("%s was omitted", tc.name)
			}
			if strings.TrimSpace(reply) == "" {
				t.Fatalf("empty reply: the %s file was not read", tc.name)
			}
		})
	}
}
