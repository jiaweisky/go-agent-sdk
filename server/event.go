package server

import (
	"context"
	"net/http"
	"strconv"
	"sync"

	"github.com/jiaweisky/go-agent-sdk/schema"
)

// handleEvent streams every live event (all sessions, including subagents).
func (s *Server) handleEvent(w http.ResponseWriter, r *http.Request) {
	sw, ok := openSSE(w)
	if !ok {
		return
	}
	ctx, cancel := s.streamContext(r)
	defer cancel()
	startHeartbeat(ctx, sw, s.opts.KeepAlive)
	if sw.event(connectedEvent()) != nil {
		return
	}
	for ev, err := range s.client.EventStream().AllBounded(ctx, s.opts.SubscriberCapacity) {
		if err != nil {
			_ = sw.errorFrame("subscriber_overflow", s.opts.SubscriberCapacity)
			return
		}
		if sw.event(toWireEvent(ev)) != nil {
			return
		}
	}
}

// handleSessionEvent streams one session: durable replay (when ?after= is
// given) merged with the live filtered stream.
func (s *Server) handleSessionEvent(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("sessionID")
	if _, err := s.client.GetSession(r.Context(), id); err != nil {
		writeSDKError(w, err)
		return
	}
	after := -1
	if raw := r.URL.Query().Get("after"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", "invalid after")
			return
		}
		after = value
	}
	sw, ok := openSSE(w)
	if !ok {
		return
	}
	ctx, cancel := s.streamContext(r)
	defer cancel()
	startHeartbeat(ctx, sw, s.opts.KeepAlive)
	if sw.event(connectedEvent()) != nil {
		return
	}
	s.streamSessionEvents(ctx, sw, id, after)
}

func (s *Server) streamSessionEvents(ctx context.Context, sw *sseWriter, id string, after int) {
	mergeCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	out := make(chan schema.EventPayload, s.opts.SubscriberCapacity)
	var wg sync.WaitGroup

	// live leg (all events for this session, including live-only deltas/HITL)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for ev := range s.client.EventStream().AllBounded(mergeCtx, s.opts.SubscriberCapacity) {
			if eventSessionID(ev) != id {
				continue
			}
			select {
			case out <- ev:
			case <-mergeCtx.Done():
				return
			}
		}
	}()

	// durable replay leg (persisted events after `after`), only when requested
	if after >= 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ev := range s.client.EventStream().Durable(mergeCtx, id, after) {
				select {
				case out <- ev:
				case <-mergeCtx.Done():
					return
				}
			}
		}()
	}

	go func() {
		wg.Wait()
		close(out)
	}()

	seen := map[string]bool{}
	for ev := range out {
		key := string(ev.ID)
		if key != "" {
			if seen[key] {
				continue
			}
			seen[key] = true
		}
		if sw.event(toWireEvent(ev)) != nil {
			return
		}
	}
}
