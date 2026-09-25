package protocols

import (
	"encoding/binary"
	"encoding/json"
	"hash/crc32"
	"strings"
	"testing"

	"github.com/user/go-agent-sdk/llm/protocols/utils"
	"github.com/user/go-agent-sdk/llm/schema"
)

func bedrockHeaders(fields [][2]string) []byte {
	var out []byte
	for _, field := range fields {
		name, value := field[0], field[1]
		out = append(out, byte(len(name)))
		out = append(out, name...)
		out = append(out, 7)
		out = append(out, byte(len(value)>>8), byte(len(value)))
		out = append(out, value...)
	}
	return out
}

func bedrockFrame(eventType, messageType, payload string) []byte {
	headers := bedrockHeaders([][2]string{
		{":event-type", eventType},
		{":message-type", messageType},
		{":content-type", "application/json"},
	})
	total := 12 + len(headers) + len(payload) + 4
	frame := make([]byte, 12, total)
	binary.BigEndian.PutUint32(frame[0:], uint32(total))
	binary.BigEndian.PutUint32(frame[4:], uint32(len(headers)))
	binary.BigEndian.PutUint32(frame[8:], crc32.ChecksumIEEE(frame[0:8]))
	frame = append(frame, headers...)
	frame = append(frame, payload...)
	var msgCRC [4]byte
	binary.BigEndian.PutUint32(msgCRC[:], crc32.ChecksumIEEE(frame))
	return append(frame, msgCRC[:]...)
}

func bedrockTestHeaders(t *testing.T) []byte {
	t.Helper()
	return bedrockHeaders([][2]string{{":event-type", "contentBlockDelta"}, {":message-type", "event"}})
}

func TestParseEventStreamHeaders(t *testing.T) {
	headers := bedrockHeaders([][2]string{
		{":event-type", "contentBlockDelta"},
		{":message-type", "event"},
	})
	got := ParseBedrockEventStreamHeaders(headers)
	if got[":event-type"] != "contentBlockDelta" {
		t.Fatalf("event-type = %q", got[":event-type"])
	}
	if got[":message-type"] != "event" {
		t.Fatalf("message-type = %q", got[":message-type"])
	}
	if len(got) != 2 {
		t.Fatalf("headers = %v", got)
	}
}

func TestParseEventStreamHeadersDropsTruncatedValue(t *testing.T) {
	headers := bedrockHeaders([][2]string{{":message-type", "event"}, {":event-type", "contentBlockDelta"}})
	got := ParseBedrockEventStreamHeaders(headers[:len(headers)-2])
	if got[":message-type"] != "event" {
		t.Fatalf("message-type = %q, want the header before the truncation", got[":message-type"])
	}
	if _, ok := got[":event-type"]; ok {
		t.Fatalf("truncated header was kept: %v", got)
	}
}

func TestDecodeSingleFrame(t *testing.T) {
	var fb BedrockFrameBuffer
	decoded, err := DecodeBedrockEventStreamFrames(&fb, bedrockFrame("contentBlockDelta", "event", `{"delta":{"text":"hi"},"p":"x"}`))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(decoded) != 1 {
		t.Fatalf("decoded = %d frames, want 1", len(decoded))
	}
	if decoded[0].EventType != "contentBlockDelta" {
		t.Fatalf("event type = %q", decoded[0].EventType)
	}
	if _, ok := decoded[0].Payload["p"]; ok {
		t.Fatalf("padding field p was not dropped: %v", decoded[0].Payload)
	}
	delta, _ := decoded[0].Payload["delta"].(map[string]any)
	if delta["text"] != "hi" {
		t.Fatalf("payload = %v", decoded[0].Payload)
	}
	if len(fb.Buffer) != 0 {
		t.Fatalf("buffer not compacted: %d bytes", len(fb.Buffer))
	}
}

func TestDecodeFrameSplitAcrossChunks(t *testing.T) {
	frame := bedrockFrame("contentBlockDelta", "event", `{"delta":{"text":"hi"}}`)
	pieces := [][]byte{frame[:5], frame[5:20], frame[20:]}
	var fb BedrockFrameBuffer
	for i, piece := range pieces {
		decoded, err := DecodeBedrockEventStreamFrames(&fb, piece)
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		want := 0
		if i == len(pieces)-1 {
			want = 1
		}
		if len(decoded) != want {
			t.Fatalf("piece %d decoded %d frames, want %d", i, len(decoded), want)
		}
	}
	decoded, err := DecodeBedrockEventStreamFrames(&fb, nil)
	if err != nil {
		t.Fatalf("flush: %v", err)
	}
	if len(decoded) != 0 {
		t.Fatalf("flush decoded %d frames, want 0", len(decoded))
	}
}

