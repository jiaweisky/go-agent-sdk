package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/jiaweisky/go-agent-sdk/schema"
)

type wireEvent struct {
	ID       string         `json:"id"`
	Type     string         `json:"type"`
	Data     any            `json:"data"`
	Durable  *wireDurable   `json:"durable,omitempty"`
	Location *wireLocation  `json:"location,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

type wireDurable struct {
	AggregateID string `json:"aggregateID"`
	Seq         int    `json:"seq"`
	Version     int    `json:"version"`
}

type wireLocation struct {
	Directory   string  `json:"directory"`
	WorkspaceID *string `json:"workspaceID,omitempty"`
}

func toWireEvent(p schema.EventPayload) wireEvent {
	ev := wireEvent{ID: string(p.ID), Type: p.Type, Data: p.Data, Metadata: p.Metadata}
	if p.Durable != nil {
		ev.Durable = &wireDurable{AggregateID: p.Durable.AggregateID, Seq: p.Durable.Seq, Version: p.Durable.Version}
	}
	if p.Location != nil {
		loc := &wireLocation{Directory: string(p.Location.Directory)}
		if p.Location.WorkspaceID != nil {
			ws := string(*p.Location.WorkspaceID)
			loc.WorkspaceID = &ws
		}
		ev.Location = loc
	}
	return ev
}

func connectedEvent() wireEvent {
	return wireEvent{ID: string(schema.NewEventID()), Type: "server.connected", Data: map[string]any{}}
}

// sseWriter serialises writes to one SSE response.
type sseWriter struct {
	mu sync.Mutex
	w  http.ResponseWriter
	f  http.Flusher
}

func (s *sseWriter) event(ev wireEvent) error {
	body, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	return s.raw("event: message\ndata: %s\n\n", body)
}

func (s *sseWriter) comment(text string) error {
	return s.raw(": %s\n\n", text)
}

func (s *sseWriter) errorFrame(code string, capacity int) error {
	body, _ := json.Marshal(map[string]any{"error": code, "capacity": capacity})
	return s.raw("event: error\ndata: %s\n\n", body)
}

func (s *sseWriter) raw(format string, args ...any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := fmt.Fprintf(s.w, format, args...); err != nil {
		return err
	}
	s.f.Flush()
	return nil
}

func startHeartbeat(ctx context.Context, sw *sseWriter, interval time.Duration) {
	if interval <= 0 {
		return
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if sw.comment("heartbeat") != nil {
					return
				}
			}
		}
	}()
}

// streamContext returns a context cancelled when either the request or the
// server closes.
func (s *Server) streamContext(r *http.Request) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(r.Context())
	stop := context.AfterFunc(s.ctx, cancel)
	return ctx, func() {
		stop()
		cancel()
	}
}

// openSSE writes the event-stream headers and returns a writer, or false.
func openSSE(w http.ResponseWriter) (*sseWriter, bool) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "internal", "streaming unsupported")
		return nil, false
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()
	return &sseWriter{w: w, f: flusher}, true
}

// eventSessionID reads the sessionID from a raw event payload, if any.
func eventSessionID(p schema.EventPayload) string {
	data, ok := p.Data.(map[string]any)
	if !ok {
		return ""
	}
	switch id := data["sessionID"].(type) {
	case string:
		return id
	case schema.SessionID:
		return string(id)
	default:
		return ""
	}
}
