package runner

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jiaweisky/go-agent-sdk/internal/core/session"
	llmschema "github.com/jiaweisky/go-agent-sdk/pkg/llm/schema"
	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

// LLMEventPublisherInput describes the session a publisher writes for.
type LLMEventPublisherInput struct {
	SessionID schema.SessionID
	Agent     string
	Model     schema.ModelRef
	Snapshot  *string
}

// LLMEventTokenCache is the cache read/write part of a step's tokens.
type LLMEventTokenCache struct {
	Read  int `json:"read"`
	Write int `json:"write"`
}

// LLMEventTokens is a step's token usage, as the provider reported it.
type LLMEventTokens struct {
	Input     int                `json:"input"`
	Output    int                `json:"output"`
	Reasoning int                `json:"reasoning"`
	Cache     LLMEventTokenCache `json:"cache"`
	// ProviderMetadata carries the provider's original usage payload keyed by
	// provider name, passed through verbatim for billing/audit.
	ProviderMetadata map[string]map[string]any `json:"providerMetadata,omitempty"`
}

// LLMEventStepSettlement is the settled result of the last step.
type LLMEventStepSettlement struct {
	Finish string
	Tokens LLMEventTokens
}

type llmToolEntry struct {
	AssistantMessageID schema.MessageID
	Name               string
	InputEnded         bool
	Called             bool
	Settled            bool
	ProviderExecuted   bool
	ProviderMetadata   llmschema.ProviderMetadata
}

// LLMEventPublisher translates streamed LLM events into durable session events.
type LLMEventPublisher struct {
	events session.EventPublisher
	input  LLMEventPublisherInput

	tools              map[string]*llmToolEntry
	toolOrder          []string
	assistantMessageID *schema.MessageID
	assistantActive    bool
	assistantFailed    bool
	providerFailed     bool
	stepSettlement     *LLMEventStepSettlement

	text      *llmFragmentSet
	reasoning *llmFragmentSet
	toolInput *llmFragmentSet
}

// NewLLMEventPublisher builds a publisher for one session.
func NewLLMEventPublisher(events session.EventPublisher, input LLMEventPublisherInput) *LLMEventPublisher {
	p := &LLMEventPublisher{
		events: events,
		input:  input,
		tools:  map[string]*llmToolEntry{},
	}
	p.text = newLLMFragmentSet("text", func(id, value string, _ llmschema.ProviderMetadata) error {
		assistantID, err := p.currentAssistantMessageID()
		if err != nil {
			return err
		}
		_, err = p.events.Publish(schema.SessionEventTextEnded, map[string]any{
			"sessionID":          p.input.SessionID,
			"assistantMessageID": assistantID,
			"timestamp":          time.Now().UnixMilli(),
			"textID":             id,
			"text":               value,
		})
		return err
	})
	p.reasoning = newLLMFragmentSet(
		"reasoning",
		func(id, value string, providerMetadata llmschema.ProviderMetadata) error {
			assistantID, err := p.currentAssistantMessageID()
			if err != nil {
				return err
			}
			data := map[string]any{
				"sessionID":          p.input.SessionID,
				"assistantMessageID": assistantID,
				"timestamp":          time.Now().UnixMilli(),
				"reasoningID":        id,
				"text":               value,
			}
			if providerMetadata != nil {
				data["providerMetadata"] = providerMetadata
			}
			_, err = p.events.Publish(schema.SessionEventReasoningEnded, data)
			return err
		},
	)
	p.toolInput = newLLMFragmentSet("tool input", func(callID, value string, _ llmschema.ProviderMetadata) error {
		tool := p.tools[callID]
		if tool == nil {
			return errors.New("Tool input end before start: " + callID)
		}
		if _, err := p.events.Publish(schema.SessionEventToolInputEnded, map[string]any{
			"sessionID":          p.input.SessionID,
			"timestamp":          time.Now().UnixMilli(),
			"assistantMessageID": tool.AssistantMessageID,
			"callID":             callID,
			"text":               value,
		}); err != nil {
			return err
		}
		tool.InputEnded = true
		return nil
	})
	return p
}

