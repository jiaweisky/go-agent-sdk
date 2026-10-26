package media

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"testing"
	"time"

	"github.com/jiaweisky/go-agent-sdk/cmd/agentd/config"
	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

// TestLiveDeepSeekFilesAPI is an opt-in integration test. It runs only when
// DEEPSEEK_API_KEY is set and otherwise skips, so the normal suite stays
// hermetic.
func TestLiveDeepSeekFilesAPI(t *testing.T) {
	key := os.Getenv("DEEPSEEK_API_KEY")
	if key == "" {
		t.Skip("DEEPSEEK_API_KEY is not set")
	}
	var body bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			img.Set(x, y, color.RGBA{R: 255, A: 255})
		}
	}
	if err := png.Encode(&body, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	uploader, err := NewUploader("deepseek-live", &config.ProviderFileAPI{
		Kind:    "multipart-json",
		URL:     "https://api.deepseek.com/files",
		Auth:    config.ProviderFileAuth{Headers: map[string]string{"Authorization": "Bearer " + key}},
		Fields:  map[string]string{"purpose": "user_data"},
		IDField: "id",
		Expiry:  config.ProviderFileExpiry{Style: "expires_after", Seconds: 3600},
		MIME:    config.ProviderFileMIME{Allowed: []string{"image/png"}},
		Limits:  config.ProviderFileLimits{MaxBytes: 1 << 20, Timeout: "30s"},
	})
	if err != nil {
		t.Fatalf("new uploader: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	name := "agentd-live-test.png"
	ref, err := uploader.Upload(ctx, schema.MediaUpload{Mime: "image/png", Name: &name, Data: body.Bytes()})
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	if ref.Source != "file" || ref.Value == "" || ref.ExpiresAt == nil {
		t.Fatalf("reference = %+v", ref)
	}
}
