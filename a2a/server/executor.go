package server

import (
	"context"
	"fmt"
	"iter"
	"sync"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/a2aproject/a2a-go/v2/a2asrv/taskstore"

	a2acommon "github.com/jiaweisky/go-agent-sdk/a2a"
	"github.com/jiaweisky/go-agent-sdk/schema"
)

// pendingState is a turn parked at input-required, waiting for the client to
// send a reply message.
type pendingState struct {
	session   Session
	requestID string
	question  bool
}

// executor implements a2asrv.AgentExecutor: it drives the SDK session for an
// exposed agent and maps its events onto A2A events.
type executor struct {
	factory    Factory
	agentID    string
	store      taskstore.Store
	workspaces WorkspaceResolver
	artifacts  ArtifactExternalizer

	mu       sync.Mutex
	sessions map[string]Session
	pending  map[a2a.TaskID]*pendingState
}

var _ a2asrv.AgentExecutor = (*executor)(nil)

// executorConfig collects the executor's dependencies.
type executorConfig struct {
	factory    Factory
	agentID    string
	store      taskstore.Store
	workspaces WorkspaceResolver
	artifacts  ArtifactExternalizer
}

func newExecutor(config executorConfig) *executor {
	return &executor{
		factory:    config.factory,
		agentID:    config.agentID,
		store:      config.store,
		workspaces: config.workspaces,
		artifacts:  config.artifacts,
		sessions:   map[string]Session{},
		pending:    map[a2a.TaskID]*pendingState{},
	}
}

// Execute implements a2asrv.AgentExecutor.
func (e *executor) Execute(ctx context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		sess, err := e.resolveSession(ctx, execCtx, yield)
		if err != nil {
			yield(nil, err)
			return
		}

		// A message for a task that is still working steers the active turn
		// rather than starting another one (docs/DESIGN-A2A.md §5.3). It runs on
		// the already-active turn, so it registers no end hook of its own.
		if steerable(execCtx.StoredTask) {
			if err := sess.Steer(messageText(execCtx.Message)); err != nil {
				yield(nil, err)
				return
			}
			yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateWorking, nil), nil)
			return
		}

		// Release the session once this turn fully settles, including a cancel
		// that parks the turn with no terminal event of its own.
		sess.OnTurnEnd(string(execCtx.TaskID), func() { e.releaseIfIdle(sess) })

		if !yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateWorking, nil), nil) {
			return
		}

		stream := e.streamFor(ctx, execCtx, sess)
		e.mapStream(ctx, execCtx, sess, stream, yield)
	}
}

// resolveWorkspace resolves the execution workspace for an inbound call. With
// no resolver configured the factory's defaults apply.
func (e *executor) resolveWorkspace(ctx context.Context, execCtx *a2asrv.ExecutorContext) (Workspace, error) {
	if e.workspaces == nil {
		return Workspace{}, nil
	}
	user := ""
	if execCtx.User != nil {
		user = execCtx.User.Name
	}
	return e.workspaces.Resolve(ctx, WorkspaceRequest{User: user, AgentID: e.agentID, ContextID: execCtx.ContextID})
}

// resolveSession creates a new session for a new task or resolves the mirrored
// session for a resumed task, yielding the submitted task when it creates one.
func (e *executor) resolveSession(ctx context.Context, execCtx *a2asrv.ExecutorContext, yield func(a2a.Event, error) bool) (Session, error) {
	workspace, err := e.resolveWorkspace(ctx, execCtx)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace: %w", err)
	}
	if execCtx.StoredTask == nil {
		sess, err := e.sessionForContext(ctx, execCtx.ContextID, workspace)
		if err != nil {
			return nil, err
		}
		if sess == nil {
			sess, err = e.factory.CreateSession(ctx, SessionRequest{AgentID: e.agentID, Workspace: workspace})
			if err != nil {
				return nil, fmt.Errorf("create session: %w", err)
			}
			e.remember(sess)
		}
		task := a2a.NewSubmittedTask(execCtx, execCtx.Message)
		if task.Metadata == nil {
			task.Metadata = map[string]any{}
		}
		task.Metadata[metaKeySession] = sess.ID()
		task.Metadata[metaKeyAgent] = e.agentID
		task.Metadata[a2acommon.ToA2AMetaKey(a2acommon.MetaContextID)] = execCtx.ContextID
		if !yield(task, nil) {
			return nil, errYieldStopped
		}
		return sess, nil
	}

	sessionID, _ := execCtx.StoredTask.Metadata[metaKeySession].(string)
	if sessionID == "" {
		return nil, fmt.Errorf("stored task %s has no %s metadata", execCtx.TaskID, metaKeySession)
	}
	return e.sessionFor(ctx, sessionID, workspace)
}

