package server

import (
	"fmt"

	agent "github.com/jiaweisky/go-agent-sdk"
)

// worker drains a session's queued prompts one turn at a time.
type worker struct {
	s     *Server
	id    string
	queue []agent.MessageOptions
}

// enqueue appends a prompt to the session's worker, starting it if idle.
func (s *Server) enqueue(sessionID string, msg agent.MessageOptions) {
	e := s.entry(sessionID)
	e.mu.Lock()
	if e.worker == nil {
		w := &worker{s: s, id: sessionID, queue: []agent.MessageOptions{msg}}
		e.worker = w
		go w.run()
	} else {
		e.worker.queue = append(e.worker.queue, msg)
	}
	e.mu.Unlock()
}

func (w *worker) run() {
	for {
		e := w.s.entry(w.id)
		e.mu.Lock()
		if len(w.queue) == 0 {
			if e.worker == w {
				e.worker = nil
			}
			e.mu.Unlock()
			w.s.maybeEvict(w.id)
			return
		}
		msg := w.queue[0]
		w.queue = w.queue[1:]
		e.mu.Unlock()
		w.runOne(msg)
	}
}

func (w *worker) runOne(msg agent.MessageOptions) {
	handle, err := w.s.attach(w.s.ctx, w.id)
	if err != nil {
		w.report(err)
		return
	}
	// Drain the turn to completion. The prompt request already returned 202, so
	// a failure cannot be returned to the caller; report it instead of dropping
	// it.
	for _, sendErr := range handle.Send(w.s.ctx, msg) {
		if sendErr != nil {
			w.report(sendErr)
			break
		}
	}
}

// report surfaces an asynchronous turn failure to Options.OnError.
func (w *worker) report(err error) {
	if w.s.onError != nil {
		w.s.onError(fmt.Errorf("session %s: turn failed: %w", w.id, err))
	}
}
