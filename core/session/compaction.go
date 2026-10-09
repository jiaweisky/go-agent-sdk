package session

import (
	"context"
	"encoding/json"
	"errors"
	"iter"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/jiaweisky/go-agent-sdk/core/util"
	"github.com/jiaweisky/go-agent-sdk/pkg/llm"
	"github.com/jiaweisky/go-agent-sdk/pkg/llm/route"
	llmschema "github.com/jiaweisky/go-agent-sdk/pkg/llm/schema"
	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

const (
	compactionDefaultBuffer       = 20_000
	compactionDefaultKeepTokens   = 8_000
	compactionToolOutputMaxChars  = 2_000
	compactionSummaryOutputTokens = 4_096
)

//nolint:lll // prose prompt template
const compactionSummaryTemplate = `Output exactly the Markdown structure shown inside <template> and keep the section order unchanged. Do not include the <template> tags in your response.
<template>
## Objective
- [one or two brief sentences describing what the user is trying to accomplish]

## Important Details
- [constraints/preferences, decisions and why, important facts/assumptions, exact context needed to continue, or "(none)"]

## Work State
### Completed
- [finished work, verified facts, or changes made; otherwise "(none)"]

### Active
- [current work, partial changes, or investigation state; otherwise "(none)"]

### Blocked
- [blockers, failing commands, or unknowns; otherwise "(none)"]

## Next Move
1. [immediate concrete action, or "(none)"]
2. [next action if known, or "(none)"]

## Relevant Files
- [file or directory path: why it matters, or "(none)"]
</template>

Rules:
- Keep every section, even when empty.
- Use terse bullets, not prose paragraphs.
- Preserve exact file paths, symbols, commands, error strings, URLs, and identifiers when known.
- Do not mention the summary process or that context was compacted.`

//nolint:lll // prose prompt template
const compactionSummaryUpdateInstructions = `The <prior-summary> summarizes everything that happened before the <conversation>. Construct a new summary that combines both. The <prior-summary> is discarded after this: anything you do not carry into the new summary is lost.

When combining:
- Carry forward objectives, constraints, user directives, decisions, and parallel workstreams from the <prior-summary> even when the <conversation> does not mention them. Drop only what is finished and no longer needed.
- The <conversation> is more recent than the <prior-summary>. Where they conflict, the conversation wins: state the corrected fact and drop the old claim.
- Add new progress, decisions, constraints, and context from the conversation.
- Move completed work from "Active" to "Completed".
- If a blocker has been resolved, update the summary to reflect that while keeping any details still needed to continue the work.
- Update "Objective" and "Next Move" to reflect the current work state.`

// CompactionEntry is one entry of the compaction history.
type CompactionEntry struct {
	Seq     int64
	Message schema.SessionMessage
}

// CompactionSettings bounds when and how much history is compacted.
type CompactionSettings struct {
	Auto   bool
	Buffer int
	Tokens int
}

// CompactionDependencies is what compaction needs: the store, the events and the
// settings.
type CompactionDependencies struct {
	Events EventPublisher
	LLM    func(ctx context.Context, request llm.LLMRequest) iter.Seq2[llm.LLMEvent, error]
	// Settings carries the resolved compaction settings.
	Settings CompactionSettings
}

// CompactionInput describes the session whose history may be compacted.
type CompactionInput struct {
	SessionID schema.SessionID
	Entries   []CompactionEntry
	Model     route.Model
	Request   llm.LLMRequest
	// Context carries the caller's cancellation context into the summary
	// provider call. A nil Context behaves as context.Background().
	Context context.Context
}

// CompactionPromptInput is what the compaction prompt is built from.
type CompactionPromptInput struct {
	PreviousSummary string
	Context         []string
}

// Compaction compacts a session's history once it grows past the model's context
// window.
type Compaction struct {
	events EventPublisher
	llm    func(ctx context.Context, request llm.LLMRequest) iter.Seq2[llm.LLMEvent, error]
	config CompactionSettings
}

// NewCompaction builds the compaction service.
func NewCompaction(dependencies CompactionDependencies) *Compaction {
	return &Compaction{
		events: dependencies.Events,
		llm:    dependencies.LLM,
		config: dependencies.Settings,
	}
}

// CompactIfNeeded compacts the history when it exceeds the configured threshold.
func (c *Compaction) CompactIfNeeded(input CompactionInput) (bool, error) {
	if !c.config.Auto {
		return false, nil
	}
	limits := compactionModelLimits(input.Model)
	if limits == nil || limits.Context <= 0 {
		return false, nil
	}
	output := compactionOutput(input.Request, limits)
	estimated := compactionEstimate(compactionEstimateInput{
		System:   input.Request.System,
		Messages: input.Request.Messages,
		Tools:    input.Request.Tools,
	})
	if estimated <= limits.Context-max(output, c.config.Buffer) {
		return false, nil
	}
	return c.CompactAfterOverflow(input)
}

// Compact compacts the history on demand, regardless of the automatic
// threshold. It reports whether anything was compacted.
func (c *Compaction) Compact(input CompactionInput) (bool, error) {
	return c.compact(input, "manual", false)
}

// CompactAfterOverflow compacts the history after the provider reported a context
// overflow.
func (c *Compaction) CompactAfterOverflow(input CompactionInput) (bool, error) {
	return c.compact(input, "auto", true)
}

// compact selects the history to summarize and publishes the summary. It skips
// sessions whose model has no context limit unless requireLimit is false, so
// manual compaction still works when the caller supplied no limits.
func (c *Compaction) compact(input CompactionInput, reason string, requireLimit bool) (bool, error) {
	limits := compactionModelLimits(input.Model)
	contextLimit := 0
	if limits != nil {
		contextLimit = limits.Context
	}
	if contextLimit <= 0 {
		if requireLimit {
			return false, nil
		}
		limits = nil
	}
	output := compactionOutput(input.Request, limits)

	selected, ok := compactionSelect(input.Entries, c.config.Tokens)
	if !ok {
		return false, nil
	}
	previousSummary := compactionPreviousSummary(input.Entries)
	if selected.Head == "" && previousSummary == nil {
		return false, nil
	}

	var priorSummary, priorRecent string
	if previousSummary != nil {
		priorSummary = previousSummary.Summary
		priorRecent = previousSummary.Recent
	}
	var contextParts []string
	if priorRecent != "" {
		contextParts = append(contextParts, priorRecent)
	}
	if selected.Head != "" {
		contextParts = append(contextParts, selected.Head)
	}
	summaryPrompt := BuildCompactionPrompt(CompactionPromptInput{PreviousSummary: priorSummary, Context: contextParts})
	summaryOutput := output
	if summaryOutput == 0 {
		summaryOutput = compactionSummaryOutputTokens
	}
	summaryOutput = min(summaryOutput, compactionSummaryOutputTokens)
	if contextLimit > 0 && util.TokenEstimate(summaryPrompt) > contextLimit-summaryOutput {
		return false, nil
	}

	messageID := schema.NewMessageID()
	if _, err := c.events.Publish(schema.SessionEventCompactionStarted, map[string]any{
		"sessionID": input.SessionID,
		"messageID": messageID,
		"timestamp": time.Now().UnixMilli(),
		"reason":    reason,
	}); err != nil {
		return false, err
	}

	summary, ok, err := c.summarizeCompaction(input, summaryPrompt, summaryOutput)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, nil
	}

	if _, err := c.events.Publish(schema.SessionEventCompactionEnded, map[string]any{
		"sessionID": input.SessionID,
		"messageID": messageID,
		"timestamp": time.Now().UnixMilli(),
		"reason":    reason,
		"text":      summary,
		"recent":    selected.Recent,
	}); err != nil {
		return false, err
	}
	return true, nil
}