// sessionForContext reuses the local session previously bound to an A2A context
// for this agent.
func (e *executor) sessionForContext(ctx context.Context, contextID string, workspace Workspace) (Session, error) {
	lookup, ok := e.store.(interface {
		SessionForContext(agentID, contextID string) (string, bool)
	})
	if !ok {
		return nil, nil
	}
	sessionID, found := lookup.SessionForContext(e.agentID, contextID)
	if !found {
		return nil, nil
	}
	return e.sessionFor(ctx, sessionID, workspace)
}

// sessionFor returns an attached session or resumes it.
func (e *executor) sessionFor(ctx context.Context, sessionID string, workspace Workspace) (Session, error) {
	e.mu.Lock()
	sess := e.sessions[sessionID]
	e.mu.Unlock()
	if sess != nil {
		return sess, nil
	}
	sess, err := e.factory.ResumeSession(ctx, sessionID, workspace)
	if err != nil {
		return nil, fmt.Errorf("resume session %s: %w", sessionID, err)
	}
	e.remember(sess)
	return sess, nil
}

func (e *executor) remember(sess Session) {
	e.mu.Lock()
	e.sessions[sess.ID()] = sess
	e.mu.Unlock()
}

// streamFor continues a parked turn after delivering the reply, or starts a
// fresh turn from the incoming message. The turn is keyed by the A2A task id.
// When a restart dropped the parked turn (no in-process pending) but the task
// is still input-required, it rebuilds the turn from the durable history.
func (e *executor) streamFor(ctx context.Context, execCtx *a2asrv.ExecutorContext, sess Session) iter.Seq2[Event, error] {
	turnID := string(execCtx.TaskID)
	e.mu.Lock()
	pending := e.pending[execCtx.TaskID]
	e.mu.Unlock()
	if pending != nil {
		if err := e.deliverReply(pending, execCtx.Message); err == nil {
			e.clearPending(execCtx.TaskID)
			return sess.Resume(ctx, turnID)
		}
	}
	prompt := messageText(execCtx.Message)
	if awaitingInput(execCtx.StoredTask) {
		prompt = rebuildPrompt(execCtx.StoredTask, execCtx.Message)
	}
	return sess.Send(ctx, turnID, SendRequest{Prompt: prompt, Files: messageFiles(execCtx.Message)})
}

// awaitingInput reports whether a stored task is parked at an input-required
// state, which marks a turn that must be rebuilt after a restart.
func awaitingInput(task *a2a.Task) bool {
	if task == nil {
		return false
	}
	return task.Status.State == a2a.TaskStateInputRequired || task.Status.State == a2a.TaskStateAuthRequired
}

// steerable reports whether a stored task is still working, so a new message
// should steer its active turn rather than start another.
func steerable(task *a2a.Task) bool {
	return task != nil && task.Status.State == a2a.TaskStateWorking
}

func (e *executor) deliverReply(pending *pendingState, message *a2a.Message) error {
	if pending.question {
		answers := questionAnswersFromMessage(message)
		if len(answers) == 0 {
			return pending.session.ReplyQuestion(pending.requestID, []Answer{{Labels: []string{messageText(message)}}})
		}
		return pending.session.ReplyQuestion(pending.requestID, answers)
	}
	return pending.session.ReplyPermission(pending.requestID, permissionReplyFromMessage(message))
}

// mapStream maps a session event stream onto A2A events.
func (e *executor) mapStream(
	ctx context.Context,
	execCtx *a2asrv.ExecutorContext,
	sess Session,
	stream iter.Seq2[Event, error],
	yield func(a2a.Event, error) bool,
) {
	var artifactID a2a.ArtifactID
	var usage any

	appendPart := func(part *a2a.Part) bool {
		var event *a2a.TaskArtifactUpdateEvent
		if artifactID == "" {
			event = a2a.NewArtifactEvent(execCtx, part)
			artifactID = event.Artifact.ID
		} else {
			event = a2a.NewArtifactUpdateEvent(execCtx, artifactID, part)
		}
		if e.artifacts != nil {
			replacement, err := e.artifacts(ctx, string(execCtx.TaskID), event.Artifact)
			if err != nil {
				e.fail(execCtx, err, yield)
				return false
			}
			if artifact, ok := replacement.(*a2a.Artifact); ok && artifact != nil {
				event.Artifact = artifact
			}
		}
		return yield(event, nil)
	}

	for event, err := range stream {
		if err != nil {
			e.fail(execCtx, err, yield)
			return
		}
		switch event.Kind {
		case EventText:
			if event.Text == "" {
				continue
			}
			if !appendPart(a2a.NewTextPart(event.Text)) {
				return
			}
		case EventReasoning:
			if event.Text == "" {
				continue
			}
			part := a2a.NewTextPart(event.Text)
			part.SetMeta(a2acommon.ToA2AMetaKey(a2acommon.MetaThought), true)
			if !appendPart(part) {
				return
			}
		case EventToolCalled:
			if !appendPart(toolCallPart(event)) {
				return
			}
		case EventToolResult:
			if !appendPart(toolResultPart(event)) {
				return
			}
			for _, diff := range fileDiffParts(event.Structured) {
				if !appendPart(diff) {
					return
				}
			}
		case EventToolProgress, EventStepStarted:
			if !yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateWorking, nil), nil) {
				return
			}
		case EventUsage:
			usage = event.Usage
		case EventAwaitInput:
			e.awaitInput(execCtx, sess, artifactID, event.RequestID, event.IsQuestion, event.Questions, yield)
			return
		case EventError:
			e.fail(execCtx, event.Err, yield)
			return
		case EventIdle:
			e.complete(execCtx, artifactID, usage, yield)
			return
		}
	}
	// The stream ended without EventIdle; treat it as a completed turn.
	e.complete(execCtx, artifactID, usage, yield)
}

