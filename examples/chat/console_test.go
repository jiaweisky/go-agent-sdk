package main

import (
	"bufio"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/jiaweisky/go-agent-sdk"
)

// TestEditorEscIsIgnored guards that a lone Esc does not end input; exit is
// Ctrl+C / Ctrl+D.
func TestEditorEscIsIgnored(t *testing.T) {
	var out strings.Builder
	editor := &editor{in: bufio.NewReader(strings.NewReader("")), out: &out}
	state := &lineState{}
	if err := editor.applyEscape(state); err != nil {
		t.Fatalf("lone Esc returned %v, want nil", err)
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

// TestWatchInterruptDoubleEsc guards that a single Esc only arms the interrupt
// and a second Esc aborts the turn.
func TestWatchInterruptDoubleEsc(t *testing.T) {
	keys := make(chan rune, 4)
	c := &console{keys: keys}
	done := make(chan struct{})
	aborted := make(chan struct{})
	armed := make(chan bool, 4)

	go func() {
		keys <- 'a'    // ignored during a turn
		keys <- keyEsc // arm
		keys <- keyEsc // abort
	}()

	action := c.watchInterrupt(done, func() { close(aborted) }, nil, nil, func(v bool) { armed <- v })
	if action != interruptAbort {
		t.Fatalf("action = %v, want interruptAbort", action)
	}
	<-aborted
	if first := <-armed; !first {
		t.Fatal("the first Esc should arm the interrupt")
	}
}

// TestWatchInterruptSingleEscArms guards that one Esc does not abort the turn.
func TestWatchInterruptSingleEscArms(t *testing.T) {
	keys := make(chan rune, 1)
	c := &console{keys: keys}
	done := make(chan struct{})
	aborted := false
	var armed []bool

	go func() {
		keys <- keyEsc
		time.Sleep(50 * time.Millisecond) // let it arm, then finish the turn
		close(done)
	}()

	action := c.watchInterrupt(done, func() { aborted = true }, nil, nil, func(v bool) { armed = append(armed, v) })
	if action != interruptNone || aborted {
		t.Fatalf("action = %v aborted = %v, want none and not aborted", action, aborted)
	}
	if len(armed) == 0 || !armed[0] {
		t.Fatalf("armed = %v, want the first Esc to arm", armed)
	}
}

// TestWatchInterruptCtrlCExits guards that Ctrl+C aborts and asks to exit.
func TestWatchInterruptCtrlCExits(t *testing.T) {
	keys := make(chan rune, 1)
	c := &console{keys: keys}
	done := make(chan struct{})
	aborted := make(chan struct{})

	go func() { keys <- keyCtrlC }()

	action := c.watchInterrupt(done, func() { close(aborted) }, nil, nil, nil)
	if action != interruptExit {
		t.Fatalf("action = %v, want interruptExit", action)
	}
	<-aborted
}

// TestWatchInterruptSteersTypedInput guards that text typed during a turn is
// buffered and submitted as steering input on Enter.
func TestWatchInterruptSteersTypedInput(t *testing.T) {
	keys := make(chan rune, 16)
	c := &console{keys: keys}
	done := make(chan struct{})
	steered := make(chan string, 1)

	go func() {
		for _, r := range "hi" {
			keys <- r
		}
		keys <- keyEnter
		time.Sleep(20 * time.Millisecond)
		close(done)
	}()

	action := c.watchInterrupt(done, func() {}, func(s string) { steered <- s }, nil, nil)
	if action != interruptNone {
		t.Fatalf("action = %v, want interruptNone", action)
	}
	select {
	case s := <-steered:
		if s != "hi" {
			t.Fatalf("steered = %q, want %q", s, "hi")
		}
	default:
		t.Fatal("typed input was not steered")
	}
}

// TestWatchInterruptEditsSteerDraft guards backspace editing of the draft before
// it is submitted.
func TestWatchInterruptEditsSteerDraft(t *testing.T) {
	keys := make(chan rune, 16)
	c := &console{keys: keys}
	done := make(chan struct{})
	steered := make(chan string, 1)

	go func() {
		for _, r := range "hey" {
			keys <- r
		}
		keys <- keyBackspace // drop the trailing "y"
		keys <- keyEnter
		time.Sleep(20 * time.Millisecond)
		close(done)
	}()

	action := c.watchInterrupt(done, func() {}, func(s string) { steered <- s }, nil, nil)
	if action != interruptNone {
		t.Fatalf("action = %v, want interruptNone", action)
	}
	select {
	case s := <-steered:
		if s != "he" {
			t.Fatalf("steered = %q, want %q", s, "he")
		}
	default:
		t.Fatal("edited draft was not steered")
	}
}

// TestWatchInterruptIgnoresEscapeSequence guards that an arrow key (ESC [ A) is
// consumed as a sequence: it neither arms the interrupt nor leaks into the draft.
func TestWatchInterruptIgnoresEscapeSequence(t *testing.T) {
	keys := make(chan rune, 8)
	c := &console{keys: keys}
	done := make(chan struct{})
	armed := make(chan bool, 4)
	steered := make(chan string, 4)

	go func() {
		keys <- keyEsc
		keys <- '['
		keys <- keyUp
		time.Sleep(20 * time.Millisecond)
		close(done)
	}()

	action := c.watchInterrupt(done, func() {}, func(s string) { steered <- s }, nil, func(v bool) { armed <- v })
	if action != interruptNone {
		t.Fatalf("action = %v, want interruptNone", action)
	}
	select {
	case v := <-armed:
		t.Fatalf("armed = %v, want no arm for an escape sequence", v)
	default:
	}
	select {
	case s := <-steered:
		t.Fatalf("steered = %q, want nothing", s)
	default:
	}
}

func TestWatchInterruptStopsWhenTurnDone(t *testing.T) {
	c := &console{keys: make(chan rune)}
	done := make(chan struct{})
	close(done)
	if action := c.watchInterrupt(done, func() { t.Fatal("unexpected interrupt") }, nil, nil, nil); action != interruptNone {
		t.Fatalf("action = %v, want interruptNone", action)
	}
}

func TestWatchInterruptNonTTY(t *testing.T) {
	c := &console{} // keys == nil on a non-TTY
	done := make(chan struct{})
	close(done)
	if action := c.watchInterrupt(done, func() { t.Fatal("unexpected interrupt") }, nil, nil, nil); action != interruptNone {
		t.Fatalf("action = %v, want interruptNone", action)
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
		for _, option := range clientOptions(&console{out: io.Discard}, tc.yolo, "deepseek-v4-flash", "") {
			option(&opts)
		}
		if got := opts.Permission != nil; got != tc.wantPermission {
			t.Fatalf("yolo=%v: permission handler set = %v, want %v", tc.yolo, got, tc.wantPermission)
		}
	}
}
