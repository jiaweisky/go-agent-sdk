package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/user/go-agent-sdk/core"
	"github.com/user/go-agent-sdk/core/util"
	"github.com/user/go-agent-sdk/jsonschema"
	coreschema "github.com/user/go-agent-sdk/schema"
)

const ApplyPatchName = "apply_patch"

var ApplyPatchInputSchema = jsonschema.Object(
	jsonschema.Required("patchText", jsonschema.String(jsonschema.Description("The full patch text describing add, update, and delete operations"))),
)

var applyPatchAppliedSchema = jsonschema.Object(
	jsonschema.Required("type", jsonschema.Literal("add", "update", "delete")),
	jsonschema.Required("resource", jsonschema.String()),
	jsonschema.Required("target", jsonschema.String()),
)

var ApplyPatchOutputSchema = jsonschema.Object(
	jsonschema.Required("applied", jsonschema.Array(applyPatchAppliedSchema)),
	jsonschema.Required("files", jsonschema.Array(editFileDiffSchema)),
)

type ApplyPatchInput struct {
	PatchText string `json:"patchText"`
}

type ApplyPatchApplied struct {
	Type     string `json:"type"`
	Resource string `json:"resource"`
	Target   string `json:"target"`
}

type ApplyPatchOutput struct {
	Applied []ApplyPatchApplied       `json:"applied"`
	Files   []coreschema.FileDiffInfo `json:"files"`
}

func ApplyPatchToModelOutput(output ApplyPatchOutput) string {
	lines := []string{"Applied patch sequentially:"}
	for _, item := range output.Applied {
		prefix := "M"
		switch item.Type {
		case "add":
			prefix = "A"
		case "delete":
			prefix = "D"
		}
		lines = append(lines, prefix+" "+item.Resource)
	}
	return strings.Join(lines, "\n")
}

type ApplyPatchDeps struct {
	Mutation   core.LocationMutation
	Files      core.FileMutation
	Permission core.Permission
}

func MakeApplyPatch(deps ApplyPatchDeps) *Def[ApplyPatchInput, ApplyPatchOutput, ApplyPatchOutput] {
	return Make(Config[ApplyPatchInput, ApplyPatchOutput, ApplyPatchOutput]{
		Description: "Apply one patch containing add, update, and delete file operations. " +
			"All targets are resolved and approved before target contents are read. " +
			"Operations apply sequentially; if a later operation fails, earlier operations remain " +
			"applied and the failure reports them explicitly. Moves and atomic rollback are not " +
			"supported yet.",
		Input:  ApplyPatchInputSchema,
		Output: ApplyPatchOutputSchema,
		ToModelOutput: func(input ApplyPatchInput, output any) []Content {
			var out ApplyPatchOutput
			if b, err := json.Marshal(output); err == nil {
				json.Unmarshal(b, &out)
			}
			return []Content{TextContent{Text: ApplyPatchToModelOutput(out)}}
		},
		Execute: func(execCtx context.Context, input ApplyPatchInput, ctx Context) (ApplyPatchOutput, error) {
			if err := execCtx.Err(); err != nil {
				return ApplyPatchOutput{}, err
			}
			return executeApplyPatch(deps, input, ctx)
		},
	})
}

func WithApplyPatchPermission(tool *Def[ApplyPatchInput, ApplyPatchOutput, ApplyPatchOutput]) *Def[ApplyPatchInput, ApplyPatchOutput, ApplyPatchOutput] {
	return WithPermission(tool, "edit")
}

func RegisterApplyPatch(tools Tools, deps ApplyPatchDeps) (func(), error) {
	return tools.Register(map[string]Tool{
		ApplyPatchName: WithApplyPatchPermission(MakeApplyPatch(deps)),
	})
}

type applyPatchRun struct {
	deps    ApplyPatchDeps
	ctx     Context
	applied []ApplyPatchApplied
}

func executeApplyPatch(deps ApplyPatchDeps, input ApplyPatchInput, ctx Context) (ApplyPatchOutput, error) {
	run := &applyPatchRun{deps: deps, ctx: ctx, applied: []ApplyPatchApplied{}}
	output, err := run.run(input)
	if err != nil {
		var failure *Failure
		if errors.As(err, &failure) {
			return ApplyPatchOutput{}, err
		}
		return ApplyPatchOutput{}, run.fail("patch")
	}
	return output, nil
}

func (r *applyPatchRun) fail(path string) error {
	prefix := fmt.Sprintf("Unable to apply patch at %s", path)
	if len(r.applied) > 0 {
		resources := make([]string, len(r.applied))
		for i, item := range r.applied {
			resources[i] = item.Resource
		}
		prefix = fmt.Sprintf(
			"Patch partially applied before failing at %s. Applied: %s",
			path,
			strings.Join(resources, ", "),
		)
	}
	return &Failure{Message: prefix}
}