func TestDecodeMultipleFramesInOneChunk(t *testing.T) {
	chunk := append(
		bedrockFrame("contentBlockDelta", "event", `{"delta":{"text":"a"}}`),
		bedrockFrame("contentBlockStop", "event", `{"contentBlockIndex":0}`)...,
	)
	var fb BedrockFrameBuffer
	decoded, err := DecodeBedrockEventStreamFrames(&fb, chunk)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(decoded) != 2 {
		t.Fatalf("decoded = %d frames, want 2", len(decoded))
	}
	if decoded[0].EventType != "contentBlockDelta" || decoded[1].EventType != "contentBlockStop" {
		t.Fatalf("event types = %q, %q", decoded[0].EventType, decoded[1].EventType)
	}
}

func TestDecodeSkipsNonEventFrames(t *testing.T) {
	var fb BedrockFrameBuffer
	decoded, err := DecodeBedrockEventStreamFrames(&fb, bedrockFrame("internalServerException", "exception", `{"message":"boom"}`))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(decoded) != 0 {
		t.Fatalf("decoded = %+v, want no frames", decoded)
	}
}

func TestDecodeSkipsEmptyPayload(t *testing.T) {
	var fb BedrockFrameBuffer
	decoded, err := DecodeBedrockEventStreamFrames(&fb, bedrockFrame("contentBlockStop", "event", ""))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(decoded) != 0 {
		t.Fatalf("decoded = %+v, want no frames", decoded)
	}
}

func TestDecodePreludeCRCMismatch(t *testing.T) {
	frame := bedrockFrame("contentBlockDelta", "event", `{"delta":{"text":"hi"}}`)
	frame[9] ^= 0xff
	var fb BedrockFrameBuffer
	if _, err := DecodeBedrockEventStreamFrames(&fb, frame); err == nil || !strings.Contains(err.Error(), "prelude CRC") {
		t.Fatalf("err = %v, want a prelude CRC error", err)
	}
}

func TestDecodeMessageCRCMismatch(t *testing.T) {
	frame := bedrockFrame("contentBlockDelta", "event", `{"delta":{"text":"hi"}}`)
	frame[len(frame)-8] ^= 0xff
	var fb BedrockFrameBuffer
	if _, err := DecodeBedrockEventStreamFrames(&fb, frame); err == nil || !strings.Contains(err.Error(), "message CRC") {
		t.Fatalf("err = %v, want a message CRC error", err)
	}
}

func TestDecodeInvalidFrameLength(t *testing.T) {
	frame := bedrockFrame("contentBlockDelta", "event", `{}`)
	binary.BigEndian.PutUint32(frame[0:], 10)
	binary.BigEndian.PutUint32(frame[8:], crc32.ChecksumIEEE(frame[0:8]))
	var fb BedrockFrameBuffer
	if _, err := DecodeBedrockEventStreamFrames(&fb, frame); err == nil || !strings.Contains(err.Error(), "invalid frame length") {
		t.Fatalf("err = %v, want an invalid length error", err)
	}
}

func TestDecodeBadPayload(t *testing.T) {
	var fb BedrockFrameBuffer
	if _, err := DecodeBedrockEventStreamFrames(&fb, bedrockFrame("contentBlockDelta", "event", "not json")); err == nil || !strings.Contains(err.Error(), "bad payload") {
		t.Fatalf("err = %v, want a bad payload error", err)
	}
}

