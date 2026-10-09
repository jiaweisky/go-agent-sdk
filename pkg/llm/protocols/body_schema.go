package protocols

import "github.com/jiaweisky/go-agent-sdk/pkg/jsonschema"

// Body schemas describe the JSON each protocol puts on the wire. Route
// construction compiles them and compile() validates every built body, so a
// lowering bug fails as an invalid request instead of reaching the provider.

var openaiChatToolSchema = jsonschema.Object(
	jsonschema.Required("type", jsonschema.Literal("function")),
	jsonschema.Required("function", jsonschema.Object(
		jsonschema.Required("name", jsonschema.String()),
		jsonschema.Required("description", jsonschema.String()),
		jsonschema.Required("parameters", jsonschema.Raw(map[string]any{"type": "object"})),
	)),
)

var openaiChatAssistantToolCallSchema = jsonschema.Object(
	jsonschema.Required("id", jsonschema.String()),
	jsonschema.Required("type", jsonschema.Literal("function")),
	jsonschema.Required("function", jsonschema.Object(
		jsonschema.Required("name", jsonschema.String()),
		jsonschema.Required("arguments", jsonschema.String()),
	)),
)

var openaiChatUserContentSchema = jsonschema.Union(
	jsonschema.Object(
		jsonschema.Required("type", jsonschema.Literal("text")),
		jsonschema.Required("text", jsonschema.String()),
	),
	jsonschema.Object(
		jsonschema.Required("type", jsonschema.Literal("image_url")),
		jsonschema.Required("image_url", jsonschema.Object(
			jsonschema.Required("url", jsonschema.String()),
			jsonschema.Optional("detail", jsonschema.Enum([]any{"low", "high", "original", "auto"})),
		)),
	),
	jsonschema.Object(
		jsonschema.Required("type", jsonschema.Literal("input_audio")),
		jsonschema.Required("input_audio", jsonschema.Object(
			jsonschema.Required("data", jsonschema.String()),
			jsonschema.Required("format", jsonschema.String()),
		)),
	),
	jsonschema.Object(
		jsonschema.Required("type", jsonschema.Literal("audio_url")),
		jsonschema.Required("audio_url", jsonschema.Object(
			jsonschema.Required("url", jsonschema.String()),
		)),
	),
	jsonschema.Object(
		jsonschema.Required("type", jsonschema.Literal("video_url")),
		jsonschema.Required("video_url", jsonschema.Object(
			jsonschema.Required("url", jsonschema.String()),
		)),
	),
)

var openaiChatMessageSchema = jsonschema.Union(
	jsonschema.Object(
		jsonschema.Required("role", jsonschema.Literal("system")),
		jsonschema.Required("content", jsonschema.String()),
	),
	jsonschema.Object(
		jsonschema.Required("role", jsonschema.Literal("user")),
		jsonschema.Required("content", jsonschema.Union(
			jsonschema.String(),
			jsonschema.Array(openaiChatUserContentSchema),
		)),
	),
	jsonschema.Object(
		jsonschema.Required("role", jsonschema.Literal("assistant")),
		jsonschema.Required("content", jsonschema.Union(
			jsonschema.String(),
			jsonschema.Raw(map[string]any{"type": "null"}),
		)),
		jsonschema.Optional("tool_calls", jsonschema.Array(openaiChatAssistantToolCallSchema)),
		jsonschema.Optional("reasoning_content", jsonschema.String()),
	),
	jsonschema.Object(
		jsonschema.Required("role", jsonschema.Literal("tool")),
		jsonschema.Required("tool_call_id", jsonschema.String()),
		jsonschema.Required("content", jsonschema.String()),
	),
)

var openaiChatToolChoiceSchema = jsonschema.Union(
	jsonschema.Enum([]any{"auto", "none", "required"}),
	jsonschema.Object(
		jsonschema.Required("type", jsonschema.Literal("function")),
		jsonschema.Required("function", jsonschema.Object(
			jsonschema.Required("name", jsonschema.String()),
		)),
	),
)

