package protocols

import (
	"testing"

	"github.com/jiaweisky/go-agent-sdk/pkg/llm/schema"
)

func TestOpenAIChatMediaProfiles(t *testing.T) {
	cases := []struct {
		name    string
		profile MediaEncodingProfile
		part    schema.MediaPart
		want    string
		wantErr bool
	}{
		{"image inline", openAIChatProfile, media(schema.MediaSourceInline, "image/png", ""), "image_url", false},
		{"image url", openAIChatProfile, media(schema.MediaSourceURL, "image/png", "https://x/a.png"), "image_url", false},
		{"openai audio inline", openAIChatProfile, media(schema.MediaSourceInline, "audio/wav", ""), "input_audio", false},
		{"openai video unsupported", openAIChatProfile, media(schema.MediaSourceInline, "video/mp4", ""), "", true},
		{"vllm audio inline", openAICompatibleProfile, media(schema.MediaSourceInline, "audio/wav", ""), "input_audio", false},
		{"vllm audio url", openAICompatibleProfile, media(schema.MediaSourceURL, "audio/wav", "https://x/a.wav"), "audio_url", false},
		{"vllm video inline", openAICompatibleProfile, media(schema.MediaSourceInline, "video/mp4", ""), "video_url", false},
		{"vllm video url", openAICompatibleProfile, media(schema.MediaSourceURL, "video/mp4", "https://x/a.mp4"), "video_url", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			content, err := openaiChatLowerMedia(tc.part, tc.profile)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("%s should be rejected", tc.part.MediaType)
				}
				return
			}
			if err != nil {
				t.Fatalf("lower %s: %v", tc.part.MediaType, err)
			}
			if content.Type != tc.want {
				t.Fatalf("type = %q, want %q", content.Type, tc.want)
			}
			if content.Type == "input_audio" && content.InputAudio.Format != "wav" {
				t.Fatalf("format = %q, want wav", content.InputAudio.Format)
			}
			if content.Type == "image_url" && tc.part.Source == schema.MediaSourceURL && content.ImageURL.URL != "https://x/a.png" {
				t.Fatalf("image url = %q, want the raw URL", content.ImageURL.URL)
			}
		})
	}
}

// media builds a MediaPart; data is a URL string for url sources, else bytes.
func media(source schema.MediaSource, mime string, url string) schema.MediaPart {
	part := schema.MediaPart{Type: "media", MediaType: mime, Source: source}
	if source == schema.MediaSourceURL {
		part.Data = url
	} else {
		part.Data = []byte{0, 1, 2, 3}
	}
	return part
}

func TestGeminiLowerMediaSources(t *testing.T) {
	inline, err := geminiLowerMedia(schema.MediaPart{Type: "media", MediaType: "image/png", Source: schema.MediaSourceInline, Data: []byte{0, 1, 2, 3}})
	if err != nil {
		t.Fatalf("inline: %v", err)
	}
	if _, ok := inline.(geminiInlineDataPart); !ok {
		t.Fatalf("inline = %T, want geminiInlineDataPart", inline)
	}
	remote, err := geminiLowerMedia(schema.MediaPart{Type: "media", MediaType: "video/mp4", Source: schema.MediaSourceURL, Data: "https://x/v.mp4"})
	if err != nil {
		t.Fatalf("url: %v", err)
	}
	fileData, ok := remote.(geminiFileDataPart)
	if !ok {
		t.Fatalf("url = %T, want geminiFileDataPart", remote)
	}
	if fileData.FileData.FileURI != "https://x/v.mp4" {
		t.Fatalf("fileUri = %q", fileData.FileData.FileURI)
	}
}

func TestOpenAIChatImageDetail(t *testing.T) {
	part := schema.MediaPart{Type: "media", MediaType: "image/png", Source: schema.MediaSourceInline, Data: []byte{0, 1, 2, 3}, Detail: "low"}
	content, err := openaiChatLowerMedia(part, openAIChatProfile)
	if err != nil {
		t.Fatalf("lower: %v", err)
	}
	if content.ImageURL == nil || content.ImageURL.Detail != "low" {
		t.Fatalf("detail = %+v, want low", content.ImageURL)
	}
}

func TestDescriptorMIMEsMatchProfiles(t *testing.T) {
	chat, ok := LookupProtocol(schema.ProtocolID(OpenAIChatHTTPRoute.ID))
	if !ok {
		t.Fatal("openai-chat protocol not registered")
	}
	if !chat.Media.AllowsMIME("audio/wav") {
		t.Fatal("openai-chat descriptor should carry audio")
	}
	if chat.Media.AllowsMIME("video/mp4") {
		t.Fatal("openai-chat descriptor should not carry video")
	}

	compatible, ok := LookupProtocol(schema.ProtocolID(OpenAICompatibleChatHTTPRoute.ID))
	if !ok {
		t.Fatal("openai-compatible-chat protocol not registered")
	}
	if !compatible.Media.AllowsMIME("video/mp4") || !compatible.Media.AllowsMIME("audio/wav") {
		t.Fatal("openai-compatible-chat descriptor should carry audio and video")
	}
}
