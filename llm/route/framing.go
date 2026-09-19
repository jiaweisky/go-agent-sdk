package route

import (
	"bufio"
	"io"
	"iter"
	"strings"
)

type Framing[T any] struct {
	ID    string
	Frame func(reader io.Reader) iter.Seq2[T, error]
}

var SSE = Framing[string]{
	ID:    "sse",
	Frame: sseFraming,
}

func sseFraming(reader io.Reader) iter.Seq2[string, error] {
	return func(yield func(string, error) bool) {
		scanner := bufio.NewScanner(reader)
		var data strings.Builder
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "data: ") {
				data.WriteString(line[6:])
			} else if line == "" {
				if data.Len() > 0 {
					msg := data.String()
					data.Reset()
					if msg == "[DONE]" {
						continue
					}
					if !yield(msg, nil) {
						return
					}
				}
			}
		}
		if err := scanner.Err(); err != nil {
			yield("", err)
		}
	}
}
