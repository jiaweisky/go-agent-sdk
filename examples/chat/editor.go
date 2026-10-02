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

// lineState is the editable state of one prompt: the runes typed so far, the
// cursor, and the history position (len(history) means "the line being typed").
type lineState struct {
	runes        []rune
	pos          int
	historyIndex int
}

func (e *editor) readLine(prompt string) (string, error) {
	state := lineState{runes: make([]rune, 0, 64), historyIndex: len(e.history)}
	e.draw(prompt, state.runes, state.pos)
	for {
		r, _, err := e.in.ReadRune()
		if err != nil {
			return "", err
		}
		submit, err := e.applyKey(&state, r)
		if err != nil {
			return "", err
		}
		if submit {
			return e.remember(state.runes), nil
		}
		e.draw(prompt, state.runes, state.pos)
	}
}

// applyKey applies one key to the line, and reports whether it was submitted
// (in which case the caller returns the line).
func (e *editor) applyKey(state *lineState, r rune) (bool, error) {
	switch r {
	case keyEnter, keyLF:
		fmt.Fprint(e.out, "\r\n")
		return true, nil
	case keyCtrlC:
		fmt.Fprint(e.out, "\r\n")
		return false, io.EOF
	case keyCtrlD:
		if len(state.runes) == 0 {
			fmt.Fprint(e.out, "\r\n")
			return false, io.EOF
		}
	case keyBackspace, keyCtrlH:
		state.deleteBefore()
	case keyCtrlA:
		state.pos = 0
	case keyCtrlE:
		state.pos = len(state.runes)
	case keyCtrlU:
		state.runes = state.runes[:0]
		state.pos = 0
	case keyCtrlW:
		state.deleteWordBefore()
	case keyEsc:
		return false, e.applyEscape(state)
	default:
		if r >= keySpace {
			state.insert(r)
		}
	}
	return false, nil
}

// applyEscape consumes an escape sequence, or a lone Esc, which is ignored;
// exiting is Ctrl+C or Ctrl+D.
func (e *editor) applyEscape(state *lineState) error {
	// A lone Esc has no buffered follow-up rune; a real escape sequence (arrows,
	// etc.) is sent as one write and shows up shortly after.
	if !waitRune(e.in, 30*time.Millisecond) {
		return nil
	}
	key, err := e.readEscape()
	if err != nil {
		return err
	}
	switch key {
	case keyLeft:
		state.move(-1)
	case keyRight:
		state.move(1)
	case keyHome:
		state.pos = 0
	case keyEnd:
		state.pos = len(state.runes)
	case keyUp:
		state.historyUp(e.history)
	case keyDown:
		state.historyDown(e.history)
	case seqDelete:
		state.deleteAt()
	}
	return nil
}

// remember records a non-empty line in the history and returns it.
func (e *editor) remember(line []rune) string {
	result := string(line)
	if strings.TrimSpace(result) != "" {
		e.history = append(e.history, result)
	}
	return result
}

// insert writes r at the cursor.
func (s *lineState) insert(r rune) {
	s.runes = append(s.runes, 0)
	copy(s.runes[s.pos+1:], s.runes[s.pos:])
	s.runes[s.pos] = r
	s.pos++
}

// deleteBefore removes the rune before the cursor.
func (s *lineState) deleteBefore() {
	if s.pos == 0 {
		return
	}
	s.runes = append(s.runes[:s.pos-1], s.runes[s.pos:]...)
	s.pos--
}

// deleteAt removes the rune under the cursor.
func (s *lineState) deleteAt() {
	if s.pos < len(s.runes) {
		s.runes = append(s.runes[:s.pos], s.runes[s.pos+1:]...)
	}
}

// deleteWordBefore removes the spaces before the cursor, then the word.
func (s *lineState) deleteWordBefore() {
	for s.pos > 0 && s.runes[s.pos-1] == keySpace {
		s.deleteBefore()
	}
	for s.pos > 0 && s.runes[s.pos-1] != keySpace {
		s.deleteBefore()
	}
}

// move shifts the cursor by delta, staying inside the line.
func (s *lineState) move(delta int) {
	if next := s.pos + delta; next >= 0 && next <= len(s.runes) {
		s.pos = next
	}
}

// historyUp recalls the previous history entry.
func (s *lineState) historyUp(history []string) {
	if s.historyIndex == 0 {
		return
	}
	s.historyIndex--
	s.runes = []rune(history[s.historyIndex])
	s.pos = len(s.runes)
}

// historyDown recalls the next history entry, or the line being typed once the
// newest entry is passed.
func (s *lineState) historyDown(history []string) {
	if s.historyIndex >= len(history) {
		return
	}
	s.historyIndex++
	if s.historyIndex == len(history) {
		s.runes = s.runes[:0]
	} else {
		s.runes = []rune(history[s.historyIndex])
	}
	s.pos = len(s.runes)
}

// waitRune reports whether in has a rune within timeout. A lone Esc yields none;
// an escape sequence's remaining runes arrive within a few milliseconds.
func waitRune(in keyReader, timeout time.Duration) bool {
	if in.Buffered() > 0 {
		return true
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
		if in.Buffered() > 0 {
			return true
		}
	}
	return false
}

// isEscapeFinal reports whether r ends an escape sequence: the letter final of a
// CSI/SS3 sequence, or the "~" of a numeric-parameter sequence.
func isEscapeFinal(r rune) bool {
	return (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || r == keyTilde
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
		return e.readCSI()
	default:
		return r, nil
	}
}

// readCSI consumes the rest of an "ESC [ ..." sequence: a letter final is the
// key itself, and a "~" final maps its numeric parameter.
func (e *editor) readCSI() (rune, error) {
	last := rune(0)
	for {
		r, _, err := e.in.ReadRune()
		if err != nil {
			return 0, err
		}
		if !isEscapeFinal(r) {
			last = r
			continue
		}
		if r == keyTilde {
			return tildeKey(last), nil
		}
		return r, nil
	}
}

// tildeKey maps the numeric parameter of an "ESC [ n ~" sequence to its key.
func tildeKey(param rune) rune {
	switch param {
	case seqHome:
		return keyHome
	case seqEnd:
		return keyEnd
	case seqDelete:
		return seqDelete
	}
	return 0
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
