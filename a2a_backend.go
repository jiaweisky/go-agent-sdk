package agent

import (
	"context"
	"encoding/base64"
	"fmt"
	"iter"
	"strings"
	"sync"
	"time"

	"github.com/jiaweisky/go-agent-sdk/a2a/a2ago"
	"github.com/jiaweisky/go-agent-sdk/a2a/client"
	"github.com/jiaweisky/go-agent-sdk/core/session/runner"
	"github.com/jiaweisky/go-agent-sdk/llm/route"
	llmschema "github.com/jiaweisky/go-agent-sdk/llm/schema"
	"github.com/jiaweisky/go-agent-sdk/schema"
)

// a2aQuestionAsker asks the local user for input a remote agent requested,
// returning the answer text to relay back to the remote.
type a2aQuestionAsker func(ctx context.Context, sessionID string, questions []schema.QuestionInfo) (string, error)

// a2aBackendPool supplies the execution backend for agents whose backend is a
// remote A2A agent. It caches one remote connection per endpoint.
type a2aBackendPool struct {
	mu      sync.Mutex
	clients map[string]client.Client
	asker   a2aQuestionAsker
	// newClient dials a remote agent; overridable for tests.
	newClient func(ctx context.Context, endpoint string, headers map[string]string) (client.Client, error)
	// persistTask records the remote task a local turn talks to (empty clears);
	// loadTask returns the most recent one for a session, so a parked remote
	// task is resumed. Nil disables the behavior.
	persistTask func(sessionID, turnID, taskID, contextID string) error
	loadTask    func(sessionID string) (taskID, contextID string, ok bool)
}

func newA2ABackendPool(asker a2aQuestionAsker) *a2aBackendPool {
	return &a2aBackendPool{clients: map[string]client.Client{}, asker: asker, newClient: a2ago.New}
}

// forAgent returns a backend client for the agent, or nil for local agents.
func (b *a2aBackendPool) forAgent(info *schema.AgentInfo) route.LLMClient {
	if info == nil || info.Backend == nil || info.Backend.Type != schema.AgentBackendA2A || info.Backend.A2A == nil {
		return nil
	}
	return &a2aBackend{backends: b, spec: *info.Backend.A2A, asker: b.asker}
}

func (b *a2aBackendPool) remote(ctx context.Context, spec schema.AgentA2ABackend) (client.Client, error) {
	endpoint := spec.Endpoint
	if endpoint == "" {
		endpoint = spec.CardURL
	}
	// Hold the lock across dialing so two sessions cannot both miss the cache
	// and leak a connection.
	b.mu.Lock()
	defer b.mu.Unlock()
	if cached := b.clients[endpoint]; cached != nil {
		return cached, nil
	}
	remote, err := b.newClient(ctx, endpoint, spec.Headers)
	if err != nil {
		return nil, err
	}
	b.clients[endpoint] = remote
	return remote, nil
}

func (b *a2aBackendPool) close() {
	b.mu.Lock()
	clients := b.clients
	b.clients = map[string]client.Client{}
	b.mu.Unlock()
	for _, remote := range clients {
		_ = remote.Close()
	}
}

// a2aBackend is the execution backend for a session whose agent is a remote A2A
// agent. A2A is agent-to-agent, not agent-to-model; the backend implements
// route.LLMClient only because that is the seam through which the session
// runner drives every turn. Posing as a "model" lets the existing loop mirror
// the remote agent's events into the local session unchanged.
type a2aBackend struct {
	backends *a2aBackendPool
	spec     schema.AgentA2ABackend
	asker    a2aQuestionAsker
}

var _ route.LLMClient = (*a2aBackend)(nil)

func (l *a2aBackend) Prepare(context.Context, *llmschema.LLMRequest) (*llmschema.PreparedRequest, error) {
	return nil, nil
}

func (l *a2aBackend) Generate(context.Context, *llmschema.LLMRequest) (*llmschema.LLMResponse, error) {
	return nil, fmt.Errorf("a2a: Generate is not supported for remote agents; use Stream")
}

