package runner

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"iter"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/jiaweisky/go-agent-sdk/core"
	"github.com/jiaweisky/go-agent-sdk/core/database"
	"github.com/jiaweisky/go-agent-sdk/core/reference"
	"github.com/jiaweisky/go-agent-sdk/core/session"
	"github.com/jiaweisky/go-agent-sdk/core/skill"
	"github.com/jiaweisky/go-agent-sdk/core/systemcontext"
	"github.com/jiaweisky/go-agent-sdk/core/tool"
	"github.com/jiaweisky/go-agent-sdk/internal/llm/protocols/utils"
	"github.com/jiaweisky/go-agent-sdk/pkg/llm"
	"github.com/jiaweisky/go-agent-sdk/pkg/llm/protocols"
	"github.com/jiaweisky/go-agent-sdk/pkg/llm/route"
	llmschema "github.com/jiaweisky/go-agent-sdk/pkg/llm/schema"
	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

// RunnerEvents is the event surface the runner needs: publishing plus the
// commit-aware publish used by the context epoch.
type RunnerEvents interface {
	session.EventPublisher
	session.ContextEpochPublisher
}

// AgentSelector is the narrow agent surface the runner needs. The session id
// lets embedders resolve a session-scoped agent override before falling back to
// the stored agent.
type AgentSelector interface {
	Select(sessionID schema.SessionID, id *string) core.AgentSelection
}

// Metadata keys the runner puts on the provider request. An execution backend
// (for example a remote A2A agent) reads them to find the session it mirrors.
const (
	RequestMetaSessionID = "sessionID"
	RequestMetaAgentID   = "agentID"
	RequestMetaTurnID    = "turnID"
)

// Snapshotter is the narrow Snapshot surface the runner needs.
type Snapshotter interface {
	Capture() (*core.SnapshotID, error)
	Files(input core.SnapshotCompareInput) ([]schema.RelativePath, error)
}

var _ Snapshotter = (*core.Snapshot)(nil)

// ToolRegistry is the narrow tool-registry surface the runner needs. The
// session id lets embedders scope tools per session.
type ToolRegistry interface {
	MaterializeFor(sessionID string, permissions []core.PermissionRule) (*tool.Materialization, error)
}

// BackendSelector returns a per-agent LLM client for agents with a non-local
// execution backend (for example a remote A2A agent). Returning nil uses the
// default LLM client.
type BackendSelector func(info *schema.AgentInfo) route.LLMClient

// mediaUploadCache deduplicates uploads by content hash. It is FIFO-bounded so
// a long-lived runner does not grow without limit.
type mediaUploadCache struct {
	mu    sync.Mutex
	index map[string]schema.MediaRef
	order []string
}

// maxMediaCacheEntries bounds the upload dedup cache.
const maxMediaCacheEntries = 512

// LLMRunner runs one durable coding-agent session until it settles.
type LLMRunner struct {
	Events   RunnerEvents
	LLM      route.LLMClient
	Backends BackendSelector
	// MediaUploader resolves a provider's media uploader for inline→file
	// adaptation. Nil disables adaptation.
	MediaUploader func(providerID schema.ProviderID) schema.MediaUploader
	// OnMediaDecision reports each omit/upload for host metrics. Nil reports
	// nothing.
	OnMediaDecision   func(schema.MediaDecision)
	Agents            AgentSelector
	Tools             ToolRegistry
	Models            ModelResolver
	Store             *session.Store
	Location          core.Location
	SystemContext     *systemcontext.Registry
	SkillGuidance     *skill.Guidance
	ReferenceGuidance *reference.Guidance
	Compaction        session.CompactionSettings
	Title             session.TitleSettings
	Snapshots         Snapshotter
	DB                *database.Database
	// InboundSessions reports whether a session's turn is owned by an inbound
	// server (for example an A2A task store); the runner then records no turn
	// of its own. Nil means every session is local.
	InboundSessions func(sessionID schema.SessionID) bool
	// OnError reports turn bookkeeping errors that do not fail the turn. Nil
	// ignores them.
	OnError func(error)
	// DropErroredTurns excludes a turn that ended in error from later request
	// contexts, so a persistently rejected message cannot wedge the session.
	DropErroredTurns bool

	compaction *session.Compaction
	turns      *session.TurnStore
	tracker    *turnTracker
	media      *mediaUploadCache
	omissions  *omissionTracker
}

// omissionTracker records attachments already reported as omitted, so the
// durable event is published once per attachment rather than on every request
// rebuild (a turn rebuilds its request per step, and history repeats it). The
// set holds one short key per distinct omitted attachment for the process
// lifetime — small, since unsupported attachments are rare.
type omissionTracker struct {
	mu   sync.Mutex
	seen map[string]struct{}
}

// markOnce records key and reports whether it is new. A nil tracker treats
// every key as new.
func (t *omissionTracker) markOnce(key string) bool {
	if t == nil {
		return true
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, ok := t.seen[key]; ok {
		return false
	}
	t.seen[key] = struct{}{}
	return true
}

// turnTracker records the turn the runner currently owns per session, so a
// subagent's turn can link its parent. It is a pointer so LLMRunner stays
// copyable (go vet forbids copying a struct that embeds a mutex).
type turnTracker struct {
	mu     sync.Mutex
	active map[schema.SessionID]schema.TurnID
}

func newTurnTracker() *turnTracker {
	return &turnTracker{active: map[schema.SessionID]schema.TurnID{}}
}

// currentTurn returns the turn the runner is running for a session.
func (r *LLMRunner) currentTurn(sessionID schema.SessionID) (schema.TurnID, bool) {
	r.tracker.mu.Lock()
	defer r.tracker.mu.Unlock()
	id, ok := r.tracker.active[sessionID]
	return id, ok
}

var _ SessionRunner = (*LLMRunner)(nil)

func NewLLMRunner(runner LLMRunner) *LLMRunner {
	result := &runner
	result.compaction = session.NewCompaction(session.CompactionDependencies{
		Events:   runner.Events,
		LLM:      runner.compactionStream,
		Settings: runner.Compaction,
	})
	result.tracker = newTurnTracker()
	result.omissions = &omissionTracker{seen: map[string]struct{}{}}
	result.media = &mediaUploadCache{}
	if runner.DB != nil {
		result.turns = session.NewTurnStore(runner.DB)
	}
	return result
}

func (r *LLMRunner) compactionStream(
	ctx context.Context,
	request llmschema.LLMRequest,
) iter.Seq2[llmschema.LLMEvent, error] {
	stream, err := r.LLM.Stream(ctx, &request)
	if err != nil {
		return func(yield func(llmschema.LLMEvent, error) bool) { yield(llmschema.LLMEvent{}, err) }
	}
	return stream
}

// Compact summarizes a session's stored history on demand, replacing it with the
// summary and the most recent messages. It reports whether anything was
// compacted, and is intended to run while the session is idle.
func (r *LLMRunner) Compact(ctx context.Context, sessionID schema.SessionID) (bool, error) {
	stored, err := r.Store.Get(sessionID)
	if err != nil {
		return false, err
	}
	if stored == nil {
		return false, fmt.Errorf("Session not found: %s", sessionID)
	}
	model, err := r.Models(*stored)
	if err != nil {
		return false, err
	}
	baseline, err := session.ContextEpochBaseline(r.DB, sessionID)
	if err != nil {
		return false, err
	}
	entries, err := session.EntriesForRunner(r.DB, sessionID, baseline)
	if err != nil {
		return false, err
	}
	request := llm.NewRequest(llm.RequestInput{
		Model: model,
		Http:  &llmschema.HttpOptions{Headers: sessionHeaders(sessionID, stored)},
	})
	return r.compaction.Compact(session.CompactionInput{
		SessionID: sessionID,
		Entries:   compactionEntries(entries),
		Model:     model,
		Request:   *request,
		Context:   ctx,
	})
}

// errInterrupted wraps context.Canceled, so callers can distinguish an
// interrupted turn from a failed one with errors.Is.
var errInterrupted = fmt.Errorf("session runner interrupted: %w", context.Canceled)

var sessionIDShape = regexp.MustCompile(`^ses_[0-9a-f]{64}$`)

type turnResult struct {
	NeedsContinuation bool
	Step              int
}

type turnTransition struct {
	Tag  string
	Step int
}

type turnTransitionError struct{ Transition turnTransition }

func (e *turnTransitionError) Error() string { return "turn transition: " + e.Transition.Tag }

func isInterrupt(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, errInterrupted)
}

