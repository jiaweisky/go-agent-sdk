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
	// prompting is set while a line editor owns stdin (the main prompt or a
	// permission/question prompt), so the turn watcher yields input to it.
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
	c.keys = make(chan rune, 64)
	go func() {
		reader := bufio.NewReader(os.Stdin)
		for {
			r, _, err := reader.ReadRune()
			if err != nil {
				close(c.keys)
				return
			}
			c.keys <- r
		}
	}()
	c.editor = &editor{in: &chanRuneReader{keys: c.keys}, out: os.Stdout}
	c.out = crlfWriter{os.Stdout}
	return c, func() { _ = term.Restore(fd, state) }, nil
}

// watchInterrupt waits until done closes or the user presses Esc/Ctrl+C,
// calling onInterrupt in the latter case. It reports whether an interrupt key
// was pressed. onTick (if non-nil) is called about every watchTick with a
// monotonically increasing frame counter, so callers can animate a spinner.
//
// While a line editor owns stdin (a prompt is active) the watcher yields input
// to it instead of consuming keys.
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
		if c.prompting.Load() {
			select {
			case <-done:
				return false
			case <-ticker.C:
				tick()
			case <-time.After(20 * time.Millisecond):
				continue
			}
		}
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

// watchTick is the status-line refresh interval (spinner animation).
const watchTick = 100 * time.Millisecond

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
		return c.editor.readLine(prompt)
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
			if !e.hasPending(30 * time.Millisecond) {
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

// hasPending reports whether another rune is available within timeout. A lone
// Esc yields none; an escape sequence's remaining runes arrive within a few ms.
func (e *editor) hasPending(timeout time.Duration) bool {
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
	if r != '[' {
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
	fmt.Fprint(e.out, "\r\x1b[K")
	fmt.Fprint(e.out, prompt)
	fmt.Fprint(e.out, string(line))
	if back := runesWidth(line[pos:]); back > 0 {
		fmt.Fprintf(e.out, "\x1b[%dD", back)
	}
}

func runesWidth(runes []rune) int {
	total := 0
	for _, r := range runes {
		total += runeWidth(r)
	}
	return total
}

// runeWidth returns the terminal cell width of r, treating East Asian wide and
// fullwidth runes as two cells.
func runeWidth(r rune) int {
	if r == 0 || r < keySpace || (r >= asciiDEL && r < asciiC1End) {
		return 0
	}
	switch width.LookupRune(r).Kind() {
	case width.EastAsianWide, width.EastAsianFullwidth:
		return 2
	}
	return 1
}
