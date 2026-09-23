package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"

	jsonschema6 "github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/user/go-agent-sdk/jsonschema"
	"github.com/user/go-agent-sdk/llm/schema"
)

type Context struct {
	SessionID          string
	Agent              string
	AssistantMessageID string
	ToolCallID         string
	// Progress publishes bounded running-tool state as a durable
	// session.next.tool.progress event, mirroring the v2 tool invocation
	// progress context.
	Progress func(structured map[string]any, content []Content) error
}

type Content interface {
	toolContentTag()
}

type TextContent struct {
	Text string
}

func (TextContent) toolContentTag() {}

type FileContent struct {
	Data string
	MIME string
	Name *string
}

func (FileContent) toolContentTag() {}

type Failure = schema.ToolFailure

type RegistrationError struct {
	Name    string
	Message string
}

func (e *RegistrationError) Error() string {
	return fmt.Sprintf("tool registration error: %s: %s", e.Name, e.Message)
}

type Tool interface {
	getRuntime() *runtime
}

type Def[Input, Output, Structured any] struct {
	rt *runtime
}

func (d *Def[Input, Output, Structured]) getRuntime() *runtime {
	return d.rt
}

type AnyTool = Tool

type runtime struct {
	permission string
	definition func(name string) schema.ToolDefinition
	settle     func(execCtx context.Context, call schema.ToolCallPart, ctx Context) (schema.ToolOutput, error)
}

type Config[Input, Output, Structured any] struct {
	Description        string
	Input              *jsonschema.Schema
	Output             *jsonschema.Schema
	Structured         *jsonschema.Schema
	Execute            func(execCtx context.Context, input Input, ctx Context) (Output, error)
	ToStructuredOutput func(input Input, output any) (Structured, error)
	ToModelOutput      func(input Input, output any) []Content
}

func Make[Input, Output, Structured any](cfg Config[Input, Output, Structured]) *Def[Input, Output, Structured] {
	var inputSchema, outputSchema, structuredSchema *jsonschema6.Schema
	if cfg.Input != nil {
		inputSchema = mustCompileSchema(cfg.Input.Validation())
	}
	if cfg.Output != nil {
		outputSchema = mustCompileSchema(cfg.Output.Validation())
	}
	if cfg.Structured != nil {
		structuredSchema = mustCompileSchema(cfg.Structured.Validation())
	}

	t := &Def[Input, Output, Structured]{}
	definitions := make(map[string]schema.ToolDefinition)

	t.rt = &runtime{
		definition: func(name string) schema.ToolDefinition {
			if cached, ok := definitions[name]; ok {
				return cached
			}
			outSchema := cfg.Output
			if cfg.Structured != nil {
				outSchema = cfg.Structured
			}
			input := schema.ToolDefinitionInput{
				Name:        name,
				Description: cfg.Description,
			}
			if cfg.Input != nil {
				input.InputSchema = cfg.Input.Model()
			}
			if outSchema != nil {
				model := schema.JsonSchema(outSchema.Model())
				input.OutputSchema = &model
			}
			def := schema.NewToolDefinition(input)
			definitions[name] = def
			return def
		},
		settle: func(execCtx context.Context, call schema.ToolCallPart, ctx Context) (schema.ToolOutput, error) {
			var input Input
			if err := decodeAndValidate(call.Input, &input, inputSchema); err != nil {
				return schema.ToolOutput{}, &Failure{
					Message: fmt.Sprintf("Invalid tool input: %v", err),
				}
			}

			output, err := cfg.Execute(execCtx, input, ctx)
			if err != nil {
				return schema.ToolOutput{}, err
			}

			encoded, err := encodeOutput(output, outputSchema)
			if err != nil {
				return schema.ToolOutput{}, &Failure{
					Message: fmt.Sprintf("Tool returned an invalid value for its output schema: %v", err),
				}
			}

			var structured any
			if cfg.ToStructuredOutput != nil && cfg.Structured != nil {
				structured, err = cfg.ToStructuredOutput(input, encoded)
				if err != nil {
					return schema.ToolOutput{}, &Failure{
						Message: fmt.Sprintf("Tool returned an invalid value for its output schema: %v", err),
					}
				}
				structured, err = encodeOutput(structured, structuredSchema)
				if err != nil {
					return schema.ToolOutput{}, &Failure{
						Message: fmt.Sprintf("Tool returned an invalid value for its output schema: %v", err),
					}
				}
			} else {
				structured = encoded
			}

			var content []Content
			if cfg.ToModelOutput != nil {
				content = cfg.ToModelOutput(input, encoded)
			} else if s, ok := encoded.(string); ok {
				content = []Content{TextContent{Text: s}}
			}

			return toToolOutput(structured, content), nil
		},
	}

	return t
}

