package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/text/encoding/unicode"

	"github.com/user/go-agent-sdk/core"
	"github.com/user/go-agent-sdk/core/util"
	"github.com/user/go-agent-sdk/jsonschema"
	coreschema "github.com/user/go-agent-sdk/schema"
)

// EditTool is the model-facing V2 exact-edit leaf. Relative paths resolve
// within the active Location. Absolute paths inside that Location are accepted,
// while explicit absolute external paths retain mutation capability through a
// separate external_directory approval before edit approval.
const EditName = "edit"

var EditInputSchema = jsonschema.Object(
	jsonschema.Required("path", jsonschema.String(jsonschema.Description(
		"File path to edit. Relative paths resolve within the active Location. "+
			"Absolute paths inside that Location are accepted; external absolute paths require "+
			"external_directory approval."))),
	jsonschema.Required("oldString", jsonschema.String(jsonschema.Description("Exact text to replace"))),
	jsonschema.Required("newString", jsonschema.String(jsonschema.Description("Replacement text, which must differ from oldString"))),
	jsonschema.Optional("replaceAll", jsonschema.Boolean(jsonschema.Description("Replace all exact occurrences of oldString (default false)"))),
)

var editFileDiffSchema = jsonschema.Object(
	jsonschema.Optional("file", jsonschema.String()),
	jsonschema.Optional("patch", jsonschema.String()),
	jsonschema.Required("additions", jsonschema.Number()),
	jsonschema.Required("deletions", jsonschema.Number()),
	jsonschema.Optional("status", jsonschema.Literal("added", "deleted", "modified")),
)

var EditOutputSchema = jsonschema.Object(
	jsonschema.Required("files", jsonschema.Array(editFileDiffSchema)),
	jsonschema.Required("replacements", jsonschema.Number()),
)

type EditInput struct {
	Path       string `json:"path"`
	OldString  string `json:"oldString"`
	NewString  string `json:"newString"`
	ReplaceAll bool   `json:"replaceAll"`
}

type EditOutput struct {
	Files        []coreschema.FileDiffInfo `json:"files"`
	Replacements int                       `json:"replacements"`
}

type EditDeps struct {
	Mutation   core.LocationMutation
	Files      core.FileMutation
	Permission core.Permission
}

func MakeEdit(deps EditDeps) *Def[EditInput, EditOutput, EditOutput] {
	return Make(Config[EditInput, EditOutput, EditOutput]{
		Description: "Replace exact text in one file. Relative paths resolve within the active Location. " +
			"Absolute paths inside the Location are accepted. Explicit external absolute paths require " +
			"external_directory approval before edit approval.",
		Input:  EditInputSchema,
		Output: EditOutputSchema,
		ToModelOutput: func(input EditInput, output any) []Content {
			var out EditOutput
			if b, err := json.Marshal(output); err == nil {
				json.Unmarshal(b, &out)
			}
			return []Content{TextContent{Text: EditToModelOutput(out, input.OldString, input.NewString)}}
		},
		Execute: func(execCtx context.Context, input EditInput, ctx Context) (EditOutput, error) {
			if err := execCtx.Err(); err != nil {
				return EditOutput{}, err
			}
			result, err := executeEdit(deps, input, ctx)
			if err != nil {
				return EditOutput{}, editUnableToEdit(err, input.Path)
			}
			return result, nil
		},
	})
}

// Deferred V2 edit behavior and UX integrations remain visible at the model-facing seam.
// TODO: Port V1 fuzzy correction strategies only after exact-edit behavior is established:
// line-trimmed matching, block-anchor fallback, indentation correction, and similarity-threshold review.
// TODO: Add formatter integration after V2 formatter runtime exists.
// TODO: Publish watcher/file-edit events after V2 watcher integration exists.
// TODO: Add snapshots / undo after design exists.
// TODO: Add LSP notification and diagnostics after V2 LSP runtime exists.

