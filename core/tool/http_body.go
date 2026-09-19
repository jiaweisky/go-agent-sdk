package tool

import (
	"io"
	"net/http"
	"strconv"
)

// CollectBoundedResponseBody reads an HTTP response body while enforcing a
// maximum size. It fails fast when the declared Content-Length already exceeds
// the bound and also rejects bodies that stream past it.
func CollectBoundedResponseBody(response *http.Response, maximumBytes int, tooLarge func() error) ([]byte, error) {
	if contentLength := response.Header.Get("Content-Length"); contentLength != "" {
		if parsed, err := strconv.ParseInt(contentLength, 10, 64); err == nil && parsed >= 0 {
			if parsed > int64(maximumBytes) {
				return nil, tooLarge()
			}
		}
	}
	if response.Body == nil {
		return []byte{}, nil
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, int64(maximumBytes)+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maximumBytes {
		return nil, tooLarge()
	}
	return body, nil
}
