package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf16"
)

// Ripgrep is a small ripgrep execution adapter. It deliberately exposes raw
// process-oriented rows, not model text or permission behavior: search maps these
// rows into filesystem results, and the leaf tools own presentation and
// permission prompts.

const (
	ripgrepErrorBytes     = 8 * 1024
	ripgrepMaxRecordBytes = 64 * 1024
	ripgrepMaxSubmatches  = 100
)

type RipgrepError struct {
	Message string
	Cause   error
}

func (e *RipgrepError) Error() string { return e.Message }

func (e *RipgrepError) Unwrap() error { return e.Cause }

type RipgrepInvalidPatternError struct {
	Pattern string
	Message string
}

func (e *RipgrepInvalidPatternError) Error() string { return e.Message }

type RipgrepFindInput struct {
	Cwd     string
	Pattern string
	Limit   int
	Hidden  bool
	Follow  bool
	Context context.Context
	OnEntry func(FileSystemEntry) error
}

type RipgrepGlobInput struct {
	Cwd     string
	Pattern string
	Limit   int
	Hidden  bool
	Follow  bool
	Context context.Context
}

type RipgrepGrepInput struct {
	Cwd     string
	Pattern string
	File    *string
	Include *string
	Limit   int
	Context context.Context
}

type Ripgrep interface {
	Find(input RipgrepFindInput) ([]FileSystemEntry, error)
	Glob(input RipgrepGlobInput) ([]FileSystemEntry, error)
	Grep(input RipgrepGrepInput) ([]FileSystemMatch, error)
}

type RipgrepBinary interface {
	Filepath() (string, error)
}

type ripgrepService struct {
	process AppProcess
	binary  RipgrepBinary
}

func NewRipgrep(process AppProcess, binary RipgrepBinary) Ripgrep {
	return &ripgrepService{process: process, binary: binary}
}

type ripgrepRunResult[A any] struct {
	Items     []A
	Truncated bool
	Partial   bool
}

type ripgrepRunInput[A any] struct {
	Cwd     string
	Args    []string
	Limit   int
	Pattern string
	Context context.Context
	Parse   func(line string) (*A, error)
	OnItem  func(A) error
}

// ripgrepRun runs ripgrep and parses its rows, stopping after limit+1 rows as the
// stream bound requires.
func ripgrepRun[A any](
	process AppProcess,
	binary RipgrepBinary,
	input ripgrepRunInput[A],
) (*ripgrepRunResult[A], error) {
	path, err := binary.Filepath()
	if err != nil {
		return nil, err
	}

	ctx := input.Context
	if ctx == nil {
		ctx = context.Background()
	}
	maxError := ripgrepErrorBytes
	result, err := process.Run(ctx, path, input.Args, RunOptions{
		Dir:           input.Cwd,
		ExtendEnv:     true,
		MaxErrorBytes: &maxError,
	})
	if err != nil {
		return nil, err
	}

	limit := input.Limit
	if limit < 0 {
		limit = 0
	}
	collected, err := ripgrepCollect(input, string(result.Stdout), limit)
	if err != nil {
		return nil, err
	}
	if len(collected) > limit {
		return &ripgrepRunResult[A]{Items: collected[:limit], Truncated: true}, nil
	}
	return ripgrepInterpret(result.ExitCode, string(result.Stderr), input.Pattern, collected)
}

// ripgrepCollect parses at most limit+1 rows so the caller can detect
// truncation.
func ripgrepCollect[A any](input ripgrepRunInput[A], stdout string, limit int) ([]A, error) {
	take := limit + 1
	if take < limit {
		take = limit
	}
	var collected []A
	for _, line := range ripgrepSplitLines(stdout) {
		if line == "" {
			continue
		}
		if len(collected) >= take {
			break
		}
		row, parseErr := input.Parse(line)
		if parseErr != nil {
			return nil, parseErr
		}
		if row == nil {
			continue
		}
		if input.OnItem != nil && len(collected) < limit {
			if err := input.OnItem(*row); err != nil {
				return nil, err
			}
		}
		collected = append(collected, *row)
	}
	return collected, nil
}

