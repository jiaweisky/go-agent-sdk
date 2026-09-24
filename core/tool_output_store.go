package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	llmschema "github.com/user/go-agent-sdk/llm/schema"
	"github.com/user/go-agent-sdk/schema"
)

const (
	ToolOutputStoreMaxLines         = 2_000
	ToolOutputStoreMaxBytes         = 50 * 1024
	ToolOutputStoreRetention        = 7 * 24 * time.Hour
	ToolOutputStoreManagedDirectory = "tool-output"
)

// ToolOutputStoreBoundInput mirrors ToolOutputStore.BoundInput.
type ToolOutputStoreBoundInput struct {
	SessionID  schema.SessionID
	ToolCallID string
	Output     llmschema.ToolOutput
}

// ToolOutputStoreBoundResult mirrors ToolOutputStore.BoundResult.
type ToolOutputStoreBoundResult struct {
	Output      llmschema.ToolOutput
	OutputPaths []string
}

// ToolOutputStoreStorageError mirrors ToolOutputStore.StorageError.
type ToolOutputStoreStorageError struct {
	Operation string
	Cause     error
}

func (e *ToolOutputStoreStorageError) Error() string {
	detail := ""
	if e.Cause != nil {
		detail = e.Cause.Error()
	}
	if detail == "" {
		return "Failed to " + e.Operation + " tool output"
	}
	return "Failed to " + e.Operation + " tool output: " + detail
}

func (e *ToolOutputStoreStorageError) Unwrap() error { return e.Cause }

// ToolOutputSettings bounds oversized tool results. Zero values use the
// built-in defaults (ToolOutputStoreMaxLines / ToolOutputStoreMaxBytes).
type ToolOutputSettings struct {
	MaxLines int
	MaxBytes int
}

// ToolOutputStore mirrors the ToolOutputStore service.
type ToolOutputStore struct {
	directory string
	settings  ToolOutputSettings
}

func NewToolOutputStore(global Global, settings ToolOutputSettings) *ToolOutputStore {
	return &ToolOutputStore{
		directory: filepath.Join(global.Data, ToolOutputStoreManagedDirectory),
		settings:  settings,
	}
}

// Limits mirrors ToolOutputStore.limits.
func (s *ToolOutputStore) Limits() (maxLines int, maxBytes int) {
	maxLines, maxBytes = ToolOutputStoreMaxLines, ToolOutputStoreMaxBytes
	if s.settings.MaxLines > 0 {
		maxLines = s.settings.MaxLines
	}
	if s.settings.MaxBytes > 0 {
		maxBytes = s.settings.MaxBytes
	}
	return maxLines, maxBytes
}

// Bound mirrors ToolOutputStore.bound.
func (s *ToolOutputStore) Bound(input ToolOutputStoreBoundInput) (ToolOutputStoreBoundResult, error) {
	maxLines, maxBytes := s.Limits()
	var media []llmschema.ToolContent
	var text []string
	for _, item := range input.Output.Content {
		switch value := item.(type) {
		case llmschema.ToolFileContent:
			media = append(media, value)
		case llmschema.ToolTextContent:
			text = append(text, value.Text)
		}
	}

	var contextual string
	if len(input.Output.Content) == 0 {
		if input.Output.Structured == nil {
			contextual = "undefined"
		} else {
			data, err := json.MarshalIndent(input.Output.Structured, "", "  ")
			if err != nil {
				return ToolOutputStoreBoundResult{}, &ToolOutputStoreStorageError{Operation: "encode", Cause: err}
			}
			contextual = string(data)
		}
	} else {
		contextual = strings.Join(text, "")
	}

	if toolOutputLineCount(contextual) <= maxLines && len(contextual) <= maxBytes {
		return ToolOutputStoreBoundResult{Output: input.Output, OutputPaths: []string{}}, nil
	}

	outputPath, err := s.write(contextual)
	if err != nil {
		return ToolOutputStoreBoundResult{}, err
	}
	marker := "... output truncated; full content saved to " + outputPath + " ..."
	content := make([]llmschema.ToolContent, 0, 1+len(media))
	content = append(content, llmschema.ToolTextContent{
		Type: "text",
		Text: toolOutputBoundedPreview(contextual, marker, maxLines, maxBytes),
	})
	content = append(content, media...)
	return ToolOutputStoreBoundResult{
		Output:      llmschema.ToolOutput{Structured: input.Output.Structured, Content: content},
		OutputPaths: []string{outputPath},
	}, nil
}