func (p *LLMEventPublisher) currentAssistantMessageID() (schema.MessageID, error) {
	if p.assistantMessageID == nil {
		return "", errors.New("Tool event before assistant step start")
	}
	return *p.assistantMessageID, nil
}

// StartAssistant opens the assistant message a step writes into.
func (p *LLMEventPublisher) StartAssistant() (schema.MessageID, error) {
	if p.assistantMessageID != nil {
		return *p.assistantMessageID, nil
	}
	id := schema.NewMessageID()
	p.assistantMessageID = &id
	p.assistantActive = true
	data := map[string]any{
		"sessionID":          p.input.SessionID,
		"agent":              p.input.Agent,
		"model":              p.input.Model,
		"assistantMessageID": id,
		"timestamp":          time.Now().UnixMilli(),
	}
	if p.input.Snapshot != nil {
		data["snapshot"] = *p.input.Snapshot
	}
	if _, err := p.events.Publish(schema.SessionEventStepStarted, data); err != nil {
		return "", err
	}
	return id, nil
}

// Flush closes the open text and reasoning parts.
func (p *LLMEventPublisher) Flush() error {
	if err := p.text.Flush(); err != nil {
		return err
	}
	if err := p.reasoning.Flush(); err != nil {
		return err
	}
	return p.toolInput.Flush()
}

// FailAssistant marks the assistant message as failed without a provider
// classification — an interrupt or a declined tool. Use FailAssistantProvider
// for a rejection the model itself returned.
func (p *LLMEventPublisher) FailAssistant(message string) error {
	return p.failAssistant(message, "unknown")
}

// FailAssistantProvider marks the assistant message as failed by a provider
// rejection, which DropErroredTurns excludes from later contexts.
func (p *LLMEventPublisher) FailAssistantProvider(message string) error {
	return p.failAssistant(message, schema.SessionMessageErrorProvider)
}

func (p *LLMEventPublisher) failAssistant(message, kind string) error {
	if p.assistantFailed {
		return nil
	}
	if err := p.Flush(); err != nil {
		return err
	}
	assistantID, err := p.StartAssistant()
	if err != nil {
		return err
	}
	p.assistantActive = false
	p.assistantFailed = true
	_, err = p.events.Publish(schema.SessionEventStepFailed, map[string]any{
		"sessionID":          p.input.SessionID,
		"timestamp":          time.Now().UnixMilli(),
		"assistantMessageID": assistantID,
		"error":              schema.SessionMessageUnknownError{Type: kind, Message: message},
	})
	return err
}

// FailUnsettledTools fails every tool call that never settled.
func (p *LLMEventPublisher) FailUnsettledTools(message string, hostedOnly bool) error {
	for _, callID := range p.toolOrder {
		tool := p.tools[callID]
		if tool == nil || tool.Settled {
			continue
		}
		if hostedOnly && !tool.ProviderExecuted {
			continue
		}
		tool.Settled = true
		if _, err := p.events.Publish(schema.SessionEventToolFailed, map[string]any{
			"sessionID":          p.input.SessionID,
			"timestamp":          time.Now().UnixMilli(),
			"assistantMessageID": tool.AssistantMessageID,
			"callID":             callID,
			"error":              schema.SessionMessageUnknownError{Type: "unknown", Message: message},
			"provider":           llmToolProvider(tool.ProviderExecuted, tool.ProviderMetadata),
		}); err != nil {
			return err
		}
	}
	return nil
}

// AssistantMessageID is the assistant message a tool call belongs to.
func (p *LLMEventPublisher) AssistantMessageID(callID string) (schema.MessageID, error) {
	tool := p.tools[callID]
	if tool == nil {
		return "", errors.New("Unknown tool call: " + callID)
	}
	return tool.AssistantMessageID, nil
}

// HasActiveAssistant reports whether an assistant message is open.
func (p *LLMEventPublisher) HasActiveAssistant() bool { return p.assistantActive }

// HasAssistantStarted reports whether an assistant message was opened.
func (p *LLMEventPublisher) HasAssistantStarted() bool { return p.assistantMessageID != nil }

