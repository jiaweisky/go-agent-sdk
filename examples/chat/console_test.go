package main

import (
	"bufio"
	"io"
	"strings"
	"testing"

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

// TestEditorSS3ArrowKeys guards application cursor keys (DECCKM), which send
// ESC O A/B/C/D instead of ESC [ A/B/C/D: the final rune must be treated as the
// key, not typed into the line.
func TestEditorSS3ArrowKeys(t *testing.T) {
	var out strings.Builder
	editor := &editor{in: bufio.NewReader(strings.NewReader("ab\x1bODX\n")), out: &out}
	line, err := editor.readLine("> ")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if line != "aXb" {
		t.Fatalf("line = %q, want %q", line, "aXb")
	}
}

// TestEditorScrollsLongLine guards that a line wider than the terminal is drawn
// as a window instead of being allowed to wrap (wrapping breaks the in-place
// redraw).
func TestEditorScrollsLongLine(t *testing.T) {
	var out strings.Builder
	editor := &editor{
		in:    bufio.NewReader(strings.NewReader("\n")),
		out:   &out,
		width: func() int { return 10 },
	}
	editor.draw("> ", []rune("abcdefghijkl"), 12)
	got := out.String()
	if strings.Contains(got, "abcdefghijkl") {
		t.Fatalf("line was drawn in full (it would wrap): %q", got)
	}
	// 10 columns minus the 2-column prompt leaves 8 for the tail of the line.
	if !strings.HasSuffix(got, "> efghijkl") {
		t.Fatalf("draw = %q, want the tail of the line", got)
	}
}

// TestEditorKeepsCursorVisibleWhenScrolled covers the cursor sitting inside the
// part of a long line that scrolls out.
func TestEditorKeepsCursorVisibleWhenScrolled(t *testing.T) {
	var out strings.Builder
	editor := &editor{
		in:    bufio.NewReader(strings.NewReader("\n")),
		out:   &out,
		width: func() int { return 4 },
	}
	editor.draw("", []rune("abcdefgh"), 1)
	got := out.String()
	if strings.Contains(got, "abcdefgh") {
		t.Fatalf("line was drawn in full: %q", got)
	}
	// Four visible columns starting at the cursor, with the cursor moved back to
	// column 0.
	if !strings.HasSuffix(got, "bcde\x1b[4D") {
		t.Fatalf("draw = %q, want the window at the cursor", got)
	}
}

func TestRuneWidth(t *testing.T) {
	cases := []struct {
		r    rune
		want int
	}{
		{'a', 1},
		{'你', 2},
		{'　', 2},      // ideographic space
		{'\u0301', 0}, // combining acute accent
		{'\u20e3', 0}, // combining enclosing keycap
		{0, 0},
		{0x7f, 0}, // DEL
	}
	for _, c := range cases {
		if got := runeWidth(c.r); got != c.want {
			t.Errorf("runeWidth(%q) = %d, want %d", c.r, got, c.want)
		}
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

func TestStdinRunesAreRoutedToTheirOwner(t *testing.T) {
	c := &console{keys: make(chan rune, 1), edit: make(chan rune, 1)}

	// No prompt active: the turn watcher owns stdin (and drops what it does not
	// act on).
	c.deliver('x')
	select {
	case r := <-c.keys:
		if r != 'x' {
			t.Fatalf("watcher got %q, want 'x'", r)
		}
	default:
		t.Fatal("rune was not routed to the turn watcher")
	}

	// A prompt owns stdin: the answer must reach the editor, never the watcher,
	// which would turn a typed "n" into an empty (and therefore approving)
	// permission answer.
	c.prompting.Store(true)
	c.deliver('n')
	select {
	case r := <-c.edit:
		if r != 'n' {
			t.Fatalf("editor got %q, want 'n'", r)
		}
	default:
		t.Fatal("rune was not routed to the line editor")
	}
	select {
	case r := <-c.keys:
		t.Fatalf("watcher consumed %q while a prompt was active", r)
	default:
	}
}

func TestDeliverDoesNotBlockWhenFull(t *testing.T) {
	c := &console{keys: make(chan rune, 1), edit: make(chan rune, 1)}
	c.deliver('a')
	c.deliver('b') // full queue: dropped rather than blocking the reader
	if got := len(c.keys); got != 1 {
		t.Fatalf("queue holds %d runes, want 1", got)
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
