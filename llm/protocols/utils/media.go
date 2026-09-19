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

const MaxMediaEncodedBytes = 28 * 1024 * 1024
const MaxMediaDecodedBytes = 20 * 1024 * 1024

type ValidatedMedia struct {
	MIME    string
	Base64  string
	DataURL string
	Bytes   []byte
}

func ValidateMedia(route string, part schema.MediaPart, supportedMIMEs map[string]struct{}) (*ValidatedMedia, error) {
	mime := strings.ToLower(part.MediaType)
	if _, ok := supportedMIMEs[mime]; !ok {
		return nil, fmt.Errorf("%s does not support media type %s", route, part.MediaType)
	}
	var base64Str string
	switch data := part.Data.(type) {
	case string:
		if strings.HasPrefix(data, "data:") {
			m := dataURLPattern.FindStringSubmatch(data)
			if m == nil {
				return nil, fmt.Errorf("%s media data URL must contain valid base64", route)
			}
			if strings.ToLower(m[1]) != mime {
				return nil, fmt.Errorf("%s media type %s does not match data URL type %s", route, part.MediaType, m[1])
			}
			base64Str = m[2]
		} else {
			base64Str = data
		}
	case []byte:
		if len(data) > MaxMediaDecodedBytes {
			return nil, fmt.Errorf("%s media exceeds the %d byte decoded limit", route, MaxMediaDecodedBytes)
		}
		base64Str = base64.StdEncoding.EncodeToString(data)
	default:
		return nil, fmt.Errorf("%s: media data must be a string or byte array", route)
	}
	encodedLen := len(base64Str)
	if encodedLen > MaxMediaEncodedBytes {
		return nil, fmt.Errorf("%s media exceeds the %d byte encoded limit", route, MaxMediaEncodedBytes)
	}
	if base64Str == "" || len(base64Str)%4 != 0 || !base64Pattern.MatchString(base64Str) {
		return nil, fmt.Errorf("%s media must contain valid base64", route)
	}
	bytes, err := base64.StdEncoding.DecodeString(base64Str)
	if err != nil {
		return nil, fmt.Errorf("%s media must contain valid base64: %w", route, err)
	}
	if len(bytes) > MaxMediaDecodedBytes {
		return nil, fmt.Errorf("%s media exceeds the %d byte decoded limit", route, MaxMediaDecodedBytes)
	}
	if base64.StdEncoding.EncodeToString(bytes) != base64Str {
		return nil, fmt.Errorf("%s media must contain canonical base64", route)
	}
	return &ValidatedMedia{
		MIME:    mime,
		Base64:  base64Str,
		DataURL: fmt.Sprintf("data:%s;base64,%s", mime, base64Str),
		Bytes:   bytes,
	}, nil
}

func ValidateToolFile(route string, part schema.ToolFileContent, supportedMIMEs map[string]struct{}) (*ValidatedMedia, error) {
	return ValidateMedia(route, schema.MediaPart{
		Type:      "media",
		MediaType: part.Mime,
		Data:      part.URI,
		Filename:  part.Name,
	}, supportedMIMEs)
}
