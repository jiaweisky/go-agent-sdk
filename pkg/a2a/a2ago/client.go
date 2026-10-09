// Package a2ago implements the SDK's thin A2A client surface over a2a-go. It is
// the only outbound package that imports github.com/a2aproject/a2a-go/v2, so
// upgrading the dependency is a single-package change.
package a2ago

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"strings"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2aclient/agentcard"

	a2acommon "github.com/jiaweisky/go-agent-sdk/pkg/a2a"
	"github.com/jiaweisky/go-agent-sdk/pkg/a2a/client"
	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

type goClient struct {
	inner *a2aclient.Client
	card  client.Card
}

var _ client.Client = (*goClient)(nil)

// New resolves the remote agent card at endpoint and returns a client. Headers
// are attached to card resolution and every subsequent request.
func New(ctx context.Context, endpoint string, headers map[string]string) (client.Client, error) {
	if endpoint == "" {
		return nil, fmt.Errorf("a2ago: endpoint is required")
	}
	resolveOptions := make([]agentcard.ResolveOption, 0, len(headers))
	for name, value := range headers {
		resolveOptions = append(resolveOptions, agentcard.WithRequestHeader(name, value))
	}
	card, err := agentcard.DefaultResolver.Resolve(ctx, endpoint, resolveOptions...)
	if err != nil {
		return nil, fmt.Errorf("resolve agent card: %w", err)
	}
	factoryOptions := make([]a2aclient.FactoryOption, 0, 1)
	if len(headers) > 0 {
		factoryOptions = append(factoryOptions, a2aclient.WithCallInterceptors(headerInterceptor{headers: headers}))
	}
	inner, err := a2aclient.NewFromCard(ctx, card, factoryOptions...)
	if err != nil {
		return nil, fmt.Errorf("connect to remote agent: %w", err)
	}
	return &goClient{inner: inner, card: cardFromA2A(card)}, nil
}

// headerInterceptor attaches static headers to every request as service params.
// The card resolver's headers only cover the discovery GET, so calls need this.
type headerInterceptor struct {
	a2aclient.PassthroughInterceptor
	headers map[string]string
}

var _ a2aclient.CallInterceptor = (*headerInterceptor)(nil)

func (h headerInterceptor) Before(ctx context.Context, req *a2aclient.Request) (context.Context, any, error) {
	if req.ServiceParams == nil {
		req.ServiceParams = a2aclient.ServiceParams{}
	}
	for name, value := range h.headers {
		req.ServiceParams[name] = []string{value}
	}
	return ctx, nil, nil
}

func (c *goClient) Card() client.Card { return c.card }

func (c *goClient) Send(ctx context.Context, req client.Request) iter.Seq2[client.Event, error] {
	return func(yield func(client.Event, error) bool) {
		parts := []*a2a.Part{a2a.NewTextPart(req.Prompt)}
		for _, file := range req.Files {
			parts = append(parts, filePart(file))
		}
		if req.Response != nil {
			parts = append(parts, functionResponsePart(*req.Response))
		}
		message := a2a.NewMessage(a2a.MessageRoleUser, parts...)
		message.TaskID = a2a.TaskID(req.TaskID)
		message.ContextID = req.ContextID
		sendRequest := &a2a.SendMessageRequest{Message: message}

		// emit forwards one A2A event's mapped client events; false means the
		// consumer stopped.
		emit := func(event a2a.Event) bool {
			for _, mapped := range eventsFromA2A(event) {
				if !yield(mapped, nil) {
					return false
				}
			}
			return true
		}

		if !c.card.Streaming {
			result, err := c.inner.SendMessage(ctx, sendRequest)
			if err != nil {
				yield(client.Event{}, err)
				return
			}
			emit(result)
			return
		}

		for item, err := range c.inner.SendStreamingMessage(ctx, sendRequest) {
			if err != nil {
				yield(client.Event{}, err)
				return
			}
			if !emit(item) {
				return
			}
		}
	}
}

