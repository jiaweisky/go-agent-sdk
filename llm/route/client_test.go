package route

import (
	"context"
	"errors"
	"iter"
	"strings"
	"testing"

	"github.com/jiaweisky/go-agent-sdk/llm/schema"
)

// mockFrames is a Transport that yields frames (plus an optional trailing error)
// without any network access, so the stream loop can be driven with mock SSE.
type mockFrames struct {
	frames []any
	err    error
}

func (mockFrames) ID() string                                 { return "mock" }
func (mockFrames) Prepare(TransportPrepareInput) (any, error) { return nil, nil }
func (m mockFrames) Frames(context.Context, any, *schema.LLMRequest, TransportRuntime) iter.Seq2[any, error] {
	return func(yield func(any, error) bool) {
		for _, frame := range m.frames {
			if !yield(frame, nil) {
				return
			}
		}
		if m.err != nil {
			yield(nil, m.err)
		}
	}
}

// echoProtocol emits one text-delta per frame and halts on "halt", so the loop's
// Step / Terminal / OnHalt wiring can be asserted without a real provider.
func echoProtocol() Protocol {
	return Protocol{
		ID:   "echo",
		Body: ProtocolBody{From: func(*schema.LLMRequest) (any, error) { return nil, nil }},
		Stream: ProtocolStream{
			DecodeEvent: func(frame string) (any, error) {
				if errText, ok := strings.CutPrefix(frame, "err:"); ok {
					return nil, errors.New(errText)
				}
				return frame, nil
			},
			Initial: func(*schema.LLMRequest) any { return 0 },
			Step: func(state any, event any) (any, []schema.LLMEvent, error) {
				return state.(int) + 1, []schema.LLMEvent{{Type: schema.EventTextDelta, Text: event.(string)}}, nil
			},
			Terminal: func(event any) bool { return event.(string) == "halt" },
			OnHalt: func(state any) []schema.LLMEvent {
				return []schema.LLMEvent{{Type: schema.EventFinish, Reason: schema.ReasonStop, Index: state.(int)}}
			},
		},
	}
}

func streamRoute(transport Transport) Route {
	return MakeFromTransport(MakeTransportInput{
		ID: "test", Provider: "test", Protocol: echoProtocol(), Transport: transport,
		Endpoint: Endpoint{BaseURL: "https://example.test/", Path: func(EndpointInput) string { return "/v1" }},
	})
}

func drainStream(route Route) ([]schema.LLMEvent, error) {
	var events []schema.LLMEvent
	for evt, err := range route.StreamPrepared(context.Background(), nil, &schema.LLMRequest{}, TransportRuntime{}) {
		if err != nil {
			return events, err
		}
		events = append(events, evt.(schema.LLMEvent))
	}
	return events, nil
}

func TestStreamStepsEachFrameThenHalts(t *testing.T) {
	events, err := drainStream(streamRoute(mockFrames{frames: []any{"a", "b"}}))
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("events = %+v", events)
	}
	if events[0].Type != schema.EventTextDelta || events[0].Text != "a" {
		t.Fatalf("event[0] = %+v", events[0])
	}
	if events[1].Type != schema.EventTextDelta || events[1].Text != "b" {
		t.Fatalf("event[1] = %+v", events[1])
	}
	// OnHalt runs once after the frames are exhausted, carrying the step count.
	if events[2].Type != schema.EventFinish || events[2].Reason != schema.ReasonStop || events[2].Index != 2 {
		t.Fatalf("event[2] = %+v", events[2])
	}
}

func TestStreamTerminalFrameStopsAndHalts(t *testing.T) {
	events, err := drainStream(streamRoute(mockFrames{frames: []any{"a", "halt", "c"}}))
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	// "c" is never read: the terminal frame stops the loop and runs OnHalt.
	if len(events) != 3 {
		t.Fatalf("events = %+v", events)
	}
	if events[1].Text != "halt" || events[2].Type != schema.EventFinish {
		t.Fatalf("events = %+v", events)
	}
}

func TestStreamPropagatesDecodeError(t *testing.T) {
	_, err := drainStream(streamRoute(mockFrames{frames: []any{"err:boom"}}))
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("err = %v, want boom", err)
	}
}

func TestStreamPropagatesTransportError(t *testing.T) {
	want := errors.New("network down")
	_, err := drainStream(streamRoute(mockFrames{frames: []any{"a"}, err: want}))
	if !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
}

func TestStreamStopsWhenConsumerBreaks(t *testing.T) {
	route := streamRoute(mockFrames{frames: []any{"a", "b", "c"}})
	count := 0
	for range route.StreamPrepared(context.Background(), nil, &schema.LLMRequest{}, TransportRuntime{}) {
		count++
		break
	}
	if count != 1 {
		t.Fatalf("events consumed = %d, want 1", count)
	}
}