// ripgrepInterpret maps ripgrep's exit code to a result or error.
func ripgrepInterpret[A any](code int, stderr, pattern string, collected []A) (*ripgrepRunResult[A], error) {
	if pattern != "" && code == 2 && ripgrepIsInvalidPattern(stderr) {
		return nil, &RipgrepInvalidPatternError{Pattern: pattern, Message: strings.TrimSpace(stderr)}
	}
	if code != 0 && code != 1 && code != 2 {
		message := strings.TrimSpace(stderr)
		if message == "" {
			message = fmt.Sprintf("ripgrep failed with code %d", code)
		}
		return nil, ripgrepFailure(message, nil)
	}
	if code == 1 {
		return &ripgrepRunResult[A]{Items: []A{}}, nil
	}
	return &ripgrepRunResult[A]{Items: collected, Partial: code == 2}, nil
}

func (s *ripgrepService) Glob(input RipgrepGlobInput) ([]FileSystemEntry, error) {
	args := []string{"--no-config", "--files"}
	if input.Hidden {
		args = append(args, "--hidden")
	}
	if input.Follow {
		args = append(args, "--follow")
	}
	args = append(args, "--glob="+input.Pattern, "--glob=!**/.git/**", ".")

	result, err := ripgrepRun(s.process, s.binary, ripgrepRunInput[string]{
		Cwd:     input.Cwd,
		Args:    args,
		Limit:   input.Limit,
		Context: input.Context,
		Parse: func(line string) (*string, error) {
			value := ripgrepNormalizeRelative(line)
			return &value, nil
		},
	})
	if err != nil {
		return nil, ripgrepAsFailure(err)
	}
	entries := make([]FileSystemEntry, len(result.Items))
	for i, relative := range result.Items {
		entries[i] = FileSystemEntry{Path: relative, Type: FileKindFile}
	}
	return entries, nil
}

func (s *ripgrepService) Find(input RipgrepFindInput) ([]FileSystemEntry, error) {
	args := []string{"--no-config", "--files"}
	if input.Hidden {
		args = append(args, "--hidden")
	}
	if input.Follow {
		args = append(args, "--follow")
	}
	if input.Pattern != "*" {
		args = append(args, "--glob="+input.Pattern)
	}
	args = append(args, "--glob=!**/.git/**", ".")

	result, err := ripgrepRun(s.process, s.binary, ripgrepRunInput[FileSystemEntry]{
		Cwd:     input.Cwd,
		Args:    args,
		Limit:   input.Limit,
		Context: input.Context,
		Parse: func(line string) (*FileSystemEntry, error) {
			entry := FileSystemEntry{Path: ripgrepNormalizeRelative(line), Type: FileKindFile}
			return &entry, nil
		},
		OnItem: input.OnEntry,
	})
	if err != nil {
		return nil, ripgrepAsFailure(err)
	}
	return result.Items, nil
}

type ripgrepRawMatch struct {
	Type string `json:"type"`
	Data struct {
		Path struct {
			Text string `json:"text"`
		} `json:"path"`
		Lines struct {
			Text string `json:"text"`
		} `json:"lines"`
		LineNumber     int `json:"line_number"`
		AbsoluteOffset int `json:"absolute_offset"`
		Submatches     []struct {
			Match struct {
				Text string `json:"text"`
			} `json:"match"`
			Start int `json:"start"`
			End   int `json:"end"`
		} `json:"submatches"`
	} `json:"data"`
}