func compactionPreviousSummary(entries []CompactionEntry) *schema.SessionMessageCompaction {
	for i := range entries {
		if message, isCompaction := entries[i].Message.(schema.SessionMessageCompaction); isCompaction {
			return &message
		}
	}
	return nil
}

// summarizeCompaction streams the summary prompt and reports whether it
// produced usable text.
func (c *Compaction) summarizeCompaction(
	input CompactionInput,
	summaryPrompt string,
	summaryOutput int,
) (string, bool, error) {
	maxTokens := summaryOutput
	request := llm.NewRequest(llm.RequestInput{
		Model:      input.Model,
		Http:       input.Request.Http,
		Messages:   []llm.Message{llmschema.MessageUser(summaryPrompt)},
		Tools:      []llm.ToolDefinition{},
		Generation: &llm.GenerationOptions{MaxTokens: &maxTokens},
	})

	var chunks []string
	failed := false
	summarized := true
	ctx := input.Context
	if ctx == nil {
		ctx = context.Background()
	}
	for event, err := range c.llm(ctx, *request) {
		if err != nil {
			var llmError *llm.LLMError
			if errors.As(err, &llmError) {
				summarized = false
				break
			}
			return "", false, err
		}
		if event.IsProviderError() {
			failed = true
		}
		if event.IsTextDelta() {
			chunks = append(chunks, event.Text)
		}
	}
	summary := strings.Join(chunks, "")
	if !summarized || failed || strings.TrimSpace(summary) == "" {
		return "", false, nil
	}
	return summary, true, nil
}

