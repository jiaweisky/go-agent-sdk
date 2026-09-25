package util

import (
	"bytes"
	"errors"
)

// CleanJSONC strips JSONC line and block comments and trailing commas, returning
// standard JSON suitable for encoding/json. A subsequent json.Unmarshal failure
// means the document itself is invalid.
func CleanJSONC(text string) ([]byte, error) {
	var out bytes.Buffer
	i := 0
	n := len(text)
	for i < n {
		c := text[i]
		switch {
		case c == '"':
			next, err := copyJSONCString(&out, text, i)
			if err != nil {
				return nil, err
			}
			i = next
		case c == '/' && i+1 < n && text[i+1] == '/':
			i = skipJSONCLineComment(text, i+2)
		case c == '/' && i+1 < n && text[i+1] == '*':
			next, err := skipJSONCBlockComment(text, i+2)
			if err != nil {
				return nil, err
			}
			i = next
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

// copyJSONCString copies the string literal starting at the opening quote and
// returns the index after the closing quote.
func copyJSONCString(out *bytes.Buffer, text string, i int) (int, error) {
	n := len(text)
	out.WriteByte(text[i])
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
		return 0, errors.New("unterminated string")
	}
	return i, nil
}

// skipJSONCLineComment returns the index after a line comment, starting after
// the leading "//".
func skipJSONCLineComment(text string, i int) int {
	n := len(text)
	for i < n && text[i] != '\n' {
		i++
	}
	return i
}

// skipJSONCBlockComment returns the index after a block comment, starting after
// the leading "/*".
func skipJSONCBlockComment(text string, i int) (int, error) {
	n := len(text)
	for i+1 < n && !(text[i] == '*' && text[i+1] == '/') {
		i++
	}
	if i+1 >= n {
		return 0, errors.New("unterminated comment")
	}
	return i + 2, nil
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
			i = skipJSONCLineComment(text, i+2)
			continue
		}
		if c == '/' && i+1 < n && text[i+1] == '*' {
			next, err := skipJSONCBlockComment(text, i+2)
			if err != nil {
				break
			}
			i = next
			continue
		}
		break
	}
	return i
}
