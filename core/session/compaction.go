package session

import (
	"encoding/json"
	"errors"
	"iter"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/user/go-agent-sdk/core/config"
	"github.com/user/go-agent-sdk/core/util"
	"github.com/user/go-agent-sdk/llm"
	"github.com/user/go-agent-sdk/llm/route"
	llmschema "github.com/user/go-agent-sdk/llm/schema"
	"github.com/user/go-agent-sdk/schema"
)

const (
	compactionDefaultBuffer       = 20_000
	compactionDefaultKeepTokens   = 8_000
	compactionToolOutputMaxChars  = 2_000
	compactionSummaryOutputTokens = 4_096
)

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

const compactionSummaryUpdateInstructions = `The <prior-summary> summarizes everything that happened before the <conversation>. Construct a new summary that combines both. The <prior-summary> is discarded after this: anything you do not carry into the new summary is lost.

When combining:
- Carry forward objectives, constraints, user directives, decisions, and parallel workstreams from the <prior-summary> even when the <conversation> does not mention them. Drop only what is finished and no longer needed.
- The <conversation> is more recent than the <prior-summary>. Where they conflict, the conversation wins: state the corrected fact and drop the old claim.
- Add new progress, decisions, constraints, and context from the conversation.
- Move completed work from "Active" to "Completed".
- If a blocker has been resolved, update the summary to reflect that while keeping any details still needed to continue the work.
- Update "Objective" and "Next Move" to reflect the current work state.`

// CompactionEntry mirrors SessionCompaction.Entry.
type CompactionEntry struct {
	Seq     int64
	Message schema.SessionMessage
}

// CompactionSettings mirrors SessionCompaction.Settings.
type CompactionSettings struct {
	Auto   bool
	Buffer int
	Tokens int
}

// CompactionConfigEntry is the narrow Config.Entry surface SessionCompaction
// needs. Entries that are not config documents (or carry no compaction settings)
// report a nil CompactionInfo.
type CompactionConfigEntry interface {
	CompactionInfo() *config.CompactionInfo
}

// CompactionDependencies mirrors SessionCompaction.Dependencies.
type CompactionDependencies struct {
	Events EventPublisher
	LLM    func(request llm.LLMRequest) iter.Seq2[llm.LLMEvent, error]
	Config []CompactionConfigEntry
}

// CompactionInput mirrors SessionCompaction.Input.
type CompactionInput struct {
	SessionID schema.SessionID
	Entries   []CompactionEntry
	Model     route.Model
	Request   llm.LLMRequest
}

// CompactionPromptInput mirrors the buildPrompt argument.
type CompactionPromptInput struct {
	PreviousSummary string
	Context         []string
}

// Compaction mirrors the object returned by SessionCompaction.make.
type Compaction struct {
	events EventPublisher
	llm    func(request llm.LLMRequest) iter.Seq2[llm.LLMEvent, error]
	config CompactionSettings
}

// NewCompaction mirrors SessionCompaction.make.
func NewCompaction(dependencies CompactionDependencies) *Compaction {
	return &Compaction{
		events: dependencies.Events,
		llm:    dependencies.LLM,
		config: resolveCompactionSettings(dependencies.Config),
	}
}

// CompactIfNeeded mirrors SessionCompaction.compactIfNeeded.
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

// CompactAfterOverflow mirrors SessionCompaction.compactAfterOverflow.
func (c *Compaction) CompactAfterOverflow(input CompactionInput) (bool, error) {
	limits := compactionModelLimits(input.Model)
	if limits == nil || limits.Context <= 0 {
		return false, nil
	}
	contextLimit := limits.Context
	output := compactionOutput(input.Request, limits)

	selected, ok := compactionSelect(input.Entries, c.config.Tokens)
	if !ok {
		return false, nil
	}
	var previousSummary *schema.SessionMessageCompaction
	for i := range input.Entries {
		if message, isCompaction := input.Entries[i].Message.(schema.SessionMessageCompaction); isCompaction {
			previousSummary = &message
			break
		}
	}
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
	if util.TokenEstimate(summaryPrompt) > contextLimit-summaryOutput {
		return false, nil
	}

	messageID := schema.NewMessageID()
	if _, err := c.events.Publish(schema.SessionEventCompactionStarted, map[string]any{
		"sessionID": input.SessionID,
		"messageID": messageID,
		"timestamp": time.Now().UnixMilli(),
		"reason":    "auto",
	}); err != nil {
		return false, err
	}

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
	for event, err := range c.llm(*request) {
		if err != nil {
			var llmError *llm.LLMError
			if errors.As(err, &llmError) {
				summarized = false
				break
			}
			return false, err
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
		return false, nil
	}

	if _, err := c.events.Publish(schema.SessionEventCompactionEnded, map[string]any{
		"sessionID": input.SessionID,
		"messageID": messageID,
		"timestamp": time.Now().UnixMilli(),
		"reason":    "auto",
		"text":      summary,
		"recent":    selected.Recent,
	}); err != nil {
		return false, err
	}
	return true, nil
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

// SerializeToolContent mirrors SessionCompaction.serializeToolContent.
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

// BuildCompactionPrompt mirrors SessionCompaction.buildPrompt.
func BuildCompactionPrompt(input CompactionPromptInput) string {
	conversation := "Here is the conversation so far:\n\n<conversation>\n" +
		strings.Join(input.Context, "\n\n") + "\n</conversation>"
	if input.PreviousSummary == "" {
		return strings.Join([]string{
			conversation,
			"Create a new anchored summary from the conversation history in the <conversation> tags above so another coding agent can continue the work.",
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

func resolveCompactionSettings(entries []CompactionConfigEntry) CompactionSettings {
	result := CompactionSettings{Auto: true, Buffer: compactionDefaultBuffer, Tokens: compactionDefaultKeepTokens}
	for _, entry := range entries {
		info := entry.CompactionInfo()
		if info == nil {
			continue
		}
		if info.Auto != nil {
			result.Auto = *info.Auto
		}
		if info.Buffer != nil {
			result.Buffer = int(*info.Buffer)
		}
		if info.Keep != nil && info.Keep.Tokens != nil {
			result.Tokens = int(*info.Keep.Tokens)
		}
	}
	return result
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

// compactionUTF16Length mirrors String.prototype.length (UTF-16 code units).
func compactionUTF16Length(value string) int {
	return len(utf16.Encode([]rune(value)))
}

// compactionUTF16Slice mirrors String.prototype.slice(0, end) for the leading
// run of UTF-16 code units.
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
