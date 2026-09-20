package tool

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/user/go-agent-sdk/core"
	"github.com/user/go-agent-sdk/llm/schema"
)

const QuestionName = "question"

const QuestionDescription = `Use this tool when you need to ask the user questions during execution. This allows you to:
1. Gather user preferences or requirements
2. Clarify ambiguous instructions
3. Get decisions on implementation choices as you work
4. Offer choices to the user about what direction to take.

Usage notes:
- When ` + "`custom`" + ` is enabled (default), a "Type your own answer" option is added automatically; don't include "Other" or catch-all options
- Answers are returned as arrays of labels; set ` + "`multiple: true`" + ` to allow selecting more than one
- If you recommend a specific option, make that the first option in the list and add "(Recommended)" at the end of the label`

var questionOptionSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"label":       map[string]any{"type": "string", "description": "Display text (1-5 words, concise)"},
		"description": map[string]any{"type": "string", "description": "Explanation of choice"},
	},
	"required": []any{"label", "description"},
}

var questionPromptSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"question": map[string]any{"type": "string", "description": "Complete question"},
		"header":   map[string]any{"type": "string", "description": "Very short label (max 30 chars)"},
		"options": map[string]any{
			"type":        "array",
			"description": "Available choices",
			"items":       questionOptionSchema,
		},
		"multiple": map[string]any{"type": "boolean", "description": "Allow selecting multiple choices"},
	},
	"required": []any{"question", "header", "options"},
}

var QuestionInputSchema = schema.JsonSchema{
	"type": "object",
	"properties": map[string]any{
		"questions": map[string]any{
			"type":        "array",
			"description": "Questions to ask",
			"items":       questionPromptSchema,
		},
	},
	"required": []any{"questions"},
}

var QuestionOutputSchema = schema.JsonSchema{
	"type": "object",
	"properties": map[string]any{
		"answers": map[string]any{
			"type":  "array",
			"items": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		},
	},
	"required": []any{"answers"},
}

type QuestionInput struct {
	Questions []core.QuestionPrompt `json:"questions"`
}

type QuestionOutput struct {
	Answers []core.QuestionAnswer `json:"answers"`
}

type QuestionDeps struct {
	Permission core.Permission
	Question   core.Question
}

func questionInfosFromPrompts(prompts []core.QuestionPrompt) []core.QuestionInfo {
	if prompts == nil {
		return nil
	}
	infos := make([]core.QuestionInfo, len(prompts))
	for i, prompt := range prompts {
		infos[i] = core.QuestionInfo{
			Question: prompt.Question,
			Header:   prompt.Header,
			Options:  prompt.Options,
			Multiple: prompt.Multiple,
		}
	}
	return infos
}

func QuestionToModelOutput(questions []core.QuestionPrompt, answers []core.QuestionAnswer) string {
	parts := make([]string, 0, len(questions))
	for index, question := range questions {
		value := "Unanswered"
		if index < len(answers) && len(answers[index]) > 0 {
			value = strings.Join(answers[index], ", ")
		}
		parts = append(parts, `"`+question.Question+`"="`+value+`"`)
	}
	return "User has answered your questions: " + strings.Join(parts, ", ") +
		". You can now continue with the user's answers in mind."
}

func MakeQuestion(deps QuestionDeps) *Def[QuestionInput, QuestionOutput, QuestionOutput] {
	return Make(Config[QuestionInput, QuestionOutput, QuestionOutput]{
		Description: QuestionDescription,
		Input:       QuestionInputSchema,
		Output:      QuestionOutputSchema,
		ToModelOutput: func(input QuestionInput, output any) []Content {
			var out QuestionOutput
			if b, err := json.Marshal(output); err == nil {
				json.Unmarshal(b, &out)
			}
			return []Content{TextContent{Text: QuestionToModelOutput(input.Questions, out.Answers)}}
		},
		Execute: func(execCtx context.Context, input QuestionInput, ctx Context) (QuestionOutput, error) {
			source := &core.PermissionSource{
				Type:      core.PermissionSourceTypeTool,
				MessageID: ctx.AssistantMessageID,
				CallID:    ctx.ToolCallID,
			}
			if err := deps.Permission.Assert(core.PermissionAssertInput{
				SessionID: ctx.SessionID,
				Action:    "question",
				Resources: []string{"*"},
				Source:    source,
				Agent:     &ctx.Agent,
			}); err != nil {
				return QuestionOutput{}, &Failure{Message: "Permission denied: question"}
			}

			answers, err := deps.Question.Ask(execCtx, core.QuestionAskInput{
				SessionID: ctx.SessionID,
				Questions: questionInfosFromPrompts(input.Questions),
				Tool:      &core.QuestionTool{MessageID: ctx.AssistantMessageID, CallID: ctx.ToolCallID},
			})
			if err != nil {
				return QuestionOutput{}, err
			}
			if answers == nil {
				answers = []core.QuestionAnswer{}
			}
			return QuestionOutput{Answers: answers}, nil
		},
	})
}

func RegisterQuestion(registry *Registry, deps QuestionDeps) (func(), error) {
	return registry.Register(map[string]Tool{
		QuestionName: MakeQuestion(deps),
	})
}
