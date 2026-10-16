package protocols

import "strings"

// BlockKind names one wire encoding a protocol can emit. A kind implies its
// modality and source.
type BlockKind string

const (
	// ImageInlineDataURL is image_url with a data URL.
	ImageInlineDataURL BlockKind = "image_inline_data_url"
	// ImageURL is image_url with an http(s) URL.
	ImageURL BlockKind = "image_url"
	// InputAudioBase64 is OpenAI input_audio {data, format} (bare base64).
	InputAudioBase64 BlockKind = "input_audio_base64"
	// AudioURLDataURL is vLLM audio_url with a data URL.
	AudioURLDataURL BlockKind = "audio_url_data_url"
	// AudioURLHTTP is vLLM audio_url with an http(s) URL.
	AudioURLHTTP BlockKind = "audio_url"
	// VideoURLDataURL is vLLM video_url with a data URL.
	VideoURLDataURL BlockKind = "video_url_data_url"
	// VideoURLHTTP is vLLM video_url with an http(s) URL.
	VideoURLHTTP BlockKind = "video_url"
	// OpenAIFileBlock is DeepSeek's `file` block carrying a Files API file_id
	// (`{"type":"file","file_id":…}`) on the OpenAI-compatible chat wire.
	OpenAIFileBlock BlockKind = "openai_file"
)

// MediaEncodingProfile is the ordered set of block kinds a wire may emit. The
// order is the encoding preference for a given modality+source.
type MediaEncodingProfile []BlockKind

// MIMEs returns the union of the MIME types the profile's kinds accept.
func (p MediaEncodingProfile) MIMEs() map[string]struct{} {
	set := make(map[string]struct{})
	for _, kind := range p {
		for _, mime := range kind.mimes() {
			set[mime] = struct{}{}
		}
	}
	return set
}

// blockFor returns the first enabled kind that accepts a MIME and source.
func (p MediaEncodingProfile) blockFor(mime string, source MediaSource) (BlockKind, bool) {
	for _, kind := range p {
		if kind.source() == source && kind.accepts(mime) {
			return kind, true
		}
	}
	return "", false
}

// has reports whether a kind is enabled.
func (p MediaEncodingProfile) has(kind BlockKind) bool {
	for _, k := range p {
		if k == kind {
			return true
		}
	}
	return false
}

// modality returns the modality a kind carries.
func (k BlockKind) modality() string {
	switch k {
	case ImageInlineDataURL, ImageURL:
		return "image"
	case InputAudioBase64, AudioURLDataURL, AudioURLHTTP:
		return "audio"
	case VideoURLDataURL, VideoURLHTTP:
		return "video"
	case OpenAIFileBlock:
		return "image"
	default:
		return ""
	}
}

// source returns the source a kind carries.
func (k BlockKind) source() MediaSource {
	switch k {
	case ImageURL, AudioURLHTTP, VideoURLHTTP:
		return MediaSourceURL
	case OpenAIFileBlock:
		return MediaSourceFile
	default:
		return MediaSourceInline
	}
}

// mimes returns the MIME types a kind accepts.
func (k BlockKind) mimes() []string {
	switch k {
	case ImageInlineDataURL, ImageURL:
		return imageMIMEs
	case InputAudioBase64:
		return []string{"audio/wav", "audio/mp3"}
	case AudioURLDataURL, AudioURLHTTP:
		return audioMIMEs
	case VideoURLDataURL, VideoURLHTTP:
		return videoMIMEs
	case OpenAIFileBlock:
		return imageMIMEs
	default:
		return nil
	}
}

// mimeSet returns a lookup set of the kind's MIME types.
func (k BlockKind) mimeSet() map[string]struct{} {
	set := make(map[string]struct{})
	for _, mime := range k.mimes() {
		set[mime] = struct{}{}
	}
	return set
}

func (k BlockKind) accepts(mime string) bool {
	mime = strings.ToLower(mime)
	for _, m := range k.mimes() {
		if m == mime {
			return true
		}
	}
	return false
}

// openAIChatProfile: OpenAI proper — images (inline + url) + input_audio (no video).
var openAIChatProfile = MediaEncodingProfile{ImageInlineDataURL, ImageURL, InputAudioBase64}

// openAICompatibleProfile: vLLM extensions — images (inline + url) + audio
// (input_audio + audio_url) + video_url. No `file` block (vLLM has no file_id).
var openAICompatibleProfile = MediaEncodingProfile{
	ImageInlineDataURL, ImageURL,
	InputAudioBase64, AudioURLDataURL, AudioURLHTTP,
	VideoURLDataURL, VideoURLHTTP,
}

// deepSeekProfile: DeepSeek's OpenAI-compatible chat — images (image_url data
// URL / external url) plus its `file` block (a Files API file_id). DeepSeek is
// image-only, so no audio/video blocks. Kept separate from the vLLM profile so
// `openai-compatible-chat` does not advertise a `file` block vLLM cannot carry.
var deepSeekProfile = MediaEncodingProfile{ImageInlineDataURL, ImageURL, OpenAIFileBlock}

// audioFormat maps an audio MIME to OpenAI's input_audio format value.
func audioFormat(mime string) string {
	switch strings.ToLower(mime) {
	case "audio/wav":
		return "wav"
	case "audio/mp3":
		return "mp3"
	default:
		return strings.TrimPrefix(strings.ToLower(mime), "audio/")
	}
}
