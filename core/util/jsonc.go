package util

import (
	"bytes"
	"errors"
)

// CleanJSONC strips JSONC line/block comments and trailing commas, returning
// standard JSON suitable for encoding/json. It mirrors jsonc-parser's
// `parse(text, errors, { allowTrailingComma: true })` followed by a decode:
// callers treat a subsequent json.Unmarshal failure as an invalid document.
func CleanJSONC(text string) ([]byte, error) {
	var out bytes.Buffer
	i := 0
	n := len(text)
	for i < n {
		c := text[i]
		switch {
		case c == '"':
			out.WriteByte(c)
			i++
			closed := false
			for i < n {
				ch := text[i]
				out.WriteByte(ch)
				i++
				if ch == '\\' {
					if i < n {
						out.WriteByte(text[i])
						i++
					}
					continue
				}
				if ch == '"' {
					closed = true
					break
				}
			}
			if !closed {
				return nil, errors.New("unterminated string")
			}
		case c == '/' && i+1 < n && text[i+1] == '/':
			i += 2
			for i < n && text[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < n && text[i+1] == '*':
			i += 2
			for i+1 < n && !(text[i] == '*' && text[i+1] == '/') {
				i++
			}
			if i+1 >= n {
				return nil, errors.New("unterminated comment")
			}
			i += 2
		case c == ',':
			j := skipJSONCSpace(text, i+1)
			if j < n && (text[j] == '}' || text[j] == ']') {
				i++
				continue
			}
			out.WriteByte(c)
			i++
		default:
			out.WriteByte(c)
			i++
		}
	}
	return out.Bytes(), nil
}

func skipJSONCSpace(text string, i int) int {
	n := len(text)
	for i < n {
		c := text[i]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			i++
			continue
		}
		if c == '/' && i+1 < n && text[i+1] == '/' {
			i += 2
			for i < n && text[i] != '\n' {
				i++
			}
			continue
		}
		if c == '/' && i+1 < n && text[i+1] == '*' {
			i += 2
			for i+1 < n && !(text[i] == '*' && text[i+1] == '/') {
				i++
			}
			i += 2
			continue
		}
		break
	}
	return i
}