func TestRewrapFrame(t *testing.T) {
	got, err := RewrapBedrockFrame(BedrockFrame{
		EventType: "contentBlockDelta",
		Payload:   map[string]any{"delta": map[string]any{"text": "hi"}},
	})
	if err != nil {
		t.Fatalf("rewrap: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(got), &decoded); err != nil {
		t.Fatalf("rewrapped frame is not JSON: %q", got)
	}
	if _, ok := decoded["contentBlockDelta"]; !ok {
		t.Fatalf("rewrapped frame = %q", got)
	}
}

func TestParseEventStreamHeadersBinaryValue(t *testing.T) {
	var headers []byte
	headers = append(headers, byte(len(":binary")))
	headers = append(headers, ":binary"...)
	headers = append(headers, 6)
	var binLen [4]byte
	binary.BigEndian.PutUint32(binLen[:], 3)
	headers = append(headers, binLen[:]...)
	headers = append(headers, 1, 2, 3)
	got := ParseBedrockEventStreamHeaders(headers)
	if got[":binary"] != string([]byte{1, 2, 3}) {
		t.Fatalf("binary header = %q", got[":binary"])
	}
}

func TestParseEventStreamHeadersTruncatedName(t *testing.T) {
	if got := ParseBedrockEventStreamHeaders([]byte{200, 'a'}); len(got) != 0 {
		t.Fatalf("headers = %v, want empty", got)
	}
}

func TestParseEventStreamHeadersUnknownType(t *testing.T) {
	var headers []byte
	headers = append(headers, byte(len(":x")))
	headers = append(headers, ":x"...)
	headers = append(headers, 4)
	if got := ParseBedrockEventStreamHeaders(headers); len(got) != 0 {
		t.Fatalf("headers = %v, want empty", got)
	}
}

func TestRewrapFrameMarshalError(t *testing.T) {
	if _, err := RewrapBedrockFrame(BedrockFrame{EventType: "x", Payload: map[string]any{"bad": func() {}}}); err == nil {
		t.Fatal("want a marshal error")
	}
}

func TestFrameBufferAppendCompacts(t *testing.T) {
	fb := NewBedrockFrameBuffer()
	fb.Append([]byte("hello"))
	fb.Offset = 2
	fb.Append([]byte("world"))
	if got := string(fb.Buffer); got != "lloworld" {
		t.Fatalf("buffer = %q, want %q", got, "lloworld")
	}
	if fb.Offset != 0 {
		t.Fatalf("offset = %d, want 0", fb.Offset)
	}
	fb.Offset = len(fb.Buffer)
	fb.Compact()
	if fb.Buffer != nil || fb.Offset != 0 {
		t.Fatalf("buffer not reset: %+v", fb)
	}
}

func TestBedrockConverseLowerSystemCachePoint(t *testing.T) {
	bp := utils.NewBedrockBreakpoints()
	blocks := bedrockConverseLowerSystem(&bp, []schema.SystemPart{
		{Type: "text", Text: "be brief", Cache: &schema.CacheHint{Type: "ephemeral"}},
	})
	if len(blocks) != 2 {
		t.Fatalf("blocks = %#v, want a text block and a cache point", blocks)
	}
	encoded, err := json.Marshal(blocks)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(encoded) != `[{"text":"be brief"},{"cachePoint":{"type":"default"}}]` {
		t.Fatalf("blocks = %s", encoded)
	}
}

func TestBedrockConverseLowerToolResultEmptyContent(t *testing.T) {
	items, err := bedrockConverseLowerToolResultContent(schema.ToolResultPart{
		Type: "tool-result",
		ID:   "call_1",
		Name: "lookup",
		Result: schema.ToolResultValue{
			Type:  "content",
			Value: []schema.ToolContent{},
		},
	})
	if err != nil {
		t.Fatalf("lower: %v", err)
	}
	encoded, err := json.Marshal(items)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(encoded) != "[]" {
		t.Fatalf("content = %s, want []", encoded)
	}
}

func TestBedrockConverseKeepsUsageWhenMetadataPrecedesMessageStop(t *testing.T) {
	input, output := 5, 2
	state := any(bedrockConverseParserState{tools: utils.ToolStreamEmpty(), lifecycle: utils.LifecycleInitial()})

	next, _, err := bedrockConverseStep(state, bedrockConverseEvent{
		Metadata: &bedrockConverseMetadataEvent{
			Usage: &bedrockConverseUsage{InputTokens: &input, OutputTokens: &output},
		},
	})
	if err != nil {
		t.Fatalf("metadata: %v", err)
	}
	next, _, err = bedrockConverseStep(next, bedrockConverseEvent{
		MessageStop: &bedrockConverseMessageStopEvent{StopReason: "end_turn"},
	})
	if err != nil {
		t.Fatalf("messageStop: %v", err)
	}

	finish := firstOfType(bedrockConverseOnHalt(next), schema.EventFinish)
	if finish == nil || finish.Usage == nil || finish.Usage.InputTokens != 5 || finish.Usage.OutputTokens != 2 {
		t.Fatalf("finish = %+v", finish)
	}
}

func TestBedrockConverseSkipsEmptyDeltas(t *testing.T) {
	event, err := bedrockConverseDecodeEvent(`{"contentBlockDelta":{"contentBlockIndex":0,"delta":{"text":""}}}`)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	state := any(bedrockConverseParserState{tools: utils.ToolStreamEmpty(), lifecycle: utils.LifecycleInitial()})
	next, events, err := bedrockConverseStep(state, event)
	if err != nil {
		t.Fatalf("delta: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("events = %v, want none for an empty text delta", eventTypes(events))
	}
	if s, ok := next.(bedrockConverseParserState); !ok || len(s.lifecycle.Text) != 0 {
		t.Fatalf("state = %+v", next)
	}
}
