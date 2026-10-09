package main

import (
	"bufio"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/jiaweisky/go-agent-sdk/pkg/agent"
	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

// TestExpandPath guards the .env path handling: the loader stores values
// verbatim, so weatherMCP expands $VAR and a leading ~ itself.
func TestExpandPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("AGENT_UNSET", "")

	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"absolute", "/tmp/server.js", "/tmp/server.js"},
		{"dollar home", "$HOME/server.js", filepath.Join(home, "server.js")},
		{"braced home", "${HOME}/server.js", filepath.Join(home, "server.js")},
		{"tilde", "~/server.js", filepath.Join(home, "server.js")},
		{"bare tilde", "~", home},
		{"unset var", "$AGENT_UNSET/server.js", "/server.js"},
	}
	for _, c := range cases {
		if got := expandPath(c.in); got != c.want {
			t.Errorf("%s: expandPath(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}

func TestParseAnswer(t *testing.T) {
	options := []schema.QuestionOption{{Label: "One"}, {Label: "Two"}, {Label: "Three"}}
	cases := []struct {
		name  string
		input string
		want  []string
	}{
		{"empty", "", []string{}},
		{"index", "2", []string{"Two"}},
		{"several indexes", "1,3", []string{"One", "Three"}},
		{"spaces around indexes", " 1 , 2 ", []string{"One", "Two"}},
		{"index out of range is text", "9", []string{"9"}},
		{"free text", "something else", []string{"something else"}},
		{"partly indexes is text", "1, nope", []string{"1, nope"}},
	}
	for _, c := range cases {
		if got := parseAnswer(c.input, options); !slices.Equal(got, c.want) {
			t.Errorf("%s: parseAnswer(%q) = %q, want %q", c.name, c.input, got, c.want)
		}
	}
}

// TestInteractiveQuestionSkipsAfterEOF guards that a closed stdin does not leave
// the handler prompting: each question keeps its slot, so the answers stay
// aligned with the questions.
func TestInteractiveQuestionSkipsAfterEOF(t *testing.T) {
	var out strings.Builder
	console := &console{reader: bufio.NewReader(strings.NewReader("")), out: &out}
	request := agent.QuestionRequest{
		SessionID: "ses_1",
		Questions: []schema.QuestionInfo{
			{Question: "first?", Options: []schema.QuestionOption{{Label: "A", Description: "a"}}},
			{Question: "second?", Options: []schema.QuestionOption{{Label: "B", Description: "b"}}},
		},
	}
	answers, err := interactiveQuestion(console)(request)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(answers) != len(request.Questions) {
		t.Fatalf("answers = %v, want one per question", answers)
	}
	for i, answer := range answers {
		if len(answer) != 0 {
			t.Fatalf("answer %d = %v, want it skipped", i, answer)
		}
	}
}

// TestInteractiveQuestionMultiple guards that a question allowing several
// answers says so and returns every selected label.
func TestInteractiveQuestionMultiple(t *testing.T) {
	var out strings.Builder
	console := &console{reader: bufio.NewReader(strings.NewReader("1,2\n")), out: &out}
	multiple := true
	request := agent.QuestionRequest{Questions: []schema.QuestionInfo{{
		Question: "pick?",
		Multiple: &multiple,
		Options:  []schema.QuestionOption{{Label: "A"}, {Label: "B"}},
	}}}
	answers, err := interactiveQuestion(console)(request)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(answers) != 1 || !slices.Equal(answers[0], []string{"A", "B"}) {
		t.Fatalf("answers = %v, want A,B", answers)
	}
	if !strings.Contains(out.String(), "numbers separated by comma") {
		t.Fatalf("out = %q, want the multi-select prompt", out.String())
	}
}

// TestHandleCommandTrimsInput guards that a command is recognised whatever
// whitespace surrounds it.
func TestHandleCommandTrimsInput(t *testing.T) {
	var out strings.Builder
	c := newChat(&console{out: &out, lineStart: true}, nil, "ses_1", "")

	if !handleCommand(c, "  /task  ") {
		t.Fatal("a padded command was not recognised")
	}
	if !strings.Contains(out.String(), "(no tasks)") {
		t.Fatalf("out = %q, want the empty task list", out.String())
	}

	// An unknown slash command is reported, not sent to the model, and only the
	// command token is named.
	out.Reset()
	if !handleCommand(c, "/nope now") {
		t.Fatal("an unknown slash command was not recognised")
	}
	if !strings.Contains(out.String(), "unknown command: /nope") {
		t.Fatalf("out = %q, want the unknown-command hint", out.String())
	}

	// Plain input is a prompt, not a command.
	out.Reset()
	if handleCommand(c, "plain text") {
		t.Fatal("plain input was treated as a command")
	}
	if out.String() != "" {
		t.Fatalf("out = %q, want plain input to print nothing", out.String())
	}
}