// HasProviderError reports whether the step saw a provider error.
func (p *LLMEventPublisher) HasProviderError() bool { return p.providerFailed }

// StepSettlement is the last step's settlement, or nil.
func (p *LLMEventPublisher) StepSettlement() *LLMEventStepSettlement { return p.stepSettlement }

func (p *LLMEventPublisher) startToolInput(id, name string) error {
	if _, ok := p.tools[id]; ok {
		return errors.New("Duplicate tool input start: " + id)
	}
	assistantID, err := p.StartAssistant()
	if err != nil {
		return err
	}
	p.tools[id] = &llmToolEntry{AssistantMessageID: assistantID, Name: name}
	p.toolOrder = append(p.toolOrder, id)
	if err := p.toolInput.Start(id); err != nil {
		return err
	}
	_, err = p.events.Publish(schema.SessionEventToolInputStarted, map[string]any{
		"sessionID":          p.input.SessionID,
		"timestamp":          time.Now().UnixMilli(),
		"assistantMessageID": assistantID,
		"callID":             id,
		"name":               name,
	})
	return err
}

func (p *LLMEventPublisher) endToolInput(id, name string) error {
	tool := p.tools[id]
	if tool == nil {
		return errors.New("Tool input end before start: " + id)
	}
	if tool.Name != name {
		return fmt.Errorf("Tool input name changed for %s: %s -> %s", id, tool.Name, name)
	}
	if tool.InputEnded {
		return errors.New("Duplicate tool input end: " + id)
	}
	return p.toolInput.End(id, nil)
}

// Publish folds one LLM event into the session's stored state and events. Step
// start and finish events carry no state and are ignored.
func (p *LLMEventPublisher) Publish(event llmschema.LLMEvent, outputPaths []string) error {
	if handler, ok := llmEventHandlers[event.Type]; ok {
		return handler(p, event, outputPaths)
	}
	return nil
}

var llmEventHandlers = map[llmschema.LLMEventType]func(*LLMEventPublisher, llmschema.LLMEvent, []string) error{
	llmschema.EventTextStart:      (*LLMEventPublisher).publishTextStart,
	llmschema.EventTextDelta:      (*LLMEventPublisher).publishTextDelta,
	llmschema.EventTextEnd:        (*LLMEventPublisher).publishTextEnd,
	llmschema.EventReasoningStart: (*LLMEventPublisher).publishReasoningStart,
	llmschema.EventReasoningDelta: (*LLMEventPublisher).publishReasoningDelta,
	llmschema.EventReasoningEnd:   (*LLMEventPublisher).publishReasoningEnd,
	llmschema.EventToolInputStart: (*LLMEventPublisher).publishToolInputStart,
	llmschema.EventToolInputDelta: (*LLMEventPublisher).publishToolInputDelta,
	llmschema.EventToolInputEnd:   (*LLMEventPublisher).publishToolInputEnd,
	llmschema.EventToolCall:       (*LLMEventPublisher).publishToolCall,
	llmschema.EventToolResult:     (*LLMEventPublisher).publishToolResult,
	llmschema.EventToolError:      (*LLMEventPublisher).publishToolError,
	llmschema.EventStepFinish:     (*LLMEventPublisher).publishStepFinish,
	llmschema.EventProviderError:  (*LLMEventPublisher).publishProviderError,
}

func (p *LLMEventPublisher) publishTextStart(event llmschema.LLMEvent, _ []string) error {
	if err := p.text.Start(event.ID); err != nil {
		return err
	}
	assistantID, err := p.StartAssistant()
	if err != nil {
		return err
	}
	_, err = p.events.Publish(schema.SessionEventTextStarted, map[string]any{
		"sessionID":          p.input.SessionID,
		"assistantMessageID": assistantID,
		"timestamp":          time.Now().UnixMilli(),
		"textID":             event.ID,
	})
	return err
}

