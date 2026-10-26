package server

import (
	"context"
	"fmt"
	"iter"
	"strings"
	"sync"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/a2aproject/a2a-go/v2/a2asrv/taskstore"

	a2acommon "github.com/jiaweisky/go-agent-sdk/pkg/a2a"
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
		// rather than starting another one. It runs on
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
func (e *executor) resolveSession(
	ctx context.Context,
	execCtx *a2asrv.ExecutorContext,
	yield func(a2a.Event, error) bool,
) (Session, error) {
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
func (e *executor) streamFor(
	ctx context.Context,
	execCtx *a2asrv.ExecutorContext,
	sess Session,
) iter.Seq2[Event, error] {
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

// mapStream maps a session event stream onto A2A events. Text and reasoning
// deltas are batched (see deltaBuffer): consecutive deltas become one artifact
// part instead of one part per token, which otherwise makes a2a-go persist the
// whole task per token and slows turns down quadratically.
func (e *executor) mapStream(
	ctx context.Context,
	execCtx *a2asrv.ExecutorContext,
	sess Session,
	stream iter.Seq2[Event, error],
	yield func(a2a.Event, error) bool,
) {
	m := &streamMapper{
		exec:    e,
		ctx:     ctx,
		execCtx: execCtx,
		sess:    sess,
		yield:   yield,
	}
	m.deltas = &deltaBuffer{emit: m.appendText}
	m.run(stream)
}

// streamMapper owns the per-turn artifact and usage state while one session
// event stream is mapped to A2A events.
type streamMapper struct {
	exec       *executor
	ctx        context.Context
	execCtx    *a2asrv.ExecutorContext
	sess       Session
	yield      func(a2a.Event, error) bool
	artifactID a2a.ArtifactID
	usage      any
	deltas     *deltaBuffer
}

// run consumes the session stream and stops on the first terminal event or
// failed yield.
func (m *streamMapper) run(stream iter.Seq2[Event, error]) {
	for event, err := range stream {
		if err != nil {
			if !m.flush() {
				return
			}
			m.exec.fail(m.execCtx, err, m.yield)
			return
		}
		if isDeltaEvent(event.Kind) {
			if event.Text == "" {
				continue
			}
			if !m.deltas.add(event.Kind, event.Text) {
				return
			}
			continue
		}
		if !m.flush() {
			return
		}
		if m.handleEvent(event) {
			return
		}
	}
	// The stream ended without EventIdle; treat it as a completed turn.
	if !m.flush() {
		return
	}
	m.exec.complete(m.execCtx, m.artifactID, m.usage, m.yield)
}

// isDeltaEvent reports whether kind contributes to the text/reasoning buffer.
func isDeltaEvent(kind EventKind) bool {
	return kind == EventText || kind == EventReasoning
}

// handleEvent emits one non-delta event. It reports whether the stream is done.
func (m *streamMapper) handleEvent(event Event) bool {
	switch event.Kind {
	case EventToolCalled:
		return !m.appendPart(toolCallPart(event))
	case EventToolResult:
		if !m.appendPart(toolResultPart(event)) {
			return true
		}
		for _, diff := range fileDiffParts(event.Structured) {
			if !m.appendPart(diff) {
				return true
			}
		}
		return false
	case EventNotice:
		if event.Text == "" {
			return false
		}
		return !m.yield(a2a.NewStatusUpdateEvent(m.execCtx, a2a.TaskStateWorking, noticeMessage(m.execCtx, event.Text)), nil)
	case EventToolProgress, EventStepStarted:
		return !m.yield(a2a.NewStatusUpdateEvent(m.execCtx, a2a.TaskStateWorking, nil), nil)
	case EventUsage:
		m.usage = event.Usage
		return false
	case EventAwaitInput:
		m.exec.awaitInput(m.execCtx, m.sess, m.artifactID, event, m.yield)
		return true
	case EventError:
		m.exec.fail(m.execCtx, event.Err, m.yield)
		return true
	case EventIdle:
		m.exec.complete(m.execCtx, m.artifactID, m.usage, m.yield)
		return true
	default:
		return false
	}
}

// appendPart appends one part to the turn's artifact, creating the artifact on
// the first part and applying the optional externalizer.
func (m *streamMapper) appendPart(part *a2a.Part) bool {
	var event *a2a.TaskArtifactUpdateEvent
	if m.artifactID == "" {
		event = a2a.NewArtifactEvent(m.execCtx, part)
		m.artifactID = event.Artifact.ID
	} else {
		event = a2a.NewArtifactUpdateEvent(m.execCtx, m.artifactID, part)
	}
	if m.exec.artifacts != nil {
		replacement, err := m.exec.artifacts(m.ctx, string(m.execCtx.TaskID), event.Artifact)
		if err != nil {
			m.exec.fail(m.execCtx, err, m.yield)
			return false
		}
		if artifact, ok := replacement.(*a2a.Artifact); ok && artifact != nil {
			event.Artifact = artifact
		}
	}
	return m.yield(event, nil)
}

// appendText emits one buffered text chunk as a text part, marking reasoning
// output with the thought metadata.
func (m *streamMapper) appendText(text string, thought bool) bool {
	part := a2a.NewTextPart(text)
	if thought {
		part.SetMeta(a2acommon.ToA2AMetaKey(a2acommon.MetaThought), true)
	}
	return m.appendPart(part)
}

// flush emits any buffered deltas.
func (m *streamMapper) flush() bool {
	return m.deltas.flush()
}

// Text and reasoning deltas are accumulated before being emitted as one
// artifact part. One token per part would make a2a-go deep-copy the task and
// the turn task store re-serialize it on every event; batching keeps a turn's
// event count independent of the model's token count. A buffer flushes when a
// non-delta event arrives, the kind changes, it reaches deltaFlushThreshold
// bytes, or deltaFlushInterval elapses since the last flush (so output still
// streams live instead of appearing only at the end of the turn).
const (
	deltaFlushThreshold = 4 << 10 // 4 KiB
	deltaFlushInterval  = 250 * time.Millisecond
)

// deltaBuffer accumulates consecutive streamed deltas of one kind (EventText
// or EventReasoning) and emits them through emit as a single text part, with
// the thought flag set for reasoning deltas. An empty buffer accepts the first
// delta of either kind; add flushes before switching kinds and whenever a
// flush trigger (size or elapsed time) fires. flush is safe to call at any
// time and is a no-op on an empty buffer, so event consumers can call it
// unconditionally before emitting non-delta events.
type deltaBuffer struct {
	// emit delivers one merged text chunk; it returns false when the event
	// consumer stopped and no further events should be emitted.
	emit func(text string, thought bool) bool

	builder   strings.Builder
	kind      EventKind
	lastFlush time.Time
}

// add appends one delta of the given kind, flushing the buffer first when the
// kind differs from what is buffered, and flushing again when a size or time
// trigger fires. It reports whether emitting may continue.
func (b *deltaBuffer) add(kind EventKind, text string) bool {
	if b.builder.Len() > 0 && b.kind != kind {
		if !b.flush() {
			return false
		}
	}
	if b.builder.Len() == 0 {
		b.kind = kind
		b.lastFlush = time.Now()
	}
	b.builder.WriteString(text)
	if b.builder.Len() >= deltaFlushThreshold || time.Since(b.lastFlush) >= deltaFlushInterval {
		return b.flush()
	}
	return true
}

// flush emits the buffered text as one part and empties the buffer. It returns
// true when there was nothing to emit or emission succeeded.
func (b *deltaBuffer) flush() bool {
	if b.builder.Len() == 0 {
		return true
	}
	text := b.builder.String()
	b.builder.Reset()
	b.lastFlush = time.Now()
	return b.emit(text, b.kind == EventReasoning)
}

// awaitInput parks the turn: it seals the open artifact, records the pending
// request, and emits the input-required status so the client can reply on the
// next message. The status message carries what the client needs to answer:
// the questions a question request asked, or what a permission request wants.
func (e *executor) awaitInput(
	execCtx *a2asrv.ExecutorContext,
	sess Session,
	artifactID a2a.ArtifactID,
	event Event,
	yield func(a2a.Event, error) bool,
) {
	if artifactID != "" && !yield(sealArtifact(execCtx, artifactID), nil) {
		return
	}
	e.setPending(execCtx.TaskID, sess, event.RequestID, event.IsQuestion)
	status := a2a.NewStatusUpdateEvent(
		execCtx,
		a2a.TaskStateInputRequired,
		awaitInputMessage(execCtx, event),
	)
	status.Metadata = map[string]any{
		metaKeySession:        sess.ID(),
		metaKeyPendingRequest: event.RequestID,
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
