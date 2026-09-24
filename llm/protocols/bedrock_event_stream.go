package protocols

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"strings"
)

type BedrockFrameBuffer struct {
	Buffer []byte
	Offset int
}

func NewBedrockFrameBuffer() BedrockFrameBuffer {
	return BedrockFrameBuffer{Buffer: make([]byte, 0)}
}

func (fb *BedrockFrameBuffer) Append(chunk []byte) {
	remaining := len(fb.Buffer) - fb.Offset
	next := make([]byte, remaining+len(chunk))
	copy(next, fb.Buffer[fb.Offset:])
	copy(next[remaining:], chunk)
	fb.Buffer = next
	fb.Offset = 0
}

func (fb *BedrockFrameBuffer) Compact() {
	if fb.Offset > 0 && fb.Offset == len(fb.Buffer) {
		fb.Buffer = nil
		fb.Offset = 0
	}
}

type BedrockFrame struct {
	EventType string
	Payload   map[string]any
}

func ParseBedrockEventStreamHeaders(data []byte) map[string]string {
	result := make(map[string]string)
	for i := 0; i < len(data); {
		if i >= len(data) {
			break
		}
		nameLen := int(data[i])
		i++
		if i+nameLen > len(data) {
			break
		}
		name := string(data[i : i+nameLen])
		i += nameLen
		if i+2 > len(data) {
			break
		}
		kind := data[i]
		i++
		switch kind {
		case 7:
			if i+2 > len(data) {
				return result
			}
			strLen := int(binary.BigEndian.Uint16(data[i:]))
			i += 2
			if i+strLen > len(data) {
				return result
			}
			result[name] = string(data[i : i+strLen])
			i += strLen
		case 6:
			if i+4 > len(data) {
				return result
			}
			binLen := int(binary.BigEndian.Uint32(data[i:]))
			i += 4
			if i+binLen > len(data) {
				return result
			}
			result[name] = string(data[i : i+binLen])
			i += binLen
		default:
			return result
		}
	}
	return result
}

func DecodeBedrockEventStreamFrames(fb *BedrockFrameBuffer, chunk []byte) ([]BedrockFrame, error) {
	if chunk != nil {
		fb.Append(chunk)
	}
	var out []BedrockFrame
	for fb.Offset+4 <= len(fb.Buffer) {
		totalLen := int(binary.BigEndian.Uint32(fb.Buffer[fb.Offset:]))
		if fb.Offset+totalLen > len(fb.Buffer) {
			break
		}
		if totalLen < 16 {
			return out, fmt.Errorf("Bedrock event stream: invalid frame length %d", totalLen)
		}
		preludeCRC := binary.BigEndian.Uint32(fb.Buffer[fb.Offset+8:])
		if crc32.ChecksumIEEE(fb.Buffer[fb.Offset:fb.Offset+8]) != preludeCRC {
			return out, fmt.Errorf("Bedrock event stream: prelude CRC mismatch")
		}
		headerLen := int(binary.BigEndian.Uint32(fb.Buffer[fb.Offset+4:]))
		headersEnd := fb.Offset + 12 + headerLen
		payloadEnd := fb.Offset + totalLen - 4
		if headersEnd > payloadEnd {
			return out, fmt.Errorf("Bedrock event stream: invalid header length")
		}
		msgCRC := binary.BigEndian.Uint32(fb.Buffer[payloadEnd:])
		if crc32.ChecksumIEEE(fb.Buffer[fb.Offset:payloadEnd]) != msgCRC {
			return out, fmt.Errorf("Bedrock event stream: message CRC mismatch")
		}
		hdrs := ParseBedrockEventStreamHeaders(fb.Buffer[fb.Offset+12 : headersEnd])
		eventType := hdrs[":event-type"]
		msgType := hdrs[":message-type"]
		payload := strings.TrimRight(string(fb.Buffer[headersEnd:payloadEnd]), "\x00")
		if msgType == "event" && eventType != "" {
			if payload == "" {
				fb.Offset += totalLen
				continue
			}
			var parsed map[string]any
			if payload != "" {
				if err := json.Unmarshal([]byte(payload), &parsed); err != nil {
					return out, fmt.Errorf("Bedrock event stream: bad payload: %w", err)
				}
				delete(parsed, "p")
			}
			out = append(out, BedrockFrame{EventType: eventType, Payload: parsed})
		}
		fb.Offset += totalLen
	}
	fb.Compact()
	return out, nil
}

func RewrapBedrockFrame(frame BedrockFrame) (string, error) {
	reWrapped, err := json.Marshal(map[string]any{frame.EventType: frame.Payload})
	if err != nil {
		return "", err
	}
	return string(reWrapped), nil
}