func (l *a2aBackend) Stream(
	ctx context.Context,
	req *llmschema.LLMRequest,
) (iter.Seq2[llmschema.LLMEvent, error], error) {
	remote, err := l.backends.remote(ctx, l.spec)
	if err != nil {
		return func(yield func(llmschema.LLMEvent, error) bool) { yield(llmschema.LLMEvent{}, err) }, nil
	}
	sessionID, turnID := requestTurn(req)
	send, err := l.firstRequest(req, sessionID)
	if err != nil {
		return func(yield func(llmschema.LLMEvent, error) bool) { yield(llmschema.LLMEvent{}, err) }, nil
	}
	return func(yield func(llmschema.LLMEvent, error) bool) {
		out := &a2aStream{yield: yield, sessionID: sessionID, turnID: turnID}
		if !out.stepStart() {
			return
		}
		// A remote turn may park at input-required; the local user's answer
		// resumes it. Each leg is a separate streaming request carrying the
		// remote task id.
		for {
			next, outcome, legErr := l.runLeg(ctx, remote, send, out)
			if legErr != nil {
				yield(llmschema.LLMEvent{}, legErr)
				return
			}
			if outcome == legStopped {
				return
			}
			if outcome == legDone {
				break
			}
			send = next
		}
		out.finish()
	}, nil
}

// legOutcome is how one remote streaming leg ended.
type legOutcome int

const (
	// legDone: the stream ended (settled or exhausted).
	legDone legOutcome = iota
	// legParked: the remote asked for input; resume with the returned request.
	legParked
	// legStopped: the consumer stopped iterating.
	legStopped
)

// runLeg consumes one remote streaming leg, forwarding its events. When the
// remote parks at input-required it returns the request that resumes it.
func (l *a2aBackend) runLeg(
	ctx context.Context,
	remote client.Client,
	send client.Request,
	out *a2aStream,
) (client.Request, legOutcome, error) {
	for event, sendErr := range remote.Send(ctx, send) {
		if sendErr != nil {
			if ctx.Err() != nil && !out.settled && out.remoteTaskID != "" {
				cancelRemote(remote, out.remoteTaskID)
			}
			return client.Request{}, legDone, sendErr
		}
		out.note(event)
		if event.Kind == client.EventInputRequired {
			// Record the parked remote task so a later turn can resume it.
			l.recordTask(out.sessionID, out.turnID, event.TaskID, event.ContextID)
			answer, askErr := l.answer(ctx, out.sessionID, event)
			if askErr != nil {
				return client.Request{}, legDone, askErr
			}
			out.settled = false // a new leg is now running
			return resumeRequest(event, answer), legParked, nil
		}
		if event.Kind == client.EventCompleted || event.Kind == client.EventFailed || event.Kind == client.EventCanceled {
			l.recordTask(out.sessionID, out.turnID, "", "")
		}
		if !out.forward(event) {
			return client.Request{}, legStopped, nil
		}
	}
	return client.Request{}, legDone, nil
}

// requestTurn reads the session and turn ids the runner put on the request.
func requestTurn(req *llmschema.LLMRequest) (sessionID, turnID string) {
	sessionID, _ = req.Metadata[runner.RequestMetaSessionID].(string)
	turnID, _ = req.Metadata[runner.RequestMetaTurnID].(string)
	return sessionID, turnID
}

// firstRequest builds the opening request, resuming a remote task a previous
// turn recorded as parked. It carries the latest user message's text and any
// attached media.
func (l *a2aBackend) firstRequest(req *llmschema.LLMRequest, sessionID string) (client.Request, error) {
	files, err := lastUserFiles(req)
	if err != nil {
		return client.Request{}, err
	}
	start := client.Request{Prompt: lastUserText(req), ContextID: sessionID, Files: files}
	if l.backends.loadTask != nil {
		if taskID, contextID, ok := l.backends.loadTask(sessionID); ok {
			start.TaskID = taskID
			start.ContextID = contextID
		}
	}
	return start, nil
}

// resumeRequest answers a parked remote function call, echoing its call id so
// an ADK server can match the reply.
func resumeRequest(event client.Event, answer string) client.Request {
	req := client.Request{Prompt: answer, TaskID: event.TaskID, ContextID: event.ContextID}
	if event.RequestID != "" {
		req.Response = &client.Response{CallID: event.RequestID, Name: event.ToolName, Text: answer}
	}
	return req
}

// cancelRemoteTimeout bounds the best-effort CancelTask issued when a local
// turn is aborted.
const cancelRemoteTimeout = 5 * time.Second

// cancelRemote asks the remote to stop a task the local turn abandoned.
func cancelRemote(remote client.Client, taskID string) {
	ctx, cancel := context.WithTimeout(context.Background(), cancelRemoteTimeout)
	defer cancel()
	_ = remote.Cancel(ctx, taskID)
}

