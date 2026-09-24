package protocols

import (
	"encoding/binary"
	"encoding/json"
	"hash/crc32"
	"strings"
	"testing"
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
	got := ParseEventStreamHeaders(headers)
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
	got := ParseEventStreamHeaders(headers[:len(headers)-2])
	if got[":message-type"] != "event" {
		t.Fatalf("message-type = %q, want the header before the truncation", got[":message-type"])
	}
	if _, ok := got[":event-type"]; ok {
		t.Fatalf("truncated header was kept: %v", got)
	}
}

func TestDecodeSingleFrame(t *testing.T) {
	var fb FrameBufferState
	decoded, err := DecodeEventStreamFrames(&fb, bedrockFrame("contentBlockDelta", "event", `{"delta":{"text":"hi"},"p":"x"}`))
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
	var fb FrameBufferState
	for i, piece := range pieces {
		decoded, err := DecodeEventStreamFrames(&fb, piece)
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
	decoded, err := DecodeEventStreamFrames(&fb, nil)
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
	var fb FrameBufferState
	decoded, err := DecodeEventStreamFrames(&fb, chunk)
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
	var fb FrameBufferState
	decoded, err := DecodeEventStreamFrames(&fb, bedrockFrame("internalServerException", "exception", `{"message":"boom"}`))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(decoded) != 0 {
		t.Fatalf("decoded = %+v, want no frames", decoded)
	}
}

func TestDecodeSkipsEmptyPayload(t *testing.T) {
	var fb FrameBufferState
	decoded, err := DecodeEventStreamFrames(&fb, bedrockFrame("contentBlockStop", "event", ""))
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
	var fb FrameBufferState
	if _, err := DecodeEventStreamFrames(&fb, frame); err == nil || !strings.Contains(err.Error(), "prelude CRC") {
		t.Fatalf("err = %v, want a prelude CRC error", err)
	}
}

func TestDecodeMessageCRCMismatch(t *testing.T) {
	frame := bedrockFrame("contentBlockDelta", "event", `{"delta":{"text":"hi"}}`)
	frame[len(frame)-8] ^= 0xff
	var fb FrameBufferState
	if _, err := DecodeEventStreamFrames(&fb, frame); err == nil || !strings.Contains(err.Error(), "message CRC") {
		t.Fatalf("err = %v, want a message CRC error", err)
	}
}

func TestDecodeInvalidFrameLength(t *testing.T) {
	frame := bedrockFrame("contentBlockDelta", "event", `{}`)
	binary.BigEndian.PutUint32(frame[0:], 10)
	binary.BigEndian.PutUint32(frame[8:], crc32.ChecksumIEEE(frame[0:8]))
	var fb FrameBufferState
	if _, err := DecodeEventStreamFrames(&fb, frame); err == nil || !strings.Contains(err.Error(), "invalid frame length") {
		t.Fatalf("err = %v, want an invalid length error", err)
	}
}

func TestDecodeBadPayload(t *testing.T) {
	var fb FrameBufferState
	if _, err := DecodeEventStreamFrames(&fb, bedrockFrame("contentBlockDelta", "event", "not json")); err == nil || !strings.Contains(err.Error(), "bad payload") {
		t.Fatalf("err = %v, want a bad payload error", err)
	}
}

func TestRewrapFrame(t *testing.T) {
	got, err := RewrapFrame(DecodedFrame{
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
	got := ParseEventStreamHeaders(headers)
	if got[":binary"] != string([]byte{1, 2, 3}) {
		t.Fatalf("binary header = %q", got[":binary"])
	}
}

func TestParseEventStreamHeadersTruncatedName(t *testing.T) {
	if got := ParseEventStreamHeaders([]byte{200, 'a'}); len(got) != 0 {
		t.Fatalf("headers = %v, want empty", got)
	}
}

func TestParseEventStreamHeadersUnknownType(t *testing.T) {
	var headers []byte
	headers = append(headers, byte(len(":x")))
	headers = append(headers, ":x"...)
	headers = append(headers, 4)
	if got := ParseEventStreamHeaders(headers); len(got) != 0 {
		t.Fatalf("headers = %v, want empty", got)
	}
}

func TestRewrapFrameMarshalError(t *testing.T) {
	if _, err := RewrapFrame(DecodedFrame{EventType: "x", Payload: map[string]any{"bad": func() {}}}); err == nil {
		t.Fatal("want a marshal error")
	}
}

func TestFrameBufferAppendCompacts(t *testing.T) {
	fb := NewFrameBuffer()
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
