//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image/color"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"

	"github.com/jiaweisky/go-agent-sdk/pkg/agent"
	"github.com/jiaweisky/go-agent-sdk/pkg/core"
	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

// DeepSeek's Files API is usable across its three compatible wires. Keyed by the
// SDK protocol id, the `file` reference is lowered to:
//
//	deepseek-chat      -> {"type":"file","file_id":...}
//	openai-responses   -> {"type":"input_image","file_id":...}
//	anthropic-messages -> {"type":"image","source":{"type":"file","file_id":...}}
//	                       (+ anthropic-beta: files-api-2025-04-14)
func TestDeepSeekChatFileID(t *testing.T) {
	runDeepSeekFileID(t, "deepseek-chat", deepseekChatBase, nil)
}

func TestDeepSeekResponsesFileID(t *testing.T) {
	runDeepSeekFileID(t, "openai-responses", deepseekResponsesBase, nil)
}

func TestDeepSeekAnthropicFileID(t *testing.T) {
	runDeepSeekFileID(t, "anthropic-messages", deepseekAnthropicBase, map[string]string{
		"anthropic-beta": "files-api-2025-04-14",
	})
}

// runDeepSeekFileID uploads an inline image through the SDK's MediaUploader and
// runs one turn whose attachment reaches the model only as a provider file_id
// (the model accepts an image only via `file`). It fails unless the upload
// happened and the attachment was not omitted, and cleans up the object.
func runDeepSeekFileID(t *testing.T, protocol, baseURL string, headers map[string]string) (reply string, omitted bool) {
	t.Helper()
	key := deepseekKey(t)
	var uploadedID string
	uploader := deepseekUploader{key: key, onID: func(id string) { uploadedID = id }}

	client, err := agent.NewClient(&agent.ClientOptions{
		Root:        t.TempDir(),
		Model:       deepseekModel,
		Permission:  agent.ApproveAll,
		Credentials: agent.APIKey(key),
		ModelInfo: func(id string) (schema.ModelInfo, error) {
			info, err := agent.NewModelInfo(schema.ProtocolID(protocol), id, baseURL, schema.ModelLimit{Output: 1024})
			if err != nil {
				return schema.ModelInfo{}, err
			}
			info.Capabilities.Input = []string{"text", "image"}
			// The model accepts an image only as a provider file reference.
			info.Capabilities.Media = &schema.ModelMediaCapability{Forms: map[string][]string{"image": {"file"}}}
			if len(headers) > 0 {
				info.Request.Headers = headers
			}
			return info, nil
		},
		MediaUploader: func(schema.ProviderID) schema.MediaUploader { return uploader },
		MediaPolicy:   &core.MediaPolicy{Upload: true, Prefer: []core.MediaPolicySource{core.MediaPolicySourceFile}},
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	defer client.Close()

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

	uri := dataURL("image/png", solidPNG(t, color.RGBA{R: 255, A: 255}))
	var replyBuilder strings.Builder
	for event, err := range session.Send(context.Background(), agent.MessageOptions{
		Prompt: "Describe this image in one color word.",
		Files:  []schema.PromptFileAttachment{{URI: uri, Mime: "image/png"}},
	}) {
		if err != nil {
			t.Fatalf("%s send: %v", protocol, err)
		}
		if delta, ok := event.(agent.TextDelta); ok {
			replyBuilder.WriteString(delta.Delta)
		}
	}
	reply = replyBuilder.String()

	if uploadedID == "" {
		t.Fatalf("%s: the image was not uploaded to DeepSeek's Files API", protocol)
	}
	if omitted {
		t.Fatalf("%s: the attachment was omitted: the file_id path did not work", protocol)
	}
	t.Logf("%s: uploaded file_id=%s reply=%q", protocol, uploadedID, reply)

	req, _ := http.NewRequest(http.MethodDelete, deepseekChatBase+"/files/"+uploadedID, nil)
	req.Header.Set("Authorization", "Bearer "+key)
	if resp, err := http.DefaultClient.Do(req); err == nil {
		resp.Body.Close()
	}
	return reply, omitted
}

// deepseekUploader is a minimal Files API uploader for the E2E.
type deepseekUploader struct {
	key  string
	onID func(string)
}

func (u deepseekUploader) Upload(ctx context.Context, media schema.MediaUpload) (schema.Reference, error) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	name := "image.png"
	if media.Name != nil && *media.Name != "" {
		name = *media.Name
	}
	part, err := writer.CreateFormFile("file", name)
	if err != nil {
		return schema.Reference{}, err
	}
	if _, err := part.Write(media.Data); err != nil {
		return schema.Reference{}, err
	}
	if err := writer.WriteField("purpose", "user_data"); err != nil {
		return schema.Reference{}, err
	}
	if err := writer.Close(); err != nil {
		return schema.Reference{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, deepseekChatBase+"/files", &buf)
	if err != nil {
		return schema.Reference{}, err
	}
	req.Header.Set("Authorization", "Bearer "+u.key)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return schema.Reference{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return schema.Reference{}, errors.New("deepseek files upload: " + resp.Status)
	}
	var out struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return schema.Reference{}, err
	}
	if out.ID == "" {
		return schema.Reference{}, errors.New("deepseek files upload: response has no id")
	}
	if u.onID != nil {
		u.onID(out.ID)
	}
	return schema.Reference{Source: "file", Value: out.ID}, nil
}
