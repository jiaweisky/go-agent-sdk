package tool

import (
	"errors"

	"github.com/user/go-agent-sdk/core"
	"github.com/user/go-agent-sdk/llm/schema"
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

var ReadInputSchema = schema.JsonSchema{
	"type": "object",
	"properties": map[string]any{
		"path": map[string]any{"type": "string"},
		"offset": map[string]any{
			"type":             "integer",
			"exclusiveMinimum": 0,
			"description":      "The 1-based directory entry or text line offset to start reading from",
		},
		"limit": map[string]any{
			"type":             "integer",
			"exclusiveMinimum": 0,
			"maximum":          ReadMaxReadLines,
			"description":      "The maximum number of directory entries or text lines to read",
		},
	},
	"required": []any{"path"},
}

var readEntrySchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"path": map[string]any{"type": "string"},
		"type": map[string]any{"enum": []any{"file", "directory"}},
	},
	"required": []any{"path", "type"},
}

var readContentSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"uri":      map[string]any{"type": "string"},
		"name":     map[string]any{"type": "string"},
		"content":  map[string]any{"type": "string"},
		"encoding": map[string]any{"enum": []any{"utf8", "base64"}},
		"mime":     map[string]any{"type": "string"},
	},
	"required": []any{"uri", "content", "encoding", "mime"},
}

var readTextPageSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"type":      map[string]any{"const": "text-page"},
		"content":   map[string]any{"type": "string"},
		"mime":      map[string]any{"type": "string"},
		"offset":    map[string]any{"type": "integer", "exclusiveMinimum": 0},
		"truncated": map[string]any{"type": "boolean"},
		"next":      map[string]any{"type": "integer", "exclusiveMinimum": 0},
	},
	"required": []any{"type", "content", "mime", "offset", "truncated"},
}

var readListPageSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"entries":   map[string]any{"type": "array", "items": readEntrySchema},
		"truncated": map[string]any{"type": "boolean"},
		"next":      map[string]any{"type": "integer", "exclusiveMinimum": 0},
	},
	"required": []any{"entries", "truncated"},
}

var ReadOutputSchema = schema.JsonSchema{
	"anyOf": []any{readContentSchema, readTextPageSchema, readListPageSchema},
}

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
		Execute: func(input ReadInput, ctx Context) (any, error) {
			result, err := executeRead(deps, input, ctx)
			if err != nil {
				return nil, &Failure{Message: readToolErrorMessage(err, input.Path)}
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

func RegisterRead(registry *Registry, deps ReadDeps) (func(), error) {
	return registry.Register(map[string]Tool{
		ReadName: MakeRead(deps),
	})
}
