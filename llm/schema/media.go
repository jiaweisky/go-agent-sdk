package schema

import "strings"

// MediaSource is where a media part's bytes come from.
type MediaSource string

const (
	// MediaSourceInline carries the bytes in the request (base64/data URL).
	MediaSourceInline MediaSource = "inline"
	// MediaSourceURL is an http(s) reference the provider fetches.
	MediaSourceURL MediaSource = "url"
	// MediaSourceFile is a provider-native reference (file id / file URI / s3).
	MediaSourceFile MediaSource = "file"
)

// InferMediaSource derives a source from a URI scheme when none is declared:
// data: → inline, http(s):// → url, s3:// → file; anything else is inline.
func InferMediaSource(uri string) MediaSource {
	switch {
	case strings.HasPrefix(uri, "data:"):
		return MediaSourceInline
	case strings.HasPrefix(uri, "http://"), strings.HasPrefix(uri, "https://"):
		return MediaSourceURL
	case strings.HasPrefix(uri, "s3://"):
		return MediaSourceFile
	default:
		return MediaSourceInline
	}
}
