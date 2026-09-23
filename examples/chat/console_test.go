package main

import (
	"bufio"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/user/go-agent-sdk/agent"
)

func TestEditorEscExits(t *testing.T) {
	var out strings.Builder
	editor := &editor{in: bufio.NewReader(strings.NewReader("\x1b")), out: &out}
	if _, err := editor.readLine("> "); err != io.EOF {
		t.Fatalf("err = %v, want io.EOF", err)
	}
}

func TestEditorArrowDoesNotExit(t *testing.T) {
	var out strings.Builder
	editor := &editor{in: bufio.NewReader(strings.NewReader("\x1b[D\n")), out: &out}
	line, err := editor.readLine("> ")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if line != "" {
		t.Fatalf("line = %q", line)
	}
}

func TestWatchInterrupt(t *testing.T) {
	keys := make(chan rune, 4)
	c := &console{keys: keys}
	done := make(chan struct{})
	interrupted := make(chan struct{})

	go func() {
		keys <- 'a' // ignored during a turn
		keys <- keyEsc
	}()

	if !c.watchInterrupt(done, func() { close(interrupted) }, nil) {
		t.Fatal("expected an interrupt")
	}
	<-interrupted
}

func TestWatchInterruptStopsWhenTurnDone(t *testing.T) {
	c := &console{keys: make(chan rune)}
	done := make(chan struct{})
	close(done)
	if c.watchInterrupt(done, func() { t.Fatal("unexpected interrupt") }, nil) {
		t.Fatal("expected no interrupt")
	}
}

func TestWatchInterruptNonTTY(t *testing.T) {
	c := &console{} // keys == nil on a non-TTY
	done := make(chan struct{})
	close(done)
	if c.watchInterrupt(done, func() { t.Fatal("unexpected interrupt") }, nil) {
		t.Fatal("expected no interrupt")
	}
}

func TestWatchInterruptYieldsToPrompt(t *testing.T) {
	c := &console{keys: make(chan rune)}
	c.prompting.Store(true)
	done := make(chan struct{})

	finished := make(chan bool, 1)
	go func() { finished <- c.watchInterrupt(done, func() { t.Error("unexpected interrupt") }, nil) }()

	select {
	case <-finished:
		t.Fatal("watcher returned while a prompt was active")
	case <-time.After(60 * time.Millisecond):
	}
	close(done)
	if <-finished {
		t.Fatal("expected no interrupt")
	}
}

// TestConsoleStatusLine guards the in-place status line: normal writes erase it,
// then redraw it below, and it is only redrawn at the start of a line.
func TestConsoleStatusLine(t *testing.T) {
	var out strings.Builder
	c := &console{editor: &editor{}, out: &out, lineStart: true}

	c.setStatus("BUSY")
	if out.String() != "BUSY" {
		t.Fatalf("status not drawn: %q", out.String())
	}

	out.Reset()
	c.write("hello\n")
	if got := out.String(); got != "\r\x1b[2Khello\nBUSY" {
		t.Fatalf("write with status = %q", got)
	}

	// A partial line suppresses the redraw until the next newline.
	out.Reset()
	c.write("partial")
	if got := out.String(); got != "\r\x1b[2Kpartial" {
		t.Fatalf("partial write = %q", got)
	}

	out.Reset()
	c.write("\n") // completes the line; the status is redrawn below it
	if got := out.String(); got != "\nBUSY" {
		t.Fatalf("line completion = %q", got)
	}

	out.Reset()
	c.setStatus("")
	if got := out.String(); got != "\r\x1b[2K" {
		t.Fatalf("clear status = %q", got)
	}
}

func TestInteractivePermissionDefaultsToAllow(t *testing.T) {
	cases := []struct {
		input string
		want  bool
	}{
		{"\n", true}, // empty answer defaults to yes
		{"y\n", true},
		{"yes\n", true},
		{"n\n", false},
		{"no\n", false},
	}
	for _, tc := range cases {
		var out strings.Builder
		c := &console{reader: bufio.NewReader(strings.NewReader(tc.input)), out: &out}
		decision, err := interactivePermission(c)(agent.PermissionRequest{Action: "bash", Resources: []string{"ls"}})
		if err != nil {
			t.Fatalf("input %q: %v", tc.input, err)
		}
		if decision.Allow != tc.want {
			t.Fatalf("input %q: allow = %v, want %v", tc.input, decision.Allow, tc.want)
		}
	}
}

func TestYoloSkipsInteractivePermission(t *testing.T) {
	cases := []struct {
		yolo           bool
		wantPermission bool
	}{
		{false, true},
		{true, false},
	}
	for _, tc := range cases {
		var opts agent.ClientOptions
		for _, option := range clientOptions(&console{out: io.Discard}, tc.yolo) {
			option(&opts)
		}
		if got := opts.Permission != nil; got != tc.wantPermission {
			t.Fatalf("yolo=%v: permission handler set = %v, want %v", tc.yolo, got, tc.wantPermission)
		}
	}
}