func (r *applyPatchRun) run(input ApplyPatchInput) (ApplyPatchOutput, error) {
	ctx := r.ctx
	source := &core.PermissionSource{
		Type:      core.PermissionSourceTypeTool,
		MessageID: ctx.AssistantMessageID,
		CallID:    ctx.ToolCallID,
	}

	if strings.TrimSpace(input.PatchText) == "" {
		return ApplyPatchOutput{}, &Failure{Message: "patchText is required"}
	}
	hunks, err := core.PatchParse(input.PatchText)
	if err != nil {
		return ApplyPatchOutput{}, &Failure{Message: fmt.Sprintf("apply_patch verification failed: %s", err)}
	}
	if len(hunks) == 0 {
		return ApplyPatchOutput{}, &Failure{Message: "patch rejected: empty patch"}
	}
	for _, hunk := range hunks {
		if update, ok := hunk.(core.PatchUpdateHunk); ok && update.MovePath != nil {
			return ApplyPatchOutput{}, &Failure{Message: "apply_patch moves are not supported yet"}
		}
	}

	type targetEntry struct {
		hunk   core.PatchHunk
		target *core.LocationMutationTarget
	}
	kind := core.LocationMutationKindFile
	targets := make([]targetEntry, 0, len(hunks))
	for _, hunk := range hunks {
		target, err := r.deps.Mutation.Resolve(core.LocationMutationResolveInput{
			Path: patchHunkPath(hunk),
			Kind: &kind,
		})
		if err != nil {
			return ApplyPatchOutput{}, r.fail("patch")
		}
		targets = append(targets, targetEntry{hunk: hunk, target: target})
	}

	var externalOrder []string
	externalDirectories := make(map[string]core.LocationMutationExternalDirectoryAuthorization)
	for _, entry := range targets {
		if external := entry.target.ExternalDirectory; external != nil {
			if _, ok := externalDirectories[external.Resource]; !ok {
				externalOrder = append(externalOrder, external.Resource)
			}
			externalDirectories[external.Resource] = *external
		}
	}
	for _, key := range externalOrder {
		externalPermission := core.LocationMutationExternalDirectoryPermission(externalDirectories[key])
		if err := r.deps.Permission.Assert(core.PermissionAssertInput{
			SessionID: ctx.SessionID,
			Action:    externalPermission.Action,
			Resources: externalPermission.Resources,
			Save:      externalPermission.Save,
			Source:    source,
			Agent:     &ctx.Agent,
		}); err != nil {
			if decline := userDecline(err); decline != nil {
				return ApplyPatchOutput{}, decline
			}
			return ApplyPatchOutput{}, r.fail("patch")
		}
	}

	var editResources []string
	seen := make(map[string]bool)
	for _, entry := range targets {
		if !seen[entry.target.Resource] {
			seen[entry.target.Resource] = true
			editResources = append(editResources, entry.target.Resource)
		}
	}
	if err := r.deps.Permission.Assert(core.PermissionAssertInput{
		SessionID: ctx.SessionID,
		Action:    "edit",
		Resources: editResources,
		Save:      []string{"*"},
		Source:    source,
		Agent:     &ctx.Agent,
	}); err != nil {
		if decline := userDecline(err); decline != nil {
			return ApplyPatchOutput{}, decline
		}
		return ApplyPatchOutput{}, r.fail("patch")
	}

	prepared := make([]applyPatchPrepared, 0, len(targets))
	for _, entry := range targets {
		change, err := prepareApplyPatch(entry.hunk, entry.target)
		if err != nil {
			return ApplyPatchOutput{}, r.fail(patchHunkPath(entry.hunk))
		}
		prepared = append(prepared, change)
	}

	patchFiles := make([]coreschema.FileDiffInfo, len(prepared))
	for i, change := range prepared {
		patchFiles[i] = applyPatchFile(change)
	}

	for _, change := range prepared {
		switch change.hunk.(type) {
		case core.PatchAddHunk:
			content := change.contents
			if !strings.HasSuffix(content, "\n") && content != "" {
				content += "\n"
			}
			result, err := r.deps.Files.Create(core.FileMutationWriteInput{
				Target: core.FileMutationTarget{
					Canonical: change.target.Canonical,
					Resource:  change.target.Resource,
				},
				Content: []byte(content),
			})
			if err != nil {
				return ApplyPatchOutput{}, r.fail(change.path)
			}
			r.applied = append(r.applied, ApplyPatchApplied{
				Type:     "add",
				Resource: result.Resource,
				Target:   result.Target,
			})
		case core.PatchDeleteHunk:
			result, err := r.deps.Files.Remove(core.FileMutationRemoveInput{
				Target: core.FileMutationTarget{
					Canonical: change.target.Canonical,
					Resource:  change.target.Resource,
				},
			})
			if err != nil {
				return ApplyPatchOutput{}, r.fail(change.path)
			}
			r.applied = append(r.applied, ApplyPatchApplied{
				Type:     "delete",
				Resource: result.Resource,
				Target:   result.Target,
			})
		default:
			result, err := r.deps.Files.WriteIfUnchanged(core.FileMutationConditionalWriteInput{
				Target: core.FileMutationTarget{
					Canonical: change.target.Canonical,
					Resource:  change.target.Resource,
				},
				Expected: change.source,
				Content:  []byte(change.content),
			})
			if err != nil {
				return ApplyPatchOutput{}, r.fail(change.path)
			}
			r.applied = append(r.applied, ApplyPatchApplied{
				Type:     "update",
				Resource: result.Resource,
				Target:   result.Target,
			})
		}
	}

	return ApplyPatchOutput{Applied: r.applied, Files: patchFiles}, nil
}

