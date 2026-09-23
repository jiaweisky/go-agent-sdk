package agent

import (
	"context"
	"sync"

	"github.com/user/go-agent-sdk/core"
	"github.com/user/go-agent-sdk/schema"
)

// QuestionRequest describes a question the model wants the user to answer via
// the built-in "question" tool.
type QuestionRequest struct {
	SessionID string
	Questions []schema.QuestionInfo
	Tool      *schema.QuestionTool
}

// QuestionHandlerFunc answers a question request. Returning an error dismisses
// the question; the error is reported back to the model.
type QuestionHandlerFunc func(request QuestionRequest) ([]schema.QuestionAnswer, error)

// RejectQuestions dismisses every question. It is the default handler, so a
// session never blocks waiting for an answer nobody can provide.
var RejectQuestions QuestionHandlerFunc = func(QuestionRequest) ([]schema.QuestionAnswer, error) {
	return nil, &core.QuestionRejectedError{}
}

// handlerQuestion decorates a core.Question so the "question" tool is answered
// by a QuestionHandlerFunc instead of blocking for an external reply. A
// per-session handler overrides the client default.
type handlerQuestion struct {
	inner core.Question

	mu       sync.Mutex
	def      QuestionHandlerFunc
	handlers map[string]QuestionHandlerFunc
}

func newHandlerQuestion(inner core.Question, def QuestionHandlerFunc) *handlerQuestion {
	return &handlerQuestion{inner: inner, def: def, handlers: map[string]QuestionHandlerFunc{}}
}

func (q *handlerQuestion) setHandler(sessionID string, handler QuestionHandlerFunc) {
	if handler == nil {
		return
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.handlers == nil {
		q.handlers = map[string]QuestionHandlerFunc{}
	}
	q.handlers[sessionID] = handler
}

func (q *handlerQuestion) removeHandler(sessionID string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	delete(q.handlers, sessionID)
}

func (q *handlerQuestion) handlerFor(sessionID string) QuestionHandlerFunc {
	q.mu.Lock()
	defer q.mu.Unlock()
	if handler, ok := q.handlers[sessionID]; ok && handler != nil {
		return handler
	}
	if q.def != nil {
		return q.def
	}
	return RejectQuestions
}

func (q *handlerQuestion) Ask(_ context.Context, input core.QuestionAskInput) ([]core.QuestionAnswer, error) {
	return q.handlerFor(input.SessionID)(QuestionRequest{
		SessionID: input.SessionID,
		Questions: input.Questions,
		Tool:      input.Tool,
	})
}

func (q *handlerQuestion) Reply(input core.QuestionReplyInput) error { return q.inner.Reply(input) }
func (q *handlerQuestion) Reject(requestID core.QuestionID) error    { return q.inner.Reject(requestID) }
func (q *handlerQuestion) List() []core.QuestionRequest              { return q.inner.List() }
func (q *handlerQuestion) Close() error                              { return q.inner.Close() }
