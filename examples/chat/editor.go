package main

import (
	"fmt"
	"io"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/width"
)

// Non-printing control ranges for width calculation.
const (
	asciiDEL   = 0x7f
	asciiC1End = 0xa0
)

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
