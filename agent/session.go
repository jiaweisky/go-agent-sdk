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
	// Tools are session-scoped user tools.
	Tools []Tool
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
	hooks  *SessionHooks

	mu        sync.Mutex
	listeners map[int]func(Event)
	next      int
	unsub     func()
	closed    bool
}

// ID returns the session identifier.
func (s *Session) ID() string { return string(s.id) }

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
func (s *Session) Send(ctx context.Context, msg MessageOptions) iter.Seq2[Event, error] {
	return func(yield func(Event, error) bool) {
		events := make(chan Event, 256)
		unsubscribe := s.On(func(event Event) {
			select {
			case events <- event:
			default:
			}
		})
		defer unsubscribe()

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
							yield(ErrorEvent{SessionID: s.ID(), Err: err}, nil)
						}
						yield(Idle{SessionID: s.ID()}, nil)
						return
					}
				}
			case <-ctx.Done():
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
	if s.hooks != nil && s.hooks.OnSessionEnd != nil {
		_, _ = s.hooks.OnSessionEnd(SessionEndInput{SessionID: s.ID(), Reason: "closed"})
	}
	return nil
}

func (s *Session) dispatch(payload schema.EventPayload) {
	event, ok := mapEvent(payload)
	if !ok || eventSessionID(event) != s.ID() {
		return
	}
	s.mu.Lock()
	handlers := make([]func(Event), 0, len(s.listeners))
	for _, handler := range s.listeners {
		handlers = append(handlers, handler)
	}
	s.mu.Unlock()
	for _, handler := range handlers {
		handler(event)
	}
}

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
	case ToolCalled:
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
