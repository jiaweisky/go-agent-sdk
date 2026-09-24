package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/term"
	"golang.org/x/text/width"
)

// Raw-mode key codes (ASCII control codes) and escape-sequence finals.
const (
	keyCtrlA     = 1  // ^A: move to line start
	keyCtrlC     = 3  // ^C: exit
	keyCtrlD     = 4  // ^D: exit on empty line
	keyCtrlE     = 5  // ^E: move to line end
	keyCtrlH     = 8  // ^H: backspace
	keyCtrlU     = 21 // ^U: clear line
	keyCtrlW     = 23 // ^W: delete word
	keyEsc       = 27 // ESC: exit (or start of a sequence)
	keyBackspace = 127
	keyEnter     = '\r'
	keyLF        = '\n'
	keySpace     = ' '

	keyUp    = 'A'
	keyDown  = 'B'
	keyRight = 'C'
	keyLeft  = 'D'
	keyEnd   = 'F'
	keyHome  = 'H'
	keyTilde = '~'

	// Numeric parameters of ESC[n~ sequences.
	seqHome   = '1'
	seqDelete = '3'
	seqEnd    = '4'
)

// Non-printing control ranges for width calculation.
const (
	asciiDEL   = 0x7f
	asciiC1End = 0xa0
)

// Buffering and refresh intervals.
const (
	// stdinQueue is the rune buffer on each stdin stream: enough for typing
	// ahead, and never a blocking point for the reader.
	stdinQueue = 64
	// watchTick is the status-line refresh interval (spinner animation).
	watchTick = 100 * time.Millisecond
)

// console owns terminal input and output. On a TTY it runs a raw-mode,
// width-aware line editor so backspace/delete work for CJK as well as ASCII,
// independent of the terminal's erase character.
type console struct {
	mu     sync.Mutex
	editor *editor
	reader *bufio.Reader
	// keys streams raw runes from stdin on a TTY so the turn loop can watch
	// for Esc/Ctrl+C while a turn is running. Nil on a non-TTY.
	keys chan rune
	// edit streams the runes typed while a line editor owns stdin (the main
	// prompt or a permission/question prompt). Non-nil exactly when keys is.
	edit chan rune
	// prompting is set while a line editor owns stdin, so incoming runes are
	// routed to the editor and the status line is not drawn over the prompt.
	prompting atomic.Bool
	out       io.Writer

	// status is the live status line drawn at the bottom (e.g. a spinner +
	// running subagents). statusDrawn tracks whether it is currently on screen;
	// lineStart tracks whether the cursor sits at column 0 so the status line
	// can be redrawn without clobbering a partial line.
	status      string
	statusDrawn bool
	lineStart   bool
}

func newConsole() (*console, func(), error) {
	fd := int(os.Stdin.Fd())
	c := &console{out: os.Stdout, lineStart: true}
	if !term.IsTerminal(fd) {
		c.reader = bufio.NewReader(os.Stdin)
		return c, func() {}, nil
	}
	state, err := term.MakeRaw(fd)
	if err != nil {
		return nil, nil, err
	}
	c.keys = make(chan rune, stdinQueue)
	c.edit = make(chan rune, stdinQueue)
	go c.readStdin()
	c.editor = &editor{
		in:  &chanRuneReader{keys: c.edit},
		out: os.Stdout,
		width: func() int {
			if columns, _, err := term.GetSize(fd); err == nil {
				return columns
			}
			return 0
		},
	}
	c.out = crlfWriter{os.Stdout}
	return c, func() { _ = term.Restore(fd, state) }, nil
}

// readStdin is the only goroutine that reads stdin, and it routes every rune to
// exactly one owner: the line editor while a prompt is active, the turn watcher
// otherwise. Handing out runes here — instead of letting both consumers read the
// same channel — is what stops the turn watcher from swallowing a keystroke that
// was meant for a prompt: a lost "n" reaches interactivePermission as an empty
// answer, which defaults to allow.
func (c *console) readStdin() {
	reader := bufio.NewReader(os.Stdin)
	for {
		r, _, err := reader.ReadRune()
		if err != nil {
			close(c.keys)
			close(c.edit)
			return
		}
		c.deliver(r)
	}
}