var openaiChatBodySchema = jsonschema.Object(
	jsonschema.Required("model", jsonschema.String()),
	jsonschema.Required("messages", jsonschema.Array(openaiChatMessageSchema)),
	jsonschema.Optional("modalities", jsonschema.Array(jsonschema.String())),
	jsonschema.Optional("tools", jsonschema.Array(openaiChatToolSchema)),
	jsonschema.Optional("tool_choice", openaiChatToolChoiceSchema),
	jsonschema.Required("stream", jsonschema.Literal(true)),
	jsonschema.Optional("stream_options", jsonschema.Object(
		jsonschema.Required("include_usage", jsonschema.Boolean()),
	)),
	jsonschema.Optional("store", jsonschema.Boolean()),
	jsonschema.Optional("reasoning_effort", jsonschema.Enum([]any{
		"none", "minimal", "low", "medium", "high", "xhigh",
	})),
	jsonschema.Optional("max_tokens", jsonschema.Number()),
	jsonschema.Optional("temperature", jsonschema.Number()),
	jsonschema.Optional("top_p", jsonschema.Number()),
	jsonschema.Optional("frequency_penalty", jsonschema.Number()),
	jsonschema.Optional("presence_penalty", jsonschema.Number()),
	jsonschema.Optional("seed", jsonschema.Number()),
	jsonschema.Optional("stop", jsonschema.Array(jsonschema.String())),
)

var openaiResponsesInputTextSchema = jsonschema.Object(
	jsonschema.Required("type", jsonschema.Literal("input_text")),
	jsonschema.Required("text", jsonschema.String()),
)

var openaiResponsesInputImageSchema = jsonschema.Union(
	jsonschema.Object(
		jsonschema.Required("type", jsonschema.Literal("input_image")),
		jsonschema.Required("image_url", jsonschema.String()),
		jsonschema.Optional("detail", jsonschema.Enum([]any{"low", "high", "original", "auto"})),
	),
	jsonschema.Object(
		jsonschema.Required("type", jsonschema.Literal("input_image")),
		jsonschema.Required("file_id", jsonschema.String()),
	),
)

var openaiResponsesInputContentSchema = jsonschema.Union(
	openaiResponsesInputTextSchema,
	openaiResponsesInputImageSchema,
)

var openaiResponsesOutputTextSchema = jsonschema.Object(
	jsonschema.Required("type", jsonschema.Literal("output_text")),
	jsonschema.Required("text", jsonschema.String()),
)

var openaiResponsesSummaryTextSchema = jsonschema.Object(
	jsonschema.Required("type", jsonschema.Literal("summary_text")),
	jsonschema.Required("text", jsonschema.String()),
)

var openaiResponsesInputItemSchema = jsonschema.Union(
	jsonschema.Object(
		jsonschema.Required("role", jsonschema.Literal("system")),
		jsonschema.Required("content", jsonschema.String()),
	),
	jsonschema.Object(
		jsonschema.Required("role", jsonschema.Literal("user")),
		jsonschema.Required("content", jsonschema.Array(openaiResponsesInputContentSchema)),
	),
	jsonschema.Object(
		jsonschema.Required("role", jsonschema.Literal("assistant")),
		jsonschema.Required("content", jsonschema.Array(openaiResponsesOutputTextSchema)),
	),
	jsonschema.Object(
		jsonschema.Required("type", jsonschema.Literal("reasoning")),
		jsonschema.Optional("id", jsonschema.String()),
		jsonschema.Required("summary", jsonschema.Array(openaiResponsesSummaryTextSchema)),
		jsonschema.Optional("encrypted_content", jsonschema.Union(
			jsonschema.String(),
			jsonschema.Raw(map[string]any{"type": "null"}),
		)),
	),
	jsonschema.Object(
		jsonschema.Required("type", jsonschema.Literal("item_reference")),
		jsonschema.Required("id", jsonschema.String()),
	),
	jsonschema.Object(
		jsonschema.Required("type", jsonschema.Literal("function_call")),
		jsonschema.Required("call_id", jsonschema.String()),
		jsonschema.Required("name", jsonschema.String()),
		jsonschema.Required("arguments", jsonschema.String()),
	),
	jsonschema.Object(
		jsonschema.Required("type", jsonschema.Literal("function_call_output")),
		jsonschema.Required("call_id", jsonschema.String()),
		jsonschema.Required("output", jsonschema.Union(
			jsonschema.String(),
			jsonschema.Array(openaiResponsesInputContentSchema),
		)),
	),
)

var openaiResponsesToolSchema = jsonschema.Object(
	jsonschema.Required("type", jsonschema.Literal("function")),
	jsonschema.Required("name", jsonschema.String()),
	jsonschema.Required("description", jsonschema.String()),
	jsonschema.Required("parameters", jsonschema.Raw(map[string]any{"type": "object"})),
	jsonschema.Optional("strict", jsonschema.Boolean()),
)

