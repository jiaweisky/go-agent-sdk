package config

import (
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// MarkdownResult mirrors the gray-matter result fields used by ConfigMarkdown.
type MarkdownResult struct {
	Data    any
	Content string
}

var (
	markdownFrontmatter = regexp.MustCompile(`(?s)^---\r?\n(.*?)\r?\n---`)
	markdownCommentLine = regexp.MustCompile(`(?m)^\s*#[^\n]+`)
	markdownEntry       = regexp.MustCompile(`^([a-zA-Z_][a-zA-Z0-9_]*)\s*:\s*(.*)$`)
	markdownIndent      = regexp.MustCompile(`^\s+`)
	markdownLineBreak   = regexp.MustCompile(`\r?\n`)
)

// ParseMarkdown mirrors ConfigMarkdown.parse: it extracts and parses YAML
// frontmatter, retrying with SanitizeMarkdown when the YAML is invalid.
func ParseMarkdown(content string) (MarkdownResult, error) {
	result, err := parseMarkdownRaw(content)
	if err == nil {
		return result, nil
	}
	sanitized := SanitizeMarkdown(content)
	if sanitized == content {
		// gray-matter caches the file object by content before parsing, so the
		// retry after a failed parse returns the partially built result (empty
		// data, original content) rather than failing again.
		return MarkdownResult{Data: map[string]any{}, Content: content}, nil
	}
	return parseMarkdownRaw(sanitized)
}

// ParseMarkdownOption mirrors ConfigMarkdown.parseOption.
func ParseMarkdownOption(content string) (MarkdownResult, bool) {
	result, err := ParseMarkdown(content)
	if err != nil {
		return MarkdownResult{}, false
	}
	return result, true
}

// SanitizeMarkdown mirrors ConfigMarkdown.sanitize: unquoted colons in
// frontmatter values are retried as YAML block scalars.
func SanitizeMarkdown(content string) string {
	match := markdownFrontmatter.FindStringSubmatchIndex(content)
	if match == nil {
		return content
	}
	frontmatter := content[match[2]:match[3]]
	result := make([]string, 0)
	for _, line := range markdownLineBreak.Split(frontmatter, -1) {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") || trimmed == "" || markdownIndent.MatchString(line) {
			result = append(result, line)
			continue
		}
		entry := markdownEntry.FindStringSubmatch(line)
		if entry == nil {
			result = append(result, line)
			continue
		}
		value := strings.TrimSpace(entry[2])
		if value == "" || value == ">" || value == "|" ||
			strings.HasPrefix(value, `"`) || strings.HasPrefix(value, "'") ||
			!strings.Contains(value, ":") {
			result = append(result, line)
			continue
		}
		result = append(result, entry[1]+": |-", "  "+value)
	}
	return strings.Replace(content, frontmatter, strings.Join(result, "\n"), 1)
}

func parseMarkdownRaw(content string) (MarkdownResult, error) {
	match := markdownFrontmatter.FindStringSubmatchIndex(content)
	if match == nil {
		return MarkdownResult{Data: map[string]any{}, Content: content}, nil
	}
	block := content[match[2]:match[3]]
	var data any = map[string]any{}
	if strings.TrimSpace(markdownCommentLine.ReplaceAllString(block, "")) != "" {
		if err := yaml.Unmarshal([]byte(block), &data); err != nil {
			return MarkdownResult{}, err
		}
	}
	body := content[match[1]:]
	body = strings.TrimPrefix(body, "\r")
	body = strings.TrimPrefix(body, "\n")
	return MarkdownResult{Data: data, Content: body}, nil
}
