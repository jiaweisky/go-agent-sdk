package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/user/go-agent-sdk/core"
	"github.com/user/go-agent-sdk/llm/schema"
)

const GrepName = "grep"

// GrepMaxSafeInteger mirrors Number.MAX_SAFE_INTEGER, the default match bound
// used when the caller does not supply a limit.
const GrepMaxSafeInteger = int64(1)<<53 - 1

// grepDefaultLimit returns Number.MAX_SAFE_INTEGER clamped to the platform int
// range, so 32-bit builds stay representable.
func grepDefaultLimit() int {
	maxInt := int64(^uint(0) >> 1)
	if GrepMaxSafeInteger > maxInt {
		return int(maxInt)
	}
	value := GrepMaxSafeInteger
	return int(value)
}

var GrepInputSchema = schema.JsonSchema{
	"type": "object",
	"properties": map[string]any{
		"pattern": map[string]any{
			"type":        "string",
			"description": "Regex pattern to search for in file contents",
		},
		"path": map[string]any{
			"type":        "string",
			"description": "Relative directory to search. Defaults to the active Location.",
		},
		"include": map[string]any{
			"type":        "string",
			"description": `File glob to include in the search (for example, "*.js" or "*.{ts,tsx}")`,
		},
		"limit": map[string]any{
			"type":             "integer",
			"exclusiveMinimum": 0,
			"description":      "Maximum matches to return",
		},
	},
	"required": []any{"pattern"},
}

var grepEntrySchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"path": map[string]any{"type": "string"},
		"type": map[string]any{"enum": []any{"file", "directory"}},
	},
	"required": []any{"path", "type"},
}

var grepSubmatchSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"text":  map[string]any{"type": "string"},
		"start": map[string]any{"type": "integer", "minimum": 0},
		"end":   map[string]any{"type": "integer", "minimum": 0},
	},
	"required": []any{"text", "start", "end"},
}

var grepMatchSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"entry":      grepEntrySchema,
		"line":       map[string]any{"type": "integer", "exclusiveMinimum": 0},
		"offset":     map[string]any{"type": "integer", "minimum": 0},
		"text":       map[string]any{"type": "string"},
		"submatches": map[string]any{"type": "array", "items": grepSubmatchSchema},
	},
	"required": []any{"entry", "line", "offset", "text", "submatches"},
}

var GrepOutputSchema = schema.JsonSchema{
	"type":  "array",
	"items": grepMatchSchema,
}

type GrepInput struct {
	Pattern string  `json:"pattern"`
	Path    *string `json:"path,omitempty"`
	Include *string `json:"include,omitempty"`
	Limit   *int    `json:"limit,omitempty"`
}

type GrepOutput = []core.FileSystemMatch

// GrepToModelOutput formats raw search matches into the concise model output
// models expect, grouping matches by file and printing bounded line previews.
func GrepToModelOutput(output []core.FileSystemMatch) string {
	lines := make([]string, 0, len(output)+1)
	if len(output) == 0 {
		lines = append(lines, "No files found")
	} else {
		lines = append(lines, fmt.Sprintf("Found %d matches", len(output)))
	}
	current := ""
	for _, match := range output {
		if current != match.Entry.Path {
			if current != "" {
				lines = append(lines, "")
			}
			current = match.Entry.Path
			lines = append(lines, match.Entry.Path+":")
		}
		lines = append(lines, fmt.Sprintf("  Line %d: %s", match.Line, match.Text))
	}
	return strings.Join(lines, "\n")
}

type GrepDeps struct {
	Ripgrep    core.Ripgrep
	Directory  string
	Permission core.Permission
}