var openaiResponsesToolChoiceSchema = jsonschema.Union(
	jsonschema.Enum([]any{"auto", "none", "required"}),
	jsonschema.Object(
		jsonschema.Required("type", jsonschema.Literal("function")),
		jsonschema.Required("name", jsonschema.String()),
	),
)

var openaiResponsesBodySchema = jsonschema.Object(
	jsonschema.Required("model", jsonschema.String()),
	jsonschema.Required("input", jsonschema.Array(openaiResponsesInputItemSchema)),
	jsonschema.Optional("instructions", jsonschema.String()),
	jsonschema.Optional("tools", jsonschema.Array(openaiResponsesToolSchema)),
	jsonschema.Optional("tool_choice", openaiResponsesToolChoiceSchema),
	jsonschema.Optional("store", jsonschema.Boolean()),
	jsonschema.Optional("service_tier", jsonschema.Enum([]any{"auto", "default", "flex", "priority"})),
	jsonschema.Optional("prompt_cache_key", jsonschema.String()),
	jsonschema.Optional("include", jsonschema.Array(jsonschema.Enum([]any{
		"file_search_call.results",
		"web_search_call.results",
		"web_search_call.action.sources",
		"message.input_image.image_url",
		"computer_call_output.output.image_url",
		"code_interpreter_call.outputs",
		"reasoning.encrypted_content",
		"message.output_text.logprobs",
	}))),
	jsonschema.Optional("reasoning", jsonschema.Object(
		jsonschema.Optional("effort", jsonschema.Enum([]any{"none", "minimal", "low", "medium", "high", "xhigh"})),
		jsonschema.Optional("summary", jsonschema.Literal("auto")),
	)),
	jsonschema.Optional("text", jsonschema.Object(
		jsonschema.Optional("verbosity", jsonschema.Enum([]any{"low", "medium", "high"})),
	)),
	jsonschema.Optional("max_output_tokens", jsonschema.Number()),
	jsonschema.Optional("temperature", jsonschema.Number()),
	jsonschema.Optional("top_p", jsonschema.Number()),
	jsonschema.Required("stream", jsonschema.Literal(true)),
)

var anthropicCacheControlSchema = jsonschema.Object(
	jsonschema.Required("type", jsonschema.Literal("ephemeral")),
	jsonschema.Optional("ttl", jsonschema.Enum([]any{"5m", "1h"})),
)

var anthropicTextBlockSchema = jsonschema.Object(
	jsonschema.Required("type", jsonschema.Literal("text")),
	jsonschema.Required("text", jsonschema.String()),
	jsonschema.Optional("cache_control", anthropicCacheControlSchema),
)

var anthropicImageSourceSchema = jsonschema.Union(
	jsonschema.Object(
		jsonschema.Required("type", jsonschema.Literal("base64")),
		jsonschema.Required("media_type", jsonschema.String()),
		jsonschema.Required("data", jsonschema.String()),
	),
	jsonschema.Object(
		jsonschema.Required("type", jsonschema.Literal("url")),
		jsonschema.Required("url", jsonschema.String()),
	),
)

var anthropicImageBlockSchema = jsonschema.Object(
	jsonschema.Required("type", jsonschema.Literal("image")),
	jsonschema.Required("source", anthropicImageSourceSchema),
	jsonschema.Optional("cache_control", anthropicCacheControlSchema),
)

var anthropicThinkingBlockSchema = jsonschema.Object(
	jsonschema.Required("type", jsonschema.Literal("thinking")),
	jsonschema.Required("thinking", jsonschema.String()),
	jsonschema.Optional("signature", jsonschema.String()),
	jsonschema.Optional("cache_control", anthropicCacheControlSchema),
)

var anthropicToolUseBlockSchema = jsonschema.Object(
	jsonschema.Required("type", jsonschema.Literal("tool_use")),
	jsonschema.Required("id", jsonschema.String()),
	jsonschema.Required("name", jsonschema.String()),
	jsonschema.Required("input", jsonschema.Raw(map[string]any{})),
	jsonschema.Optional("cache_control", anthropicCacheControlSchema),
)

var anthropicServerToolUseBlockSchema = jsonschema.Object(
	jsonschema.Required("type", jsonschema.Literal("server_tool_use")),
	jsonschema.Required("id", jsonschema.String()),
	jsonschema.Required("name", jsonschema.String()),
	jsonschema.Required("input", jsonschema.Raw(map[string]any{})),
	jsonschema.Optional("cache_control", anthropicCacheControlSchema),
)

