package main

import (
	"io"
	"testing"
	"time"
)

// TestConcurrentPromptsSerialize guards that overlapping prompts each receive
// their own line. Parallel tool calls can request permission (or a question) at
// the same time; a prompt whose input is misrouted would hang or be declined.
func TestConcurrentPromptsSerialize(t *testing.T) {
	c := &console{out: io.Discard, lineStart: true}
	c.keys = make(chan rune, 64)
	c.edit = make(chan rune, 64)
	c.editor = &editor{in: &chanRuneReader{keys: c.edit}, out: io.Discard}

	first := make(chan string, 1)
	go func() { s, _ := c.prompt("> "); first <- s }()
	for !c.prompting.Load() {
		time.Sleep(time.Millisecond)
	}

	second := make(chan string, 1)
	go func() { s, _ := c.prompt("> "); second <- s }()
	time.Sleep(30 * time.Millisecond)

	for _, r := range "one\n" {
		c.deliver(r)
	}
	if got := <-first; got != "one" {
		t.Fatalf("first = %q, want one", got)
	}
	time.Sleep(30 * time.Millisecond)

	for _, r := range "two\n" {
		c.deliver(r)
	}
	select {
	case got := <-second:
		if got != "two" {
			t.Fatalf("second = %q, want two", got)
		}
	case <-time.After(300 * time.Millisecond):
		t.Fatal("second prompt hung: input was routed to the wrong owner")
	}
}