// Run drains the session's admitted inputs until it settles or is interrupted.
func (r *LLMRunner) Run(ctx context.Context, input RunInput) error {
	hasSteer, err := session.HasPending(r.DB.DB, input.SessionID, schema.SessionDeliverySteer)
	if err != nil {
		return err
	}
	hasQueue := false
	if !hasSteer {
		hasQueue, err = session.HasPending(r.DB.DB, input.SessionID, schema.SessionDeliveryQueue)
		if err != nil {
			return err
		}
	}
	if !input.Force && !hasSteer && !hasQueue {
		return nil
	}
	if err := r.failInterruptedTools(input.SessionID); err != nil {
		return err
	}

	promotion := deliveryPromotion(hasSteer, hasQueue)
	shouldRun := input.Force || hasSteer || hasQueue
	for shouldRun {
		if err := r.drainTurns(ctx, input.SessionID, promotion); err != nil {
			return err
		}
		shouldRun, err = session.HasPending(r.DB.DB, input.SessionID, schema.SessionDeliveryQueue)
		if err != nil {
			return err
		}
		promotion = queuePromotion(shouldRun)
	}
	r.ensureTitle(ctx, input.SessionID)
	return nil
}

// ensureTitle generates a session title once, after the first user turn, when
// title generation is enabled and the session is a root with a default title
// and exactly one user message. Failures are ignored: a missing title must not
// fail the turn.
func (r *LLMRunner) ensureTitle(ctx context.Context, sessionID schema.SessionID) {
	if !r.Title.Auto || ctx.Err() != nil {
		return
	}
	stored, err := r.Store.Get(sessionID)
	if err != nil || stored == nil {
		return
	}
	if !titleEligible(stored) {
		return
	}
	prompt, ok := singleUserPrompt(r.DB, sessionID)
	if !ok {
		return
	}
	titleID := "title"
	agent := r.Agents.Select(sessionID, &titleID)
	if agent.Info == nil {
		return
	}
	model, ok := r.titleModel(stored, agent.Info)
	if !ok {
		return
	}
	title, ok := r.generateTitle(ctx, titleRequest(sessionID, stored, agent.Info, model, prompt))
	if !ok {
		return
	}
	r.publishTitle(sessionID, title)
}

// titleEligible reports whether a session can still be auto-titled: it must be
// a root whose title is still the placeholder.
func titleEligible(stored *schema.SessionInfo) bool {
	return stored.ParentID == nil && session.IsDefaultTitle(stored.Title)
}

// singleUserPrompt returns the text of a session's only user message, and
// whether the history has exactly one non-empty user message. A conversation
// with any other shape is not titled.
func singleUserPrompt(db *database.Database, sessionID schema.SessionID) (string, bool) {
	entries, err := session.EntriesForRunner(db, sessionID, 0)
	if err != nil {
		return "", false
	}
	prompt := ""
	users := 0
	for _, entry := range entries {
		user, ok := entry.Message.(schema.SessionMessageUser)
		if !ok {
			continue
		}
		users++
		if prompt == "" {
			prompt = strings.TrimSpace(user.Text)
		}
	}
	return prompt, users == 1 && prompt != ""
}

// titleModel resolves the model used to generate titles: the explicit
// configuration, the title agent's own model, or the session model, in order of
// precedence. It reports false when even the session model cannot be resolved.
func (r *LLMRunner) titleModel(stored *schema.SessionInfo, agent *schema.AgentInfo) (route.Model, bool) {
	model, ok := r.modelForRef(stored, stored.Model)
	if !ok {
		return route.Model{}, false
	}
	if agent.Model != nil {
		if agentModel, valid := r.modelForRef(stored, agent.Model); valid {
			model = agentModel
		}
	}
	if r.Title.Model != "" {
		override := &schema.ModelRef{ID: schema.ModelID(r.Title.Model)}
		if overrideModel, valid := r.modelForRef(stored, override); valid {
			model = overrideModel
		}
	}
	return model, true
}

// modelForRef resolves a model reference against the session's routing context.
func (r *LLMRunner) modelForRef(stored *schema.SessionInfo, ref *schema.ModelRef) (route.Model, bool) {
	synthetic := *stored
	synthetic.Model = ref
	model, err := r.Models(synthetic)
	if err != nil {
		return route.Model{}, false
	}
	return model, true
}

// titleRequest builds the provider request that asks the title agent to name the
// conversation.
func titleRequest(
	sessionID schema.SessionID,
	stored *schema.SessionInfo,
	agent *schema.AgentInfo,
	model route.Model,
	prompt string,
) llm.LLMRequest {
	var system []llmschema.SystemPart
	if agent.System != nil && *agent.System != "" {
		system = append(system, llmschema.NewSystemPart(*agent.System))
	}
	return *llm.NewRequest(llm.RequestInput{
		Model:    model,
		Http:     &llmschema.HttpOptions{Headers: sessionHeaders(sessionID, stored)},
		System:   system,
		Messages: []llm.Message{llmschema.MessageUser(session.TitlePromptPrefix + prompt)},
		Tools:    []llm.ToolDefinition{},
	})
}

// generateTitle streams the title request and returns the cleaned reply, or
// false when the stream failed or produced nothing usable.
func (r *LLMRunner) generateTitle(ctx context.Context, request llm.LLMRequest) (string, bool) {
	stream, err := r.LLM.Stream(ctx, &request)
	if err != nil {
		return "", false
	}
	var chunks []string
	for event, streamErr := range stream {
		if streamErr != nil {
			return "", false
		}
		if event.IsTextDelta() {
			chunks = append(chunks, event.Text)
		}
	}
	title := session.CleanTitle(strings.Join(chunks, ""))
	return title, title != ""
}

// publishTitle records a generated title as a durable session event.
func (r *LLMRunner) publishTitle(sessionID schema.SessionID, title string) {
	_, _ = r.Events.Publish(schema.SessionEventTitleUpdated, map[string]any{
		"timestamp": time.Now().UnixMilli(),
		"sessionID": sessionID,
		"title":     title,
	})
}

// deliveryPromotion is the initial input kind to promote, if any.
func deliveryPromotion(hasSteer, hasQueue bool) *schema.SessionDelivery {
	if hasSteer {
		value := schema.SessionDeliverySteer
		return &value
	}
	if hasQueue {
		value := schema.SessionDeliveryQueue
		return &value
	}
	return nil
}

