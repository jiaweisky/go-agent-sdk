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
		switch {
		case s[i] == '`':
			if end := strings.IndexByte(s[i+1:], '`'); end >= 0 {
				out.WriteString(cyan + s[i+1:i+1+end] + reset)
				i += end + 2
				continue
			}
		case strings.HasPrefix(s[i:], "**"):
			if end := strings.Index(s[i+2:], "**"); end >= 0 {
				out.WriteString(bold + inline(s[i+2:i+2+end]) + reset)
				i += end + 4
				continue
			}
		case s[i] == '[':
			close := strings.IndexByte(s[i:], ']')
			if close >= 0 && i+close+1 < len(s) && s[i+close+1] == '(' {
				if urlEnd := strings.IndexByte(s[i+close+2:], ')'); urlEnd >= 0 {
					text := s[i+1 : i+close]
					url := s[i+close+2 : i+close+2+urlEnd]
					out.WriteString(underline + inline(text) + reset + dim + " (" + url + ")" + reset)
					i += close + 2 + urlEnd + 1
					continue
				}
			}
		case s[i] == '*':
			if end := strings.IndexByte(s[i+1:], '*'); end > 0 {
				text := s[i+1 : i+1+end]
				// Emphasis cannot start or end with a space, so arithmetic like
				// "2 * 3 * 4" stays literal instead of rendering as italic.
				if text == strings.TrimSpace(text) {
					out.WriteString(italic + inline(text) + reset)
					i += end + 2
					continue
				}
			}
		}
		out.WriteByte(s[i])
		i++
	}
	return out.String()
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