// deliver routes one raw stdin rune to its owner.
func (c *console) deliver(r rune) {
	if c.prompting.Load() {
		enqueue(c.edit, r)
		return
	}
	enqueue(c.keys, r)
}

// enqueue queues r, dropping it when the queue is full: stdin must never block
// the reader, which would also stop interrupts from being delivered.
func enqueue(ch chan rune, r rune) {
	select {
	case ch <- r:
	default:
	}
}

// watchInterrupt waits until done closes or the user presses Esc/Ctrl+C,
// calling onInterrupt in the latter case. It reports whether an interrupt key
// was pressed. onTick (if non-nil) is called about every watchTick with a
// monotonically increasing frame counter, so callers can animate a spinner.
//
// Runes typed while a prompt owns stdin are routed to the editor by readStdin,
// so the watcher only ever sees keys meant for the turn it is watching.
func (c *console) watchInterrupt(done <-chan struct{}, onInterrupt func(), onTick func(int)) bool {
	ticker := time.NewTicker(watchTick)
	defer ticker.Stop()
	frame := 0
	tick := func() {
		if onTick != nil {
			frame++
			onTick(frame)
		}
	}
	if c.keys == nil {
		for {
			select {
			case <-done:
				return false
			case <-ticker.C:
				tick()
			}
		}
	}
	for {
		select {
		case <-done:
			return false
		case <-ticker.C:
			tick()
		case r, ok := <-c.keys:
			if !ok {
				<-done
				return false
			}
			if r == keyEsc || r == keyCtrlC {
				onInterrupt()
				return true
			}
		}
	}
}

// keyReader is the rune source the line editor consumes: a bufio.Reader in
// tests, a channel-backed reader on a TTY.
type keyReader interface {
	ReadRune() (rune, int, error)
	Buffered() int
}

// chanRuneReader adapts the stdin rune channel to keyReader.
type chanRuneReader struct {
	keys <-chan rune
}

func (r *chanRuneReader) ReadRune() (rune, int, error) {
	ch, ok := <-r.keys
	if !ok {
		return 0, 0, io.EOF
	}
	return ch, utf8.RuneLen(ch), nil
}

func (r *chanRuneReader) Buffered() int { return len(r.keys) }

// write appends normal output above the status line.
func (c *console) write(s string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.eraseStatusLocked()
	fmt.Fprint(c.out, s)
	if s != "" {
		c.lineStart = strings.HasSuffix(s, "\n")
	}
	c.drawStatusLocked()
}

// setStatus updates the bottom status line in place. An empty string clears it.
func (c *console) setStatus(status string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if status == c.status && (c.statusDrawn || status == "") {
		return
	}
	c.eraseStatusLocked()
	c.status = status
	c.drawStatusLocked()
}

func (c *console) eraseStatusLocked() {
	if c.statusDrawn {
		fmt.Fprint(c.out, "\r\x1b[2K")
		c.statusDrawn = false
	}
}

// drawStatusLocked redraws the status line, but only when it is safe: on a TTY,
// at the start of a line, and not while a prompt owns the line.
func (c *console) drawStatusLocked() {
	if c.status == "" || !c.lineStart || c.editor == nil || c.prompting.Load() {
		return
	}
	fmt.Fprint(c.out, c.status)
	c.statusDrawn = true
}

