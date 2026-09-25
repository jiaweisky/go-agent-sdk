package agent

import (
	"context"

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

// questionHandler decorates a core.Question so the "question" tool is answered
// by a QuestionHandlerFunc instead of blocking for an external reply. A
// per-session handler overrides the client default.
type questionHandler struct {
	inner    core.Question
	handlers *sessionHandlers[QuestionHandlerFunc]
}

func newQuestionHandler(inner core.Question, def QuestionHandlerFunc) *questionHandler {
	if def == nil {
		def = RejectQuestions
	}
	return &questionHandler{inner: inner, handlers: newSessionHandlers(def)}
}

func (q *questionHandler) setHandler(sessionID string, handler QuestionHandlerFunc) {
	if handler == nil {
		return
	}
	q.handlers.set(sessionID, handler)
}

func (q *questionHandler) removeHandler(sessionID string) {
	q.handlers.remove(sessionID)
}

func (q *questionHandler) Ask(_ context.Context, input core.QuestionAskInput) ([]core.QuestionAnswer, error) {
	return q.handlers.resolve(input.SessionID)(QuestionRequest{
		SessionID: input.SessionID,
		Questions: input.Questions,
		Tool:      input.Tool,
	})
}

func (q *questionHandler) Reply(input core.QuestionReplyInput) error { return q.inner.Reply(input) }
func (q *questionHandler) Reject(requestID core.QuestionID) error    { return q.inner.Reject(requestID) }
func (q *questionHandler) List() []core.QuestionRequest              { return q.inner.List() }
func (q *questionHandler) Close() error                              { return q.inner.Close() }
