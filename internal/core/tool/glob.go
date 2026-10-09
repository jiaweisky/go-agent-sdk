package tool

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"

	"github.com/jiaweisky/go-agent-sdk/pkg/core"
	"github.com/jiaweisky/go-agent-sdk/pkg/jsonschema"
)

const GlobName = "glob"

// GlobMaxSafeInteger is the default result bound, used when the caller supplies
// no limit.
const GlobMaxSafeInteger = int64(1)<<53 - 1

// globDefaultLimit returns Number.MAX_SAFE_INTEGER clamped to the platform int
// range, so 32-bit builds stay representable.
func globDefaultLimit() int {
	maxInt := int64(^uint(0) >> 1)
	if GlobMaxSafeInteger > maxInt {
		return int(maxInt)
	}
	value := GlobMaxSafeInteger
	return int(value)
}

var GlobInputSchema = jsonschema.Object(
	jsonschema.Required("pattern", jsonschema.String(jsonschema.Description("Glob pattern to match files against"))),
	jsonschema.Optional(
		"path",
		jsonschema.String(jsonschema.Description("Relative directory to search. Defaults to the active Location.")),
	),
	jsonschema.Optional("limit", jsonschema.Integer(
		jsonschema.ExclusiveMinimum(0),
		jsonschema.Description("Maximum results to return"),
	)),
)

var globEntrySchema = jsonschema.Object(
	jsonschema.Required("path", jsonschema.String()),
	jsonschema.Required("type", jsonschema.Literal("file", "directory")),
)

var GlobOutputSchema = jsonschema.Array(globEntrySchema)

type GlobInput struct {
	Pattern string  `json:"pattern"`
	Path    *string `json:"path,omitempty"`
	Limit   *int    `json:"limit,omitempty"`
}

type GlobOutput = []core.FileSystemEntry

// GlobToModelOutput formats raw search results into the concise line-oriented
// output models expect.
func GlobToModelOutput(output []core.FileSystemEntry) string {
	lines := make([]string, 0, len(output))
	if len(output) == 0 {
		lines = append(lines, "No files found")
	} else {
		for _, item := range output {
			lines = append(lines, item.Path)
		}
	}
	return strings.Join(lines, "\n")
}

type GlobDeps struct {
	Ripgrep    core.Ripgrep
	Directory  string
	Permission core.Permission
}

func MakeGlob(deps GlobDeps) *Def[GlobInput, GlobOutput, GlobOutput] {
	return Make(Config[GlobInput, GlobOutput, GlobOutput]{
		Description: "Find files by glob pattern within the active Location. Returns concise relative file " +
			"resources. Use a relative path to narrow the search and limit to bound the result count.",
		Input:  GlobInputSchema,
		Output: GlobOutputSchema,
		ToModelOutput: func(input GlobInput, output any) []Content {
			entries := decodeGlobOutput(output)
			for i := range entries {
				entries[i].Path = globResolve(deps.Directory, entries[i].Path)
			}
			return []Content{TextContent{Text: GlobToModelOutput(entries)}}
		},
		Execute: func(execCtx context.Context, input GlobInput, ctx Context) (GlobOutput, error) {
			if err := execCtx.Err(); err != nil {
				return GlobOutput{}, err
			}
			result, err := executeGlob(deps, input, ctx)
			if err != nil {
				return nil, failure("Unable to find files matching "+input.Pattern, err)
			}
			return result, nil
		},
	})
}

func executeGlob(deps GlobDeps, input GlobInput, ctx Context) (GlobOutput, error) {
	source := &core.PermissionSource{
		Type:      core.PermissionSourceTypeTool,
		MessageID: ctx.AssistantMessageID,
		CallID:    ctx.ToolCallID,
	}

	metadata := map[string]any{"root": "."}
	if input.Path != nil {
		metadata["root"] = *input.Path
		metadata["path"] = *input.Path
	}
	if input.Limit != nil {
		metadata["limit"] = *input.Limit
	}

	if err := deps.Permission.Assert(core.PermissionAssertInput{
		SessionID: ctx.SessionID,
		Action:    GlobName,
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
	cwd := globResolve(deps.Directory, value)

	limit := globDefaultLimit()
	if input.Limit != nil {
		limit = *input.Limit
	}

	entries, err := deps.Ripgrep.Glob(core.RipgrepGlobInput{
		Cwd:     cwd,
		Pattern: input.Pattern,
		Limit:   limit,
	})
	if err != nil {
		return nil, err
	}

	result := make(GlobOutput, len(entries))
	for i, entry := range entries {
		absolute := globResolve(cwd, entry.Path)
		relative, err := filepath.Rel(deps.Directory, absolute)
		if err != nil {
			return nil, err
		}
		result[i] = core.FileSystemEntry{Path: relative, Type: entry.Type}
	}
	return result, nil
}

// globResolve resolves a path against the base directory; an absolute input is
// returned cleaned.
func globResolve(directory, value string) string {
	if filepath.IsAbs(value) {
		return filepath.Clean(value)
	}
	return filepath.Join(directory, value)
}

func decodeGlobOutput(output any) []core.FileSystemEntry {
	if entries, ok := output.([]core.FileSystemEntry); ok {
		return entries
	}
	data, err := json.Marshal(output)
	if err != nil {
		return nil
	}
	var entries []core.FileSystemEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil
	}
	return entries
}

func RegisterGlob(tools Tools, deps GlobDeps) (func(), error) {
	return tools.Register(map[string]Tool{
		GlobName: MakeGlob(deps),
	})
}