type compactionSelection struct {
	Head   string
	Recent string
}

type compactionEstimateInput struct {
	System   []llm.SystemPart     `json:"system"`
	Messages []llm.Message        `json:"messages"`
	Tools    []llm.ToolDefinition `json:"tools"`
}

// SerializeToolContent renders tool content as text for the summary prompt.
func SerializeToolContent(content []schema.ToolContent) string {
	parts := make([]string, 0, len(content))
	for _, item := range content {
		switch value := item.(type) {
		case schema.ToolTextContent:
			parts = append(parts, value.Text)
		case schema.ToolFileContent:
			label := "[Attached " + value.Mime
			if value.Name != nil {
				label += ": " + *value.Name
			}
			parts = append(parts, label+"]")
		}
	}
	return strings.Join(parts, "\n")
}

// BuildCompactionPrompt renders the prompt that asks the model to summarize the
// history.
func BuildCompactionPrompt(input CompactionPromptInput) string {
	conversation := "Here is the conversation so far:\n\n<conversation>\n" +
		strings.Join(input.Context, "\n\n") + "\n</conversation>"
	if input.PreviousSummary == "" {
		return strings.Join([]string{
			conversation,
			"Create a new anchored summary from the conversation history in the <conversation> tags above so another coding agent can continue the work.", //nolint:lll // prose prompt
			compactionSummaryTemplate,
		}, "\n\n")
	}
	return strings.Join([]string{
		conversation,
		"Here is the summary of the conversation before the <conversation> above:\n\n<prior-summary>\n" +
			input.PreviousSummary + "\n</prior-summary>",
		compactionSummaryUpdateInstructions,
		compactionSummaryTemplate,
	}, "\n\n")
}

// DefaultCompactionSettings are the built-in compaction defaults.
func DefaultCompactionSettings() CompactionSettings {
	return CompactionSettings{Auto: true, Buffer: compactionDefaultBuffer, Tokens: compactionDefaultKeepTokens}
}

func compactionModelLimits(model route.Model) *llmschema.ModelLimits {
	return model.Route.Defaults.Limits
}

func compactionOutput(request llm.LLMRequest, limits *llmschema.ModelLimits) int {
	if request.Generation != nil && request.Generation.MaxTokens != nil {
		return *request.Generation.MaxTokens
	}
	if limits != nil {
		return limits.Output
	}
	return 0
}

