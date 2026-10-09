package tool

import (
	"context"
	"fmt"

	"github.com/jiaweisky/go-agent-sdk/pkg/core"
	"github.com/jiaweisky/go-agent-sdk/pkg/jsonschema"
)

const WriteName = "write"

// TODO: Revisit whether model-facing mutation schemas should prefer absolute
// `filePath` naming for trained-in compatibility after evaluating model behavior.
var WriteInputSchema = jsonschema.Object(
	jsonschema.Required("path", jsonschema.String(jsonschema.Description(
		"File path to write. Relative paths resolve within the active Location. "+
			"Absolute paths inside that Location are accepted; external absolute paths require "+
			"external_directory approval."))),
	jsonschema.Required("content", jsonschema.String(jsonschema.Description("Content to write to the file"))),
)

var WriteOutputSchema = jsonschema.Object(
	jsonschema.Required("operation", jsonschema.Literal("write")),
	jsonschema.Required("target", jsonschema.String()),
	jsonschema.Required("resource", jsonschema.String()),
	jsonschema.Required("existed", jsonschema.Boolean()),
)

type WriteInput struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type WriteOutput struct {
	Operation string `json:"operation"`
	Target    string `json:"target"`
	Resource  string `json:"resource"`
	Existed   bool   `json:"existed"`
}

func WriteToModelOutput(output WriteOutput) string {
	verb := "Created"
	if output.Existed {
		verb = "Wrote"
	}
	return fmt.Sprintf("%s file successfully: %s", verb, output.Resource)
}

type WriteDeps struct {
	Mutation   core.LocationMutation
	Files      core.FileMutation
	Permission core.Permission
}

// Deferred V2 write UX integrations remain visible at the model-facing seam.
// TODO: Add formatter integration after V2 formatter runtime exists.
// TODO: Publish watcher/file-edit events after V2 watcher integration exists.
// TODO: Add snapshots / undo after design exists.
// TODO: Add LSP notification and diagnostics after V2 LSP runtime exists.

func MakeWrite(deps WriteDeps) *Def[WriteInput, WriteOutput, WriteOutput] {
	return Make(Config[WriteInput, WriteOutput, WriteOutput]{
		Description: "Write content to one file. Relative paths resolve within the active Location. " +
			"Absolute paths inside the Location are accepted. Explicit external absolute paths " +
			"require external_directory approval before edit approval.",
		Input:  WriteInputSchema,
		Output: WriteOutputSchema,
		ToModelOutput: func(input WriteInput, output any) []Content {
			return []Content{TextContent{Text: WriteToModelOutput(DecodeStructured[WriteOutput](output))}}
		},
		Execute: func(execCtx context.Context, input WriteInput, ctx Context) (WriteOutput, error) {
			if err := execCtx.Err(); err != nil {
				return WriteOutput{}, err
			}
			result, err := executeWrite(deps, input, ctx)
			if err != nil {
				return WriteOutput{}, failure(fmt.Sprintf("Unable to write %s", input.Path), err)
			}
			return result, nil
		},
	})
}

func executeWrite(deps WriteDeps, input WriteInput, ctx Context) (WriteOutput, error) {
	kind := core.LocationMutationKindFile
	source := &core.PermissionSource{
		Type:      core.PermissionSourceTypeTool,
		MessageID: ctx.AssistantMessageID,
		CallID:    ctx.ToolCallID,
	}

	target, err := deps.Mutation.Resolve(core.LocationMutationResolveInput{Path: input.Path, Kind: &kind})
	if err != nil {
		return WriteOutput{}, err
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
			return WriteOutput{}, err
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
		return WriteOutput{}, err
	}

	written, err := deps.Files.WriteTextPreservingBom(core.FileMutationTextWriteInput{
		Target:  core.FileMutationTarget{Canonical: target.Canonical, Resource: target.Resource},
		Content: input.Content,
	})
	if err != nil {
		return WriteOutput{}, err
	}

	return WriteOutput{
		Operation: "write",
		Target:    written.Target,
		Resource:  written.Resource,
		Existed:   written.Existed,
	}, nil
}

func WithWritePermission(tool *Def[WriteInput, WriteOutput, WriteOutput]) *Def[WriteInput, WriteOutput, WriteOutput] {
	return WithPermission(tool, "edit")
}

func RegisterWrite(tools Tools, deps WriteDeps) (func(), error) {
	return tools.Register(map[string]Tool{
		WriteName: WithWritePermission(MakeWrite(deps)),
	})
}