func (p *LLMEventPublisher) publishTextDelta(event llmschema.LLMEvent, _ []string) error {
	if err := p.text.Append(event.ID, event.Text); err != nil {
		return err
	}
	assistantID, err := p.currentAssistantMessageID()
	if err != nil {
		return err
	}
	_, err = p.events.Publish(schema.SessionEventTextDelta, map[string]any{
		"sessionID":          p.input.SessionID,
		"assistantMessageID": assistantID,
		"timestamp":          time.Now().UnixMilli(),
		"textID":             event.ID,
		"delta":              event.Text,
	})
	return err
}

func (p *LLMEventPublisher) publishTextEnd(event llmschema.LLMEvent, _ []string) error {
	return p.text.End(event.ID, nil)
}

func (p *LLMEventPublisher) publishReasoningStart(event llmschema.LLMEvent, _ []string) error {
	if err := p.reasoning.Start(event.ID); err != nil {
		return err
	}
	assistantID, err := p.StartAssistant()
	if err != nil {
		return err
	}
	data := map[string]any{
		"sessionID":          p.input.SessionID,
		"assistantMessageID": assistantID,
		"timestamp":          time.Now().UnixMilli(),
		"reasoningID":        event.ID,
	}
	if event.ProviderMetadata != nil {
		data["providerMetadata"] = event.ProviderMetadata
	}
	_, err = p.events.Publish(schema.SessionEventReasoningStarted, data)
	return err
}

func (p *LLMEventPublisher) publishReasoningDelta(event llmschema.LLMEvent, _ []string) error {
	if err := p.reasoning.Append(event.ID, event.Text); err != nil {
		return err
	}
	assistantID, err := p.currentAssistantMessageID()
	if err != nil {
		return err
	}
	_, err = p.events.Publish(schema.SessionEventReasoningDelta, map[string]any{
		"sessionID":          p.input.SessionID,
		"assistantMessageID": assistantID,
		"timestamp":          time.Now().UnixMilli(),
		"reasoningID":        event.ID,
		"delta":              event.Text,
	})
	return err
}

func (p *LLMEventPublisher) publishReasoningEnd(event llmschema.LLMEvent, _ []string) error {
	return p.reasoning.End(event.ID, event.ProviderMetadata)
}

func (p *LLMEventPublisher) publishToolInputStart(event llmschema.LLMEvent, _ []string) error {
	return p.startToolInput(event.ID, event.Name)
}

func (p *LLMEventPublisher) publishToolInputDelta(event llmschema.LLMEvent, _ []string) error {
	tool := p.tools[event.ID]
	if tool == nil {
		return errors.New("Tool input delta before start: " + event.ID)
	}
	if tool.Name != event.Name {
		return fmt.Errorf("Tool input name changed for %s: %s -> %s", event.ID, tool.Name, event.Name)
	}
	if tool.InputEnded {
		return errors.New("Tool input delta after end: " + event.ID)
	}
	if err := p.toolInput.Append(event.ID, event.Text); err != nil {
		return err
	}
	_, err := p.events.Publish(schema.SessionEventToolInputDelta, map[string]any{
		"sessionID":          p.input.SessionID,
		"timestamp":          time.Now().UnixMilli(),
		"assistantMessageID": tool.AssistantMessageID,
		"callID":             event.ID,
		"delta":              event.Text,
	})
	return err
}

func (p *LLMEventPublisher) publishToolInputEnd(event llmschema.LLMEvent, _ []string) error {
	return p.endToolInput(event.ID, event.Name)
}