func compactionEstimate(value any) int {
	data, err := json.Marshal(value)
	if err != nil {
		return 0
	}
	return util.TokenEstimate(string(data))
}

func compactionSelect(entries []CompactionEntry, tokens int) (compactionSelection, bool) {
	var conversation []string
	for _, entry := range entries {
		if entry.Message.SessionMessageType() == "compaction" {
			continue
		}
		if serialized := compactionSerialize(entry.Message); serialized != "" {
			conversation = append(conversation, serialized)
		}
	}
	if len(conversation) == 0 {
		return compactionSelection{}, false
	}
	total := 0
	split := len(conversation)
	for index := len(conversation) - 1; index >= 0; index-- {
		next := total + util.TokenEstimate(conversation[index])
		if next > tokens {
			break
		}
		total = next
		split = index
	}
	return compactionSelection{
		Head:   strings.Join(conversation[:split], "\n\n"),
		Recent: strings.Join(conversation[split:], "\n\n"),
	}, true
}

func compactionSerialize(message schema.SessionMessage) string {
	switch m := message.(type) {
	case schema.SessionMessageUser:
		lines := []string{"[User]: " + m.Text}
		for _, file := range m.Files {
			name := file.URI
			if file.Name != nil {
				name = *file.Name
			}
			lines = append(lines, "[Attached "+file.Mime+": "+name+"]")
		}
		return strings.Join(lines, "\n")
	case schema.SessionMessageAssistant:
		var lines []string
		for _, part := range m.Content {
			switch p := part.(type) {
			case schema.SessionMessageAssistantText:
				lines = append(lines, "[Assistant]: "+p.Text)
			case schema.SessionMessageAssistantReasoning:
				if p.Text != "" {
					lines = append(lines, "[Assistant reasoning]: "+p.Text)
				}
			case schema.SessionMessageAssistantTool:
				call := "[Assistant tool call]: " + p.Name + "(" + compactionToolInput(p.State) + ")"
				switch state := p.State.(type) {
				case schema.SessionMessageToolStateCompleted:
					lines = append(lines, call, "[Tool result]: "+compactionTruncate(SerializeToolContent(state.Content)))
				case schema.SessionMessageToolStateError:
					lines = append(lines, call, "[Tool error]: "+state.Error.Message)
				default:
					lines = append(lines, call)
				}
			}
		}
		return strings.Join(lines, "\n")
	case schema.SessionMessageSystem:
		return "[System update]: " + m.Text
	case schema.SessionMessageSynthetic:
		return "[Synthetic context]: " + m.Text
	case schema.SessionMessageShell:
		return "[Shell]: " + m.Command + "\n" + compactionTruncate(m.Output)
	}
	return ""
}

func compactionToolInput(state schema.SessionMessageToolState) string {
	switch s := state.(type) {
	case schema.SessionMessageToolStatePending:
		return s.Input
	case schema.SessionMessageToolStateRunning:
		return compactionJSONString(s.Input)
	case schema.SessionMessageToolStateCompleted:
		return compactionJSONString(s.Input)
	case schema.SessionMessageToolStateError:
		return compactionJSONString(s.Input)
	}
	return ""
}

func compactionJSONString(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(data)
}

func compactionTruncate(value string) string {
	if compactionUTF16Length(value) <= compactionToolOutputMaxChars {
		return value
	}
	return compactionUTF16Slice(value, compactionToolOutputMaxChars) + "\n[truncated]"
}

// compactionUTF16Length is the string length in UTF-16 code units.
func compactionUTF16Length(value string) int {
	return len(utf16.Encode([]rune(value)))
}

// compactionUTF16Slice returns the leading run of at most end UTF-16 code units.
func compactionUTF16Slice(value string, end int) string {
	units := utf16.Encode([]rune(value))
	if end > len(units) {
		end = len(units)
	}
	if end < 0 {
		end = 0
	}
	return string(utf16.Decode(units[:end]))
}
