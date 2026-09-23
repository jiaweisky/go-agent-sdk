package tool

import (
	"context"
	"errors"

	"github.com/user/go-agent-sdk/core"
	"github.com/user/go-agent-sdk/jsonschema"
)

const ReadName = "read"

var readSupportedImageMIMEs = map[string]bool{
	"image/jpeg": true,
	"image/png":  true,
	"image/gif":  true,
	"image/webp": true,
}

type ReadInput struct {
	Path   string `json:"path"`
	Offset *int   `json:"offset,omitempty"`
	Limit  *int   `json:"limit,omitempty"`
}

var ReadInputSchema = jsonschema.Object(
	jsonschema.Required("path", jsonschema.String()),
	jsonschema.Optional("offset", jsonschema.Integer(
		jsonschema.ExclusiveMinimum(0),
		jsonschema.Description("The 1-based directory entry or text line offset to start reading from"),
	)),
	jsonschema.Optional("limit", jsonschema.Integer(
		jsonschema.ExclusiveMinimum(0),
		jsonschema.Maximum(ReadMaxReadLines),
		jsonschema.Description("The maximum number of directory entries or text lines to read"),
	)),
)

var readEntrySchema = jsonschema.Object(
	jsonschema.Required("path", jsonschema.String()),
	jsonschema.Required("type", jsonschema.Literal("file", "directory")),
)

var readContentSchema = jsonschema.Object(
	jsonschema.Required("uri", jsonschema.String()),
	jsonschema.Optional("name", jsonschema.String()),
	jsonschema.Required("content", jsonschema.String()),
	jsonschema.Required("encoding", jsonschema.Literal("utf8", "base64")),
	jsonschema.Required("mime", jsonschema.String()),
)

var readTextPageSchema = jsonschema.Object(
	jsonschema.Required("type", jsonschema.Literal("text-page")),
	jsonschema.Required("content", jsonschema.String()),
	jsonschema.Required("mime", jsonschema.String()),
	jsonschema.Required("offset", jsonschema.Integer(jsonschema.ExclusiveMinimum(0))),
	jsonschema.Required("truncated", jsonschema.Boolean()),
	jsonschema.Optional("next", jsonschema.Integer(jsonschema.ExclusiveMinimum(0))),
)

var readListPageSchema = jsonschema.Object(
	jsonschema.Required("entries", jsonschema.Array(readEntrySchema)),
	jsonschema.Required("truncated", jsonschema.Boolean()),
	jsonschema.Optional("next", jsonschema.Integer(jsonschema.ExclusiveMinimum(0))),
)

var ReadOutputSchema = jsonschema.Union(readContentSchema, readTextPageSchema, readListPageSchema)

type ReadDeps struct {
	Mutation   core.LocationMutation
	Reader     ReadFileSystem
	Image      core.Image
	Permission core.Permission
}

func MakeRead(deps ReadDeps) *Def[ReadInput, any, any] {
	return Make(Config[ReadInput, any, any]{
		Description: "Read a text file or supported image, page through a large UTF-8 text file by line offset, " +
			"or list a directory page. Relative paths resolve from the current location; absolute paths inside " +
			"it are accepted, while external absolute paths require external_directory approval.",
		Input:  ReadInputSchema,
		Output: ReadOutputSchema,
		ToModelOutput: func(input ReadInput, output any) []Content {
			encoded, ok := output.(map[string]any)
			if !ok {
				return nil
			}
			encoding, _ := encoded["encoding"].(string)
			mime, _ := encoded["mime"].(string)
			if encoding != "base64" || !readSupportedImageMIMEs[mime] {
				return nil
			}
			data, _ := encoded["content"].(string)
			name := input.Path
			return []Content{
				TextContent{Text: "Image read successfully"},
				FileContent{Data: data, MIME: mime, Name: &name},
			}
		},
		Execute: func(execCtx context.Context, input ReadInput, ctx Context) (any, error) {
			if err := execCtx.Err(); err != nil {
				return nil, err
			}
			result, err := executeRead(deps, input, ctx)
			if err != nil {
				return nil, failure(readToolErrorMessage(err, input.Path), err)
			}
			return result, nil
		},
	})
}

func executeRead(deps ReadDeps, input ReadInput, ctx Context) (any, error) {
	source := &core.PermissionSource{
		Type:      core.PermissionSourceTypeTool,
		MessageID: ctx.AssistantMessageID,
		CallID:    ctx.ToolCallID,
	}

	kind := core.LocationMutationKindDirectory
	target, err := deps.Mutation.Resolve(core.LocationMutationResolveInput{Path: input.Path, Kind: &kind})
	if err != nil {
		return nil, err
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
			return nil, err
		}
	}

	resource := target.Resource
	absolute := core.AbsolutePath(target.Canonical)

	typ, err := deps.Reader.Inspect(absolute)
	if err != nil {
		return nil, err
	}

	if err := deps.Permission.Assert(core.PermissionAssertInput{
		SessionID: ctx.SessionID,
		Action:    ReadName,
		Resources: []string{resource},
		Save:      []string{"*"},
		Source:    source,
		Agent:     &ctx.Agent,
	}); err != nil {
		return nil, err
	}

	page := ReadPageInput{Offset: input.Offset, Limit: input.Limit}
	if typ == "directory" {
		return deps.Reader.List(absolute, page)
	}

	content, err := deps.Reader.Read(absolute, resource, page)
	if err != nil {
		return nil, err
	}

	fileContent, ok := content.(core.FileSystemContent)
	if !ok {
		return content, nil
	}

	if fileContent.Encoding == "base64" && readSupportedImageMIMEs[fileContent.MIME] {
		normalized, nerr := deps.Image.Normalize(resource, fileContent)
		if nerr != nil {
			var unavailable *core.ImageResizerUnavailableError
			if errors.As(nerr, &unavailable) {
				return fileContent, nil
			}
			return nil, nerr
		}
		return normalized, nil
	}
	if fileContent.Encoding == "base64" {
		return nil, &ReadBinaryFileError{Resource: resource}
	}
	return fileContent, nil
}

func readToolErrorMessage(err error, path string) string {
	switch err.(type) {
	case *ReadBinaryFileError,
		*ReadMediaIngestLimitError,
		*core.ImageDecodeError,
		*core.ImageSizeError:
		return err.Error()
	default:
		return "Unable to read " + path
	}
}

func RegisterRead(tools Tools, deps ReadDeps) (func(), error) {
	return tools.Register(map[string]Tool{
		ReadName: MakeRead(deps),
	})
}