var anthropicServerToolResultBlockSchema = jsonschema.Object(
	jsonschema.Required("type", jsonschema.Enum([]any{
		"web_search_tool_result",
		"code_execution_tool_result",
		"web_fetch_tool_result",
	})),
	jsonschema.Required("tool_use_id", jsonschema.String()),
	jsonschema.Required("content", jsonschema.Raw(map[string]any{})),
	jsonschema.Optional("cache_control", anthropicCacheControlSchema),
)

var anthropicToolResultBlockSchema = jsonschema.Object(
	jsonschema.Required("type", jsonschema.Literal("tool_result")),
	jsonschema.Required("tool_use_id", jsonschema.String()),
	jsonschema.Required("content", jsonschema.Union(
		jsonschema.String(),
		jsonschema.Array(jsonschema.Union(anthropicTextBlockSchema, anthropicImageBlockSchema)),
	)),
	jsonschema.Optional("is_error", jsonschema.Boolean()),
	jsonschema.Optional("cache_control", anthropicCacheControlSchema),
)

var anthropicMessageSchema = jsonschema.Union(
	jsonschema.Object(
		jsonschema.Required("role", jsonschema.Literal("user")),
		jsonschema.Required("content", jsonschema.Array(jsonschema.Union(
			anthropicTextBlockSchema,
			anthropicImageBlockSchema,
			anthropicToolResultBlockSchema,
		))),
	),
	jsonschema.Object(
		jsonschema.Required("role", jsonschema.Literal("assistant")),
		jsonschema.Required("content", jsonschema.Array(jsonschema.Union(
			anthropicTextBlockSchema,
			anthropicThinkingBlockSchema,
			anthropicToolUseBlockSchema,
			anthropicServerToolUseBlockSchema,
			anthropicServerToolResultBlockSchema,
		))),
	),
	jsonschema.Object(
		jsonschema.Required("role", jsonschema.Literal("system")),
		jsonschema.Required("content", jsonschema.Array(anthropicTextBlockSchema)),
	),
)

var anthropicToolSchema = jsonschema.Object(
	jsonschema.Required("name", jsonschema.String()),
	jsonschema.Required("description", jsonschema.String()),
	jsonschema.Required("input_schema", jsonschema.Raw(map[string]any{"type": "object"})),
	jsonschema.Optional("cache_control", anthropicCacheControlSchema),
)

var anthropicToolChoiceSchema = jsonschema.Union(
	jsonschema.Object(jsonschema.Required("type", jsonschema.Enum([]any{"auto", "any"}))),
	jsonschema.Object(
		jsonschema.Required("type", jsonschema.Literal("tool")),
		jsonschema.Required("name", jsonschema.String()),
	),
)

var anthropicThinkingSchema = jsonschema.Object(
	jsonschema.Required("type", jsonschema.Literal("enabled")),
	jsonschema.Required("budget_tokens", jsonschema.Number()),
)

var anthropicMessagesBodySchema = jsonschema.Object(
	jsonschema.Required("model", jsonschema.String()),
	jsonschema.Optional("system", jsonschema.Array(anthropicTextBlockSchema)),
	jsonschema.Required("messages", jsonschema.Array(anthropicMessageSchema)),
	jsonschema.Optional("tools", jsonschema.Array(anthropicToolSchema)),
	jsonschema.Optional("tool_choice", anthropicToolChoiceSchema),
	jsonschema.Required("stream", jsonschema.Literal(true)),
	jsonschema.Required("max_tokens", jsonschema.Number()),
	jsonschema.Optional("temperature", jsonschema.Number()),
	jsonschema.Optional("top_p", jsonschema.Number()),
	jsonschema.Optional("top_k", jsonschema.Number()),
	jsonschema.Optional("stop_sequences", jsonschema.Array(jsonschema.String())),
	jsonschema.Optional("thinking", anthropicThinkingSchema),
)

var geminiTextPartSchema = jsonschema.Object(
	jsonschema.Required("text", jsonschema.String()),
	jsonschema.Optional("thought", jsonschema.Boolean()),
	jsonschema.Optional("thoughtSignature", jsonschema.String()),
)

var geminiInlineDataPartSchema = jsonschema.Object(
	jsonschema.Required("inlineData", jsonschema.Object(
		jsonschema.Required("mimeType", jsonschema.String()),
		jsonschema.Required("data", jsonschema.String()),
	)),
)

var geminiFileDataPartSchema = jsonschema.Object(
	jsonschema.Required("fileData", jsonschema.Object(
		jsonschema.Required("mimeType", jsonschema.String()),
		jsonschema.Required("fileUri", jsonschema.String()),
	)),
)

