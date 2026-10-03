package agent

import (
	"context"
	"sync/atomic"

	"github.com/user/go-agent-sdk/core"
	"github.com/user/go-agent-sdk/schema"
)

// QuestionRequest describes a question the model wants the user to answer via
// the built-in "question" tool.
type QuestionRequest struct {
	// ID identifies the request; pass it to Client.Question().Reply/Reject to
	// resolve a held request.
	ID        string
	SessionID string
	Questions []schema.QuestionInfo
	Tool      *schema.QuestionTool
}

// QuestionClient lists and resolves pending question requests. Obtain one with
// Client.Question; requests are pending only while their turn is held (see
// Client.HoldInteractions).
type QuestionClient interface {
	List() []QuestionRequest
	Get(id string) *QuestionRequest
	Reply(id string, answers []schema.QuestionAnswer) error
	Reject(id string) error
}

type questionClient struct{ inner *questionHandler }

func (q questionClient) List() []QuestionRequest { return questionRequestsFrom(q.inner.List()) }

func (q questionClient) Get(id string) *QuestionRequest {
	request := q.inner.get(core.QuestionID(id))
	if request == nil {
		return nil
	}
	converted := questionRequestsFrom([]core.QuestionRequest{*request})[0]
	return &converted
}

func (q questionClient) Reply(id string, answers []schema.QuestionAnswer) error {
	return q.inner.Reply(core.QuestionReplyInput{RequestID: core.QuestionID(id), Answers: answers})
}

func (q questionClient) Reject(id string) error { return q.inner.Reject(core.QuestionID(id)) }

// questionRequestsFrom flattens core requests for the high-level surface.
func questionRequestsFrom(requests []core.QuestionRequest) []QuestionRequest {
	result := make([]QuestionRequest, len(requests))
	for i, request := range requests {
		result[i] = QuestionRequest{
			ID: string(request.ID), SessionID: request.SessionID,
			Questions: request.Questions, Tool: request.Tool,
		}
	}
	return result
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
// by a QuestionHandlerFunc instead of blocking for an external reply. In hold
// mode it instead defers to the core service, which blocks until
// Client.Question().Reply/Reject. A per-session handler overrides the client
// default.
type questionHandler struct {
	inner    core.Question
	handlers *sessionHandlers[QuestionHandlerFunc]
	// hold defers asks to an external reply (core.Ask blocks) instead of
	// calling a handler.
	hold atomic.Bool
}

func newQuestionHandler(inner core.Question, def QuestionHandlerFunc) *questionHandler {
	if def == nil {
		def = RejectQuestions
	}
	return &questionHandler{inner: inner, handlers: newSessionHandlers(def)}
}

// setHold toggles out-of-band mode for subsequent asks.
func (q *questionHandler) setHold(hold bool) { q.hold.Store(hold) }

func (q *questionHandler) setHandler(sessionID string, handler QuestionHandlerFunc) {
	if handler == nil {
		return
	}
	q.handlers.set(sessionID, handler)
}

func (q *questionHandler) removeHandler(sessionID string) {
	q.handlers.remove(sessionID)
}

// handlerFor returns the session's effective handler: its override when set,
// otherwise the client default.
func (q *questionHandler) handlerFor(sessionID string) QuestionHandlerFunc {
	return q.handlers.resolve(sessionID)
}

func (q *questionHandler) Ask(ctx context.Context, input core.QuestionAskInput) ([]core.QuestionAnswer, error) {
	if q.hold.Load() {
		return q.inner.Ask(ctx, input)
	}
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

// get returns a pending request by id, or nil.
func (q *questionHandler) get(id core.QuestionID) *core.QuestionRequest {
	for _, request := range q.inner.List() {
		if request.ID == id {
			copy := request
			return &copy
		}
	}
	return nil
}