func (s *ToolOutputStore) write(content string) (string, error) {
	file := filepath.Join(s.directory, "tool_"+schema.Ascending())
	if err := EnsureDir(s.directory); err != nil {
		return "", &ToolOutputStoreStorageError{Operation: "write", Cause: err}
	}
	if err := WriteFileBytes(file, []byte(content), "wx"); err != nil {
		return "", &ToolOutputStoreStorageError{Operation: "write", Cause: err}
	}
	return file, nil
}

// Cleanup mirrors ToolOutputStore.cleanup.
func (s *ToolOutputStore) Cleanup() error {
	entries, err := ReadDir(s.directory)
	if err != nil {
		return nil
	}
	cutoff := time.Now().Add(-ToolOutputStoreRetention)
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name, "tool_") {
			continue
		}
		file := filepath.Join(s.directory, entry.Name)
		info, err := os.Stat(file)
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			_ = Remove(file)
		}
	}
	return nil
}

func toolOutputTakePrefix(input string, maximumBytes int) string {
	bytes := 0
	var builder strings.Builder
	for _, r := range input {
		size := utf8.RuneLen(r)
		if bytes+size > maximumBytes {
			break
		}
		builder.WriteRune(r)
		bytes += size
	}
	return builder.String()
}

func toolOutputTakeSuffix(input string, maximumBytes int) string {
	runes := []rune(input)
	bytes := 0
	start := len(runes)
	for i := len(runes) - 1; i >= 0; i-- {
		size := utf8.RuneLen(runes[i])
		if bytes+size > maximumBytes {
			break
		}
		start = i
		bytes += size
	}
	return string(runes[start:])
}

func toolOutputPreview(text string, maxLines, maxBytes int) (head string, tail string) {
	lines := strings.Split(text, "\n")
	headLines := (maxLines + 1) / 2
	tailLines := maxLines / 2

	sampled := text
	if len(lines) > maxLines {
		parts := []string{strings.Join(lines[:headLines], "\n")}
		if tailLines > 0 {
			parts = append(parts, strings.Join(lines[len(lines)-tailLines:], "\n"))
		}
		sampled = strings.Join(parts, "\n")
	}
	if len(sampled) <= maxBytes {
		if len(lines) <= maxLines {
			return sampled, ""
		}
		tailValue := ""
		if tailLines > 0 {
			tailValue = strings.Join(lines[len(lines)-tailLines:], "\n")
		}
		return strings.Join(lines[:headLines], "\n"), tailValue
	}
	return toolOutputTakePrefix(sampled, (maxBytes+1)/2), toolOutputTakeSuffix(sampled, maxBytes/2)
}

func toolOutputBoundedPreview(text, marker string, maxLines, maxBytes int) string {
	markerLines := strings.Split(toolOutputTakePrefix(marker, maxBytes), "\n")
	if len(markerLines) > maxLines {
		markerLines = markerLines[:maxLines]
	}
	markerOnly := strings.Join(markerLines, "\n")
	markerBytes := len(marker)
	if maxLines <= 4 || maxBytes <= markerBytes+4 {
		return markerOnly
	}
	head, tail := toolOutputPreview(text, maxLines-4, maxBytes-markerBytes-4)
	if tail != "" {
		return head + "\n\n" + marker + "\n\n" + tail
	}
	return head + "\n\n" + marker
}

func toolOutputLineCount(text string) int {
	count := 1
	for _, r := range text {
		if r == '\n' {
			count++
		}
	}
	return count
}