func queuePromotion(hasQueue bool) *schema.SessionDelivery {
	if hasQueue {
		value := schema.SessionDeliveryQueue
		return &value
	}
	return nil
}

// drainTurns runs consecutive turns until none requests continuation.
func (r *LLMRunner) drainTurns(
	ctx context.Context,
	sessionID schema.SessionID,
	promotion *schema.SessionDelivery,
) (err error) {
	handle := r.beginTurn(sessionID)
	defer r.finishTurn(handle, &err)
	needsContinuation := true
	step := 1
	for needsContinuation {
		result, err := r.runTurn(ctx, sessionID, promotion, step)
		if err != nil {
			return err
		}
		needsContinuation = result.NeedsContinuation
		step = result.Step + 1
		value := schema.SessionDeliverySteer
		promotion = &value
		if !needsContinuation {
			needsContinuation, err = session.HasPending(r.DB.DB, sessionID, schema.SessionDeliverySteer)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// turnHandle tracks the durable turn the runner opened for a drain, so it can
// be closed with the right terminal state and sequence boundaries.
type turnHandle struct {
	info schema.TurnInfo
}

// beginTurn opens a turn for a local drain. An inbound A2A task owns its
// session's turn (the task store maintains it), so nothing is opened then.
// reportError forwards a background error to the configured reporter.
func (r *LLMRunner) reportError(err error) {
	if err != nil && r.OnError != nil {
		r.OnError(err)
	}
}

func (r *LLMRunner) beginTurn(sessionID schema.SessionID) *turnHandle {
	if r.turns == nil {
		return nil
	}
	// A session served by an inbound executor owns its own turn (the executor's
	// task store maintains it); the runner records nothing.
	if r.InboundSessions != nil && r.InboundSessions(sessionID) {
		return nil
	}
	existing, err := r.turns.ActiveForSession(sessionID)
	if err != nil {
		r.reportError(err)
		return nil
	}
	if existing != nil && existing.Metadata[schema.TurnMetaOwner] == schema.TurnOwnerA2A {
		return nil
	}

	now := time.Now().UnixMilli()
	anchor, anchorErr := session.LatestMessageSeq(r.DB, sessionID)
	r.reportError(anchorErr)
	inputID, inputErr := session.LatestInputID(r.DB, sessionID)
	r.reportError(inputErr)
	info := schema.TurnInfo{
		SessionID:   sessionID,
		State:       schema.TurnStateRunning,
		InputID:     inputID,
		AnchorSeq:   &anchor,
		Metadata:    map[string]any{schema.TurnMetaOwner: schema.TurnOwnerLocal},
		TimeCreated: now,
		TimeUpdated: now,
	}
	if stored, err := r.Store.Get(sessionID); err == nil && stored != nil {
		info.Agent = stored.Agent
		info.Model = stored.Model
	}

	if existing != nil {
		// Take over a local turn a crash left running.
		info.ID = existing.ID
		info.TimeCreated = existing.TimeCreated
		if err := r.turns.Update(info); err != nil {
			r.reportError(err)
			return nil
		}
	} else {
		info.ID = schema.NewTurnID()
		info.ParentTurnID = r.parentTurn(sessionID)
		if err := r.turns.Create(info); err != nil {
			r.reportError(err)
			return nil
		}
	}

	r.tracker.mu.Lock()
	r.tracker.active[sessionID] = info.ID
	r.tracker.mu.Unlock()
	return &turnHandle{info: info}
}

// parentTurn returns the turn the delegating (parent) session is running, so a
// subagent's turn links back to it. It falls back to the database for a parent
// whose turn the runner does not own (an inbound A2A session's task-store turn).
func (r *LLMRunner) parentTurn(sessionID schema.SessionID) *schema.TurnID {
	stored, err := r.Store.Get(sessionID)
	if err != nil || stored == nil || stored.ParentID == nil {
		return nil
	}
	parentID := schema.SessionID(*stored.ParentID)
	r.tracker.mu.Lock()
	id, ok := r.tracker.active[parentID]
	r.tracker.mu.Unlock()
	if ok {
		return &id
	}
	if active, err := r.turns.ActiveForSession(parentID); err == nil && active != nil {
		return &active.ID
	}
	return nil
}

// finishTurn closes the runner's turn with its terminal state and end sequence.
// It re-reads the turn first so metadata an execution backend added during the
// stream (for example a remote A2A task id) is preserved.
func (r *LLMRunner) finishTurn(handle *turnHandle, runErr *error) {
	if handle == nil {
		return
	}
	state := schema.TurnStateCompleted
	if err := *runErr; err != nil {
		if isInterrupt(err) {
			state = schema.TurnStateCanceled
		} else {
			state = schema.TurnStateFailed
		}
	}
	info := handle.info
	if current, err := r.turns.Get(handle.info.ID); err == nil && current != nil {
		info.Metadata = current.Metadata
	}
	end, _ := session.LatestMessageSeq(r.DB, handle.info.SessionID)
	info.State = state
	info.EndSeq = &end
	info.TimeUpdated = time.Now().UnixMilli()
	r.reportError(r.turns.Update(info))

	r.tracker.mu.Lock()
	delete(r.tracker.active, handle.info.SessionID)
	r.tracker.mu.Unlock()
}

func (r *LLMRunner) failInterruptedTools(sessionID schema.SessionID) error {
	messages, err := r.Store.Context(sessionID)
	if err != nil {
		return err
	}
	for _, message := range messages {
		assistant, ok := message.(schema.SessionMessageAssistant)
		if !ok {
			continue
		}
		for _, content := range assistant.Content {
			assistantTool, ok := content.(schema.SessionMessageAssistantTool)
			if !ok {
				continue
			}
			switch assistantTool.State.(type) {
			case schema.SessionMessageToolStatePending, schema.SessionMessageToolStateRunning:
			default:
				continue
			}
			executed := assistantTool.Provider != nil && assistantTool.Provider.Executed
			provider := map[string]any{"executed": executed}
			if assistantTool.Provider != nil && assistantTool.Provider.Metadata != nil {
				provider["metadata"] = *assistantTool.Provider.Metadata
			}
			if _, err := r.Events.Publish(schema.SessionEventToolFailed, map[string]any{
				"sessionID":          sessionID,
				"timestamp":          time.Now().UnixMilli(),
				"assistantMessageID": assistant.ID,
				"callID":             assistantTool.ID,
				"error":              schema.SessionMessageUnknownError{Type: "unknown", Message: "Tool execution interrupted"},
				"provider":           provider,
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *LLMRunner) runTurn(
	ctx context.Context,
	sessionID schema.SessionID,
	promotion *schema.SessionDelivery,
	step int,
) (turnResult, error) {
	return r.runTurnLoop(ctx, sessionID, promotion, step, true)
}

func (r *LLMRunner) runTurnLoop(
	ctx context.Context,
	sessionID schema.SessionID,
	promotion *schema.SessionDelivery,
	step int,
	recoverOverflow bool,
) (turnResult, error) {
	for {
		var recover func(session.CompactionInput) (bool, error)
		if recoverOverflow {
			recover = r.compaction.CompactAfterOverflow
		}
		result, err := r.runTurnAttempt(ctx, sessionID, promotion, step, recover)
		if err == nil {
			return result, nil
		}
		var transition *turnTransitionError
		if !errors.As(err, &transition) {
			return result, err
		}
		if transition.Transition.Tag == "ContinueAfterOverflowCompaction" {
			if !recoverOverflow {
				return turnResult{}, errors.New("Post-compaction provider attempt cannot recover another overflow")
			}
			recoverOverflow = false
			promotion = nil
			step = transition.Transition.Step
			continue
		}
		promotion = nil
		step = transition.Transition.Step
	}
}

func (r *LLMRunner) loadSystemContext(agent core.AgentSelection) (systemcontext.SystemContext, error) {
	registry, err := r.SystemContext.Load()
	if err != nil {
		return systemcontext.SystemContext{}, err
	}
	skills, err := r.SkillGuidance.Load(agent.Info)
	if err != nil {
		return systemcontext.SystemContext{}, err
	}
	references, err := r.ReferenceGuidance.Load()
	if err != nil {
		return systemcontext.SystemContext{}, err
	}
	return systemcontext.Combine(registry, skills, references), nil
}

// turnAttempt holds the state of one provider attempt across its three phases:
// preparation, stream consumption, and settlement.
type turnAttempt struct {
	runner              *LLMRunner
	ctx                 context.Context
	sessionID           schema.SessionID
	step                int
	agent               core.AgentSelection
	model               route.Model
	entries             []session.HistoryEntry
	request             *llmschema.LLMRequest
	toolMaterialization *tool.Materialization
	startSnapshot       *core.SnapshotID
	recoverOverflow     func(session.CompactionInput) (bool, error)

	publisher  *LLMEventPublisher
	guard      *publishGuard
	toolCtx    context.Context
	toolCancel context.CancelFunc

	streamErr         error
	needsContinuation bool
	overflowFailure   *llmschema.LLMEvent
	toolErrs          []error
	toolErrMu         sync.Mutex
}

func (r *LLMRunner) runTurnAttempt(
	ctx context.Context,
	sessionID schema.SessionID,
	promotion *schema.SessionDelivery,
	step int,
	recoverOverflow func(session.CompactionInput) (bool, error),
) (turnResult, error) {
	attempt, err := r.prepareTurnAttempt(ctx, sessionID, promotion, step, recoverOverflow)
	if err != nil {
		return turnResult{}, err
	}
	attempt.toolCtx, attempt.toolCancel = context.WithCancel(ctx)
	defer attempt.toolCancel()

	attempt.consumeStream()
	return attempt.settle()
}

// prepareTurnAttempt validates the session, promotes pending input, and builds
// the provider request. It returns a transition error when compaction asks the
// caller to retry.
func (r *LLMRunner) prepareTurnAttempt(
	ctx context.Context,
	sessionID schema.SessionID,
	promotion *schema.SessionDelivery,
	step int,
	recoverOverflow func(session.CompactionInput) (bool, error),
) (*turnAttempt, error) {
	stored, err := r.Store.Get(sessionID)
	if err != nil {
		return nil, err
	}
	if stored == nil {
		return nil, fmt.Errorf("Session not found: %s", sessionID)
	}
	if string(stored.Location.Directory) != string(r.Location.Directory) ||
		!sameWorkspace(stored.Location.WorkspaceID, r.Location.WorkspaceID) {
		return nil, errInterrupted
	}

	var agentID *string
	if stored.Agent != nil {
		value := string(*stored.Agent)
		agentID = &value
	}
	agent := r.Agents.Select(sessionID, agentID)

	initialized, err := r.initializeSystemContext(agent, sessionID)
	if err != nil {
		return nil, err
	}

	currentStep, err := r.promotePendingDeliveries(sessionID, promotion, step)
	if err != nil {
		return nil, err
	}

	var system session.ContextEpochPrepared
	if initialized != nil {
		system = *initialized
	} else {
		system, err = r.prepareSystemContext(agent, sessionID)
		if err != nil {
			return nil, err
		}
	}

	model, err := r.Models(*stored)
	if err != nil {
		return nil, err
	}
	entries, err := session.EntriesForRunner(r.DB, sessionID, system.BaselineSeq)
	if err != nil {
		return nil, err
	}

	isLastStep := agent.Info != nil && agent.Info.Steps != nil && currentStep >= int(*agent.Info.Steps)
	toolMaterialization, err := r.materializeTools(sessionID, agent, stored, isLastStep)
	if err != nil {
		return nil, err
	}
	contextMessages := contextMessagesOf(entries)
	if r.DropErroredTurns {
		contextMessages = dropErroredTurns(contextMessages)
	}
	contextMessages = r.adaptMedia(ctx, sessionID, model, contextMessages)
	policy := r.turnMediaPolicy(model, agent, sessionID)
	request := buildTurnRequest(
		sessionID,
		agent,
		stored,
		system,
		contextMessages,
		model,
		policy,
		isLastStep,
		toolMaterialization,
	)
	if turnID, ok := r.currentTurn(sessionID); ok {
		request.Metadata[RequestMetaTurnID] = string(turnID)
	}

	if compacted, err := r.compaction.CompactIfNeeded(session.CompactionInput{
		SessionID: sessionID, Entries: compactionEntries(entries), Model: model, Request: *request, Context: ctx,
	}); err != nil {
		return nil, err
	} else if compacted {
		return nil, &turnTransitionError{Transition: turnTransition{Tag: "ContinueAfterCompaction", Step: currentStep}}
	}

	startSnapshot, err := r.Snapshots.Capture()
	if err != nil {
		return nil, err
	}
	var snapshot *string
	if startSnapshot != nil {
		value := string(*startSnapshot)
		snapshot = &value
	}
	variant := variantOf(*stored)
	publisher := NewLLMEventPublisher(r.Events, LLMEventPublisherInput{
		SessionID: sessionID,
		Agent:     string(agent.ID),
		Model: schema.ModelRef{
			ID:         schema.ModelID(model.ID),
			ProviderID: schema.ProviderID(model.Provider),
			Variant:    variant,
		},
		Snapshot: snapshot,
	})

	return &turnAttempt{
		runner:              r,
		ctx:                 ctx,
		sessionID:           sessionID,
		step:                currentStep,
		agent:               agent,
		model:               model,
		entries:             entries,
		request:             request,
		toolMaterialization: toolMaterialization,
		startSnapshot:       startSnapshot,
		recoverOverflow:     recoverOverflow,
		publisher:           publisher,
		guard:               &publishGuard{},
	}, nil
}

// initializeSystemContext loads the context sources and initializes the epoch,
// returning the prepared epoch when one already existed.
func (r *LLMRunner) initializeSystemContext(
	agent core.AgentSelection,
	sessionID schema.SessionID,
) (*session.ContextEpochPrepared, error) {
	systemContext, err := r.loadSystemContext(agent)
	if err != nil {
		return nil, err
	}
	return session.ContextEpochInitialize(r.DB.DB, systemContext, sessionID)
}

// prepareSystemContext builds a fresh epoch baseline for the attempt.
func (r *LLMRunner) prepareSystemContext(
	agent core.AgentSelection,
	sessionID schema.SessionID,
) (session.ContextEpochPrepared, error) {
	systemContext, err := r.loadSystemContext(agent)
	if err != nil {
		return session.ContextEpochPrepared{}, err
	}
	return session.ContextEpochPrepare(r.DB.DB, r.Events, systemContext, sessionID)
}

// promotePendingDeliveries admits pending steer/queue input, resetting the step
// when anything was promoted.
func (r *LLMRunner) promotePendingDeliveries(
	sessionID schema.SessionID,
	promotion *schema.SessionDelivery,
	step int,
) (int, error) {
	if promotion == nil {
		return step, nil
	}
	cutoff, err := core.EventLatestSequence(r.DB.DB, string(sessionID))
	if err != nil {
		return 0, err
	}
	promoted := 0
	if *promotion == schema.SessionDeliverySteer {
		promoted, err = session.PromoteSteers(r.DB.DB, r.Events, sessionID, int64(cutoff))
	} else {
		var queued bool
		queued, err = session.PromoteNextQueued(r.DB.DB, r.Events, sessionID)
		if err == nil && queued {
			promoted++
		}
		if err == nil {
			var steered int
			steered, err = session.PromoteSteers(r.DB.DB, r.Events, sessionID, int64(cutoff))
			promoted += steered
		}
	}
	if err != nil {
		return 0, err
	}
	if promoted > 0 {
		return 1, nil
	}
	return step, nil
}

// materializeTools resolves the tools for the attempt, or nil on the last step.
func (r *LLMRunner) materializeTools(
	sessionID schema.SessionID,
	agent core.AgentSelection,
	stored *schema.SessionInfo,
	isLastStep bool,
) (*tool.Materialization, error) {
	if isLastStep {
		return nil, nil
	}
	var permissions []core.PermissionRule
	if agent.Info != nil {
		permissions = agent.Info.Permissions
	}
	if stored.Permission != nil {
		permissions = core.PermissionMerge(permissions, *stored.Permission)
	}
	return r.Tools.MaterializeFor(string(sessionID), permissions)
}

func contextMessagesOf(entries []session.HistoryEntry) []schema.SessionMessage {
	result := make([]schema.SessionMessage, len(entries))
	for i, entry := range entries {
		result[i] = entry.Message
	}
	return result
}

// dropErroredTurns removes each turn the provider rejected — the turn's user
// message through the errored assistant message — from the request context. The
// messages stay in the transcript; only the model stops seeing them, so a
// message the provider keeps rejecting cannot wedge the session. Interrupts and
// declined tools are not dropped: they are not provider rejections and must not
// erase the user's prompt.
func dropErroredTurns(messages []schema.SessionMessage) []schema.SessionMessage {
	drop := make([]bool, len(messages))
	turnStart := -1 // index of the current turn's user message, if any
	for i, message := range messages {
		switch m := message.(type) {
		case schema.SessionMessageUser:
			turnStart = i
		case schema.SessionMessageAssistant:
			if m.Error == nil || m.Error.Type != schema.SessionMessageErrorProvider {
				continue
			}
			start := i
			if turnStart >= 0 {
				start = turnStart
			}
			for j := start; j <= i; j++ {
				drop[j] = true
			}
			turnStart = -1
		}
	}
	kept := make([]schema.SessionMessage, 0, len(messages))
	for i, message := range messages {
		if !drop[i] {
			kept = append(kept, message)
		}
	}
	return kept
}

// buildTurnRequest assembles the provider request for one attempt.
func buildTurnRequest(
	sessionID schema.SessionID,
	agent core.AgentSelection,
	stored *schema.SessionInfo,
	system session.ContextEpochPrepared,
	contextMessages []schema.SessionMessage,
	model route.Model,
	policy mediaPolicy,
	isLastStep bool,
	toolMaterialization *tool.Materialization,
) *llmschema.LLMRequest {
	promptCacheKey := string(sessionID)
	if sessionIDShape.MatchString(string(sessionID)) {
		promptCacheKey = string(sessionID)[4:]
	}
	headers := sessionHeaders(sessionID, stored)
	systemParts := make([]llmschema.SystemPart, 0, 2)
	if agent.Info != nil && agent.Info.System != nil && *agent.Info.System != "" {
		systemParts = append(systemParts, llmschema.NewSystemPart(*agent.Info.System))
	}
	if system.Baseline != "" {
		systemParts = append(systemParts, llmschema.NewSystemPart(system.Baseline))
	}
	messages := ToLLMMessages(contextMessages, model, policy)
	if isLastStep {
		messages = append(messages, llmschema.MessageAssistant(MaxStepsPrompt))
	}
	var tools []llmschema.ToolDefinition
	if toolMaterialization != nil {
		tools = toolMaterialization.Definitions
	}
	return llm.NewRequest(llm.RequestInput{
		Model:           model,
		Http:            &llmschema.HttpOptions{Headers: headers},
		ProviderOptions: llmschema.ProviderOptions{"openai": {"promptCacheKey": promptCacheKey}},
		System:          systemParts,
		Messages:        messages,
		Tools:           tools,
		ToolChoice:      toolChoice(isLastStep),
		Metadata:        map[string]any{RequestMetaSessionID: string(sessionID), RequestMetaAgentID: string(agent.ID)},
	})
}

// turnMediaPolicy is mediaPolicyForModel with the omission reporter bound to the
// runner, so every attachment the model cannot carry is published as a durable
// event (once) and the client can perceive it.
func (r *LLMRunner) turnMediaPolicy(
	model route.Model,
	agent core.AgentSelection,
	sessionID schema.SessionID,
) mediaPolicy {
	policy := mediaPolicyForModel(model, agent)
	policy.omit = func(messageID schema.MessageID, name, mime, reason string) {
		r.publishOmittedAttachment(sessionID, messageID, name, mime, reason)
	}
	return policy
}

// publishOmittedAttachment records a durable event for an attachment the model
// could not carry. It publishes once per attachment: a turn rebuilds its request
// per step, and history repeats it on later turns.
func (r *LLMRunner) publishOmittedAttachment(
	sessionID schema.SessionID,
	messageID schema.MessageID,
	name, mime, reason string,
) {
	key := string(messageID) + "\x00" + name + "\x00" + mime
	if !r.omissions.markOnce(key) {
		return
	}
	if r.OnMediaDecision != nil {
		r.OnMediaDecision(schema.MediaDecision{
			SessionID: string(sessionID), MessageID: string(messageID),
			Name: name, MIME: mime, Action: "omit", Reason: reason,
		})
	}
	payload := map[string]any{
		"sessionID": sessionID,
		"messageID": string(messageID),
		"name":      name,
		"mime":      mime,
		"timestamp": time.Now().UnixMilli(),
	}
	if reason != "" {
		payload["reason"] = reason
	}
	if _, err := r.Events.Publish(schema.SessionEventAttachmentOmitted, payload); err != nil {
		r.reportError(err)
	}
}

// mediaPolicyForModel derives the media filter for a turn: a part is kept only
// if the protocol can carry its MIME type, the model accepts its modality, and
// its payload is well-formed. Anything else is omitted from the request
// (replaced by a text note) instead of failing the turn. A remote A2A backend
// carries arbitrary file parts, so it is not filtered by the local protocol.
func mediaPolicyForModel(model route.Model, agent core.AgentSelection) mediaPolicy {
	if info := agent.Info; info != nil && info.Backend != nil && info.Backend.Type == schema.AgentBackendA2A {
		return mediaPolicy{}
	}
	// The registry is keyed by route id; openai-chat and openai-compatible-chat
	// share a protocol id, so the route id is what selects the right descriptor.
	entry, ok := protocols.LookupProtocol(llmschema.ProtocolID(model.Route.ID))
	if !ok {
		return mediaPolicy{}
	}
	var input []string
	var sources []llmschema.MediaSource
	if caps := model.GetCapabilities(); caps != nil {
		input = caps.Input
		sources = caps.Sources
	}
	routeName := model.Route.ID
	return mediaPolicy{reason: func(role, mime, source, data string) string {
		src := llmschema.MediaSource(source)
		if reason := mediaFeasibilityReason(entry.Media, input, sources, role, mime, src); reason != "" {
			return reason
		}
		if src == llmschema.MediaSourceInline {
			if _, err := utils.ValidateMedia(routeName, llmschema.MediaPart{Type: "media", MediaType: mime, Data: data}, entry.Media.MIMEs); err != nil {
				return string(schema.AttachmentOmitInvalidPayload)
			}
		}
		return ""
	}}
}

// mediaFeasibilityReason returns why a protocol descriptor plus a model's
// declared capabilities cannot carry a media part from a source in a role, or ""
// when it can. It checks MIME support, the model's modality and source
// constraints, and the protocol's role constraint, but not the payload. It is
// the single feasibility predicate shared by the turn planner and the
// inline-to-file adaptation loop.
func mediaFeasibilityReason(support protocols.ProtocolMediaSupport, modelInput []string, modelSources []llmschema.MediaSource, role, mime string, source llmschema.MediaSource) string {
	lower := strings.ToLower(mime)
	if !support.AllowsMIME(lower) {
		return string(schema.AttachmentOmitUnsupportedType)
	}
	if len(modelInput) > 0 && !slices.Contains(modelInput, utils.MediaModality(lower)) {
		return string(schema.AttachmentOmitUnsupportedModality)
	}
	if !support.AllowsSource(source) {
		return string(schema.AttachmentOmitUnsupportedSource)
	}
	if len(modelSources) > 0 && !slices.Contains(modelSources, source) {
		return string(schema.AttachmentOmitUnsupportedSource)
	}
	if !support.AllowsRole(role) {
		return string(schema.AttachmentOmitUnsupportedRole)
	}
	return ""
}

// adaptMedia rewrites inline attachments to provider file references when the
// target model/protocol cannot carry them inline but can carry a file reference,
// using the host MediaUploader. Best-effort: on any error the part is left
// unchanged and the planner omits it.
func (r *LLMRunner) adaptMedia(ctx context.Context, sessionID schema.SessionID, model route.Model, messages []schema.SessionMessage) []schema.SessionMessage {
	if r.MediaUploader == nil {
		return messages
	}
	entry, ok := protocols.LookupProtocol(llmschema.ProtocolID(model.Route.ID))
	if !ok {
		return messages
	}
	provider := schema.ProviderID(model.ProviderID())
	uploader := r.MediaUploader(provider)
	if uploader == nil {
		return messages
	}
	var input []string
	var sources []llmschema.MediaSource
	if caps := model.GetCapabilities(); caps != nil {
		input = caps.Input
		sources = caps.Sources
	}
	adapter := mediaAdapter{runner: r, entry: entry, provider: provider, input: input, sources: sources, uploader: uploader}
	for i := range messages {
		if user, ok := messages[i].(schema.SessionMessageUser); ok && len(user.Files) > 0 {
			messages[i] = adapter.adaptUserMessage(ctx, sessionID, user)
		}
	}
	return messages
}

// mediaAdapter carries the per-turn invariants the inline-to-file adaptation
// needs, so each attachment is handled without re-deriving them.
type mediaAdapter struct {
	runner   *LLMRunner
	entry    protocols.ProtocolEntry
	provider schema.ProviderID
	input    []string
	sources  []llmschema.MediaSource
	uploader schema.MediaUploader
}

// adaptUserMessage rewrites a user message's inline attachments to file
// references where needed and persists the result, so a rebuilt turn or a
// restart reuses it instead of re-uploading.
func (a mediaAdapter) adaptUserMessage(ctx context.Context, sessionID schema.SessionID, user schema.SessionMessageUser) schema.SessionMessageUser {
	files := make([]schema.PromptFileAttachment, len(user.Files))
	copy(files, user.Files)
	changed := false
	for i := range files {
		adapted := a.adapt(ctx, sessionID, files[i])
		if adapted.FileID != files[i].FileID || adapted.MediaSource != files[i].MediaSource || adapted.URI != files[i].URI {
			changed = true
		}
		files[i] = adapted
	}
	if !changed {
		return user
	}
	user.Files = files
	if err := session.PersistMessage(a.runner.DB, sessionID, user); err != nil {
		a.runner.reportError(err)
	}
	return user
}

// adapt uploads an infeasible inline attachment and rewrites it as a file
// reference, or returns it unchanged. Uploads are deduplicated by content hash;
// a cached ref is reused until it expires.
func (a mediaAdapter) adapt(ctx context.Context, sessionID schema.SessionID, file schema.PromptFileAttachment) schema.PromptFileAttachment {
	source := attachmentSource(file.MediaSource, file.URI, file.FileID)
	if source != llmschema.MediaSourceInline {
		return file
	}
	if mediaFeasibilityReason(a.entry.Media, a.input, a.sources, "user", file.Mime, source) == "" {
		return file
	}
	if mediaFeasibilityReason(a.entry.Media, a.input, a.sources, "user", file.Mime, llmschema.MediaSourceFile) != "" {
		return file
	}
	data, err := inlineBytes(file.URI)
	if err != nil {
		return file
	}
	key := mediaCacheKey(a.provider, file.Mime, data)
	if ref, ok := a.runner.cachedUpload(key); ok {
		return fileWithRef(file, ref)
	}
	ref, err := a.uploader.Upload(ctx, schema.MediaUpload{Mime: file.Mime, Name: file.Name, Data: data})
	if err != nil {
		a.runner.reportError(err)
		return file
	}
	a.runner.storeUpload(key, ref)
	if a.runner.OnMediaDecision != nil {
		name := ""
		if file.Name != nil {
			name = *file.Name
		}
		a.runner.OnMediaDecision(schema.MediaDecision{
			SessionID: string(sessionID), Name: name, MIME: file.Mime, Action: "upload",
		})
	}
	return fileWithRef(file, ref)
}

// fileWithRef rewrites an inline attachment to reference an uploaded file. The
// resolved source comes from the returned MediaRef (a provider may return a file
// id or a URI); an empty source defaults to "file".
func fileWithRef(file schema.PromptFileAttachment, ref schema.MediaRef) schema.PromptFileAttachment {
	source := ref.Source
	if source == "" {
		source = string(llmschema.MediaSourceFile)
	}
	file.MediaSource = source
	file.URI = ""
	file.FileID = ref.ID
	if source == string(llmschema.MediaSourceURL) {
		file.URI = ref.ID
		file.FileID = ""
	}
	return file
}

// cachedUpload returns a cached, unexpired upload for a content key.
func (r *LLMRunner) cachedUpload(key string) (schema.MediaRef, bool) {
	if r.media == nil {
		return schema.MediaRef{}, false
	}
	r.media.mu.Lock()
	defer r.media.mu.Unlock()
	ref, ok := r.media.index[key]
	if !ok {
		return schema.MediaRef{}, false
	}
	if ref.ExpiresAt != nil && ref.ExpiresAt.Before(time.Now()) {
		delete(r.media.index, key)
		return schema.MediaRef{}, false
	}
	return ref, true
}

func (r *LLMRunner) storeUpload(key string, ref schema.MediaRef) {
	if r.media == nil {
		r.media = &mediaUploadCache{}
	}
	r.media.mu.Lock()
	defer r.media.mu.Unlock()
	if r.media.index == nil {
		r.media.index = map[string]schema.MediaRef{}
	}
	if _, exists := r.media.index[key]; !exists {
		r.media.order = append(r.media.order, key)
	}
	r.media.index[key] = ref
	// FIFO-evict the oldest entries once the cap is exceeded.
	for len(r.media.order) > maxMediaCacheEntries {
		oldest := r.media.order[0]
		r.media.order = r.media.order[1:]
		delete(r.media.index, oldest)
	}
}

// mediaCacheKey keys an upload by provider and content hash.
func mediaCacheKey(provider schema.ProviderID, mime string, data []byte) string {
	sum := sha256.Sum256(append([]byte(mime+"\x00"), data...))
	return string(provider) + "\x00" + hex.EncodeToString(sum[:])
}

// inlineBytes decodes an inline payload: a data URL or a bare base64 string.
func inlineBytes(uri string) ([]byte, error) {
	if strings.HasPrefix(uri, "data:") {
		i := strings.Index(uri, ",")
		if i < 0 {
			return nil, fmt.Errorf("invalid data URL")
		}
		uri = uri[i+1:]
	}
	return base64.StdEncoding.DecodeString(uri)
}

// sessionHeaders carries the per-session routing headers every provider request
// for the session shares.
func sessionHeaders(sessionID schema.SessionID, stored *schema.SessionInfo) map[string]string {
	headers := map[string]string{
		"x-session-affinity": string(sessionID),
		"X-Session-Id":       string(sessionID),
	}
	if stored.ParentID != nil {
		headers["x-parent-session-id"] = string(*stored.ParentID)
	}
	return headers
}

// llmClient returns the LLM client for this turn's agent. Remote-backend agents
// (for example A2A) can supply their own client; the default is used otherwise.
func (a *turnAttempt) llmClient() route.LLMClient {
	if a.runner.Backends != nil && a.agent.Info != nil {
		if client := a.runner.Backends(a.agent.Info); client != nil {
			return client
		}
	}
	return a.runner.LLM
}

// consumeStream publishes the provider stream and settles each tool call.
func (a *turnAttempt) consumeStream() {
	var toolWait sync.WaitGroup
	stream, streamErr := a.llmClient().Stream(a.ctx, a.request)
	if streamErr == nil {
		for event, err := range stream {
			if err != nil {
				streamErr = err
				break
			}
			if handleErr, stop := a.handleStreamEvent(event, &toolWait); stop {
				streamErr = handleErr
				break
			}
		}
	}
	flushErr := a.withPublication(func() error { return a.publisher.Flush() })
	if streamErr != nil && isInterrupt(streamErr) {
		a.toolCancel()
	}
	toolWait.Wait()
	if streamErr == nil && flushErr != nil {
		streamErr = flushErr
	}
	a.streamErr = streamErr
}

// handleStreamEvent publishes one streamed event and dispatches tool calls. It
// reports whether to stop consuming, with the publication error when one
// occurred.
func (a *turnAttempt) handleStreamEvent(event llmschema.LLMEvent, toolWait *sync.WaitGroup) (error, bool) {
	if a.overflowFailure != nil || a.publisher.HasProviderError() {
		return nil, false
	}
	if event.IsProviderError() && llm.IsContextOverflow(event.Message) && !a.publisher.HasAssistantStarted() {
		captured := event
		a.overflowFailure = &captured
		return nil, false
	}
	if err := a.withPublication(func() error { return a.publisher.Publish(event, nil) }); err != nil {
		return err, true
	}
	if !event.IsToolCall() || (event.ProviderExecuted != nil && *event.ProviderExecuted) {
		return nil, false
	}
	if a.toolMaterialization == nil {
		if err := a.withPublication(func() error {
			return a.publisher.FailUnsettledTools("Tools are disabled after the maximum agent steps", false)
		}); err != nil {
			return err, true
		}
		return nil, false
	}
	a.needsContinuation = true
	var assistantMessageID schema.MessageID
	if err := a.withPublication(func() error {
		var lookupErr error
		assistantMessageID, lookupErr = a.publisher.AssistantMessageID(event.ID)
		return lookupErr
	}); err != nil {
		return err, true
	}
	toolWait.Add(1)
	go a.runTool(toolWait, event, assistantMessageID)
	return nil, false
}

// runTool executes one tool call and publishes its result.
func (a *turnAttempt) runTool(wait *sync.WaitGroup, call llmschema.LLMEvent, assistantMessageID schema.MessageID) {
	defer wait.Done()
	settlement, err := a.toolMaterialization.Settle(a.toolCtx, tool.ExecuteInput{
		SessionID:          string(a.sessionID),
		Agent:              string(a.agent.ID),
		AssistantMessageID: string(assistantMessageID),
		Call:               toolCallPart(call),
		Progress: func(structured map[string]any, content []tool.Content) error {
			return a.withPublication(func() error {
				_, pubErr := a.runner.Events.Publish(schema.SessionEventToolProgress, map[string]any{
					"sessionID":          string(a.sessionID),
					"timestamp":          time.Now().UnixMilli(),
					"assistantMessageID": string(assistantMessageID),
					"callID":             call.ID,
					"structured":         structured,
					"content":            tool.ToToolContent(content),
				})
				return pubErr
			})
		},
	})
	if err != nil {
		a.recordToolError(err)
		return
	}
	outputPaths := settlement.OutputPaths
	if outputPaths == nil {
		outputPaths = []string{}
	}
	if err := a.withPublication(func() error {
		return a.publisher.Publish(
			llmschema.NewToolResult(call.ID, call.Name, settlement.Result, llmschema.WithEventOutput(settlement.Output)),
			outputPaths,
		)
	}); err != nil {
		a.recordToolError(err)
	}
}

func (a *turnAttempt) recordToolError(err error) {
	a.toolErrMu.Lock()
	a.toolErrs = append(a.toolErrs, err)
	a.toolErrMu.Unlock()
}

func (a *turnAttempt) withPublication(fn func() error) error {
	return a.guard.run(fn)
}

// settle interprets the stream and tool outcomes, publishing failures and the
// step end, and reports whether the turn continues.
func (a *turnAttempt) settle() (turnResult, error) {
	streamErr := a.streamErr
	recovered, err := a.tryOverflowRecovery()
	if err != nil {
		return turnResult{}, err
	}
	if recovered {
		return turnResult{}, &turnTransitionError{
			Transition: turnTransition{Tag: "ContinueAfterOverflowCompaction", Step: a.step},
		}
	}
	if a.overflowFailure != nil {
		if err := a.withPublication(func() error { return a.publisher.Publish(*a.overflowFailure, nil) }); err != nil {
			return turnResult{}, err
		}
	}

	if err := a.handleProviderFailure(streamErr); err != nil {
		return turnResult{}, err
	}

	collected := a.collectToolErrors()
	settledErr := firstError(collected)

	// A user decline anywhere in the batch halts the turn, even when another tool
	// failed first.
	if declined, err := a.declineError(collected); err != nil {
		return turnResult{}, err
	} else if declined != nil {
		return turnResult{}, declined
	}

	settledInterrupt, err := a.handleInterrupts(collected, streamErr)
	if err != nil {
		return turnResult{}, err
	}
	settledInterrupted := settledInterrupt != nil

	if err := a.handleToolFailure(settledErr); err != nil {
		return turnResult{}, err
	}
	if err := a.finalizeStep(); err != nil {
		return turnResult{}, err
	}
	if err := a.finishSettlement(streamErr); err != nil {
		return turnResult{}, err
	}

	if streamErr != nil {
		return turnResult{}, streamErr
	}
	if settledInterrupted {
		return turnResult{}, settledInterrupt
	}
	return turnResult{NeedsContinuation: !a.publisher.HasProviderError() && a.needsContinuation, Step: a.step}, nil
}

// tryOverflowRecovery attempts post-overflow compaction, reporting whether it
// recovered so the caller can retry the attempt.
func (a *turnAttempt) tryOverflowRecovery() (bool, error) {
	if a.recoverOverflow == nil || a.publisher.HasAssistantStarted() ||
		!overflowContextFailure(a.overflowFailure, a.streamErr) {
		return false, nil
	}
	return a.recoverOverflow(session.CompactionInput{
		SessionID: a.sessionID, Entries: compactionEntries(a.entries), Model: a.model, Request: *a.request, Context: a.ctx,
	})
}

// handleProviderFailure reports an LLM error as a failed assistant turn unless
// the provider already reported it.
func (a *turnAttempt) handleProviderFailure(streamErr error) error {
	var llmFailure *llmschema.LLMError
	if streamErr != nil {
		var target *llmschema.LLMError
		if errors.As(streamErr, &target) {
			llmFailure = target
		}
	}
	if llmFailure == nil || a.publisher.HasProviderError() {
		return nil
	}
	if err := a.withPublication(func() error {
		return a.publisher.FailUnsettledTools("Provider did not return a tool result", true)
	}); err != nil {
		return err
	}
	return a.withPublication(func() error { return a.publisher.FailAssistantProvider(llmFailure.Reason.Message) })
}

func (a *turnAttempt) collectToolErrors() []error {
	a.toolErrMu.Lock()
	defer a.toolErrMu.Unlock()
	return append([]error(nil), a.toolErrs...)
}

func firstError(errs []error) error {
	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}

// declineError settles a user decline and returns it, or nil when there is none.
func (a *turnAttempt) declineError(collected []error) (error, error) {
	declined := firstDeclined(collected)
	if declined == nil {
		return nil, nil
	}
	a.toolCancel()
	if err := a.withPublication(func() error {
		return a.publisher.FailUnsettledTools("Tool execution declined", false)
	}); err != nil {
		return nil, err
	}
	// Surface the actual decline (e.g. "permission declined" / "The user
	// dismissed this question") instead of a generic interrupt.
	return declined, nil
}

// handleInterrupts settles an interrupted stream or tool batch and returns the
// interrupt that ended it.
func (a *turnAttempt) handleInterrupts(collected []error, streamErr error) (error, error) {
	streamInterrupted := streamErr != nil && isInterrupt(streamErr)
	var settledInterrupt error
	for _, err := range collected {
		if isInterrupt(err) {
			settledInterrupt = err
			break
		}
	}
	if !streamInterrupted && settledInterrupt == nil {
		return nil, nil
	}
	a.toolCancel()
	if err := a.withPublication(func() error {
		return a.publisher.FailUnsettledTools("Tool execution interrupted", false)
	}); err != nil {
		return nil, err
	}
	if a.publisher.HasActiveAssistant() {
		if err := a.withPublication(
			func() error { return a.publisher.FailAssistant("Provider turn interrupted") },
		); err != nil {
			return nil, err
		}
	}
	return settledInterrupt, nil
}

func (a *turnAttempt) handleToolFailure(settledErr error) error {
	if settledErr == nil || isInterrupt(settledErr) {
		return nil
	}
	return a.withPublication(func() error {
		return a.publisher.FailUnsettledTools("Tool execution failed: "+settledErr.Error(), false)
	})
}

// finalizeStep publishes the step end with its snapshot and file changes.
func (a *turnAttempt) finalizeStep() error {
	settlement := a.publisher.StepSettlement()
	if settlement == nil || a.publisher.HasProviderError() {
		return nil
	}
	endSnapshot, err := a.runner.Snapshots.Capture()
	if err != nil {
		return err
	}
	var files []schema.RelativePath
	if a.startSnapshot != nil && endSnapshot != nil {
		files, _ = a.runner.Snapshots.Files(core.SnapshotCompareInput{From: *a.startSnapshot, To: *endSnapshot})
	}
	var endSnapshotValue *string
	if endSnapshot != nil {
		value := string(*endSnapshot)
		endSnapshotValue = &value
	}
	assistantMessageID, err := a.publisher.StartAssistant()
	if err != nil {
		return err
	}
	return a.withPublication(func() error {
		_, err := a.runner.Events.Publish(schema.SessionEventStepEnded, map[string]any{
			"sessionID":          a.sessionID,
			"timestamp":          time.Now().UnixMilli(),
			"assistantMessageID": assistantMessageID,
			"finish":             settlement.Finish,
			"cost":               0,
			"tokens":             settlement.Tokens,
			"snapshot":           endSnapshotValue,
			"files":              files,
		})
		return err
	})
}

// finishSettlement fails any tools left unsettled by the provider.
func (a *turnAttempt) finishSettlement(streamErr error) error {
	if a.publisher.HasProviderError() {
		if err := a.withPublication(func() error {
			return a.publisher.FailUnsettledTools("Tool execution interrupted", false)
		}); err != nil {
			return err
		}
	}
	if streamErr == nil && !a.publisher.HasProviderError() {
		return a.withPublication(func() error {
			return a.publisher.FailUnsettledTools("Provider did not return a tool result", true)
		})
	}
	return nil
}

// isUserDeclined reports whether the error is a declined permission prompt or a
// rejected question, which halt the loop instead of becoming model output.
func isUserDeclined(err error) bool {
	var declined *core.PermissionDeclinedError
	var rejected *core.QuestionRejectedError
	return errors.As(err, &declined) || errors.As(err, &rejected)
}

// firstDeclined returns the first user decline among errs, or nil.
func firstDeclined(errs []error) error {
	for _, err := range errs {
		if isUserDeclined(err) {
			return err
		}
	}
	return nil
}

type publishGuard struct {
	mu sync.Mutex
}

func (g *publishGuard) run(fn func() error) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	return fn()
}

func toolChoice(isLastStep bool) any {
	if isLastStep {
		return llmschema.ToolChoiceNone
	}
	return nil
}

func toolCallPart(event llmschema.LLMEvent) llmschema.ToolCallPart {
	return llmschema.ToolCallPart{
		Type:             "tool-call",
		ID:               event.ID,
		Name:             event.Name,
		Input:            event.Input,
		ProviderExecuted: event.ProviderExecuted,
		ProviderMetadata: event.ProviderMetadata,
	}
}

func compactionEntries(entries []session.HistoryEntry) []session.CompactionEntry {
	result := make([]session.CompactionEntry, len(entries))
	for i, entry := range entries {
		result[i] = session.CompactionEntry(entry)
	}
	return result
}

func variantOf(session schema.SessionInfo) *schema.ModelVariantID {
	if session.Model == nil {
		return nil
	}
	return session.Model.Variant
}

func sameWorkspace(left, right *schema.WorkspaceID) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
}

func overflowContextFailure(event *llmschema.LLMEvent, err error) bool {
	if event != nil && llm.IsContextOverflow(event.Message) {
		return true
	}
	return err != nil && llm.IsContextOverflowFailure(err)
}