func (s *ripgrepService) Grep(input RipgrepGrepInput) ([]FileSystemMatch, error) {
	args := []string{"--no-config", "--json", "--hidden", "--no-messages"}
	if input.Include != nil && *input.Include != "" {
		args = append(args, "--glob="+*input.Include)
	}
	args = append(args, "--glob=!**/.git/**", "--", input.Pattern)
	if input.File != nil {
		args = append(args, *input.File)
	} else {
		args = append(args, ".")
	}

	result, err := ripgrepRun(s.process, s.binary, ripgrepRunInput[FileSystemMatch]{
		Cwd:     input.Cwd,
		Args:    args,
		Limit:   input.Limit,
		Pattern: input.Pattern,
		Context: input.Context,
		Parse:   parseRipgrepMatch,
	})
	if err != nil {
		return nil, err
	}
	return result.Items, nil
}

func parseRipgrepMatch(line string) (*FileSystemMatch, error) {
	if len(line) > ripgrepMaxRecordBytes {
		return nil, ripgrepFailure(fmt.Sprintf("Ripgrep JSON record exceeded %d bytes", ripgrepMaxRecordBytes), nil)
	}
	var value any
	if err := json.Unmarshal([]byte(line), &value); err != nil {
		return nil, ripgrepFailure("Invalid ripgrep JSON output", err)
	}
	object, ok := value.(map[string]any)
	if !ok || object["type"] != "match" {
		return nil, nil
	}
	var raw ripgrepRawMatch
	if err := json.Unmarshal([]byte(line), &raw); err != nil {
		return nil, ripgrepFailure("Invalid ripgrep match output", err)
	}

	submatches := raw.Data.Submatches
	if len(submatches) > ripgrepMaxSubmatches {
		submatches = submatches[:ripgrepMaxSubmatches]
	}
	relative := ripgrepNormalizeRelative(ripgrepSingleDotSlash.ReplaceAllString(raw.Data.Path.Text, ""))
	match := FileSystemMatch{
		Entry:      FileSystemEntry{Path: relative, Type: FileKindFile},
		Line:       raw.Data.LineNumber,
		Offset:     raw.Data.AbsoluteOffset,
		Text:       ripgrepTruncateText(raw.Data.Lines.Text),
		Submatches: make([]FileSystemSubmatch, len(submatches)),
	}
	for i, submatch := range submatches {
		match.Submatches[i] = FileSystemSubmatch{
			Text:  submatch.Match.Text,
			Start: submatch.Start,
			End:   submatch.End,
		}
	}
	return &match, nil
}

var (
	ripgrepLeadingDotSlash = regexp.MustCompile(`^(?:\.[\\/])+`)
	ripgrepLeadingSlash    = regexp.MustCompile(`^[\\/]+`)
	ripgrepSingleDotSlash  = regexp.MustCompile(`^\.[\\/]`)
)

func ripgrepNormalizeRelative(value string) string {
	value = ripgrepLeadingDotSlash.ReplaceAllString(value, "")
	value = ripgrepLeadingSlash.ReplaceAllString(value, "")
	return strings.ReplaceAll(value, "\\", "/")
}

// ripgrepTruncateText truncates text to 2000 UTF-16 code units, dropping a
// trailing lone high surrogate and appending "...".
func ripgrepTruncateText(value string) string {
	const maximum = 2000
	units := utf16.Encode([]rune(value))
	if len(units) <= maximum {
		return value
	}
	units = units[:maximum]
	if last := units[len(units)-1]; last >= 0xD800 && last <= 0xDBFF {
		units = units[:len(units)-1]
	}
	return string(utf16.Decode(units)) + "..."
}

func ripgrepIsInvalidPattern(stderr string) bool {
	return strings.Contains(stderr, "regex parse error") || strings.Contains(stderr, "error parsing regex")
}

// ripgrepSplitLines splits on `\n`, `\r`, and `\r\n` delimiters.
func ripgrepSplitLines(value string) []string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	return strings.Split(value, "\n")
}

func ripgrepFailure(message string, cause error) error {
	return &RipgrepError{Message: message, Cause: cause}
}

func ripgrepAsFailure(err error) error {
	var invalid *RipgrepInvalidPatternError
	if errors.As(err, &invalid) {
		return ripgrepFailure(invalid.Message, invalid)
	}
	return err
}
