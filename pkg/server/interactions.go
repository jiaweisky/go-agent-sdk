package server

import (
	"context"
	"time"

	agent "github.com/jiaweisky/go-agent-sdk"
)

// runWatcher auto-resolves held interactions that no client answers within
// Options.InteractionTimeout. Pending requests carry no timestamp, so arrival
// time is tracked by first observation.
func (s *Server) runWatcher(ctx context.Context) {
	if s.opts.InteractionTimeout <= 0 {
		return
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	seen := map[string]time.Time{}
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.sweepInteractions(seen, time.Now())
		}
	}
}

// sweepInteractions resolves requests whose first observation is older than the
// timeout and forgets requests that are no longer pending.
func (s *Server) sweepInteractions(seen map[string]time.Time, now time.Time) {
	pending := map[string]bool{}
	for _, req := range s.client.Permission().List() {
		pending[req.ID] = true
		if !s.expired(seen, req.ID, now) {
			continue
		}
		reply := agent.PermissionReplyReject
		if s.opts.InteractionFallback == "allow" {
			reply = agent.PermissionReplyOnce
		}
		_ = s.client.Permission().Reply(agent.PermissionReplyInput{RequestID: req.ID, Reply: reply})
		delete(seen, req.ID)
	}
	for _, req := range s.client.Question().List() {
		pending[req.ID] = true
		if !s.expired(seen, req.ID, now) {
			continue
		}
		_ = s.client.Question().Reject(req.ID)
		delete(seen, req.ID)
	}
	for id := range seen {
		if !pending[id] {
			delete(seen, id)
		}
	}
}

// expired records id's first observation and reports whether it has been
// pending longer than the interaction timeout.
func (s *Server) expired(seen map[string]time.Time, id string, now time.Time) bool {
	first, ok := seen[id]
	if !ok {
		seen[id] = now
		return false
	}
	return now.Sub(first) >= s.opts.InteractionTimeout
}