// awaitInput parks the turn: it seals the open artifact, records the pending
// request, and emits the input-required status so the client can reply on the
// next message.
func (e *executor) awaitInput(
	execCtx *a2asrv.ExecutorContext,
	sess Session,
	artifactID a2a.ArtifactID,
	requestID string,
	question bool,
	questions []schema.QuestionInfo,
	yield func(a2a.Event, error) bool,
) {
	if artifactID != "" && !yield(sealArtifact(execCtx, artifactID), nil) {
		return
	}
	e.setPending(execCtx.TaskID, sess, requestID, question)
	status := a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateInputRequired, awaitInputMessage(execCtx, requestID, questions))
	status.Metadata = map[string]any{
		metaKeySession:        sess.ID(),
		metaKeyPendingRequest: requestID,
	}
	yield(status, nil)
}

// complete seals the turn's artifact (if any) and emits the completed status.
// Provider usage is attached as artifact metadata (or on the status when the
// turn produced no artifact). The session is released by the OnTurnEnd hook.
func (e *executor) complete(
	execCtx *a2asrv.ExecutorContext,
	artifactID a2a.ArtifactID,
	usage any,
	yield func(a2a.Event, error) bool,
) {
	if artifactID != "" {
		seal := sealArtifact(execCtx, artifactID)
		if usage != nil {
			seal.Artifact.Metadata = usageMetadata(usage)
		}
		if !yield(seal, nil) {
			return
		}
	}
	e.clearPending(execCtx.TaskID)
	completed := a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateCompleted, nil)
	if artifactID == "" && usage != nil {
		completed.Metadata = usageMetadata(usage)
	}
	yield(completed, nil)
}

// fail emits the failed status. The session is released by the OnTurnEnd hook.
func (e *executor) fail(execCtx *a2asrv.ExecutorContext, err error, yield func(a2a.Event, error) bool) {
	e.clearPending(execCtx.TaskID)
	yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateFailed, errorMessage(execCtx, err)), nil)
}

// releaseIfIdle closes a session and drops it from the cache when no turn is
// still parked on it, so a long-running server does not retain a handle per
// context it has ever seen. A session with a parked turn is kept for Resume.
func (e *executor) releaseIfIdle(sess Session) {
	e.mu.Lock()
	for _, pending := range e.pending {
		if pending.session == sess {
			e.mu.Unlock()
			return
		}
	}
	if e.sessions[sess.ID()] == sess {
		delete(e.sessions, sess.ID())
	}
	e.mu.Unlock()
	_ = sess.Close()
}

// Cancel implements a2asrv.AgentExecutor. It aborts the session turn and
// reports the canceled state.
func (e *executor) Cancel(ctx context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		if sessionID, _ := execCtx.StoredTask.Metadata[metaKeySession].(string); sessionID != "" {
			e.mu.Lock()
			sess := e.sessions[sessionID]
			e.mu.Unlock()
			if sess != nil {
				sess.Abort()
			}
		}
		e.clearPending(execCtx.TaskID)
		yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateCanceled, nil), nil)
	}
}

func (e *executor) setPending(taskID a2a.TaskID, sess Session, requestID string, question bool) {
	e.mu.Lock()
	e.pending[taskID] = &pendingState{session: sess, requestID: requestID, question: question}
	e.mu.Unlock()
}

func (e *executor) clearPending(taskID a2a.TaskID) {
	e.mu.Lock()
	delete(e.pending, taskID)
	e.mu.Unlock()
}

// errYieldStopped signals that the consumer stopped iterating.
var errYieldStopped = fmt.Errorf("a2a: yield stopped")