// recordTask records (or clears) the remote task a local turn talks to, for
// resuming a parked remote task later. It is best-effort: a bookkeeping failure
// must not fail the turn.
func (l *a2aBackend) recordTask(sessionID, turnID, taskID, contextID string) {
	if l.backends.persistTask == nil {
		return
	}
	_ = l.backends.persistTask(sessionID, turnID, taskID, contextID)
}

// answer asks the local user for the input the remote agent requested, using
// the questions the remote forwarded when it provided them.
func (l *a2aBackend) answer(ctx context.Context, sessionID string, event client.Event) (string, error) {
	if l.asker == nil {
		return "", fmt.Errorf("a2a: remote agent requested input but no question handler is configured")
	}
	questions := event.Questions
	if len(questions) == 0 {
		prompt := event.Text
		if prompt == "" {
			prompt = "The remote agent is waiting for input."
		}
		questions = []schema.QuestionInfo{{Question: prompt, Header: "remote-agent"}}
	}
	return l.asker(ctx, sessionID, questions)
}

// a2aStream turns remote A2A events into an LLM event stream, tracking which
// text and reasoning parts have been opened so their starts and ends are
// emitted exactly once. Each method reports whether the consumer kept iterating.
type a2aStream struct {
	yield            func(llmschema.LLMEvent, error) bool
	textStarted      bool
	reasoningStarted bool
	// toolCalls records the remote tool calls seen this step, so a result is
	// only surfaced when its call was.
	toolCalls map[string]string
	// sessionID and turnID identify the local turn, for recording the remote task.
	sessionID string
	turnID    string
	// remoteTaskID is the remote task this leg talks to; settled records whether
	// the last event left it terminal or parked, so an aborted leg does not
	// cancel a task that is finished or resumable.
	remoteTaskID string
	settled      bool
}

// note records the remote task id and whether the last event left the remote
// settled (terminal or parked).
func (s *a2aStream) note(event client.Event) {
	if event.TaskID != "" {
		s.remoteTaskID = event.TaskID
	}
	switch event.Kind {
	case client.EventCompleted, client.EventFailed, client.EventCanceled, client.EventInputRequired:
		s.settled = true
	default:
		s.settled = false
	}
}

const (
	a2aTextID      = "a2a-text"
	a2aReasoningID = "a2a-reasoning"
)

func (s *a2aStream) stepStart() bool {
	return s.yield(llmschema.NewStepStart(0), nil)
}

// forward maps one remote event; it reports false when the consumer stopped.
func (s *a2aStream) forward(event client.Event) bool {
	switch event.Kind {
	case client.EventArtifact:
		return s.text(event.Text)
	case client.EventReasoning:
		return s.reasoning(event.Text)
	case client.EventToolCall:
		return s.toolCall(event)
	case client.EventToolResult:
		return s.toolResult(event)
	case client.EventFailed:
		s.yield(llmschema.NewProviderErrorEvent("remote A2A task failed"), nil)
		return false
	default:
		// EventInputRequired is handled by runLeg (it parks and resumes); the
		// terminal kinds carry no LLM event here.
		return true
	}
}

// toolCall surfaces a remote tool call as provider-executed: the SDK records
// and publishes it (marked with provider.executed) but never runs it locally,
// so remote tools stay observable without triggering local permissions, hooks
// or side effects.
func (s *a2aStream) toolCall(event client.Event) bool {
	if s.toolCalls == nil {
		s.toolCalls = map[string]string{}
	}
	s.toolCalls[event.CallID] = event.ToolName
	return s.yield(llmschema.NewToolCall(event.CallID, event.ToolName, event.Input,
		llmschema.WithEventProviderExecuted(true)), nil)
}

// toolResult records the outcome of a previously seen remote tool call.
func (s *a2aStream) toolResult(event client.Event) bool {
	name, ok := s.toolCalls[event.CallID]
	if !ok {
		return true
	}
	if event.IsError {
		return s.yield(llmschema.NewToolError(event.CallID, name, fmt.Sprint(event.Result),
			llmschema.WithEventProviderExecuted(true)), nil)
	}
	value := llmschema.NewToolResultValue(event.Result, toolResultType(event.Result))
	return s.yield(llmschema.NewToolResult(event.CallID, name, value,
		llmschema.WithEventProviderExecuted(true)), nil)
}

// toolResultType classifies a remote result for the SDK's tool result value.
func toolResultType(result any) string {
	if _, ok := result.(string); ok {
		return "text"
	}
	return "json"
}