func (p *LLMEventPublisher) publishToolCall(event llmschema.LLMEvent, _ []string) error {
	if _, ok := p.tools[event.ID]; !ok {
		if err := p.startToolInput(event.ID, event.Name); err != nil {
			return err
		}
	}
	tool := p.tools[event.ID]
	if !tool.InputEnded {
		if err := p.endToolInput(event.ID, event.Name); err != nil {
			return err
		}
	}
	if tool.Name != event.Name {
		return fmt.Errorf("Tool call name changed for %s: %s -> %s", event.ID, tool.Name, event.Name)
	}
	if tool.Called {
		return errors.New("Duplicate tool call: " + event.ID)
	}
	tool.Called = true
	tool.ProviderExecuted = event.ProviderExecuted != nil && *event.ProviderExecuted
	tool.ProviderMetadata = event.ProviderMetadata
	_, err := p.events.Publish(schema.SessionEventToolCalled, map[string]any{
		"sessionID":          p.input.SessionID,
		"timestamp":          time.Now().UnixMilli(),
		"assistantMessageID": tool.AssistantMessageID,
		"callID":             event.ID,
		"tool":               event.Name,
		"input":              runnerRecord(event.Input),
		"provider":           llmToolProvider(tool.ProviderExecuted, event.ProviderMetadata),
	})
	return err
}

func (p *LLMEventPublisher) publishToolResult(event llmschema.LLMEvent, outputPaths []string) error {
	tool := p.tools[event.ID]
	if tool == nil || !tool.Called {
		return errors.New("Tool result before call: " + event.ID)
	}
	if tool.Name != event.Name {
		return fmt.Errorf("Tool result name changed for %s: %s -> %s", event.ID, tool.Name, event.Name)
	}
	if tool.Settled {
		if event.Result.Type == "error" {
			return nil
		}
		return errors.New("Duplicate tool result: " + event.ID)
	}
	tool.Settled = true
	result, err := runnerSettledOutput(event.Output, event.Result)
	if err != nil {
		return err
	}
	providerExecuted := (event.ProviderExecuted != nil && *event.ProviderExecuted) || tool.ProviderExecuted
	provider := llmToolProvider(providerExecuted, event.ProviderMetadata)
	if result.Error != nil {
		_, err = p.events.Publish(schema.SessionEventToolFailed, map[string]any{
			"sessionID":          p.input.SessionID,
			"timestamp":          time.Now().UnixMilli(),
			"assistantMessageID": tool.AssistantMessageID,
			"callID":             event.ID,
			"error":              *result.Error,
			"result":             event.Result,
			"provider":           provider,
		})
		return err
	}
	data := map[string]any{
		"sessionID":          p.input.SessionID,
		"timestamp":          time.Now().UnixMilli(),
		"assistantMessageID": tool.AssistantMessageID,
		"callID":             event.ID,
		"structured":         result.Structured,
		"content":            result.Content,
		"outputPaths":        outputPaths,
		"provider":           provider,
	}
	if providerExecuted {
		data["result"] = event.Result
	}
	_, err = p.events.Publish(schema.SessionEventToolSuccess, data)
	return err
}

func (p *LLMEventPublisher) publishToolError(event llmschema.LLMEvent, _ []string) error {
	tool := p.tools[event.ID]
	if tool == nil || !tool.Called {
		return errors.New("Tool error before call: " + event.ID)
	}
	if tool.Name != event.Name {
		return fmt.Errorf("Tool error name changed for %s: %s -> %s", event.ID, tool.Name, event.Name)
	}
	if tool.Settled {
		return errors.New("Duplicate tool error: " + event.ID)
	}
	tool.Settled = true
	_, err := p.events.Publish(schema.SessionEventToolFailed, map[string]any{
		"sessionID":          p.input.SessionID,
		"timestamp":          time.Now().UnixMilli(),
		"assistantMessageID": tool.AssistantMessageID,
		"callID":             event.ID,
		"error":              schema.SessionMessageUnknownError{Type: "unknown", Message: event.Message},
		"provider":           llmToolProvider(tool.ProviderExecuted, event.ProviderMetadata),
	})
	return err
}

func (p *LLMEventPublisher) publishStepFinish(event llmschema.LLMEvent, _ []string) error {
	if err := p.Flush(); err != nil {
		return err
	}
	p.assistantActive = false
	if p.stepSettlement != nil {
		return errors.New("Duplicate step finish")
	}
	p.stepSettlement = &LLMEventStepSettlement{
		Finish: string(event.Reason),
		Tokens: llmTokensFromUsage(event.Usage),
	}
	return nil
}