func (c *goClient) Cancel(ctx context.Context, taskID string) error {
	_, err := c.inner.CancelTask(ctx, &a2a.CancelTaskRequest{ID: a2a.TaskID(taskID)})
	return err
}

// filePart encodes an attachment as an A2A file part: raw bytes when present,
// otherwise a file reference URL.
func filePart(file client.FileAttachment) *a2a.Part {
	if len(file.Data) > 0 {
		return &a2a.Part{Content: a2a.Raw(file.Data), MediaType: file.MIME, Filename: file.Name}
	}
	part := a2a.NewFileURLPart(a2a.URL(file.URI), file.MIME)
	part.Filename = file.Name
	return part
}

// functionResponsePart encodes a reply to a remote function_call, so an ADK
// server can match it by call id.
func functionResponsePart(response client.Response) *a2a.Part {
	part := a2a.NewDataPart(map[string]any{
		"name":     response.Name,
		"id":       response.CallID,
		"response": response.Text,
	})
	part.SetMeta(a2acommon.ToA2AMetaKey(a2acommon.MetaType), a2acommon.TypeFunctionResponse)
	return part
}

func (c *goClient) Close() error { return c.inner.Destroy() }

// cardFromA2A maps an AgentCard to the SDK subset.
func cardFromA2A(card *a2a.AgentCard) client.Card {
	result := client.Card{
		Name:        card.Name,
		Description: card.Description,
		Streaming:   card.Capabilities.Streaming,
	}
	if len(card.SupportedInterfaces) > 0 {
		result.Endpoint = card.SupportedInterfaces[0].URL
		result.ProtocolVersion = string(card.SupportedInterfaces[0].ProtocolVersion)
	}
	return result
}

// eventsFromA2A maps one A2A event — a streaming update or a non-streaming
// SendMessage result — to zero or more client events.
func eventsFromA2A(event a2a.Event) []client.Event {
	switch value := event.(type) {
	case *a2a.Task:
		return taskEvents(value)
	case *a2a.Message:
		return messageEvents(value)
	case *a2a.TaskStatusUpdateEvent:
		return statusEvents(value)
	case *a2a.TaskArtifactUpdateEvent:
		return artifactEvents(value)
	default:
		return nil
	}
}

// eventKindForState maps an A2A task state to the client event kind.
func eventKindForState(state a2a.TaskState) client.EventKind {
	switch state {
	case a2a.TaskStateCompleted:
		return client.EventCompleted
	case a2a.TaskStateFailed:
		return client.EventFailed
	case a2a.TaskStateCanceled:
		return client.EventCanceled
	case a2a.TaskStateInputRequired, a2a.TaskStateAuthRequired:
		return client.EventInputRequired
	default:
		return client.EventStatus
	}
}

func taskEvents(task *a2a.Task) []client.Event {
	var events []client.Event
	// A non-streaming response carries its output in Artifacts; emit those
	// before the state so consumers see the text ahead of the terminal event.
	for _, artifact := range task.Artifacts {
		events = append(events, partEvents(artifact.Parts, string(task.ID), task.ContextID)...)
	}
	return append(events, client.Event{
		Kind:      eventKindForState(task.Status.State),
		TaskID:    string(task.ID),
		ContextID: task.ContextID,
		State:     string(task.Status.State),
	})
}

func statusEvents(event *a2a.TaskStatusUpdateEvent) []client.Event {
	mapped := client.Event{
		Kind:      eventKindForState(event.Status.State),
		TaskID:    string(event.TaskID),
		ContextID: event.ContextID,
		State:     string(event.Status.State),
	}
	if mapped.Kind == client.EventInputRequired {
		mapped.Text = messageTextOf(event.Status.Message)
		mapped.RequestID, mapped.ToolName = functionCallOf(event.Status.Message)
		mapped.Questions = functionCallQuestionsOf(event.Status.Message)
	}
	return []client.Event{mapped}
}

