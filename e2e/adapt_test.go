//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"image/color"
	"os"
	"strings"
	"sync"
	"testing"

	agent "github.com/jiaweisky/go-agent-sdk/pkg/agent"
	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

// recordingUploader is a fake MediaUploader that records the payload it was
// handed and returns a fixed provider reference. It stands in for a provider
// Files API so the inline→file adaptation path can be exercised end to end.
type recordingUploader struct {
	mu    sync.Mutex
	calls int
	data  []byte
	ref   schema.MediaRef
}

func (u *recordingUploader) Upload(_ context.Context, media schema.MediaUpload) (schema.MediaRef, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.calls++
	u.data = media.Data
	return u.ref, nil
}

// TestGeminiFileSourceAdaptation forces the inline→file adaptation: the model
// declares it only accepts the "file" source, so the SDK must upload the inline
// image and send the returned reference. The reference points at a server-local
// file vLLM can read (start vLLM with --allowed-local-media-path <dir>). It
// proves the uploader is invoked with the payload and that the resulting file
// reference actually reaches the model.
//
//	export VLLM_BASE_URL=http://host:8000/v1 VLLM_MODEL=... VLLM_LOCAL_MEDIA_DIR=/host/dir
func TestGeminiFileSourceAdaptation(t *testing.T) {
	baseURL, model := os.Getenv("VLLM_BASE_URL"), os.Getenv("VLLM_MODEL")
	dir := strings.TrimSuffix(os.Getenv("VLLM_LOCAL_MEDIA_DIR"), "/")
	if baseURL == "" || model == "" || dir == "" {
		t.Skip("set VLLM_BASE_URL, VLLM_MODEL and VLLM_LOCAL_MEDIA_DIR to run the file-source E2E")
	}
	shim := geminiShim(t, strings.TrimSuffix(baseURL, "/"))
	defer shim.Close()

	uploader := &recordingUploader{ref: schema.MediaRef{Source: "file", ID: "file://" + dir + "/s-l960.jpg"}}
	png := solidPNG(t, color.RGBA{R: 255, A: 255})
	client, err := agent.NewClient(&agent.ClientOptions{
		Root:          t.TempDir(),
		Model:         model,
		Permission:    agent.ApproveAll,
		Credentials:   agent.APIKey("k"),
		MediaUploader: func(schema.ProviderID) schema.MediaUploader { return uploader },
		ModelInfo: func(id string) (schema.ModelInfo, error) {
			info, err := agent.NewModelInfo("gemini", id, shim.URL, schema.ModelLimit{Output: 1024})
			if err != nil {
				return schema.ModelInfo{}, err
			}
			info.Capabilities.Input = []string{"text", "image"}
			info.Capabilities.Media = &schema.ModelMediaCapability{Sources: []string{"file"}}
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
		Prompt: "Describe this media briefly.",
		Files:  []schema.PromptFileAttachment{{URI: dataURL("image/png", png), Mime: "image/png"}},
	}) {
		if err != nil {
			t.Fatalf("send: %v", err)
		}
		if delta, ok := event.(agent.TextDelta); ok {
			reply.WriteString(delta.Delta)
		}
	}

	uploader.mu.Lock()
	defer uploader.mu.Unlock()
	if uploader.calls == 0 {
		t.Fatal("the inline image was not uploaded for inline→file adaptation")
	}
	if !bytes.Equal(uploader.data, png) {
		t.Fatal("the uploader did not receive the inline image bytes")
	}
	if omitted {
		t.Fatal("the adapted file reference was omitted")
	}
	if strings.TrimSpace(reply.String()) == "" {
		t.Fatal("empty reply: the file reference did not reach the model")
	}
}