func (p *LLMEventPublisher) publishProviderError(event llmschema.LLMEvent, _ []string) error {
	p.providerFailed = true
	return p.FailAssistantProvider(event.Message)
}

type llmSettledOutput struct {
	Error      *schema.SessionMessageUnknownError
	Structured map[string]any
	Content    []llmschema.ToolContent
}

func runnerSettledOutput(value *llmschema.ToolOutput, result llmschema.ToolResultValue) (llmSettledOutput, error) {
	if result.Type == "error" {
		return llmSettledOutput{
			Error: &schema.SessionMessageUnknownError{Type: "unknown", Message: runnerMessage(result.Value)},
		}, nil
	}
	settled := value
	if settled == nil {
		settled = llmschema.ToolOutputFromResultValue(result)
	}
	if settled == nil {
		return llmSettledOutput{}, fmt.Errorf("Unsupported tool result: %s", runnerMessage(result))
	}
	return llmSettledOutput{Structured: runnerRecord(settled.Structured), Content: settled.Content}, nil
}

// llmToolProvider is the tool's provider payload: whether it executed, and its
// metadata.
func llmToolProvider(executed bool, metadata llmschema.ProviderMetadata) map[string]any {
	provider := map[string]any{"executed": executed}
	if metadata != nil {
		provider["metadata"] = metadata
	}
	return provider
}

func runnerRecord(value any) map[string]any {
	if object, ok := value.(map[string]any); ok {
		return object
	}
	return map[string]any{"value": value}
}

func runnerMessage(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprint(value)
	}
	return string(data)
}

func llmTokensFromUsage(usage *llmschema.Usage) LLMEventTokens {
	if usage == nil {
		return LLMEventTokens{}
	}
	return LLMEventTokens{
		Input:     llmSafe(usage.NonCachedInputTokens),
		Output:    llmSafe(usage.VisibleOutputTokens()),
		Reasoning: llmSafe(usage.ReasoningTokens),
		Cache: LLMEventTokenCache{
			Read:  llmSafe(usage.CacheReadInputTokens),
			Write: llmSafe(usage.CacheWriteInputTokens),
		},
		ProviderMetadata: usage.ProviderMetadata,
	}
}

func llmSafe(value int) int {
	if value < 0 {
		return 0
	}
	return value
}

// llmFragmentSet accumulates streamed chunks, keyed by id and kept in first-seen
// order.
type llmFragmentSet struct {
	name   string
	chunks map[string][]string
	order  []string
	ended  func(id, value string, providerMetadata llmschema.ProviderMetadata) error
}

func newLLMFragmentSet(
	name string,
	ended func(id, value string, providerMetadata llmschema.ProviderMetadata) error,
) *llmFragmentSet {
	return &llmFragmentSet{name: name, chunks: map[string][]string{}, ended: ended}
}

func (f *llmFragmentSet) Start(id string) error {
	if _, ok := f.chunks[id]; ok {
		return fmt.Errorf("Duplicate %s start: %s", f.name, id)
	}
	f.chunks[id] = []string{}
	f.order = append(f.order, id)
	return nil
}

func (f *llmFragmentSet) Append(id, value string) error {
	current, ok := f.chunks[id]
	if !ok {
		return fmt.Errorf("%s delta before start: %s", f.name, id)
	}
	f.chunks[id] = append(current, value)
	return nil
}

func (f *llmFragmentSet) End(id string, providerMetadata llmschema.ProviderMetadata) error {
	current, ok := f.chunks[id]
	if !ok {
		return fmt.Errorf("%s end before start: %s", f.name, id)
	}
	if err := f.ended(id, strings.Join(current, ""), providerMetadata); err != nil {
		return err
	}
	delete(f.chunks, id)
	for i, value := range f.order {
		if value == id {
			f.order = append(f.order[:i], f.order[i+1:]...)
			break
		}
	}
	return nil
}

func (f *llmFragmentSet) Flush() error {
	for _, id := range append([]string(nil), f.order...) {
		if _, ok := f.chunks[id]; !ok {
			continue
		}
		if err := f.End(id, nil); err != nil {
			return err
		}
	}
	return nil
}
