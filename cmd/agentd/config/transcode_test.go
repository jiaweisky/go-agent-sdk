package config

import (
	"bytes"
	"os/exec"
	"testing"

	"github.com/jiaweisky/go-agent-sdk/pkg/core"
)

func TestFFmpegTranscoderShrinksVideo(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not on PATH")
	}
	// A short, high-bitrate test clip.
	var src bytes.Buffer
	gen := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "testsrc=size=640x360:rate=30", "-t", "2",
		"-c:v", "libx264", "-crf", "18", "-preset", "veryfast",
		"-movflags", "frag_keyframe+empty_moov", "-f", "mp4", "pipe:1")
	gen.Stdout = &src
	var genErr bytes.Buffer
	gen.Stderr = &genErr
	if err := gen.Run(); err != nil {
		t.Skipf("could not generate a test clip: %v: %s", err, genErr.String())
	}

	hook := FFmpegTranscoder()
	target := int64(len(src.Bytes()) / 2)
	out, ok := hook(core.MediaTranscodeRequest{MIME: "video/mp4", Data: src.Bytes(), MaxBytes: target})
	if !ok {
		t.Fatalf("transcode failed (source %d bytes, target %d)", src.Len(), target)
	}
	if out.MIME != "video/mp4" || len(out.Data) == 0 || int64(len(out.Data)) > target {
		t.Fatalf("out mime=%q bytes=%d (target %d)", out.MIME, len(out.Data), target)
	}
}

func TestFFmpegTranscoderSoftFailsWithoutLimit(t *testing.T) {
	hook := FFmpegTranscoder()
	if _, ok := hook(core.MediaTranscodeRequest{MIME: "video/mp4", Data: []byte("x"), MaxBytes: 0}); ok {
		t.Fatal("a zero limit should soft-fail")
	}
	if _, ok := hook(core.MediaTranscodeRequest{MIME: "image/png", Data: []byte("x"), MaxBytes: 10}); ok {
		t.Fatal("image is not handled by the AV transcoder")
	}
}
