package route

import (
	"strings"
	"testing"
)

func collectFrames(t *testing.T, input string) []string {
	t.Helper()
	var frames []string
	for frame, err := range SSE.Frame(strings.NewReader(input)) {
		if err != nil {
			t.Fatalf("frame error: %v", err)
		}
		frames = append(frames, frame)
	}
	return frames
}

func TestSSEFramingYieldsDataPayloads(t *testing.T) {
	frames := collectFrames(t, "data: one\n\ndata: two\n\n")
	if len(frames) != 2 || frames[0] != "one" || frames[1] != "two" {
		t.Fatalf("frames = %q, want [one two]", frames)
	}
}

func TestSSEFramingSkipsDone(t *testing.T) {
	frames := collectFrames(t, "data: one\n\ndata: [DONE]\n\n")
	if len(frames) != 1 || frames[0] != "one" {
		t.Fatalf("frames = %q, want [one]", frames)
	}
}

func TestSSEFramingJoinsMultipleDataLines(t *testing.T) {
	// The SSE spec joins `data:` lines with a newline.
	frames := collectFrames(t, "data: {\"a\":\ndata: 1}\n\n")
	if len(frames) != 1 || frames[0] != "{\"a\":\n1}" {
		t.Fatalf("frames = %q, want [{\"a\":\n1}]", frames)
	}
}

func TestSSEFramingIgnoresNonDataLines(t *testing.T) {
	frames := collectFrames(t, "event: message\nid: 7\n: comment\ndata: payload\n\n")
	if len(frames) != 1 || frames[0] != "payload" {
		t.Fatalf("frames = %q, want [payload]", frames)
	}
}

func TestSSEFramingSkipsEmptyFrames(t *testing.T) {
	if frames := collectFrames(t, "\n\n\ndata: one\n\n\n\n"); len(frames) != 1 || frames[0] != "one" {
		t.Fatalf("frames = %q, want [one]", frames)
	}
}

func TestSSEFramingDropsTrailingFrameWithoutBlankLine(t *testing.T) {
	// A frame is only dispatched on the blank line that terminates it, matching
	// the SSE spec: the trailing partial frame is dropped.
	frames := collectFrames(t, "data: one\n\ndata: two")
	if len(frames) != 1 || frames[0] != "one" {
		t.Fatalf("frames = %q, want [one]", frames)
	}
}

func TestSSEFramingHandlesFramesLargerThanScannerDefault(t *testing.T) {
	// bufio.Scanner's default 64KB limit would split or error on this; the
	// framing raises it to maxSSEFrameBytes.
	payload := strings.Repeat("x", 256*1024)
	frames := collectFrames(t, "data: "+payload+"\n\n")
	if len(frames) != 1 || frames[0] != payload {
		t.Fatalf("frames = %d, len = %d, want one frame of %d", len(frames), len(frames[0]), len(payload))
	}
}

func TestSSEFramingStopsWhenYieldReturnsFalse(t *testing.T) {
	count := 0
	for range SSE.Frame(strings.NewReader("data: a\n\ndata: b\n\ndata: c\n\n")) {
		count++
		break
	}
	if count != 1 {
		t.Fatalf("iterated %d frames after break, want 1", count)
	}
}

func TestSSEFramingAcceptsDataWithoutSpace(t *testing.T) {
	frames := collectFrames(t, "data:{\"a\":1}\n\n")
	if len(frames) != 1 || frames[0] != `{"a":1}` {
		t.Fatalf("frames = %q, want [{\"a\":1}]", frames)
	}
}