var geminiFunctionCallPartSchema = jsonschema.Object(
	jsonschema.Required("functionCall", jsonschema.Object(
		jsonschema.Required("name", jsonschema.String()),
		jsonschema.Required("args", jsonschema.Raw(map[string]any{})),
	)),
	jsonschema.Optional("thoughtSignature", jsonschema.String()),
)

var geminiFunctionResponsePartSchema = jsonschema.Object(
	jsonschema.Required("functionResponse", jsonschema.Object(
		jsonschema.Required("name", jsonschema.String()),
		jsonschema.Required("response", jsonschema.Raw(map[string]any{})),
	)),
)

var geminiContentSchema = jsonschema.Object(
	jsonschema.Required("role", jsonschema.Enum([]any{"user", "model"})),
	jsonschema.Required("parts", jsonschema.Array(jsonschema.Union(
		geminiTextPartSchema,
		geminiInlineDataPartSchema,
		geminiFileDataPartSchema,
		geminiFunctionCallPartSchema,
		geminiFunctionResponsePartSchema,
	))),
)

var geminiSystemInstructionSchema = jsonschema.Object(
	jsonschema.Required("parts", jsonschema.Array(jsonschema.Object(
		jsonschema.Required("text", jsonschema.String()),
	))),
)

var geminiFunctionDeclarationSchema = jsonschema.Object(
	jsonschema.Required("name", jsonschema.String()),
	jsonschema.Required("description", jsonschema.String()),
	jsonschema.Optional("parameters", jsonschema.Raw(map[string]any{"type": "object"})),
)

var geminiToolSchema = jsonschema.Object(
	jsonschema.Required("functionDeclarations", jsonschema.Array(geminiFunctionDeclarationSchema)),
)

var geminiToolConfigSchema = jsonschema.Object(
	jsonschema.Required("functionCallingConfig", jsonschema.Object(
		jsonschema.Required("mode", jsonschema.Enum([]any{"AUTO", "NONE", "ANY"})),
		jsonschema.Optional("allowedFunctionNames", jsonschema.Array(jsonschema.String())),
	)),
)

var geminiThinkingConfigSchema = jsonschema.Object(
	jsonschema.Optional("thinkingBudget", jsonschema.Number()),
	jsonschema.Optional("includeThoughts", jsonschema.Boolean()),
)

var geminiGenerationConfigSchema = jsonschema.Object(
	jsonschema.Optional("maxOutputTokens", jsonschema.Number()),
	jsonschema.Optional("temperature", jsonschema.Number()),
	jsonschema.Optional("topP", jsonschema.Number()),
	jsonschema.Optional("topK", jsonschema.Number()),
	jsonschema.Optional("stopSequences", jsonschema.Array(jsonschema.String())),
	jsonschema.Optional("thinkingConfig", geminiThinkingConfigSchema),
)

var geminiBodySchema = jsonschema.Object(
	jsonschema.Required("contents", jsonschema.Array(geminiContentSchema)),
	jsonschema.Optional("systemInstruction", geminiSystemInstructionSchema),
	jsonschema.Optional("tools", jsonschema.Array(geminiToolSchema)),
	jsonschema.Optional("toolConfig", geminiToolConfigSchema),
	jsonschema.Optional("generationConfig", geminiGenerationConfigSchema),
)

var bedrockTextBlockSchema = jsonschema.Object(
	jsonschema.Required("text", jsonschema.String()),
)

var bedrockCachePointBlockSchema = jsonschema.Object(
	jsonschema.Required("cachePoint", jsonschema.Object(
		jsonschema.Required("type", jsonschema.Literal("default")),
		jsonschema.Optional("ttl", jsonschema.Enum([]any{"5m", "1h"})),
	)),
)

// bedrockSourceSchema is bytes or an S3 location.
var bedrockSourceSchema = jsonschema.Union(
	jsonschema.Object(jsonschema.Required("bytes", jsonschema.String())),
	jsonschema.Object(jsonschema.Required("s3Location", jsonschema.Object(
		jsonschema.Required("uri", jsonschema.String()),
	))),
)

var bedrockImageBlockSchema = jsonschema.Object(
	jsonschema.Required("image", jsonschema.Object(
		jsonschema.Required("format", jsonschema.Enum([]any{"png", "jpeg", "gif", "webp"})),
		jsonschema.Required("source", bedrockSourceSchema),
	)),
)

