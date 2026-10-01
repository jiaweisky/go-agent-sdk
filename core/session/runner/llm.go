package runner

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"regexp"
	"sync"
	"time"

	"github.com/user/go-agent-sdk/core"
	"github.com/user/go-agent-sdk/core/database"
	"github.com/user/go-agent-sdk/core/reference"
	"github.com/user/go-agent-sdk/core/session"
	"github.com/user/go-agent-sdk/core/skill"
	"github.com/user/go-agent-sdk/core/systemcontext"
	"github.com/user/go-agent-sdk/core/tool"
	"github.com/user/go-agent-sdk/llm"
	"github.com/user/go-agent-sdk/llm/route"
	llmschema "github.com/user/go-agent-sdk/llm/schema"
	"github.com/user/go-agent-sdk/schema"
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

// LLMRunner runs one durable coding-agent session until it settles.
type LLMRunner struct {
	Events            RunnerEvents
	LLM               route.LLMClient
	Agents            AgentSelector
	Tools             ToolRegistry
	Models            ModelResolver
	Store             *session.Store
	Location          core.Location
	SystemContext     *systemcontext.Registry
	SkillGuidance     *skill.Guidance
	ReferenceGuidance *reference.Guidance
	Compaction        session.CompactionSettings
	Snapshots         Snapshotter
	DB                *database.Database

	compaction *session.Compaction
}

var _ SessionRunner = (*LLMRunner)(nil)

func NewLLMRunner(runner LLMRunner) *LLMRunner {
	result := &runner
	result.compaction = session.NewCompaction(session.CompactionDependencies{
		Events:   runner.Events,
		LLM:      runner.compactionStream,
		Settings: runner.Compaction,
	})
	return result
}

func (r *LLMRunner) compactionStream(ctx context.Context, request llmschema.LLMRequest) iter.Seq2[llmschema.LLMEvent, error] {
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
	return nil
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
func (r *LLMRunner) drainTurns(ctx context.Context, sessionID schema.SessionID, promotion *schema.SessionDelivery) error {
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

func (r *LLMRunner) runTurn(ctx context.Context, sessionID schema.SessionID, promotion *schema.SessionDelivery, step int) (turnResult, error) {
	return r.runTurnLoop(ctx, sessionID, promotion, step, true)
}

func (r *LLMRunner) runTurnLoop(ctx context.Context, sessionID schema.SessionID, promotion *schema.SessionDelivery, step int, recoverOverflow bool) (turnResult, error) {
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
	if string(stored.Location.Directory) != string(r.Location.Directory) || !sameWorkspace(stored.Location.WorkspaceID, r.Location.WorkspaceID) {
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
	request := buildTurnRequest(sessionID, agent, stored, system, contextMessagesOf(entries), model, isLastStep, toolMaterialization)

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
		Model:     schema.ModelRef{ID: schema.ModelID(model.ID), ProviderID: schema.ProviderID(model.Provider), Variant: variant},
		Snapshot:  snapshot,
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
func (r *LLMRunner) initializeSystemContext(agent core.AgentSelection, sessionID schema.SessionID) (*session.ContextEpochPrepared, error) {
	systemContext, err := r.loadSystemContext(agent)
	if err != nil {
		return nil, err
	}
	return session.ContextEpochInitialize(r.DB.DB, systemContext, sessionID)
}

// prepareSystemContext builds a fresh epoch baseline for the attempt.
func (r *LLMRunner) prepareSystemContext(agent core.AgentSelection, sessionID schema.SessionID) (session.ContextEpochPrepared, error) {
	systemContext, err := r.loadSystemContext(agent)
	if err != nil {
		return session.ContextEpochPrepared{}, err
	}
	return session.ContextEpochPrepare(r.DB.DB, r.Events, systemContext, sessionID)
}

// promotePendingDeliveries admits pending steer/queue input, resetting the step
// when anything was promoted.
func (r *LLMRunner) promotePendingDeliveries(sessionID schema.SessionID, promotion *schema.SessionDelivery, step int) (int, error) {
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
func (r *LLMRunner) materializeTools(sessionID schema.SessionID, agent core.AgentSelection, stored *schema.SessionInfo, isLastStep bool) (*tool.Materialization, error) {
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

// buildTurnRequest assembles the provider request for one attempt.
func buildTurnRequest(
	sessionID schema.SessionID,
	agent core.AgentSelection,
	stored *schema.SessionInfo,
	system session.ContextEpochPrepared,
	contextMessages []schema.SessionMessage,
	model route.Model,
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
	messages := ToLLMMessages(contextMessages, model)
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
	})
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

// consumeStream publishes the provider stream and settles each tool call.
func (a *turnAttempt) consumeStream() {
	var toolWait sync.WaitGroup
	stream, streamErr := a.runner.LLM.Stream(a.ctx, a.request)
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
		return turnResult{}, &turnTransitionError{Transition: turnTransition{Tag: "ContinueAfterOverflowCompaction", Step: a.step}}
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
	return a.withPublication(func() error { return a.publisher.FailAssistant(llmFailure.Reason.Message) })
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
		if err := a.withPublication(func() error { return a.publisher.FailAssistant("Provider turn interrupted") }); err != nil {
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
		result[i] = session.CompactionEntry{Seq: entry.Seq, Message: entry.Message}
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