func EditToModelOutput(output EditOutput, oldString, newString string) string {
	file := ""
	if len(output.Files) > 0 && output.Files[0].File != nil {
		file = *output.Files[0].File
	}
	lines := []string{
		"Edited file successfully: " + file,
		fmt.Sprintf("Replacements: %d", output.Replacements),
		"```diff",
	}
	lines = append(lines, editPreviewLines(oldString, "-")...)
	lines = append(lines, editPreviewLines(newString, "+")...)
	lines = append(lines, "```")
	return strings.Join(lines, "\n")
}

func executeEdit(deps EditDeps, input EditInput, ctx Context) (EditOutput, error) {
	source := &core.PermissionSource{
		Type:      core.PermissionSourceTypeTool,
		MessageID: ctx.AssistantMessageID,
		CallID:    ctx.ToolCallID,
	}

	if input.OldString == input.NewString {
		return EditOutput{}, &Failure{
			Message: "No changes to apply: oldString and newString are identical.",
		}
	}
	if input.OldString == "" {
		return EditOutput{}, &Failure{
			Message: "oldString must not be empty. Use write to create or overwrite a file.",
		}
	}

	kind := core.LocationMutationKindFile
	target, err := deps.Mutation.Resolve(core.LocationMutationResolveInput{Path: input.Path, Kind: &kind})
	if err != nil {
		return EditOutput{}, err
	}

	if external := target.ExternalDirectory; external != nil {
		extPerm := core.LocationMutationExternalDirectoryPermission(*external)
		if err := deps.Permission.Assert(core.PermissionAssertInput{
			SessionID: ctx.SessionID,
			Action:    extPerm.Action,
			Resources: extPerm.Resources,
			Save:      extPerm.Save,
			Source:    source,
			Agent:     &ctx.Agent,
		}); err != nil {
			return EditOutput{}, err
		}
	}

	if err := deps.Permission.Assert(core.PermissionAssertInput{
		SessionID: ctx.SessionID,
		Action:    "edit",
		Resources: []string{target.Resource},
		Save:      []string{"*"},
		Source:    source,
		Agent:     &ctx.Agent,
	}); err != nil {
		return EditOutput{}, err
	}

	content, err := core.ReadFileBytes(target.Canonical)
	if err != nil {
		return EditOutput{}, err
	}
	decoded := editDecodeUtf8(content)
	ending := editDetectLineEnding(decoded.Text)
	oldString := editConvertToLineEnding(input.OldString, ending)
	newString := editConvertToLineEnding(input.NewString, ending)
	replacements := editCountOccurrences(decoded.Text, oldString)
	if replacements == 0 {
		return EditOutput{}, &Failure{
			Message: "Could not find oldString in the file. It must match exactly, including whitespace and indentation.",
		}
	}
	if replacements > 1 && !input.ReplaceAll {
		return EditOutput{}, &Failure{
			Message: "Found multiple exact matches for oldString. Provide more surrounding context or set replaceAll to true.",
		}
	}

	var replaced string
	if input.ReplaceAll {
		replaced = strings.ReplaceAll(decoded.Text, oldString, newString)
	} else {
		replaced = strings.Replace(decoded.Text, oldString, newString, 1)
	}
	additions, deletions := editDiffCounts(decoded.Text, replaced)

	next := editSplitBom(replaced)
	written, err := deps.Files.WriteIfUnchanged(core.FileMutationConditionalWriteInput{
		Target:   core.FileMutationTarget{Canonical: target.Canonical, Resource: target.Resource},
		Expected: decoded.Content,
		Content:  []byte(editJoinBom(next.Text, decoded.Bom || next.Bom)),
	})
	if err != nil {
		return EditOutput{}, err
	}

	resource := written.Resource
	patch := util.CreateTwoFilesPatch(resource, resource, decoded.Text, replaced)
	status := coreschema.FileDiffStatusModified
	return EditOutput{
		Files: []coreschema.FileDiffInfo{{
			File:      &resource,
			Patch:     &patch,
			Additions: additions,
			Deletions: deletions,
			Status:    &status,
		}},
		Replacements: replacements,
	}, nil
}

