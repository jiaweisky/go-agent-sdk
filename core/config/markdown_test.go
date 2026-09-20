package config

import (
	"reflect"
	"testing"
)

func TestParseMarkdown(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		data    any
		content string
	}{
		{
			name:    "frontmatter",
			input:   "---\nname: foo\ndescription: bar\n---\nBody text\n",
			data:    map[string]any{"name": "foo", "description": "bar"},
			content: "Body text\n",
		},
		{
			name:    "no frontmatter",
			input:   "no frontmatter here\n",
			data:    map[string]any{},
			content: "no frontmatter here\n",
		},
		{
			name:    "empty body",
			input:   "---\nname: foo\n---\n",
			data:    map[string]any{"name": "foo"},
			content: "",
		},
		{
			name:    "unquoted colon retried as block scalar",
			input:   "---\ndescription: Uses: colons and stuff\n---\nbody",
			data:    map[string]any{"description": "Uses: colons and stuff"},
			content: "body",
		},
		{
			name:    "quoted value",
			input:   "---\nname: \"quoted\"\n---\nbody",
			data:    map[string]any{"name": "quoted"},
			content: "body",
		},
		{
			name:    "invalid frontmatter falls back to whole content",
			input:   "---\nname: foo\n  nested: x\n---\nbody",
			data:    map[string]any{},
			content: "---\nname: foo\n  nested: x\n---\nbody",
		},
		{
			name:    "blank line before body",
			input:   "---\nname: foo\n---\n\nBody with blank\n",
			data:    map[string]any{"name": "foo"},
			content: "\nBody with blank\n",
		},
		{
			name:    "non-mapping frontmatter stays parsed",
			input:   "---\n- a\n- b\n---\nbody",
			data:    []any{"a", "b"},
			content: "body",
		},
		{
			name:    "null frontmatter",
			input:   "---\nnull\n---\nbody",
			data:    nil,
			content: "body",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result, ok := ParseMarkdownOption(tc.input)
			if !ok {
				t.Fatalf("ParseMarkdownOption returned undefined")
			}
			if !reflect.DeepEqual(result.Data, tc.data) {
				t.Fatalf("data = %#v, want %#v", result.Data, tc.data)
			}
			if result.Content != tc.content {
				t.Fatalf("content = %q, want %q", result.Content, tc.content)
			}
		})
	}
}

func TestSanitizeMarkdown(t *testing.T) {
	input := "---\ndescription: a: b\nother: plain\n---\nbody"
	want := "---\ndescription: |-\n  a: b\nother: plain\n---\nbody"
	if got := SanitizeMarkdown(input); got != want {
		t.Fatalf("sanitize = %q, want %q", got, want)
	}
	if got := SanitizeMarkdown("no frontmatter"); got != "no frontmatter" {
		t.Fatalf("sanitize without frontmatter = %q", got)
	}
}
