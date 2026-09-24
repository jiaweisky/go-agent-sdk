package route

import (
	"bufio"
	"io"
	"iter"
	"strings"
)

// Framing turns a response body into the frames a protocol decodes.
type Framing[T any] struct {
	ID    string
	Frame func(reader io.Reader) iter.Seq2[T, error]
}

// maxSSEFrameBytes bounds a single SSE frame read by the scanner. The default
// bufio.Scanner limit (64KB) is too small for provider frames carrying large
// tool arguments, base64 media, or long reasoning.
const maxSSEFrameBytes = 10 * 1024 * 1024

// SSE frames a text/event-stream body into its data payloads.
var SSE = Framing[string]{
	ID:    "sse",
	Frame: sseFraming,
}

func sseFraming(reader io.Reader) iter.Seq2[string, error] {
	return func(yield func(string, error) bool) {
		scanner := bufio.NewScanner(reader)
		// Provider SSE frames can exceed bufio.Scanner's 64KB default (large
		// tool arguments, base64 media, long reasoning). Raise the limit.
		scanner.Buffer(make([]byte, 0, 64*1024), maxSSEFrameBytes)
		var data strings.Builder
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				if data.Len() == 0 {
					continue
				}
				msg := strings.TrimSuffix(data.String(), "\n")
				data.Reset()
				if msg == "" || msg == "[DONE]" {
					continue
				}
				if !yield(msg, nil) {
					return
				}
				continue
			}
			// An SSE field is `name: value` with an optional single space after
			// the colon; `data` lines accumulate joined by newlines.
			field, value, found := strings.Cut(line, ":")
			if field != "data" {
				continue
			}
			if found {
				data.WriteString(strings.TrimPrefix(value, " "))
			}
			data.WriteString("\n")
		}
		if err := scanner.Err(); err != nil {
			yield("", err)
		}
	}
}