// functionCallQuestionsOf returns the questions a long-running function_call
// carries in its args, when the call encodes a human-in-the-loop question.
func functionCallQuestionsOf(message *a2a.Message) []schema.QuestionInfo {
	args, _ := functionCallFields(message)["args"].(map[string]any)
	raw, ok := args["questions"]
	if !ok {
		return nil
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var questions []schema.QuestionInfo
	if err := json.Unmarshal(encoded, &questions); err != nil {
		return nil
	}
	return questions
}

// functionCallOf returns the id and name of the function_call a status message
// carries, if any. Both are echoed when answering the call.
func functionCallOf(message *a2a.Message) (id, name string) {
	fields := functionCallFields(message)
	id, _ = fields["id"].(string)
	name, _ = fields["name"].(string)
	return id, name
}

// functionCallFields returns the data of the first function_call part, or nil.
func functionCallFields(message *a2a.Message) map[string]any {
	if message == nil {
		return nil
	}
	for _, part := range message.Parts {
		kind, _ := part.Metadata[a2acommon.ToA2AMetaKey(a2acommon.MetaType)].(string)
		if kind != a2acommon.TypeFunctionCall {
			continue
		}
		fields, _ := part.Data().(map[string]any)
		return fields
	}
	return nil
}

// messageTextOf concatenates the text parts of a status message.
func messageTextOf(message *a2a.Message) string {
	if message == nil {
		return ""
	}
	var builder strings.Builder
	for _, part := range message.Parts {
		if text := part.Text(); text != "" {
			if builder.Len() > 0 {
				builder.WriteByte('\n')
			}
			builder.WriteString(text)
		}
	}
	return builder.String()
}

func messageEvents(message *a2a.Message) []client.Event {
	return partEvents(message.Parts, "", message.ContextID)
}

func artifactEvents(event *a2a.TaskArtifactUpdateEvent) []client.Event {
	return partEvents(event.Artifact.Parts, string(event.TaskID), event.ContextID)
}

func partEvents(parts a2a.ContentParts, taskID, contextID string) []client.Event {
	var events []client.Event
	for _, part := range parts {
		if text := part.Text(); text != "" {
			kind := client.EventArtifact
			if thought, _ := part.Metadata[a2acommon.ToA2AMetaKey(a2acommon.MetaThought)].(bool); thought {
				kind = client.EventReasoning
			}
			events = append(events, client.Event{Kind: kind, Text: text, TaskID: taskID, ContextID: contextID})
			continue
		}
		if data := part.Data(); data != nil {
			events = append(events, dataPartEvent(data, part.Metadata, taskID, contextID))
			continue
		}
	}
	return events
}

// dataPartEvent maps a data part onto a client event. Shared parts use the ADK
// function_call/function_response types; SDK parts use the sdk_type key. Unknown
// data parts are surfaced as EventStatus.
func dataPartEvent(data any, metadata map[string]any, taskID, contextID string) client.Event {
	sharedKind, _ := metadata[a2acommon.ToA2AMetaKey(a2acommon.MetaType)].(string)
	sdkKind, _ := metadata[a2acommon.ToSDKObjectMetaKey(a2acommon.MetaType)].(string)
	fields, _ := data.(map[string]any)
	event := client.Event{TaskID: taskID, ContextID: contextID}
	event.ToolName, _ = fields["name"].(string)
	event.CallID, _ = fields["id"].(string)
	switch {
	case sharedKind == a2acommon.TypeFunctionCall:
		event.Kind = client.EventToolCall
		event.Input = fields["args"]
	case sharedKind == a2acommon.TypeFunctionResponse:
		event.Kind = client.EventToolResult
		event.Result = fields["response"]
	case sdkKind == a2acommon.MetaFileDiff:
		event.Kind = client.EventFileDiff
		event.Result = data
	default:
		event.Kind = client.EventStatus
	}
	return event
}
