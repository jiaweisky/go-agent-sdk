package server

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"

	a2acommon "github.com/jiaweisky/go-agent-sdk/pkg/a2a"
	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

// messageText concatenates the text parts of an A2A message and renders any
// structured data parts (other than tool function calls/results) as JSON, so
// inbound data is not lost.
func messageText(message *a2a.Message) string {
	if message == nil {
		return ""
	}
	var builder strings.Builder
	appendLine := func(line string) {
		if line == "" {
			return
		}
		if builder.Len() > 0 {
			builder.WriteString("\n")
		}
		builder.WriteString(line)
	}
	for _, part := range message.Parts {
		if text := part.Text(); text != "" {
			appendLine(text)
			continue
		}
		if data := part.Data(); data != nil && !isToolDataPart(part) {
			if encoded, err := json.Marshal(data); err == nil {
				appendLine(string(encoded))
			}
		}
	}
	return builder.String()
}

// isToolDataPart reports whether a data part encodes a tool call or result
// (which the executor maps separately, not as prompt text).
func isToolDataPart(part *a2a.Part) bool {
	kind, _ := part.Metadata[a2acommon.ToA2AMetaKey(a2acommon.MetaType)].(string)
	return kind == a2acommon.TypeFunctionCall || kind == a2acommon.TypeFunctionResponse
}

// messageFiles collects a message's raw and URL parts as attachments. Raw parts
// carry bytes to write; URL parts are references (the executor does not fetch
// them).
func messageFiles(message *a2a.Message) []File {
	if message == nil {
		return nil
	}
	var files []File
	for _, part := range message.Parts {
		if raw := part.Raw(); len(raw) > 0 {
			files = append(files, File{Name: attachmentName(part.Filename, part.MediaType), MIME: part.MediaType, Data: raw})
			continue
		}
		if url := part.URL(); url != "" {
			name := part.Filename
			if name == "" {
				name = string(url)
			}
			files = append(files, File{Name: name, MIME: part.MediaType, URI: string(url)})
		}
	}
	return files
}

// attachmentName derives a safe file name for an inbound attachment, appending
// an extension from the media type when the sender supplied none, so the file
// reads back as the right kind.
func attachmentName(name, mediaType string) string {
	base := filepath.Base(name)
	if base == "" || base == "." || base == string(filepath.Separator) {
		base = "attachment"
	}
	if filepath.Ext(base) == "" {
		base += attachmentExtension(mediaType)
	}
	return base
}

// attachmentExtension maps a media type to a file extension, or "" when the
// type is not recognized.
func attachmentExtension(mediaType string) string {
	mime := strings.ToLower(strings.TrimSpace(mediaType))
	if semi := strings.IndexByte(mime, ';'); semi >= 0 {
		mime = strings.TrimSpace(mime[:semi])
	}
	switch mime {
	case "image/png":
		return ".png"
	case "image/jpeg", "image/jpg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "image/bmp":
		return ".bmp"
	case "image/tiff":
		return ".tiff"
	case "application/pdf":
		return ".pdf"
	case "application/json":
		return ".json"
	case "text/plain":
		return ".txt"
	case "text/markdown":
		return ".md"
	case "text/csv":
		return ".csv"
	case "audio/mpeg", "audio/mp3":
		return ".mp3"
	case "audio/wav":
		return ".wav"
	case "audio/ogg":
		return ".ogg"
	case "video/mp4":
		return ".mp4"
	case "video/webm":
		return ".webm"
	default:
		return ""
	}
}

// questionAnswersFromMessage extracts answer labels from a reply message.
func questionAnswersFromMessage(message *a2a.Message) []Answer {
	text := messageText(message)
	if text == "" {
		return nil
	}
	return []Answer{{Labels: []string{text}}}
}

// permissionReplyFromMessage maps a reply message onto a permission reply.
// Recognition is whole-word and fails closed: an explicit rejection wins over
// any allowance, and a message with no recognizable decision is rejected rather
// than silently allowed once.
func permissionReplyFromMessage(message *a2a.Message) string {
	words := strings.FieldsFunc(strings.ToLower(messageText(message)), func(r rune) bool {
		return !unicode.IsLetter(r)
	})
	allowed := false
	always := false
	for _, word := range words {
		switch word {
		case "reject", "deny", "no", "never", "cancel", "stop", "abort", "not", "don", "dont":
			return "reject"
		case "always":
			allowed, always = true, true
		case "once", "yes", "y", "ok", "okay", "allow", "approve", "accept":
			allowed = true
		}
	}
	if !allowed {
		return "reject"
	}
	if always {
		return "always"
	}
	return "once"
}

// toolCallPart encodes a tool invocation as an ADK-compatible function_call
// data part.
func toolCallPart(event Event) *a2a.Part {
	part := a2a.NewDataPart(map[string]any{
		"name": event.ToolName,
		"id":   event.CallID,
		"args": normalizeValue(event.Input),
	})
	part.SetMeta(a2acommon.ToA2AMetaKey(a2acommon.MetaType), a2acommon.TypeFunctionCall)
	return part
}

// toolResultPart encodes a tool result as an ADK-compatible function_response
// data part.
func toolResultPart(event Event) *a2a.Part {
	payload := map[string]any{
		"name":     event.ToolName,
		"id":       event.CallID,
		"response": normalizeValue(event.Result),
	}
	if event.IsError {
		payload["response"] = map[string]any{"error": normalizeValue(event.Result)}
	}
	part := a2a.NewDataPart(payload)
	part.SetMeta(a2acommon.ToA2AMetaKey(a2acommon.MetaType), a2acommon.TypeFunctionResponse)
	return part
}

