package main

import (
	"strings"
	"time"
)

// escapeWait is how long the turn watcher waits for the rune after an Esc before
// deciding it was a lone key rather than the start of an escape sequence. A real
// sequence (arrow keys, delete) arrives as one write, well within this window.
const escapeWait = 30 * time.Millisecond

// turnWatcher interprets input while a turn runs: Esc/Ctrl+C interrupt, typed
// text becomes a steering draft, and Enter submits it.
type turnWatcher struct {
	console    *console
	abort      func()
	steer      func(string)
	onArm      func(bool)
	armed      bool
	armTimer   *time.Timer
	armTimeout <-chan time.Time
	// pending holds a rune read while deciding whether an Esc started an escape
	// sequence, so the rune is handled next instead of being lost.
	pending *rune
}

func newTurnWatcher(console *console, abort func(), steer func(string), onArm func(bool)) *turnWatcher {
	return &turnWatcher{console: console, abort: abort, steer: steer, onArm: onArm}
}

// next returns a rune pushed back by an earlier Esc, if any.
func (w *turnWatcher) next() (rune, bool) {
	if w.pending == nil {
		return 0, false
	}
	r := *w.pending
	w.pending = nil
	return r, true
}

// handle applies one key, reporting the interrupt it caused (if any).
func (w *turnWatcher) handle(r rune) (interruptAction, bool) {
	switch {
	case r == keyCtrlC:
		w.abort()
		return interruptExit, true
	case r == keyEsc:
		return w.escape()
	case r == keyEnter || r == keyLF:
		w.submitDraft()
	case r == keyBackspace || r == keyCtrlH:
		w.console.draftBackspace()
		w.console.redrawStatus()
	case r == keyCtrlU:
		w.console.draftClear()
		w.console.redrawStatus()
	default:
		if r >= keySpace {
			w.console.draftAppend(r)
			w.console.redrawStatus()
		}
	}
	return interruptNone, false
}

// escape handles an Esc: a lone Esc arms the interrupt (and a second aborts),
// while an escape sequence (arrow keys, delete) is consumed and ignored.
func (w *turnWatcher) escape() (interruptAction, bool) {
	if follow, ok := w.console.takeKey(escapeWait); ok {
		if follow == '[' || follow == 'O' {
			w.console.drainEscape(follow)
			return interruptNone, false
		}
		// Not an escape sequence: keep the rune for the next round.
		w.pending = &follow
	}
	if !w.armed {
		w.armed = true
		if w.onArm != nil {
			w.onArm(true)
		}
		if w.armTimer == nil {
			w.armTimer = time.NewTimer(interruptWindow)
		} else {
			w.armTimer.Reset(interruptWindow)
		}
		w.armTimeout = w.armTimer.C
		return interruptNone, false
	}
	w.abort()
	return interruptAbort, true
}

// submitDraft sends the typed line as steering input and clears the draft.
func (w *turnWatcher) submitDraft() {
	if text := strings.TrimSpace(w.console.draftText()); text != "" && w.steer != nil {
		w.console.draftClear()
		w.steer(text)
	}
	w.console.redrawStatus()
}

// disarm clears a pending arm, so the UI stops prompting for a second Esc.
func (w *turnWatcher) disarm() {
	if !w.armed {
		return
	}
	w.armed = false
	if w.onArm != nil {
		w.onArm(false)
	}
}

// close stops the arm timer and clears the draft when the turn ends.
func (w *turnWatcher) close() {
	if w.armTimer != nil {
		w.armTimer.Stop()
	}
	w.disarm()
	w.console.draftClear()
	w.console.redrawStatus()
}

// takeKey reads the next rune within timeout, or reports false when none arrives.
// It lets an Esc be told apart from an escape sequence without losing the rune.
func (c *console) takeKey(timeout time.Duration) (rune, bool) {
	in := &chanRuneReader{keys: c.keys}
	if !waitRune(in, timeout) {
		return 0, false
	}
	r, _, err := in.ReadRune()
	if err != nil {
		return 0, false
	}
	return r, true
}

// drainEscape consumes the rest of an escape sequence whose introducer (the rune
// after Esc) was already read, so its bytes never reach the steering draft.
func (c *console) drainEscape(intro rune) {
	if intro == 'O' {
		// SS3: ESC O <final>.
		_, _ = c.takeKey(escapeWait)
		return
	}
	// CSI: ESC [ <params> <final>.
	for i := 0; i < 8; i++ {
		r, ok := c.takeKey(escapeWait)
		if !ok || isEscapeFinal(r) {
			return
		}
	}
}

// draftAppend adds a rune to the steering draft.
func (c *console) draftAppend(r rune) {
	c.mu.Lock()
	c.draft = append(c.draft, r)
	c.mu.Unlock()
}

// draftBackspace removes the last rune of the steering draft.
func (c *console) draftBackspace() {
	c.mu.Lock()
	if n := len(c.draft); n > 0 {
		c.draft = c.draft[:n-1]
	}
	c.mu.Unlock()
}

// draftClear empties the steering draft.
func (c *console) draftClear() {
	c.mu.Lock()
	c.draft = c.draft[:0]
	c.mu.Unlock()
}

// draftText returns the steering draft as a string.
func (c *console) draftText() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return string(c.draft)
}

// redrawStatus erases and redraws the status line, including the steering draft.
func (c *console) redrawStatus() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.eraseStatusLocked()
	c.drawStatusLocked()
}
