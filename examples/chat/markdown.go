package main

import (
	"strconv"
	"strings"
)

// markdown is a minimal, line-based markdown → ANSI renderer with fence state.
type markdown struct {
	inFence bool
}

func (m *markdown) render(line string) string {
	trimmed := strings.TrimRight(line, " \t")
	if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
		m.inFence = !m.inFence
		return dim + trimmed + reset
	}
	if m.inFence {
		return cyan + line + reset
	}
	if _, text, ok := heading(trimmed); ok {
		return bold + blue + inline(text) + reset
	}
	if isRule(trimmed) {
		return dim + strings.Repeat("─", 40) + reset
	}
	if strings.HasPrefix(trimmed, "> ") {
		return dim + "│ " + reset + inline(trimmed[2:])
	}
	if indent, marker, content, ok := listItem(trimmed); ok {
		return indent + marker + " " + inline(content)
	}
	return inline(line)
}

// listItem splits a list line into its indentation, marker and content, so
// nested items keep their indentation and ordered items keep their number (the
// model emits both).
func listItem(line string) (indent, marker, content string, ok bool) {
	body := strings.TrimLeft(line, " \t")
	indent = line[:len(line)-len(body)]
	if len(body) >= 2 {
		switch body[0] {
		case '-', '*', '+':
			if body[1] == ' ' {
				return indent, "•", body[2:], true
			}
		}
	}
	if end := strings.IndexAny(body, ".)"); end > 0 && end <= 3 && end+1 < len(body) && body[end+1] == ' ' {
		if _, err := strconv.Atoi(body[:end]); err == nil {
			return indent, body[:end+1], body[end+2:], true
		}
	}
	return "", "", "", false
}

func heading(line string) (int, string, bool) {
	if !strings.HasPrefix(line, "#") {
		return 0, "", false
	}
	level := 0
	for level < len(line) && line[level] == '#' {
		level++
	}
	if level > 6 || level >= len(line) || line[level] != ' ' {
		return 0, "", false
	}
	return level, strings.TrimSpace(line[level:]), true
}

func isRule(line string) bool {
	if len(line) < 3 {
		return false
	}
	c := line[0]
	if c != '-' && c != '*' && c != '_' {
		return false
	}
	for i := 0; i < len(line); i++ {
		if line[i] != c {
			return false
		}
	}
	return true
}

// inline renders the common inline spans: code, bold, links, italic.
func inline(s string) string {
	var out strings.Builder
	for i := 0; i < len(s); {
		span, size, ok := inlineSpan(s[i:])
		if !ok {
			out.WriteByte(s[i])
			i++
			continue
		}
		out.WriteString(span)
		i += size
	}
	return out.String()
}

// inlineSpan matches one inline span at the start of s, returning its rendering
// and its length in bytes.
func inlineSpan(s string) (string, int, bool) {
	switch {
	case s[0] == '`':
		return codeSpan(s)
	case strings.HasPrefix(s, "**"):
		return boldSpan(s)
	case s[0] == '[':
		return linkSpan(s)
	case s[0] == '*':
		return italicSpan(s)
	}
	return "", 0, false
}

// codeSpan renders `code`.
func codeSpan(s string) (string, int, bool) {
	end := strings.IndexByte(s[1:], '`')
	if end < 0 {
		return "", 0, false
	}
	return cyan + s[1:1+end] + reset, end + 2, true
}

// boldSpan renders **bold**, recursing so nested spans still render.
func boldSpan(s string) (string, int, bool) {
	end := strings.Index(s[2:], "**")
	if end < 0 {
		return "", 0, false
	}
	return bold + inline(s[2:2+end]) + reset, end + 4, true
}

// linkSpan renders [text](url) as the underlined text followed by the dimmed URL.
func linkSpan(s string) (string, int, bool) {
	close := strings.IndexByte(s, ']')
	if close < 0 || close+1 >= len(s) || s[close+1] != '(' {
		return "", 0, false
	}
	urlEnd := strings.IndexByte(s[close+2:], ')')
	if urlEnd < 0 {
		return "", 0, false
	}
	text, url := s[1:close], s[close+2:close+2+urlEnd]
	return underline + inline(text) + reset + dim + " (" + url + ")" + reset, close + 2 + urlEnd + 1, true
}

// italicSpan renders *italic*. Emphasis cannot start or end with a space, so
// arithmetic like "2 * 3 * 4" stays literal instead of rendering as italic.
func italicSpan(s string) (string, int, bool) {
	end := strings.IndexByte(s[1:], '*')
	if end <= 0 {
		return "", 0, false
	}
	text := s[1 : 1+end]
	if text != strings.TrimSpace(text) {
		return "", 0, false
	}
	return italic + inline(text) + reset, end + 2, true
}

// markdownStream renders markdown line by line as text streams in.
type markdownStream struct {
	md    markdown
	buf   strings.Builder
	write func(string)
}

func newMarkdownStream(write func(string)) *markdownStream {
	return &markdownStream{write: write}
}

func (s *markdownStream) Write(delta string) {
	for _, r := range delta {
		if r == '\n' {
			s.write(s.md.render(s.buf.String()) + "\n")
			s.buf.Reset()
			continue
		}
		s.buf.WriteRune(r)
	}
}

func (s *markdownStream) Flush() {
	if s.buf.Len() > 0 {
		s.write(s.md.render(s.buf.String()))
		s.buf.Reset()
	}
}

// renderMarkdown renders a complete markdown block (used for /task output).
func renderMarkdown(text string) string {
	md := &markdown{}
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	for i, line := range lines {
		lines[i] = md.render(line)
	}
	return strings.Join(lines, "\n") + "\n"
}