// prompt reads one line of input with the line editor. The returned line is
// trimmed (on both the TTY and the piped path), so callers can compare it to a
// command or test it for emptiness.
func (c *console) prompt(prompt string) (string, error) {
	c.prompting.Store(true)
	defer c.prompting.Store(false)
	c.mu.Lock()
	defer func() {
		// The editor wrote directly to the terminal; the next status update
		// redraws from a clean line.
		c.statusDrawn = false
		c.lineStart = true
		c.mu.Unlock()
	}()
	c.eraseStatusLocked()
	if c.editor != nil {
		line, err := c.editor.readLine(prompt)
		return strings.TrimSpace(line), err
	}
	fmt.Fprint(c.out, prompt)
	line, err := c.reader.ReadString('\n')
	return strings.TrimSpace(line), err
}

// crlfWriter converts bare newlines to CRLF for raw-mode output.
type crlfWriter struct{ w io.Writer }

func (w crlfWriter) Write(p []byte) (int, error) {
	out := make([]byte, 0, len(p)+8)
	for _, b := range p {
		if b == '\n' {
			out = append(out, '\r', '\n')
		} else {
			out = append(out, b)
		}
	}
	if _, err := w.w.Write(out); err != nil {
		return 0, err
	}
	return len(p), nil
}

// editor is a minimal raw-mode line editor that accounts for East Asian wide
// runes when moving the cursor and erasing.
type editor struct {
	in      keyReader
	out     io.Writer
	history []string
	// width reports the terminal width in columns, or 0 when unknown. A line
	// wider than the terminal is drawn as a scrolling window instead of being
	// allowed to wrap: the in-place redraw assumes the line occupies one row.
	width func() int
}

func (e *editor) readLine(prompt string) (string, error) {
	line := make([]rune, 0, 64)
	pos := 0
	historyIndex := len(e.history)

	e.draw(prompt, line, pos)
	for {
		r, _, err := e.in.ReadRune()
		if err != nil {
			return "", err
		}
		switch r {
		case keyEnter, keyLF:
			fmt.Fprint(e.out, "\r\n")
			result := string(line)
			if strings.TrimSpace(result) != "" {
				e.history = append(e.history, result)
			}
			return result, nil
		case keyCtrlC: // Ctrl+C
			fmt.Fprint(e.out, "\r\n")
			return "", io.EOF
		case keyCtrlD: // Ctrl+D
			if len(line) == 0 {
				fmt.Fprint(e.out, "\r\n")
				return "", io.EOF
			}
		case keyBackspace, keyCtrlH: // Backspace (DEL / ^H)
			if pos > 0 {
				line = append(line[:pos-1], line[pos:]...)
				pos--
			}
		case keyCtrlA: // Ctrl+A
			pos = 0
		case keyCtrlE: // Ctrl+E
			pos = len(line)
		case keyCtrlU: // Ctrl+U
			line = line[:0]
			pos = 0
		case keyCtrlW: // Ctrl+W
			for pos > 0 && line[pos-1] == keySpace {
				line = append(line[:pos-1], line[pos:]...)
				pos--
			}
			for pos > 0 && line[pos-1] != keySpace {
				line = append(line[:pos-1], line[pos:]...)
				pos--
			}
		case keyEsc: // escape
			// A lone Esc has no buffered follow-up rune; a real escape sequence
			// (arrows, etc.) is sent as one write and shows up shortly after.
			if !e.waitPending(30 * time.Millisecond) {
				fmt.Fprint(e.out, "\r\n")
				return "", io.EOF
			}
			key, err := e.readEscape()
			if err != nil {
				return "", err
			}
			switch key {
			case keyLeft: // left
				if pos > 0 {
					pos--
				}
			case keyRight: // right
				if pos < len(line) {
					pos++
				}
			case keyHome: // home
				pos = 0
			case keyEnd: // end
				pos = len(line)
			case keyUp: // history up
				if historyIndex > 0 {
					historyIndex--
					line = []rune(e.history[historyIndex])
					pos = len(line)
				}
			case keyDown: // history down
				if historyIndex < len(e.history) {
					historyIndex++
					if historyIndex == len(e.history) {
						line = line[:0]
					} else {
						line = []rune(e.history[historyIndex])
					}
					pos = len(line)
				}
			case seqDelete: // delete (ESC[3~)
				if pos < len(line) {
					line = append(line[:pos], line[pos+1:]...)
				}
			}
		default:
			if r >= keySpace {
				line = append(line, 0)
				copy(line[pos+1:], line[pos:])
				line[pos] = r
				pos++
			}
		}
		e.draw(prompt, line, pos)
	}
}

