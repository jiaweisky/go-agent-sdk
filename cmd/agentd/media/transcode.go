package media

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"time"

	"github.com/jiaweisky/go-agent-sdk/pkg/core"
)

// transcodeTimeout bounds one transcode call (all scale/bitrate attempts
// together), so a hung ffmpeg cannot block the turn indefinitely.
const transcodeTimeout = 2 * time.Minute

// videoScales are the candidate widths (largest first) tried when shrinking a
// video; the first result that fits MaxBytes is returned.
var videoScales = []string{
	"min(1280\\,iw)", "min(854\\,iw)", "min(640\\,iw)", "min(480\\,iw)",
	"min(360\\,iw)", "min(240\\,iw)", "min(160\\,iw)",
}

// audioBitrates are the candidate audio bitrates (largest first).
var audioBitrates = []string{"64k", "32k", "16k", "8k"}

// FFmpegTranscoder returns a core.MediaTranscode hook that shells out to ffmpeg
// (when on PATH) to shrink audio/video so it fits MaxBytes. The SDK leaves
// images to its own pure-Go downsampler; audio/video have no Go codec, so the
// host wires this. It is soft-fail: if ffmpeg is absent, every attempt fails, or
// none fits, it returns ok=false and the caller falls through (keep as-is / next
// prefer entry / spool).
func FFmpegTranscoder() func(core.MediaTranscodeRequest) (core.MediaTranscodeResult, bool) {
	return func(req core.MediaTranscodeRequest) (core.MediaTranscodeResult, bool) {
		if len(req.Data) == 0 || req.MaxBytes <= 0 {
			return core.MediaTranscodeResult{}, false
		}

		path, err := exec.LookPath("ffmpeg")
		if err != nil {
			return core.MediaTranscodeResult{}, false
		}

		ctx, cancel := context.WithTimeout(context.Background(), transcodeTimeout)
		defer cancel()

		mime := strings.ToLower(req.MIME)
		for _, attempt := range ffmpegAttempts(mime) {
			cmd := exec.CommandContext(ctx, path, attempt...)
			cmd.Stdin = bytes.NewReader(req.Data)

			var out bytes.Buffer
			cmd.Stdout = &out
			if err := cmd.Run(); err != nil {
				continue
			}

			if data := out.Bytes(); len(data) > 0 && int64(len(data)) <= req.MaxBytes {
				return core.MediaTranscodeResult{MIME: outputMIME(mime), Data: data}, true
			}
		}

		return core.MediaTranscodeResult{}, false
	}
}

// outputMIME is the container a transcoded payload is written to.
func outputMIME(mime string) string {
	if strings.HasPrefix(mime, "audio/") {
		return "audio/ogg"
	}
	return "video/mp4"
}

// ffmpegAttempts lists the ffmpeg argument sets to try (best quality first).
func ffmpegAttempts(mime string) [][]string {
	switch {
	case strings.HasPrefix(mime, "video/"):
		attempts := make([][]string, 0, len(videoScales))

		for _, scale := range videoScales {
			attempts = append(attempts, []string{
				"-hide_banner", "-loglevel", "error",
				"-i", "pipe:0",
				"-vf", "scale=" + scale + ":-2",
				"-c:v", "libx264", "-crf", "30", "-preset", "veryfast",
				"-c:a", "aac", "-b:a", "64k",
				"-movflags", "frag_keyframe+empty_moov",
				"-f", "mp4", "pipe:1",
			})
		}

		return attempts
	case strings.HasPrefix(mime, "audio/"):
		attempts := make([][]string, 0, len(audioBitrates))

		for _, bitrate := range audioBitrates {
			attempts = append(attempts, []string{
				"-hide_banner", "-loglevel", "error",
				"-i", "pipe:0",
				"-c:a", "libopus", "-b:a", bitrate,
				"-f", "ogg", "pipe:1",
			})
		}

		return attempts
	}
	return nil
}
