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

// AgentSelector is the narrow AgentV2 surface the runner needs.
type AgentSelector interface {
	Select(id *string) core.AgentSelection
}

// Snapshotter is the narrow Snapshot surface the runner needs.
type Snapshotter interface {
	Capture() (*core.SnapshotID, error)
	Files(input core.SnapshotCompareInput) ([]schema.RelativePath, error)
}

var (
	_ AgentSelector = (*core.Agent)(nil)
	_ Snapshotter   = (*core.Snapshot)(nil)
)

// ToolRegistry is the narrow tool-registry surface the runner needs. The
// session id lets embedders scope tools per session.
type ToolRegistry interface {
	MaterializeFor(sessionID string, permissions []core.PermissionRule) *tool.Materialization
}

// LLMRunner mirrors SessionRunner.layer: it runs one durable coding-agent
// Session until it settles.
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

// errInterrupted mirrors Effect.interrupt: it wraps context.Canceled so callers
// can distinguish an interrupted turn from a failed one via errors.Is.
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

// Run mirrors SessionRunner.run.
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

	var promotion *schema.SessionDelivery
	if hasSteer {
		value := schema.SessionDeliverySteer
		promotion = &value
	} else if hasQueue {
		value := schema.SessionDeliveryQueue
		promotion = &value
	}
	shouldRun := input.Force || hasSteer || hasQueue
	for shouldRun {
		needsContinuation := true
		step := 1
		for needsContinuation {
			result, err := r.runTurn(ctx, input.SessionID, promotion, step)
			if err != nil {
				return err
			}
			needsContinuation = result.NeedsContinuation
			step = result.Step + 1
			value := schema.SessionDeliverySteer
			promotion = &value
			if !needsContinuation {
				needsContinuation, err = session.HasPending(r.DB.DB, input.SessionID, schema.SessionDeliverySteer)
				if err != nil {
					return err
				}
			}
		}
		shouldRun, err = session.HasPending(r.DB.DB, input.SessionID, schema.SessionDeliveryQueue)
		if err != nil {
			return err
		}
		if shouldRun {
			value := schema.SessionDeliveryQueue
			promotion = &value
		} else {
			promotion = nil
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

func (r *LLMRunner) runTurnAttempt(
	ctx context.Context,
	sessionID schema.SessionID,
	promotion *schema.SessionDelivery,
	step int,
	recoverOverflow func(session.CompactionInput) (bool, error),
) (turnResult, error) {
	stored, err := r.Store.Get(sessionID)
	if err != nil {
		return turnResult{}, err
	}
	if stored == nil {
		return turnResult{}, fmt.Errorf("Session not found: %s", sessionID)
	}
	if string(stored.Location.Directory) != string(r.Location.Directory) || !sameWorkspace(stored.Location.WorkspaceID, r.Location.WorkspaceID) {
		return turnResult{}, errInterrupted
	}

	var agentID *string
	if stored.Agent != nil {
		value := string(*stored.Agent)
		agentID = &value
	}
	agent := r.Agents.Select(agentID)

	systemContext, err := r.loadSystemContext(agent)
	if err != nil {
		return turnResult{}, err
	}
	initialized, err := session.ContextEpochInitialize(r.DB.DB, systemContext, sessionID)
	if err != nil {
		return turnResult{}, err
	}

	needsContinuation := false
	currentStep := step
	if promotion != nil {
		cutoff, err := core.EventLatestSequence(r.DB.DB, string(sessionID))
		if err != nil {
			return turnResult{}, err
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
			return turnResult{}, err
		}
		if promoted > 0 {
			currentStep = 1
		}
	}

	var system session.ContextEpochPrepared
	if initialized != nil {
		system = *initialized
	} else {
		systemContext, err = r.loadSystemContext(agent)
		if err != nil {
			return turnResult{}, err
		}
		system, err = session.ContextEpochPrepare(r.DB.DB, r.Events, systemContext, sessionID)
		if err != nil {
			return turnResult{}, err
		}
	}

	model, err := r.Models(*stored)
	if err != nil {
		return turnResult{}, err
	}
	entries, err := session.EntriesForRunner(r.DB, sessionID, system.BaselineSeq)
	if err != nil {
		return turnResult{}, err
	}
	contextMessages := make([]schema.SessionMessage, len(entries))
	for i, entry := range entries {
		contextMessages[i] = entry.Message
	}

	isLastStep := agent.Info != nil && agent.Info.Steps != nil && currentStep >= int(*agent.Info.Steps)
	var toolMaterialization *tool.Materialization
	if !isLastStep {
		var permissions []core.PermissionRule
		if agent.Info != nil {
			permissions = agent.Info.Permissions
		}
		if stored.Permission != nil {
			permissions = core.PermissionMerge(permissions, *stored.Permission)
		}
		toolMaterialization = r.Tools.MaterializeFor(string(sessionID), permissions)
	}

	promptCacheKey := string(sessionID)
	if sessionIDShape.MatchString(string(sessionID)) {
		promptCacheKey = string(sessionID)[4:]
	}
	headers := map[string]string{
		"x-session-affinity": string(sessionID),
		"X-Session-Id":       string(sessionID),
	}
	if stored.ParentID != nil {
		headers["x-parent-session-id"] = string(*stored.ParentID)
	}
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
	request := llm.NewRequest(llm.RequestInput{
		Model:           model,
		Http:            &llmschema.HttpOptions{Headers: headers},
		ProviderOptions: llmschema.ProviderOptions{"openai": {"promptCacheKey": promptCacheKey}},
		System:          systemParts,
		Messages:        messages,
		Tools:           tools,
		ToolChoice:      toolChoice(isLastStep),
	})

	if compacted, err := r.compaction.CompactIfNeeded(session.CompactionInput{
		SessionID: sessionID, Entries: compactionEntries(entries), Model: model, Request: *request, Context: ctx,
	}); err != nil {
		return turnResult{}, err
	} else if compacted {
		return turnResult{}, &turnTransitionError{Transition: turnTransition{Tag: "ContinueAfterCompaction", Step: currentStep}}
	}

	startSnapshot, err := r.Snapshots.Capture()
	if err != nil {
		return turnResult{}, err
	}
	var snapshot *string
	if startSnapshot != nil {
		value := string(*startSnapshot)
		snapshot = &value
	}
	publisher := NewLLMEventPublisher(r.Events, LLMEventPublisherInput{
		SessionID: sessionID,
		Agent:     string(agent.ID),
		Model:     schema.ModelRef{ID: schema.ModelID(model.ID), ProviderID: schema.ProviderID(model.Provider), Variant: variantOf(*stored)},
		Snapshot:  snapshot,
	})

	guard := &publishGuard{}
	withPublication := func(fn func() error) error { return guard.run(fn) }
	toolCtx, toolCancel := context.WithCancel(ctx)
	defer toolCancel()

	var overflowFailure *llmschema.LLMEvent
	stream, streamErr := r.LLM.Stream(ctx, request)
	var toolWait sync.WaitGroup
	var toolErrs []error
	var toolErrMu sync.Mutex
	if streamErr == nil {
		for event, err := range stream {
			if err != nil {
				streamErr = err
				break
			}
			if overflowFailure != nil || publisher.HasProviderError() {
				continue
			}
			if event.IsProviderError() {
				if llm.IsContextOverflow(event.Message) && !publisher.HasAssistantStarted() {
					captured := event
					overflowFailure = &captured
					continue
				}
			}
			if err := withPublication(func() error { return publisher.Publish(event, nil) }); err != nil {
				streamErr = err
				break
			}
			if !event.IsToolCall() || (event.ProviderExecuted != nil && *event.ProviderExecuted) {
				continue
			}
			if toolMaterialization == nil {
				if err := withPublication(func() error {
					return publisher.FailUnsettledTools("Tools are disabled after the maximum agent steps", false)
				}); err != nil {
					streamErr = err
					break
				}
				continue
			}
			needsContinuation = true
			var assistantMessageID schema.MessageID
			if err := withPublication(func() error {
				var lookupErr error
				assistantMessageID, lookupErr = publisher.AssistantMessageID(event.ID)
				return lookupErr
			}); err != nil {
				streamErr = err
				break
			}
			toolWait.Add(1)
			go func(call llmschema.LLMEvent, assistantMessageID schema.MessageID) {
				defer toolWait.Done()
				settlement, err := toolMaterialization.Settle(toolCtx, tool.ExecuteInput{
					SessionID:          string(sessionID),
					Agent:              string(agent.ID),
					AssistantMessageID: string(assistantMessageID),
					Call:               toolCallPart(call),
					Progress: func(structured map[string]any, content []tool.Content) error {
						return withPublication(func() error {
							_, pubErr := r.Events.Publish(schema.SessionEventToolProgress, map[string]any{
								"sessionID":          string(sessionID),
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
					toolErrMu.Lock()
					toolErrs = append(toolErrs, err)
					toolErrMu.Unlock()
					return
				}
				outputPaths := settlement.OutputPaths
				if outputPaths == nil {
					outputPaths = []string{}
				}
				if err := withPublication(func() error {
					return publisher.Publish(
						llmschema.NewToolResult(call.ID, call.Name, settlement.Result, llmschema.WithEventOutput(settlement.Output)),
						outputPaths,
					)
				}); err != nil {
					toolErrMu.Lock()
					toolErrs = append(toolErrs, err)
					toolErrMu.Unlock()
				}
			}(event, assistantMessageID)
		}
	}
	flushErr := withPublication(func() error { return publisher.Flush() })
	if streamErr != nil && isInterrupt(streamErr) {
		toolCancel()
	}
	toolWait.Wait()
	if streamErr == nil && flushErr != nil {
		streamErr = flushErr
	}

	if recoverOverflow != nil && !publisher.HasAssistantStarted() &&
		overflowContextFailure(overflowFailure, streamErr) {
		recovered, recoverErr := recoverOverflow(session.CompactionInput{
			SessionID: sessionID, Entries: compactionEntries(entries), Model: model, Request: *request, Context: ctx,
		})
		if recoverErr != nil {
			return turnResult{}, recoverErr
		}
		if recovered {
			return turnResult{}, &turnTransitionError{Transition: turnTransition{Tag: "ContinueAfterOverflowCompaction", Step: currentStep}}
		}
	}
	if overflowFailure != nil {
		if err := withPublication(func() error { return publisher.Publish(*overflowFailure, nil) }); err != nil {
			return turnResult{}, err
		}
	}

	var llmFailure *llmschema.LLMError
	if streamErr != nil {
		var target *llmschema.LLMError
		if errors.As(streamErr, &target) {
			llmFailure = target
		}
	}
	if llmFailure != nil && !publisher.HasProviderError() {
		if err := withPublication(func() error {
			return publisher.FailUnsettledTools("Provider did not return a tool result", true)
		}); err != nil {
			return turnResult{}, err
		}
		if err := withPublication(func() error { return publisher.FailAssistant(llmFailure.Reason.Message) }); err != nil {
			return turnResult{}, err
		}
	}

	toolErrMu.Lock()
	collected := append([]error(nil), toolErrs...)
	toolErrMu.Unlock()
	var settledErr error
	if len(collected) > 0 {
		settledErr = collected[0]
	}

	// A user decline anywhere in the batch halts the turn (TS inspects every
	// fiber's cause), even when another tool failed first.
	if declined := firstDeclined(collected); declined != nil {
		toolCancel()
		if err := withPublication(func() error {
			return publisher.FailUnsettledTools("Tool execution declined", false)
		}); err != nil {
			return turnResult{}, err
		}
		// Surface the actual decline (e.g. "permission declined" / "The user
		// dismissed this question") instead of a generic interrupt.
		return turnResult{}, declined
	}

	streamInterrupted := streamErr != nil && isInterrupt(streamErr)
	var settledInterrupt error
	for _, err := range collected {
		if isInterrupt(err) {
			settledInterrupt = err
			break
		}
	}
	settledInterrupted := settledInterrupt != nil
	if streamInterrupted || settledInterrupted {
		toolCancel()
		if err := withPublication(func() error {
			return publisher.FailUnsettledTools("Tool execution interrupted", false)
		}); err != nil {
			return turnResult{}, err
		}
		if publisher.HasActiveAssistant() {
			if err := withPublication(func() error { return publisher.FailAssistant("Provider turn interrupted") }); err != nil {
				return turnResult{}, err
			}
		}
	}

	if settledErr != nil && !isInterrupt(settledErr) {
		if err := withPublication(func() error {
			return publisher.FailUnsettledTools("Tool execution failed: "+settledErr.Error(), false)
		}); err != nil {
			return turnResult{}, err
		}
	}

	settlement := publisher.StepSettlement()
	if settlement != nil && !publisher.HasProviderError() {
		endSnapshot, err := r.Snapshots.Capture()
		if err != nil {
			return turnResult{}, err
		}
		var files []schema.RelativePath
		if startSnapshot != nil && endSnapshot != nil {
			files, _ = r.Snapshots.Files(core.SnapshotCompareInput{From: *startSnapshot, To: *endSnapshot})
		}
		var endSnapshotValue *string
		if endSnapshot != nil {
			value := string(*endSnapshot)
			endSnapshotValue = &value
		}
		assistantMessageID, err := publisher.StartAssistant()
		if err != nil {
			return turnResult{}, err
		}
		if err := withPublication(func() error {
			_, err := r.Events.Publish(schema.SessionEventStepEnded, map[string]any{
				"sessionID":          sessionID,
				"timestamp":          time.Now().UnixMilli(),
				"assistantMessageID": assistantMessageID,
				"finish":             settlement.Finish,
				"cost":               0,
				"tokens":             settlement.Tokens,
				"snapshot":           endSnapshotValue,
				"files":              files,
			})
			return err
		}); err != nil {
			return turnResult{}, err
		}
	}
	if publisher.HasProviderError() {
		if err := withPublication(func() error {
			return publisher.FailUnsettledTools("Tool execution interrupted", false)
		}); err != nil {
			return turnResult{}, err
		}
	}
	if streamErr == nil && !publisher.HasProviderError() {
		if err := withPublication(func() error {
			return publisher.FailUnsettledTools("Provider did not return a tool result", true)
		}); err != nil {
			return turnResult{}, err
		}
	}
	if streamErr != nil {
		return turnResult{}, streamErr
	}
	if settledInterrupted {
		return turnResult{}, settledInterrupt
	}
	return turnResult{NeedsContinuation: !publisher.HasProviderError() && needsContinuation, Step: currentStep}, nil
}

// isUserDeclined mirrors SessionRunner.isUserDeclined: a declined permission
// prompt or rejected question halts the loop instead of becoming model output.
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