var bedrockDocumentBlockSchema = jsonschema.Object(
	jsonschema.Required("document", jsonschema.Object(
		jsonschema.Required("format", jsonschema.Enum([]any{
			"pdf", "csv", "doc", "docx", "xls", "xlsx", "html", "txt", "md",
		})),
		jsonschema.Required("name", jsonschema.String()),
		jsonschema.Required("source", bedrockSourceSchema),
	)),
)

var bedrockToolUseBlockSchema = jsonschema.Object(
	jsonschema.Required("toolUse", jsonschema.Object(
		jsonschema.Required("toolUseId", jsonschema.String()),
		jsonschema.Required("name", jsonschema.String()),
		jsonschema.Required("input", jsonschema.Raw(map[string]any{})),
	)),
)

var bedrockToolResultBlockSchema = jsonschema.Object(
	jsonschema.Required("toolResult", jsonschema.Object(
		jsonschema.Required("toolUseId", jsonschema.String()),
		jsonschema.Required("content", jsonschema.Array(jsonschema.Union(
			jsonschema.Object(jsonschema.Required("text", jsonschema.String())),
			jsonschema.Object(jsonschema.Required("json", jsonschema.Raw(map[string]any{}))),
			bedrockImageBlockSchema,
		))),
		jsonschema.Optional("status", jsonschema.Enum([]any{"success", "error"})),
	)),
)

var bedrockReasoningBlockSchema = jsonschema.Object(
	jsonschema.Required("reasoningContent", jsonschema.Object(
		jsonschema.Optional("reasoningText", jsonschema.Object(
			jsonschema.Required("text", jsonschema.String()),
			jsonschema.Optional("signature", jsonschema.String()),
		)),
	)),
)

var bedrockMessageSchema = jsonschema.Union(
	jsonschema.Object(
		jsonschema.Required("role", jsonschema.Literal("user")),
		jsonschema.Required("content", jsonschema.Array(jsonschema.Union(
			bedrockTextBlockSchema,
			bedrockImageBlockSchema,
			bedrockDocumentBlockSchema,
			bedrockToolResultBlockSchema,
			bedrockCachePointBlockSchema,
		))),
	),
	jsonschema.Object(
		jsonschema.Required("role", jsonschema.Literal("assistant")),
		jsonschema.Required("content", jsonschema.Array(jsonschema.Union(
			bedrockTextBlockSchema,
			bedrockReasoningBlockSchema,
			bedrockToolUseBlockSchema,
			bedrockCachePointBlockSchema,
		))),
	),
)

var bedrockToolSpecSchema = jsonschema.Object(
	jsonschema.Required("toolSpec", jsonschema.Object(
		jsonschema.Required("name", jsonschema.String()),
		jsonschema.Required("description", jsonschema.String()),
		jsonschema.Required("inputSchema", jsonschema.Object(
			jsonschema.Required("json", jsonschema.Raw(map[string]any{"type": "object"})),
		)),
	)),
)

var bedrockToolChoiceSchema = jsonschema.Union(
	jsonschema.Object(jsonschema.Required("auto", jsonschema.Raw(map[string]any{}))),
	jsonschema.Object(jsonschema.Required("any", jsonschema.Raw(map[string]any{}))),
	jsonschema.Object(jsonschema.Required("tool", jsonschema.Object(
		jsonschema.Required("name", jsonschema.String()),
	))),
)

var bedrockConverseBodySchema = jsonschema.Object(
	jsonschema.Required("modelId", jsonschema.String()),
	jsonschema.Required("messages", jsonschema.Array(bedrockMessageSchema)),
	jsonschema.Optional("system", jsonschema.Array(jsonschema.Union(
		bedrockTextBlockSchema,
		bedrockCachePointBlockSchema,
	))),
	jsonschema.Optional("inferenceConfig", jsonschema.Object(
		jsonschema.Optional("maxTokens", jsonschema.Number()),
		jsonschema.Optional("temperature", jsonschema.Number()),
		jsonschema.Optional("topP", jsonschema.Number()),
		jsonschema.Optional("stopSequences", jsonschema.Array(jsonschema.String())),
	)),
	jsonschema.Optional("toolConfig", jsonschema.Object(
		jsonschema.Required("tools", jsonschema.Array(jsonschema.Union(
			bedrockToolSpecSchema,
			bedrockCachePointBlockSchema,
		))),
		jsonschema.Optional("toolChoice", bedrockToolChoiceSchema),
	)),
	jsonschema.Optional("additionalModelRequestFields", jsonschema.Raw(map[string]any{"type": "object"})),
)
