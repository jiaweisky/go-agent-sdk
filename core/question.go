package core

import (
	"context"
	"fmt"
	"sync"

	"github.com/user/go-agent-sdk/schema"
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

func NewQuestionID(id ...string) QuestionID {
	return schema.NewQuestionID(id...)
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
	Ask(input QuestionAskInput) ([]QuestionAnswer, error)
	Reply(input QuestionReplyInput) error
	Reject(requestID QuestionID) error
	List() []QuestionRequest
	Close() error
}

type questionResult struct {
	answers []QuestionAnswer
	err     error
}

type questionPending struct {
	request QuestionRequest
	done    chan questionResult
	settled bool
}

type questionService struct {
	cfg     QuestionConfig
	mu      sync.Mutex
	pending map[QuestionID]*questionPending
	closed  bool
}

func NewQuestion(cfg QuestionConfig) Question {
	return &questionService{
		cfg:     cfg,
		pending: make(map[QuestionID]*questionPending),
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

func (s *questionService) Ask(input QuestionAskInput) ([]QuestionAnswer, error) {
	id := NewQuestionID()
	request := QuestionRequest{
		ID:        id,
		SessionID: input.SessionID,
		Questions: input.Questions,
		Tool:      input.Tool,
	}
	item := &questionPending{request: request, done: make(chan questionResult, 1)}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, &QuestionRejectedError{}
	}
	s.pending[id] = item
	s.mu.Unlock()

	if s.cfg.Events != nil {
		if err := s.publish(schema.QuestionEventAsked, request); err != nil {
			s.mu.Lock()
			delete(s.pending, id)
			s.mu.Unlock()
			return nil, err
		}
	}

	result := <-item.done

	s.mu.Lock()
	delete(s.pending, id)
	s.mu.Unlock()

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
	s.finish(input.RequestID)
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
	s.finish(requestID)
	return nil
}

// reserve atomically claims a pending request so a concurrent reply or reject
// observes the same outcome as TS's single-threaded delete-after-settle.
func (s *questionService) reserve(requestID QuestionID) (*questionPending, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.pending[requestID]
	if !ok || existing.settled {
		return nil, &QuestionNotFoundError{RequestID: requestID}
	}
	existing.settled = true
	return existing, nil
}

func (s *questionService) release(existing *questionPending) {
	s.mu.Lock()
	if s.closed {
		delete(s.pending, existing.request.ID)
		s.mu.Unlock()
		existing.done <- questionResult{err: &QuestionRejectedError{}}
		return
	}
	existing.settled = false
	s.mu.Unlock()
}

func (s *questionService) finish(requestID QuestionID) {
	s.mu.Lock()
	delete(s.pending, requestID)
	s.mu.Unlock()
}

func (s *questionService) List() []QuestionRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]QuestionRequest, 0, len(s.pending))
	for _, item := range s.pending {
		result = append(result, item.request)
	}
	return result
}

// Close rejects every pending ask with QuestionRejectedError without publishing
// an event, mirroring the TS layer finalizer. It is idempotent.
func (s *questionService) Close() error {
	s.mu.Lock()
	s.closed = true
	pending := make([]*questionPending, 0, len(s.pending))
	for id, item := range s.pending {
		if item.settled {
			continue
		}
		item.settled = true
		pending = append(pending, item)
		delete(s.pending, id)
	}
	s.mu.Unlock()

	for _, item := range pending {
		item.done <- questionResult{err: &QuestionRejectedError{}}
	}
	return nil
}
