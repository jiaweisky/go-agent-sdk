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

	"golang.org/x/term"
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

// Buffering and refresh intervals.
const (
	// stdinQueue is the rune buffer on each stdin stream: enough for typing
	// ahead, and never a blocking point for the reader.
	stdinQueue = 64
	// watchTick is the status-line refresh interval (spinner animation).
	watchTick = 100 * time.Millisecond
	// interruptWindow is how long a first Esc stays armed, so that pressing Esc
	// again within it interrupts the turn.
	interruptWindow = 5 * time.Second
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
	// draft is the steering input typed while a turn runs, shown in the status
	// line and submitted with Enter.
	draft []rune
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

// interruptAction is why watchInterrupt stopped watching a running turn.
type interruptAction int

const (
	// interruptNone: the turn finished on its own.
	interruptNone interruptAction = iota
	// interruptAbort: Esc was pressed twice; the turn was aborted and the chat
	// continues.
	interruptAbort
	// interruptExit: Ctrl+C; the turn was aborted and the chat should exit.
	interruptExit
)

// watchInterrupt waits until done closes or the user interrupts the turn. A
// first Esc arms the interrupt and calls onArm(true) so the UI can prompt for a
// second press; a second Esc within interruptWindow aborts the turn and keeps
// the chat alive. Ctrl+C aborts and asks the caller to exit. Text typed while
// the turn runs is buffered and submitted to steer on Enter. onTick (if non-nil)
// is called about every watchTick with a monotonically increasing frame counter,
// so callers can animate a spinner.
//
// Runes typed while a prompt owns stdin are routed to the editor by readStdin,
// so the watcher only ever sees keys meant for the turn it is watching.
func (c *console) watchInterrupt(
	done <-chan struct{},
	abort func(),
	steer func(string),
	onTick func(int),
	onArm func(bool),
) interruptAction {
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
				return interruptNone
			case <-ticker.C:
				tick()
			}
		}
	}

	watcher := newTurnWatcher(c, abort, steer, onArm)
	defer watcher.close()
	for {
		if r, ok := watcher.next(); ok {
			if action, stop := watcher.handle(r); stop {
				return action
			}
			continue
		}
		select {
		case <-done:
			return interruptNone
		case <-ticker.C:
			tick()
		case <-watcher.armTimeout:
			watcher.armTimeout = nil
			watcher.disarm()
		case r, ok := <-c.keys:
			if !ok {
				<-done
				return interruptNone
			}
			if action, stop := watcher.handle(r); stop {
				return action
			}
		}
	}
}

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
// at the start of a line, and not while a prompt owns the line. The steering
// draft, if any, is appended so the user sees what will be sent on Enter.
func (c *console) drawStatusLocked() {
	if !c.lineStart || c.editor == nil || c.prompting.Load() {
		return
	}
	line := c.status
	if len(c.draft) > 0 {
		label := "steer: " + string(c.draft)
		if line == "" {
			line = dim + label + reset
		} else {
			line += dim + " · " + label + reset
		}
	}
	if line == "" {
		return
	}
	fmt.Fprint(c.out, line)
	c.statusDrawn = true
}

// prompt reads one line of input with the line editor. The returned line is
// trimmed (on both the TTY and the piped path), so callers can compare it to a
// command or test it for emptiness.
func (c *console) prompt(prompt string) (string, error) {
	c.mu.Lock()
	// Claim stdin only while holding the lock, and release it before unlocking.
	// Parallel tool calls can request permission (or questions) at the same
	// time; without this a waiting prompt would find stdin already handed back
	// and read from the turn watcher's channel instead of the editor.
	c.prompting.Store(true)
	defer func() {
		c.prompting.Store(false)
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
