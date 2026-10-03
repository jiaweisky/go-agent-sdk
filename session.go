package agent

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"sync"
	"time"

	"github.com/user/go-agent-sdk/schema"
)

// ErrSessionBusy is returned by Session.Compact while the session is running a
// turn: compaction only runs on an idle session.
var ErrSessionBusy = errors.New("session is busy")

// SessionConfig configures a session.
type SessionConfig struct {
	// Agent is the agent name; defaults to "build". ResumeSession keeps the
	// agent the session was created with and ignores this field.
	Agent string
	// Model overrides the client default model. ResumeSession keeps the model
	// the session was created with and ignores this field.
	Model string
	Title string
	// SystemPrompt customizes the system prompt for this session. Pass it again
	// to ResumeSession to reapply the customization; it is not persisted.
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
	// Files are attached to the message and sent to the model as media. Each
	// URI must be a base64 payload or a data URL, since the SDK does not read
	// from a path or URL.
	Files []schema.PromptFileAttachment
}

// Session is a durable conversation bound to an agent.
type Session struct {
	client *Client
	id     schema.SessionID
	// agentID is the effective agent (a derived agent when the session
	// customizes the system prompt).
	agentID string
	hooks   *SessionHooks

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

// Compact summarizes the session's history on demand, replacing the summarized
// span with a summary message. It reports whether anything was compacted, and
// fails with ErrSessionBusy while a turn is active.
func (s *Session) Compact(ctx context.Context) (bool, error) {
	return s.client.rt.compact(ctx, s.id)
}

// RevertStage reverts the file changes made after messageID: each file a later
// assistant message changed is restored to its pre-edit snapshot. The returned
// state describes the reverted files. Pair it with RevertClear (bring the
// changes back) or RevertCommit (finalize the revert).
func (s *Session) RevertStage(messageID string) (*schema.RevertState, error) {
	if messageID == "" {
		return nil, fmt.Errorf("revert needs a message id")
	}
	return s.client.rt.revertStage(s.id, schema.MessageID(messageID))
}

// Revert returns the session's staged revert, or nil when none is staged. Read
// it (rather than tracking the boundary separately) so it survives a resume.
func (s *Session) Revert() *schema.RevertState {
	stored, err := s.client.rt.store.Get(s.id)
	if err != nil || stored == nil {
		return nil
	}
	return stored.Revert
}

// RevertClear restores the files a staged revert changed and drops the revert.
func (s *Session) RevertClear() error {
	return s.client.rt.revertClear(s.id)
}

// RevertCommit finalizes a staged revert.
func (s *Session) RevertCommit() error {
	return s.client.rt.revertCommit(s.id)
}

// SetModel switches the session's model. The change takes effect on the next
// turn: it records a model-switched event that updates the stored session, so
// the session is kept rather than replaced. Setting the current model is a
// no-op. An empty model ref provider keeps the one the session routes through.
func (s *Session) SetModel(model schema.ModelRef) error {
	stored, err := s.client.rt.store.Get(s.id)
	if err != nil {
		return err
	}
	if stored == nil {
		return fmt.Errorf("session not found: %s", s.id)
	}
	if model.ProviderID == "" && stored.Model != nil {
		model.ProviderID = stored.Model.ProviderID
	}
	if sameModelRef(stored.Model, model) {
		return nil
	}
	_, err = s.client.rt.events.Publish(schema.SessionEventModelSwitched, map[string]any{
		"sessionID": string(s.id),
		"messageID": schema.NewMessageID(),
		"timestamp": time.Now().UnixMilli(),
		"model":     model,
	}, nil)
	return err
}

// SetAgent switches the session's agent. The change takes effect on the next
// turn: it records an agent-switched event that updates the stored session, so
// the session is kept rather than replaced. Unlike SetModel it always records
// the switch, matching the upstream operation.
func (s *Session) SetAgent(agentID string) error {
	stored, err := s.client.rt.store.Get(s.id)
	if err != nil {
		return err
	}
	if stored == nil {
		return fmt.Errorf("session not found: %s", s.id)
	}
	_, err = s.client.rt.events.Publish(schema.SessionEventAgentSwitched, map[string]any{
		"sessionID": string(s.id),
		"messageID": schema.NewMessageID(),
		"timestamp": time.Now().UnixMilli(),
		"agent":     agentID,
	}, nil)
	return err
}

// sameModelRef reports whether two references select the same model and variant,
// treating a missing variant as the model's default.
func sameModelRef(current *schema.ModelRef, next schema.ModelRef) bool {
	if current == nil {
		return false
	}
	return current.ID == next.ID &&
		current.ProviderID == next.ProviderID &&
		variantOrDefault(current.Variant) == variantOrDefault(next.Variant)
}

func variantOrDefault(variant *schema.ModelVariantID) string {
	if variant == nil || *variant == "" {
		return "default"
	}
	return string(*variant)
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
// dispatched synchronously from the publisher, so a slow consumer slows the
// turn instead of losing deltas. The buffer only smooths bursts; closed
// releases a handler still waiting when the caller stops iterating.
func (s *Session) Send(ctx context.Context, msg MessageOptions) iter.Seq2[Event, error] {
	return func(yield func(Event, error) bool) {
		events := newTurnEvents()
		unsubscribe := s.On(events.offer)
		defer func() {
			events.close()
			unsubscribe()
		}()

		done := make(chan error, 1)
		go func() { done <- s.run(ctx, msg) }()

		for {
			select {
			case event := <-events.events:
				if !yield(event, nil) {
					return
				}
			case err := <-done:
				drainTurn(events, s.ID(), yield, err)
				return
			case <-ctx.Done():
				// Cancelling the caller's context only stops waiting; the active
				// turn keeps running until it finishes or Abort is called.
				return
			}
		}
	}
}

// Steer admits a prompt into the session's input queue with steer delivery and
// returns immediately: it does not run the coordinator or subscribe to events.
// The active turn picks the prompt up at its next step boundary; if no turn is
// running, it stays queued until the next Send. Use it to feed input into a
// turn already in flight without starting a new turn or duplicating its event
// stream.
func (s *Session) Steer(prompt string) error {
	return s.client.rt.admit(s.id, schema.Prompt{Text: prompt}, schema.SessionDeliverySteer)
}

// turnEvents hands a turn's events to the caller with backpressure: the offer
// blocks while the buffer is full, and close releases it once the caller stops
// iterating.
type turnEvents struct {
	events chan Event
	closed chan struct{}
}

func newTurnEvents() *turnEvents {
	return &turnEvents{events: make(chan Event, 256), closed: make(chan struct{})}
}

func (t *turnEvents) offer(event Event) {
	select {
	case t.events <- event:
	case <-t.closed:
	}
}

func (t *turnEvents) close() { close(t.closed) }

// drainTurn yields the events a finished turn left buffered, then the error, if
// any, and finally Idle. The error is carried on both channels: as an event for
// On subscribers, and as the second value for range callers.
func drainTurn(events *turnEvents, sessionID string, yield func(Event, error) bool, err error) {
	for {
		select {
		case event := <-events.events:
			if !yield(event, nil) {
				return
			}
		default:
			if err != nil {
				if !yield(ErrorEvent{SessionID: sessionID, Err: err}, err) {
					return
				}
			}
			yield(Idle{SessionID: sessionID}, nil)
			return
		}
	}
}

func (s *Session) run(ctx context.Context, msg MessageOptions) error {
	prompt, err := s.applyPromptHook(msg.Prompt)
	if err != nil {
		return err
	}
	if err := s.client.rt.admit(s.id, schema.Prompt{Text: prompt, Files: msg.Files}, deliveryForMode(msg.Mode)); err != nil {
		return err
	}
	if err := s.client.rt.coord.Run(ctx, s.id); err != nil {
		s.notifyError(err)
		return err
	}
	return nil
}

// applyPromptHook runs OnUserPromptSubmitted and returns the possibly rewritten
// prompt.
func (s *Session) applyPromptHook(prompt string) (string, error) {
	if s.hooks == nil || s.hooks.OnUserPromptSubmitted == nil {
		return prompt, nil
	}
	out, err := s.hooks.OnUserPromptSubmitted(UserPromptSubmittedInput{SessionID: s.ID(), Prompt: prompt})
	if err != nil {
		return "", err
	}
	if out != nil && out.ModifiedPrompt != "" {
		return out.ModifiedPrompt, nil
	}
	return prompt, nil
}

// deliveryForMode maps MessageOptions.Mode to a delivery kind; "steer" steers
// the active turn, anything else queues.
func deliveryForMode(mode string) schema.SessionDelivery {
	if mode == "steer" {
		return schema.SessionDeliverySteer
	}
	return schema.SessionDeliveryQueue
}

// notifyError reports a failed turn to OnErrorOccurred.
func (s *Session) notifyError(err error) {
	if s.hooks != nil && s.hooks.OnErrorOccurred != nil {
		_, _ = s.hooks.OnErrorOccurred(ErrorOccurredInput{SessionID: s.ID(), Error: err.Error()})
	}
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
	s.client.rt.detach(s.ID())
	s.client.untrackSession(s)
	if s.hooks != nil && s.hooks.OnSessionEnd != nil {
		_, _ = s.hooks.OnSessionEnd(SessionEndInput{SessionID: s.ID(), Reason: "closed"})
	}
	return nil
}

func (s *Session) dispatch(payload schema.EventPayload) {
	mapped, count := mapEvents(payload)
	if count == 0 {
		return
	}
	s.mu.Lock()
	handlers := make([]func(Event), 0, len(s.listeners))
	for _, handler := range s.listeners {
		handlers = append(handlers, handler)
	}
	s.mu.Unlock()
	for i := 0; i < count; i++ {
		event := mapped[i]
		if event.sessionID() != s.ID() {
			continue
		}
		for _, handler := range handlers {
			runHandler(handler, event, s.client.rt.onPanic, s.ID())
		}
	}
}

// SessionIDOf returns the session id an event belongs to.
func SessionIDOf(event Event) string { return event.sessionID() }
