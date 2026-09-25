package utils

import (
	"encoding/base64"
	"fmt"
	"regexp"
	"strings"

	"github.com/user/go-agent-sdk/llm/schema"
)

var base64Pattern = regexp.MustCompile(`^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$`)
var dataURLPattern = regexp.MustCompile(`^data:([^;,]+);base64,([A-Za-z0-9+/]*={0,2})$`)

// MaxMediaEncodedBytes bounds the encoded size of one media part.
const MaxMediaEncodedBytes = 28 * 1024 * 1024

// MaxMediaDecodedBytes bounds the decoded size of one media part.
const MaxMediaDecodedBytes = 20 * 1024 * 1024

// ValidatedMedia is a checked media part: its MIME type and base64 encodings.
type ValidatedMedia struct {
	MIME    string
	Base64  string
	DataURL string
}

// ValidateMedia checks a media part's type and size, and decodes its bytes.
func ValidateMedia(route string, part schema.MediaPart, supportedMIMEs map[string]struct{}) (*ValidatedMedia, error) {
	mime := strings.ToLower(part.MediaType)
	if _, ok := supportedMIMEs[mime]; !ok {
		return nil, fmt.Errorf("%s does not support media type %s", route, part.MediaType)
	}
	base64Str, err := extractBase64(route, part, mime)
	if err != nil {
		return nil, err
	}
	if err := validateBase64(route, base64Str); err != nil {
		return nil, err
	}
	return &ValidatedMedia{
		MIME:    mime,
		Base64:  base64Str,
		DataURL: fmt.Sprintf("data:%s;base64,%s", mime, base64Str),
	}, nil
}

// extractBase64 reads a media part's base64 payload from a string, a data URL,
// or raw bytes.
func extractBase64(route string, part schema.MediaPart, mime string) (string, error) {
	switch data := part.Data.(type) {
	case string:
		if !strings.HasPrefix(data, "data:") {
			return data, nil
		}
		m := dataURLPattern.FindStringSubmatch(data)
		if m == nil {
			return "", fmt.Errorf("%s media data URL must contain valid base64", route)
		}
		if strings.ToLower(m[1]) != mime {
			return "", fmt.Errorf("%s media type %s does not match data URL type %s", route, part.MediaType, m[1])
		}
		return m[2], nil
	case []byte:
		if len(data) > MaxMediaDecodedBytes {
			return "", fmt.Errorf("%s media exceeds the %d byte decoded limit", route, MaxMediaDecodedBytes)
		}
		return base64.StdEncoding.EncodeToString(data), nil
	default:
		return "", fmt.Errorf("%s: media data must be a string or byte array", route)
	}
}

// validateBase64 enforces the encoded and decoded size limits and canonical
// base64.
func validateBase64(route, base64Str string) error {
	if len(base64Str) > MaxMediaEncodedBytes {
		return fmt.Errorf("%s media exceeds the %d byte encoded limit", route, MaxMediaEncodedBytes)
	}
	if base64Str == "" || len(base64Str)%4 != 0 || !base64Pattern.MatchString(base64Str) {
		return fmt.Errorf("%s media must contain valid base64", route)
	}
	bytes, err := base64.StdEncoding.DecodeString(base64Str)
	if err != nil {
		return fmt.Errorf("%s media must contain valid base64: %w", route, err)
	}
	if len(bytes) > MaxMediaDecodedBytes {
		return fmt.Errorf("%s media exceeds the %d byte decoded limit", route, MaxMediaDecodedBytes)
	}
	if base64.StdEncoding.EncodeToString(bytes) != base64Str {
		return fmt.Errorf("%s media must contain canonical base64", route)
	}
	return nil
}

// ValidateToolFile checks a tool file result's type and size.
func ValidateToolFile(route string, part schema.ToolFileContent, supportedMIMEs map[string]struct{}) (*ValidatedMedia, error) {
	return ValidateMedia(route, schema.MediaPart{
		Type:      "media",
		MediaType: part.Mime,
		Data:      part.URI,
		Filename:  part.Name,
	}, supportedMIMEs)
}