// waitPending reports whether another rune arrives within timeout. A lone Esc
// yields none; an escape sequence's remaining runes arrive within a few ms.
func (e *editor) waitPending(timeout time.Duration) bool {
	if e.in.Buffered() > 0 {
		return true
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
		if e.in.Buffered() > 0 {
			return true
		}
	}
	return false
}

func (e *editor) readEscape() (rune, error) {
	r, _, err := e.in.ReadRune()
	if err != nil {
		return 0, err
	}
	switch r {
	case 'O':
		// SS3: application cursor keys (DECCKM) send ESC O A/B/C/D/H/F, so the
		// rune after "ESC O" is the key itself. Without this the 'O' is dropped
		// and the final rune is inserted as text (pressing Up typed an "A").
		final, _, err := e.in.ReadRune()
		if err != nil {
			return 0, err
		}
		return final, nil
	case '[':
	default:
		return r, nil
	}
	var last rune
	for {
		r, _, err = e.in.ReadRune()
		if err != nil {
			return 0, err
		}
		switch {
		case (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z'):
			return r, nil
		case r == keyTilde:
			switch last {
			case seqHome:
				return keyHome, nil
			case seqEnd:
				return keyEnd, nil
			case seqDelete:
				return seqDelete, nil
			}
			return 0, nil
		default:
			last = r
		}
	}
}

func (e *editor) draw(prompt string, line []rune, pos int) {
	from, to := e.viewport(prompt, line, pos)
	fmt.Fprint(e.out, "\r\x1b[K")
	fmt.Fprint(e.out, prompt)
	fmt.Fprint(e.out, string(line[from:to]))
	if back := runesWidth(line[pos:to]); back > 0 {
		fmt.Fprintf(e.out, "\x1b[%dD", back)
	}
}

// viewport returns the slice of line to draw: the cursor stays visible and the
// drawn runes never exceed the terminal width. A line that does not fit scrolls
// horizontally; drawing it in full would wrap and break the in-place redraw.
func (e *editor) viewport(prompt string, line []rune, pos int) (int, int) {
	if e.width == nil {
		return 0, len(line)
	}
	usable := e.width() - stringWidth(prompt)
	if usable <= 0 || runesWidth(line) <= usable {
		return 0, len(line)
	}
	// Drop leading runes until the tail fits, so the end of the line stays in
	// view while the cursor is inside the window.
	from := 0
	for from < len(line) && runesWidth(line[from:]) > usable {
		from++
	}
	if from <= pos {
		return from, len(line)
	}
	// The cursor sits in the dropped head: keep it visible and cut the tail.
	from, to := pos, pos
	for to < len(line) && runesWidth(line[from:to+1]) <= usable {
		to++
	}
	return from, to
}

func stringWidth(s string) int { return runesWidth([]rune(s)) }

func runesWidth(runes []rune) int {
	total := 0
	for _, r := range runes {
		total += runeWidth(r)
	}
	return total
}

// runeWidth returns the terminal cell width of r, treating East Asian wide and
// fullwidth runes as two cells and combining marks as none.
func runeWidth(r rune) int {
	if r == 0 || r < keySpace || (r >= asciiDEL && r < asciiC1End) {
		return 0
	}
	switch width.LookupRune(r).Kind() {
	case width.EastAsianWide, width.EastAsianFullwidth:
		return 2
	}
	// A combining mark adds no cell of its own; counting it would shift the
	// cursor one column per accent.
	if unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) {
		return 0
	}
	return 1
}
