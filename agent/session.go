package agent

import (
	"context"
	"iter"
	"sync"

	"github.com/user/go-agent-sdk/core/session"
	"github.com/user/go-agent-sdk/schema"
)

// SessionConfig configures a session.
type SessionConfig struct {
	// Agent is the agent name; defaults to "build".
	Agent string
	// Model overrides the client default model.
	Model string
	Title string
	// SystemPrompt customizes the system prompt.
	SystemPrompt *SystemPromptConfig
	// Permissions are merged over the agent's permissions for this session
	// (session rules win). Use it for per-session rules without registering a
	// separate agent.
	Permissions schema.PermissionRuleset
	// Tools are session-scoped user tools.
	Tools []Tool
	// OnPermissionRequest overrides the client permission handler for this
	// session.
	OnPermissionRequest PermissionHandlerFunc
	// OnQuestion overrides the client question handler for this session.
	OnQuestion QuestionHandlerFunc
	// Hooks intercepts session and tool lifecycle events.
	Hooks *SessionHooks
}

// MessageOptions describes a message to send.
type MessageOptions struct {
	Prompt string
	// Mode is "queue" (default) or "steer".
	Mode string
}

// Session is a durable conversation bound to an agent.
type Session struct {
	client *Client
	id     schema.SessionID
	// agentID is the effective agent (a derived agent when the session
	// customizes the system prompt).
	agentID      string
	derivedAgent bool
	hooks        *SessionHooks

	mu        sync.Mutex
	listeners map[int]func(Event)
	next      int
	unsub     func()
	closed    bool
}

// ID returns the session identifier.
func (s *Session) ID() string { return string(s.id) }

// Messages returns the session's stored messages (user, assistant, tool parts).
func (s *Session) Messages() ([]schema.SessionMessage, error) {
	return s.client.rt.store.Context(s.id)
}

// On subscribes to session events. The returned function unsubscribes.
func (s *Session) On(handler func(Event)) func() {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.next
	s.next++
	s.listeners[id] = handler
	return func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		delete(s.listeners, id)
	}
}

// Send admits a prompt and returns the events produced by the resulting turn.
// The sequence ends with an Idle event.
//
// Events are handed off to the caller with backpressure: core events are
// dispatched synchronously from the publisher (mirroring Event.listen), so a
// slow consumer slows the turn instead of losing deltas. The buffer only
// smooths bursts; closed releases a handler still waiting when the caller stops
// iterating.
func (s *Session) Send(ctx context.Context, msg MessageOptions) iter.Seq2[Event, error] {
	return func(yield func(Event, error) bool) {
		events := make(chan Event, 256)
		closed := make(chan struct{})
		unsubscribe := s.On(func(event Event) {
			select {
			case events <- event:
			case <-closed:
			}
		})
		defer func() {
			close(closed)
			unsubscribe()
		}()

		done := make(chan error, 1)
		go func() { done <- s.run(ctx, msg) }()

		for {
			select {
			case event := <-events:
				if !yield(event, nil) {
					return
				}
			case err := <-done:
				for {
					select {
					case event := <-events:
						if !yield(event, nil) {
							return
						}
					default:
						if err != nil {
							// Carry the error on both channels: the event for On
							// subscribers, the second value for range callers.
							if !yield(ErrorEvent{SessionID: s.ID(), Err: err}, err) {
								return
							}
						}
						yield(Idle{SessionID: s.ID()}, nil)
						return
					}
				}
			case <-ctx.Done():
				// Cancelling the caller's context only stops waiting; the active
				// turn keeps running until it finishes or Abort is called.
				return
			}
		}
	}
}

func (s *Session) run(ctx context.Context, msg MessageOptions) error {
	prompt := msg.Prompt
	if s.hooks != nil && s.hooks.OnUserPromptSubmitted != nil {
		out, err := s.hooks.OnUserPromptSubmitted(UserPromptSubmittedInput{SessionID: s.ID(), Prompt: prompt})
		if err != nil {
			return err
		}
		if out != nil && out.ModifiedPrompt != "" {
			prompt = out.ModifiedPrompt
		}
	}
	delivery := schema.SessionDeliveryQueue
	if msg.Mode == "steer" {
		delivery = schema.SessionDeliverySteer
	}
	if _, err := session.Admit(s.client.rt.db.DB, appEvents{events: s.client.rt.events}, session.AdmitInput{
		ID:        schema.NewMessageID(),
		SessionID: s.id,
		Prompt:    schema.Prompt{Text: prompt},
		Delivery:  delivery,
	}); err != nil {
		return err
	}
	if err := s.client.rt.coord.Run(ctx, s.id); err != nil {
		if s.hooks != nil && s.hooks.OnErrorOccurred != nil {
			_, _ = s.hooks.OnErrorOccurred(ErrorOccurredInput{SessionID: s.ID(), Error: err.Error()})
		}
		return err
	}
	return nil
}

// Abort interrupts the session's active turn.
func (s *Session) Abort() { s.client.rt.coord.Interrupt(s.id) }

// Close unsubscribes and releases the session's in-memory resources.
func (s *Session) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	unsubscribe := s.unsub
	s.mu.Unlock()
	if unsubscribe != nil {
		unsubscribe()
	}
	s.client.rt.dispatcher.remove(s.ID())
	s.client.rt.permission.removeHandler(s.ID())
	s.client.rt.question.removeHandler(s.ID())
	s.client.removeDerivedAgent(s.agentID, s.derivedAgent)
	s.client.untrackSession(s)
	if s.hooks != nil && s.hooks.OnSessionEnd != nil {
		_, _ = s.hooks.OnSessionEnd(SessionEndInput{SessionID: s.ID(), Reason: "closed"})
	}
	return nil
}

func (s *Session) dispatch(payload schema.EventPayload) {
	events := mapEvents(payload)
	if len(events) == 0 {
		return
	}
	s.mu.Lock()
	handlers := make([]func(Event), 0, len(s.listeners))
	for _, handler := range s.listeners {
		handlers = append(handlers, handler)
	}
	s.mu.Unlock()
	for _, event := range events {
		if eventSessionID(event) != s.ID() {
			continue
		}
		for _, handler := range handlers {
			handler(event)
		}
	}
}

// SessionIDOf returns the session id an event belongs to ("" when unknown).
func SessionIDOf(event Event) string { return eventSessionID(event) }

func eventSessionID(event Event) string {
	switch value := event.(type) {
	case TextDelta:
		return value.SessionID
	case TextEnded:
		return value.SessionID
	case ReasoningDelta:
		return value.SessionID
	case ReasoningEnded:
		return value.SessionID
	case ToolInputStarted:
		return value.SessionID
	case ToolProgress:
		return value.SessionID
	case ToolCalled:
		return value.SessionID
	case ReasoningStarted:
		return value.SessionID
	case Retried:
		return value.SessionID
	case SessionStatus:
		return value.SessionID
	case Usage:
		return value.SessionID
	case CompactionStarted:
		return value.SessionID
	case ShellStarted:
		return value.SessionID
	case ShellEnded:
		return value.SessionID
	case ToolSuccess:
		return value.SessionID
	case ToolFailed:
		return value.SessionID
	case StepStarted:
		return value.SessionID
	case StepEnded:
		return value.SessionID
	case StepFailed:
		return value.SessionID
	case CompactionEnded:
		return value.SessionID
	case ErrorEvent:
		return value.SessionID
	case Idle:
		return value.SessionID
	default:
		return ""
	}
}