func (s *a2aStream) text(delta string) bool {
	if !s.textStarted {
		s.textStarted = true
		if !s.yield(llmschema.NewTextStart(a2aTextID), nil) {
			return false
		}
	}
	return s.yield(llmschema.NewTextDelta(a2aTextID, delta), nil)
}

func (s *a2aStream) reasoning(delta string) bool {
	if !s.reasoningStarted {
		s.reasoningStarted = true
		if !s.yield(llmschema.NewReasoningStart(a2aReasoningID), nil) {
			return false
		}
	}
	return s.yield(llmschema.NewReasoningDelta(a2aReasoningID, delta), nil)
}

// finish closes the open parts and the step.
func (s *a2aStream) finish() {
	if s.reasoningStarted && !s.yield(llmschema.NewReasoningEnd(a2aReasoningID), nil) {
		return
	}
	if !s.textStarted && !s.yield(llmschema.NewTextStart(a2aTextID), nil) {
		return
	}
	if !s.yield(llmschema.NewTextEnd(a2aTextID), nil) {
		return
	}
	if !s.yield(llmschema.NewStepFinish(0, llmschema.ReasonStop), nil) {
		return
	}
	s.yield(llmschema.NewFinish(llmschema.ReasonStop), nil)
}

// lastUserText returns the concatenated text of the last user message.
func lastUserText(req *llmschema.LLMRequest) string {
	for i := len(req.Messages) - 1; i >= 0; i-- {
		message := req.Messages[i]
		if message.Role != llmschema.RoleUser {
			continue
		}
		var builder strings.Builder
		for _, part := range message.Content {
			text, ok := part.(llmschema.TextPart)
			if !ok || text.Text == "" {
				continue
			}
			if builder.Len() > 0 {
				builder.WriteByte('\n')
			}
			builder.WriteString(text.Text)
		}
		return builder.String()
	}
	return ""
}

// lastUserFiles returns the media attached to the last user message, decoded
// and ready to send to the remote as A2A file parts.
func lastUserFiles(req *llmschema.LLMRequest) ([]client.FileAttachment, error) {
	for i := len(req.Messages) - 1; i >= 0; i-- {
		message := req.Messages[i]
		if message.Role != llmschema.RoleUser {
			continue
		}
		var files []client.FileAttachment
		for _, part := range message.Content {
			media, ok := part.(llmschema.MediaPart)
			if !ok {
				continue
			}
			file, ok, err := attachmentFromMedia(media)
			if err != nil {
				return nil, err
			}
			if !ok {
				continue
			}
			files = append(files, file)
		}
		return files, nil
	}
	return nil, nil
}

// attachmentFromMedia converts a request media part into an A2A attachment. A
// url source becomes a URI reference; a provider file reference is not portable
// across A2A, so it is skipped (ok=false) rather than forwarded.
func attachmentFromMedia(media llmschema.MediaPart) (client.FileAttachment, bool, error) {
	file := client.FileAttachment{MIME: media.MediaType}
	if media.Filename != nil {
		file.Name = *media.Filename
	}
	source := media.Source
	if source == "" {
		source = llmschema.MediaSourceInline
	}
	switch source {
	case llmschema.MediaSourceURL:
		url, _ := media.Data.(string)
		if strings.TrimSpace(url) == "" {
			return client.FileAttachment{}, false, fmt.Errorf("a2a: empty media URL")
		}
		file.URI = url
		return file, true, nil
	case llmschema.MediaSourceFile:
		return client.FileAttachment{}, false, nil
	}
	switch data := media.Data.(type) {
	case []byte:
		file.Data = data
	case string:
		decoded, err := decodeAttachment(data)
		if err != nil {
			return client.FileAttachment{}, false, err
		}
		file.Data = decoded
	default:
		return client.FileAttachment{}, false, fmt.Errorf("a2a: unsupported attachment data type %T", media.Data)
	}
	return file, true, nil
}

// decodeAttachment decodes a PromptFileAttachment URI payload: a data URL or
// bare base64.
func decodeAttachment(data string) ([]byte, error) {
	if rest, ok := strings.CutPrefix(data, "data:"); ok {
		if comma := strings.IndexByte(rest, ','); comma >= 0 {
			data = rest[comma+1:]
		}
	}
	if decoded, err := base64.StdEncoding.DecodeString(data); err == nil {
		return decoded, nil
	}
	decoded, err := base64.RawStdEncoding.DecodeString(data)
	if err != nil {
		return nil, fmt.Errorf("a2a: decode attachment: %w", err)
	}
	return decoded, nil
}