func MakeGrep(deps GrepDeps) *Def[GrepInput, GrepOutput, GrepOutput] {
	return Make(Config[GrepInput, GrepOutput, GrepOutput]{
		Description: "Search file contents by regular expression within the active Location or an absolute " +
			"managed tool-output file. Use a path to narrow the search, include to filter files by glob, and " +
			"limit to bound the match count. Returns concise file resources, line numbers, and bounded line previews.",
		Input:  GrepInputSchema,
		Output: GrepOutputSchema,
		ToModelOutput: func(input GrepInput, output any) []Content {
			matches := decodeGrepOutput(output)
			for i := range matches {
				matches[i].Entry.Path = grepResolve(deps.Directory, matches[i].Entry.Path)
			}
			return []Content{TextContent{Text: GrepToModelOutput(matches)}}
		},
		Execute: func(execCtx context.Context, input GrepInput, ctx Context) (GrepOutput, error) {
			if err := execCtx.Err(); err != nil {
				return GrepOutput{}, err
			}
			result, err := executeGrep(deps, input, ctx)
			if err != nil {
				return nil, &Failure{Message: "Unable to grep for " + input.Pattern}
			}
			return result, nil
		},
	})
}

func executeGrep(deps GrepDeps, input GrepInput, ctx Context) (GrepOutput, error) {
	source := &core.PermissionSource{
		Type:      core.PermissionSourceTypeTool,
		MessageID: ctx.AssistantMessageID,
		CallID:    ctx.ToolCallID,
	}

	metadata := map[string]any{"root": "."}
	if input.Path != nil {
		metadata["path"] = *input.Path
	}
	if input.Include != nil {
		metadata["include"] = *input.Include
	}
	if input.Limit != nil {
		metadata["limit"] = *input.Limit
	}

	if err := deps.Permission.Assert(core.PermissionAssertInput{
		SessionID: ctx.SessionID,
		Action:    GrepName,
		Resources: []string{input.Pattern},
		Save:      []string{"*"},
		Metadata:  metadata,
		Source:    source,
		Agent:     &ctx.Agent,
	}); err != nil {
		return nil, err
	}

	value := "."
	if input.Path != nil {
		value = *input.Path
	}
	target := grepResolve(deps.Directory, value)

	info, statErr := core.Stat(target)
	cwd := filepath.Dir(target)
	var file *string
	if statErr == nil && info != nil {
		switch info.Type {
		case "Directory":
			cwd = target
		case "File":
			name := filepath.Base(target)
			file = &name
		}
	}

	limit := grepDefaultLimit()
	if input.Limit != nil {
		limit = *input.Limit
	}

	matches, err := deps.Ripgrep.Grep(core.RipgrepGrepInput{
		Cwd:     cwd,
		Pattern: input.Pattern,
		File:    file,
		Include: input.Include,
		Limit:   limit,
	})
	if err != nil {
		return nil, err
	}

	result := make(GrepOutput, len(matches))
	for i, match := range matches {
		absolute := grepResolve(cwd, match.Entry.Path)
		relative, err := filepath.Rel(deps.Directory, absolute)
		if err != nil {
			return nil, err
		}
		result[i] = match
		result[i].Entry = core.FileSystemEntry{Path: relative, Type: match.Entry.Type}
	}
	return result, nil
}

// grepResolve mirrors path.resolve: absolute inputs are returned cleaned and
// relative inputs are resolved against the base directory.
func grepResolve(directory, value string) string {
	if filepath.IsAbs(value) {
		return filepath.Clean(value)
	}
	return filepath.Join(directory, value)
}

func decodeGrepOutput(output any) []core.FileSystemMatch {
	if matches, ok := output.([]core.FileSystemMatch); ok {
		return matches
	}
	data, err := json.Marshal(output)
	if err != nil {
		return nil
	}
	var matches []core.FileSystemMatch
	if err := json.Unmarshal(data, &matches); err != nil {
		return nil
	}
	return matches
}

func RegisterGrep(registry *Registry, deps GrepDeps) (func(), error) {
	return registry.Register(map[string]Tool{
		GrepName: MakeGrep(deps),
	})
}