func editUnableToEdit(err error, path string) error {
	var declined *core.PermissionDeclinedError
	var rejected *core.QuestionRejectedError
	if errors.As(err, &declined) || errors.As(err, &rejected) {
		return err
	}
	var existing *Failure
	if errors.As(err, &existing) {
		return err
	}
	var stale *core.FileMutationStaleContentError
	if errors.As(err, &stale) {
		return &Failure{Message: "File changed after permission approval. Read it again before editing."}
	}
	return &Failure{Message: fmt.Sprintf("Unable to edit %s", path)}
}

type editBom struct {
	Bom  bool
	Text string
}

func editSplitBom(text string) editBom {
	if strings.HasPrefix(text, "\ufeff") {
		return editBom{Bom: true, Text: text[len("\ufeff"):]}
	}
	return editBom{Bom: false, Text: text}
}

func editJoinBom(text string, bom bool) string {
	if bom {
		return "\ufeff" + text
	}
	return text
}

type editDecoded struct {
	Bom     bool
	Content []byte
	Text    string
}

func editDecodeUtf8(content []byte) editDecoded {
	bom := len(content) >= 3 && content[0] == 0xef && content[1] == 0xbb && content[2] == 0xbf
	raw := content
	if bom {
		raw = content[3:]
	}
	// Match `new TextDecoder().decode(...)`: invalid sequences are replaced
	// with U+FFFD using the W3C maximal-subpart rule. unicode.UTF8 neither
	// strips nor adds a BOM, so the BOM handling above stays in charge.
	decoded, err := unicode.UTF8.NewDecoder().Bytes(raw)
	if err != nil {
		decoded = raw
	}
	return editDecoded{Bom: bom, Content: content, Text: string(decoded)}
}

func editNormalizeLineEndings(text string) string {
	return strings.ReplaceAll(text, "\r\n", "\n")
}

func editDetectLineEnding(text string) string {
	if strings.Contains(text, "\r\n") {
		return "\r\n"
	}
	return "\n"
}

func editConvertToLineEnding(text, ending string) string {
	if ending == "\n" {
		return editNormalizeLineEndings(text)
	}
	return strings.ReplaceAll(editNormalizeLineEndings(text), "\n", "\r\n")
}

func editCountOccurrences(content, search string) int {
	if search == "" {
		return len(content) + 1
	}
	count := 0
	offset := 0
	for {
		index := strings.Index(content[offset:], search)
		if index == -1 {
			break
		}
		count++
		offset += index + len(search)
	}
	return count
}

func editPreviewLines(value, prefix string) []string {
	lines := strings.Split(editNormalizeLineEndings(value), "\n")
	limit := len(lines)
	if limit > 6 {
		limit = 6
	}
	shown := make([]string, 0, limit+1)
	for _, line := range lines[:limit] {
		runes := []rune(line)
		if len(runes) > 240 {
			line = string(runes[:240]) + "..."
		}
		shown = append(shown, prefix+line)
	}
	if len(lines) > len(shown) {
		shown = append(shown, prefix+"...")
	}
	return shown
}

func editDiffCounts(oldStr, newStr string) (additions, deletions int) {
	for _, item := range util.DiffLines(oldStr, newStr) {
		if item.Added {
			additions += item.Count
		}
		if item.Removed {
			deletions += item.Count
		}
	}
	return additions, deletions
}

func WithEditPermission(tool *Def[EditInput, EditOutput, EditOutput]) *Def[EditInput, EditOutput, EditOutput] {
	return WithPermission(tool, "edit")
}

func RegisterEdit(tools Tools, deps EditDeps) (func(), error) {
	return tools.Register(map[string]Tool{
		EditName: WithEditPermission(MakeEdit(deps)),
	})
}
