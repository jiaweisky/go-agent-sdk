//go:build e2e

package e2e

import (
	"image/color"
	"os"
	"testing"
)

// DeepSeek serves the openai-compatible-chat, openai-responses and
// anthropic-messages protocols; it is the only real provider available to the
// E2E suite that speaks the latter two. Images (not audio/video) are supported.
// deepseek-flash is a reasoning model, so the token budget must leave room for
// reasoning before the answer.
//
//	export DEEPSEEK_API_KEY=...
//
// Endpoints (as the SDK composes them from baseURL + route path):
//   - openai-compatible-chat: https://api.deepseek.com        + /chat/completions
//   - openai-responses:       https://api.deepseek.com        + /responses
//   - anthropic-messages:     https://api.deepseek.com/anthropic/v1 + /messages
const (
	deepseekChatBase      = "https://api.deepseek.com"
	deepseekResponsesBase = "https://api.deepseek.com"
	deepseekAnthropicBase = "https://api.deepseek.com/anthropic/v1"
	deepseekModel         = "deepseek-flash"
)

func deepseekKey(t *testing.T) string {
	t.Helper()
	key := os.Getenv("DEEPSEEK_API_KEY")
	if key == "" {
		t.Skip("set DEEPSEEK_API_KEY to run the DeepSeek E2E")
	}
	return key
}

func deepseekImage(t *testing.T) mediaFile {
	return mediaFile{Mime: "image/png", URI: dataURL("image/png", solidPNG(t, color.RGBA{R: 255, A: 255}))}
}

func TestDeepSeekChatImage(t *testing.T) {
	key := deepseekKey(t)
	file := deepseekImage(t)
	replyRed(t, func() (string, bool) {
		return sendMedia(t, "openai-compatible-chat", deepseekChatBase, deepseekModel, key, file.Mime, file.Source, file.URI,
			"What is the dominant color of this image? Answer with one word.")
	})
}

func TestDeepSeekChatImageURL(t *testing.T) {
	key := deepseekKey(t)
	reply, omitted := sendMedia(t, "openai-compatible-chat", deepseekChatBase, deepseekModel, key, "image/png", "url", publicImageURL,
		"Describe this image in a few words.")
	if omitted {
		t.Fatal("the image was omitted")
	}
	if reply == "" {
		t.Fatal("empty reply")
	}
}

func TestDeepSeekResponsesImage(t *testing.T) {
	key := deepseekKey(t)
	file := deepseekImage(t)
	replyRed(t, func() (string, bool) {
		return sendMedia(t, "openai-responses", deepseekResponsesBase, deepseekModel, key, file.Mime, file.Source, file.URI,
			"What is the dominant color of this image? Answer with one word.")
	})
}

func TestDeepSeekAnthropicImage(t *testing.T) {
	key := deepseekKey(t)
	file := deepseekImage(t)
	replyRed(t, func() (string, bool) {
		return sendMedia(t, "anthropic-messages", deepseekAnthropicBase, deepseekModel, key, file.Mime, file.Source, file.URI,
			"What is the dominant color of this image? Answer with one word.")
	})
}