var toolNameRegex = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,63}$`)

func ValidateName(name string) error {
	if !toolNameRegex.MatchString(name) {
		return &RegistrationError{Name: name, Message: fmt.Sprintf("Invalid tool name: %s", name)}
	}
	return nil
}

func WithPermission[Input, Output, Structured any](tool *Def[Input, Output, Structured], permission string) *Def[Input, Output, Structured] {
	rt := tool.rt
	if rt == nil {
		panic("invalid tool: nil runtime")
	}
	return &Def[Input, Output, Structured]{
		rt: &runtime{
			permission: permission,
			definition: rt.definition,
			settle:     rt.settle,
		},
	}
}

func Permission(tool Tool, name string) string {
	rt := tool.getRuntime()
	if rt == nil {
		panic("invalid Core Tool value")
	}
	if rt.permission == "" {
		return name
	}
	return rt.permission
}

func Definition(name string, tool Tool) schema.ToolDefinition {
	rt := tool.getRuntime()
	if rt == nil {
		panic("invalid tool: nil runtime")
	}
	return rt.definition(name)
}

func Settle(tool Tool, execCtx context.Context, call schema.ToolCallPart, ctx Context) (schema.ToolOutput, error) {
	rt := tool.getRuntime()
	if rt == nil {
		panic("invalid Core Tool value")
	}
	return rt.settle(execCtx, call, ctx)
}

// mustCompileSchema compiles a validation schema, panicking when a non-nil
// schema cannot be compiled. Silently skipping validation would let malformed
// tool calls through, so this fails fast at Make time instead.
func mustCompileSchema(js map[string]any) *jsonschema6.Schema {
	compiled := compileSchema(js)
	if compiled == nil {
		panic("tool: schema failed to compile")
	}
	return compiled
}

func compileSchema(js map[string]any) *jsonschema6.Schema {
	if js == nil {
		return nil
	}
	data, err := json.Marshal(js)
	if err != nil {
		return nil
	}
	var document any
	if err := json.Unmarshal(data, &document); err != nil {
		return nil
	}
	c := jsonschema6.NewCompiler()
	if err := c.AddResource("schema.json", document); err != nil {
		return nil
	}
	sch, err := c.Compile("schema.json")
	if err != nil {
		return nil
	}
	return sch
}

func encodeOutput[T any](value T, schema *jsonschema6.Schema) (any, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var encoded any
	if err := json.Unmarshal(data, &encoded); err != nil {
		return nil, err
	}
	if schema != nil {
		if err := schema.Validate(encoded); err != nil {
			return nil, err
		}
	}
	return encoded, nil
}

func decodeAndValidate(input any, target any, schemas ...*jsonschema6.Schema) error {
	data, err := json.Marshal(input)
	if err != nil {
		return fmt.Errorf("cannot marshal input: %w", err)
	}
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("cannot unmarshal input: %w", err)
	}
	var generic any
	if err := json.Unmarshal(data, &generic); err != nil {
		return fmt.Errorf("cannot unmarshal input: %w", err)
	}
	for _, s := range schemas {
		if s == nil {
			continue
		}
		if err := s.Validate(generic); err != nil {
			return err
		}
	}
	return nil
}

// ToToolContent converts tool content into the event-facing representation.
func ToToolContent(content []Content) []schema.ToolContent {
	result := make([]schema.ToolContent, 0, len(content))
	for _, c := range content {
		switch v := c.(type) {
		case TextContent:
			result = append(result, schema.ToolTextContent{Type: "text", Text: v.Text})
		case FileContent:
			result = append(result, schema.ToolFileContent{
				Type: "file",
				URI:  fmt.Sprintf("data:%s;base64,%s", v.MIME, v.Data),
				Mime: v.MIME,
				Name: v.Name,
			})
		}
	}
	return result
}

func toToolOutput(structured any, content []Content) schema.ToolOutput {
	if len(content) == 0 {
		return schema.NewToolOutput(structured)
	}
	return schema.NewToolOutput(structured, ToToolContent(content)...)
}
