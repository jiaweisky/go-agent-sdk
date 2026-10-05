package core

import (
	"context"
	"fmt"

	"github.com/jiaweisky/go-agent-sdk/schema"
)

type (
	QuestionID      = schema.QuestionID
	QuestionOption  = schema.QuestionOption
	QuestionInfo    = schema.QuestionInfo
	QuestionPrompt  = schema.QuestionPrompt
	QuestionTool    = schema.QuestionTool
	QuestionRequest = schema.QuestionRequest
	QuestionAnswer  = schema.QuestionAnswer
	QuestionReply   = schema.QuestionReply
)

func NewQuestionID() QuestionID {
	return schema.NewQuestionID()
}

type QuestionRejectedError struct{}

func (e *QuestionRejectedError) Error() string {
	return "The user dismissed this question"
}

type QuestionNotFoundError struct {
	RequestID QuestionID
}

func (e *QuestionNotFoundError) Error() string {
	return fmt.Sprintf("question request not found: %s", e.RequestID)
}

type QuestionAskInput struct {
	SessionID string
	Questions []QuestionInfo
	Tool      *QuestionTool
}

type QuestionReplyInput struct {
	RequestID QuestionID
	Answers   []QuestionAnswer
}

type QuestionRepliedData struct {
	SessionID string           `json:"sessionID"`
	RequestID QuestionID       `json:"requestID"`
	Answers   []QuestionAnswer `json:"answers"`
}

type QuestionRejectedData struct {
	SessionID string     `json:"sessionID"`
	RequestID QuestionID `json:"requestID"`
}

var QuestionEvent = schema.QuestionEvent

type QuestionEventPublisher interface {
	Publish(def schema.EventDefinition, data any, options *EventPublishOptions) (schema.EventPayload, error)
}

type QuestionConfig struct {
	Events QuestionEventPublisher
}

type Question interface {
	Ask(ctx context.Context, input QuestionAskInput) ([]QuestionAnswer, error)
	Reply(input QuestionReplyInput) error
	Reject(requestID QuestionID) error
	List() []QuestionRequest
	Close() error
}

type questionResult struct {
	answers []QuestionAnswer
	err     error
}

type questionPending = pendingEntry[QuestionID, QuestionRequest, questionResult, struct{}]

type questionService struct {
	cfg     QuestionConfig
	pending *pendingRegistry[QuestionID, QuestionRequest, questionResult, struct{}]
}

func NewQuestion(cfg QuestionConfig) Question {
	return &questionService{
		cfg:     cfg,
		pending: newPendingRegistry[QuestionID, QuestionRequest, questionResult, struct{}](),
	}
}

// NewQuestionContext creates a Question service bound to ctx. A background
// goroutine calls Close when ctx is cancelled, rejecting every pending ask with
// QuestionRejectedError. Callers must cancel ctx (e.g. defer cancel()) or the
// goroutine leaks.
func NewQuestionContext(ctx context.Context, cfg QuestionConfig) Question {
	q := NewQuestion(cfg)
	go func() {
		<-ctx.Done()
		_ = q.Close()
	}()
	return q
}

func (s *questionService) publish(def schema.EventDefinition, data any) error {
	if s.cfg.Events == nil {
		return nil
	}
	_, err := s.cfg.Events.Publish(def, data, nil)
	return err
}

func (s *questionService) Ask(ctx context.Context, input QuestionAskInput) ([]QuestionAnswer, error) {
	id := NewQuestionID()
	request := QuestionRequest{
		ID:        id,
		SessionID: input.SessionID,
		Questions: input.Questions,
		Tool:      input.Tool,
	}
	item, closed, _ := s.pending.add(id, request, struct{}{})
	if closed {
		return nil, &QuestionRejectedError{}
	}

	if s.cfg.Events != nil {
		if err := s.publish(schema.QuestionEventAsked, request); err != nil {
			s.pending.finish(id)
			return nil, err
		}
	}

	var result questionResult
	select {
	case result = <-item.done:
	case <-ctx.Done():
		s.pending.finish(id)
		return nil, ctx.Err()
	}

	s.pending.finish(id)
	return result.answers, result.err
}

func (s *questionService) Reply(input QuestionReplyInput) error {
	existing, err := s.reserve(input.RequestID)
	if err != nil {
		return err
	}

	if s.cfg.Events != nil {
		eventAnswers := make([]QuestionAnswer, len(input.Answers))
		for i, answer := range input.Answers {
			eventAnswers[i] = append(QuestionAnswer(nil), answer...)
		}
		if err := s.publish(schema.QuestionEventReplied, QuestionRepliedData{
			SessionID: existing.request.SessionID,
			RequestID: existing.request.ID,
			Answers:   eventAnswers,
		}); err != nil {
			s.release(existing)
			return err
		}
	}

	existing.done <- questionResult{answers: input.Answers}
	s.pending.finish(input.RequestID)
	return nil
}

func (s *questionService) Reject(requestID QuestionID) error {
	existing, err := s.reserve(requestID)
	if err != nil {
		return err
	}

	if s.cfg.Events != nil {
		if err := s.publish(schema.QuestionEventRejected, QuestionRejectedData{
			SessionID: existing.request.SessionID,
			RequestID: existing.request.ID,
		}); err != nil {
			s.release(existing)
			return err
		}
	}

	existing.done <- questionResult{err: &QuestionRejectedError{}}
	s.pending.finish(requestID)
	return nil
}

// reserve atomically claims a pending request so a concurrent reply or reject
// cannot act on it twice.
func (s *questionService) reserve(requestID QuestionID) (*questionPending, error) {
	existing, ok := s.pending.reserve(requestID)
	if !ok {
		return nil, &QuestionNotFoundError{RequestID: requestID}
	}
	return existing, nil
}

func (s *questionService) release(existing *questionPending) {
	if s.pending.release(existing) {
		existing.done <- questionResult{err: &QuestionRejectedError{}}
	}
}

func (s *questionService) List() []QuestionRequest {
	return s.pending.listWhere(func(*questionPending) bool { return true })
}

// Close rejects every pending ask with QuestionRejectedError without publishing
// an event. It is idempotent.
func (s *questionService) Close() error {
	for _, item := range s.pending.close() {
		item.done <- questionResult{err: &QuestionRejectedError{}}
	}
	return nil
}