// fileDiffParts extracts the file diffs a mutating tool produced into
// sdk_type=file_diff data parts, so code changes travel as first-class
// artifacts. It returns nil when the structured output carries no files.
func fileDiffParts(structured any) []*a2a.Part {
	if structured == nil {
		return nil
	}
	encoded, err := json.Marshal(structured)
	if err != nil {
		return nil
	}
	var wrapper struct {
		Files []schema.FileDiffInfo `json:"files"`
	}
	if err := json.Unmarshal(encoded, &wrapper); err != nil {
		return nil
	}
	parts := make([]*a2a.Part, 0, len(wrapper.Files))
	for _, diff := range wrapper.Files {
		part := a2a.NewDataPart(normalizeValue(diff))
		part.SetMeta(a2acommon.ToSDKObjectMetaKey(a2acommon.MetaType), a2acommon.MetaFileDiff)
		parts = append(parts, part)
	}
	return parts
}

// normalizeValue converts an arbitrary Go value into JSON-friendly maps, slices
// and primitives. The A2A task store deep-copies tasks with gob, so arbitrary
// concrete types (such as tool content structs) would otherwise fail to copy.
func normalizeValue(value any) any {
	if value == nil {
		return nil
	}
	switch value.(type) {
	case string, bool, float64, int, int64, nil:
		return value
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var decoded any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		return string(encoded)
	}
	return decoded
}

// rebuildPrompt reconstructs a turn prompt after a restart dropped the parked
// turn. It replays the original request, the question that was asked and the
// reply so the model can resume with the context the parked turn held.
func rebuildPrompt(task *a2a.Task, reply *a2a.Message) string {
	sections := make([]string, 0, 3)
	if original := firstUserText(task); original != "" {
		sections = append(sections, original)
	}
	if question := messageText(task.Status.Message); question != "" {
		sections = append(sections, "[The agent paused and asked: "+question+"]")
	}
	if answer := messageText(reply); answer != "" {
		sections = append(sections, answer)
	}
	return strings.Join(sections, "\n\n")
}

// firstUserText returns the first user message's text in a task's history.
func firstUserText(task *a2a.Task) string {
	for _, message := range task.History {
		if message.Role != a2a.MessageRoleUser {
			continue
		}
		if text := messageText(message); text != "" {
			return text
		}
	}
	return ""
}

// usageMetadata wraps a step's usage as artifact/status metadata.
func usageMetadata(usage any) map[string]any {
	return map[string]any{
		a2acommon.ToA2AMetaKey(a2acommon.MetaUsageMetadata): normalizeValue(usage),
	}
}

// awaitInputMessage builds the input-required status message: a human-readable
// text part plus a long-running function_call data part, matching the ADK
// human-in-the-loop convention. The call's args carry what the client needs to
// answer: the questions a question request asked, or the action, resources and
// savable resources a permission request wants.
func awaitInputMessage(execCtx *a2asrv.ExecutorContext, event Event) *a2a.Message {
	args := map[string]any{}
	if len(event.Questions) > 0 {
		args["questions"] = normalizeValue(event.Questions)
	}
	if event.Permission != nil {
		args["permission"] = map[string]any{
			"action":    event.Permission.Action,
			"resources": normalizeValue(event.Permission.Resources),
			"save":      normalizeValue(event.Permission.Save),
		}
	}
	call := a2a.NewDataPart(map[string]any{
		"name": "await_input",
		"id":   event.RequestID,
		"args": args,
	})
	call.SetMeta(a2acommon.ToA2AMetaKey(a2acommon.MetaType), a2acommon.TypeFunctionCall)
	call.SetMeta(a2acommon.ToA2AMetaKey(a2acommon.MetaIsLongRunning), true)
	return a2a.NewMessageForTask(a2a.MessageRoleAgent, execCtx, a2a.NewTextPart("The agent is waiting for input."), call)
}

// errorMessage builds an agent message describing a failure.
func errorMessage(execCtx *a2asrv.ExecutorContext, err error) *a2a.Message {
	text := "The agent failed."
	if err != nil {
		text = err.Error()
	}
	return a2a.NewMessageForTask(a2a.MessageRoleAgent, execCtx, a2a.NewTextPart(text))
}

// noticeMessage builds an informational agent message (for example an omitted
// attachment), surfaced as a working status update.
func noticeMessage(execCtx *a2asrv.ExecutorContext, text string) *a2a.Message {
	return a2a.NewMessageForTask(a2a.MessageRoleAgent, execCtx, a2a.NewTextPart(text))
}

// sealArtifact emits the final chunk that closes a turn's streamed artifact.
func sealArtifact(execCtx *a2asrv.ExecutorContext, artifactID a2a.ArtifactID) *a2a.TaskArtifactUpdateEvent {
	return &a2a.TaskArtifactUpdateEvent{
		TaskID:    execCtx.TaskID,
		ContextID: execCtx.ContextID,
		Append:    true,
		LastChunk: true,
		Artifact: &a2a.Artifact{
			ID:    artifactID,
			Parts: a2a.ContentParts{a2a.NewTextPart("")},
		},
	}
}