type applyPatchPrepared struct {
	hunk     core.PatchHunk
	path     string
	target   *core.LocationMutationTarget
	contents string
	source   []byte
	content  string
	before   string
	after    string
}

func prepareApplyPatch(hunk core.PatchHunk, target *core.LocationMutationTarget) (applyPatchPrepared, error) {
	switch h := hunk.(type) {
	case core.PatchAddHunk:
		after := h.Contents
		if !strings.HasSuffix(h.Contents, "\n") && h.Contents != "" {
			after = h.Contents + "\n"
		}
		return applyPatchPrepared{
			hunk:     hunk,
			path:     h.Path,
			target:   target,
			contents: h.Contents,
			before:   "",
			after:    after,
		}, nil
	case core.PatchDeleteHunk:
		if err := applyPatchAssertFile(target.Canonical); err != nil {
			return applyPatchPrepared{}, err
		}
		source, err := core.ReadFileBytes(target.Canonical)
		if err != nil {
			return applyPatchPrepared{}, err
		}
		before := strings.TrimPrefix(applyPatchDecodeUtf8(source), "\ufeff")
		return applyPatchPrepared{
			hunk:   hunk,
			path:   h.Path,
			target: target,
			before: before,
			after:  "",
		}, nil
	case core.PatchUpdateHunk:
		if err := applyPatchAssertFile(target.Canonical); err != nil {
			return applyPatchPrepared{}, err
		}
		source, err := core.ReadFileBytes(target.Canonical)
		if err != nil {
			return applyPatchPrepared{}, err
		}
		original := applyPatchDecodeUtf8(source)
		before := strings.TrimPrefix(original, "\ufeff")
		update, err := core.PatchDerive(h.Path, h.Chunks, original)
		if err != nil {
			return applyPatchPrepared{}, err
		}
		return applyPatchPrepared{
			hunk:    hunk,
			path:    h.Path,
			target:  target,
			source:  source,
			content: core.PatchJoinBom(update.Content, update.Bom),
			before:  before,
			after:   update.Content,
		}, nil
	default:
		return applyPatchPrepared{}, fmt.Errorf("unknown patch hunk")
	}
}

func applyPatchAssertFile(path string) error {
	info, err := core.Stat(path)
	if err != nil {
		return err
	}
	if info.Type != "File" {
		return fmt.Errorf("not a file: %s", path)
	}
	return nil
}

// applyPatchDecodeUtf8 decodes UTF-8, replacing invalid sequences with U+FFFD and
// keeping a leading BOM (the caller strips it from `before`).
func applyPatchDecodeUtf8(content []byte) string {
	return strings.ToValidUTF8(string(content), "\uFFFD")
}

func patchHunkPath(hunk core.PatchHunk) string {
	switch h := hunk.(type) {
	case core.PatchAddHunk:
		return h.Path
	case core.PatchDeleteHunk:
		return h.Path
	case core.PatchUpdateHunk:
		return h.Path
	default:
		return ""
	}
}

func applyPatchFile(change applyPatchPrepared) coreschema.FileDiffInfo {
	additions := 0
	deletions := 0
	for _, item := range util.DiffLines(change.before, change.after) {
		if item.Added {
			additions += item.Count
		}
		if item.Removed {
			deletions += item.Count
		}
	}
	file := change.target.Resource
	patch := util.CreateTwoFilesPatch(file, file, change.before, change.after)
	var status coreschema.FileDiffStatus
	switch change.hunk.(type) {
	case core.PatchAddHunk:
		status = coreschema.FileDiffStatusAdded
	case core.PatchDeleteHunk:
		status = coreschema.FileDiffStatusDeleted
	default:
		status = coreschema.FileDiffStatusModified
	}
	return coreschema.FileDiffInfo{
		File:      &file,
		Patch:     &patch,
		Additions: additions,
		Deletions: deletions,
		Status:    &status,
	}
}
