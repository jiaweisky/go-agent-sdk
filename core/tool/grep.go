package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/jiaweisky/go-agent-sdk/core"
	"github.com/jiaweisky/go-agent-sdk/pkg/jsonschema"
)

const GrepName = "grep"

// GrepMaxSafeInteger is the default match bound, used when the caller supplies no
// limit.
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

var GrepInputSchema = jsonschema.Object(
	jsonschema.Required(
		"pattern",
		jsonschema.String(jsonschema.Description("Regex pattern to search for in file contents")),
	),
	jsonschema.Optional(
		"path",
		jsonschema.String(jsonschema.Description("Relative directory to search. Defaults to the active Location.")),
	),
	jsonschema.Optional(
		"include",
		jsonschema.String(jsonschema.Description(`File glob to include in the search (for example, "*.js" or "*.{ts,tsx}")`)),
	),
	jsonschema.Optional("limit", jsonschema.Integer(
		jsonschema.ExclusiveMinimum(0),
		jsonschema.Description("Maximum matches to return"),
	)),
)

var grepEntrySchema = jsonschema.Object(
	jsonschema.Required("path", jsonschema.String()),
	jsonschema.Required("type", jsonschema.Literal("file", "directory")),
)

var grepSubmatchSchema = jsonschema.Object(
	jsonschema.Required("text", jsonschema.String()),
	jsonschema.Required("start", jsonschema.Integer(jsonschema.Minimum(0))),
	jsonschema.Required("end", jsonschema.Integer(jsonschema.Minimum(0))),
)

var grepMatchSchema = jsonschema.Object(
	jsonschema.Required("entry", grepEntrySchema),
	jsonschema.Required("line", jsonschema.Integer(jsonschema.ExclusiveMinimum(0))),
	jsonschema.Required("offset", jsonschema.Integer(jsonschema.Minimum(0))),
	jsonschema.Required("text", jsonschema.String()),
	jsonschema.Required("submatches", jsonschema.Array(grepSubmatchSchema)),
)

var GrepOutputSchema = jsonschema.Array(grepMatchSchema)

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
				return nil, failure("Unable to grep for "+input.Pattern, err)
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
		case core.FileKindDirectory:
			cwd = target
		case core.FileKindFile:
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

// grepResolve resolves a path against the base directory; an absolute input is
// returned cleaned.
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

func RegisterGrep(tools Tools, deps GrepDeps) (func(), error) {
	return tools.Register(map[string]Tool{
		GrepName: MakeGrep(deps),
	})
}
