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
			now := time.Now()
			pending := map[string]bool{}
			for _, req := range s.client.Permission().List() {
				pending[req.ID] = true
				first, ok := seen[req.ID]
				if !ok {
					seen[req.ID] = now
					continue
				}
				if now.Sub(first) < s.opts.InteractionTimeout {
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
				first, ok := seen[req.ID]
				if !ok {
					seen[req.ID] = now
					continue
				}
				if now.Sub(first) < s.opts.InteractionTimeout {
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
	}
}
