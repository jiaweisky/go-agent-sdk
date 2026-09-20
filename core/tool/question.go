package tool

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/user/go-agent-sdk/core"
	"github.com/user/go-agent-sdk/jsonschema"
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

var questionOptionSchema = jsonschema.Object(
	jsonschema.Required("label", jsonschema.String(jsonschema.Description("Display text (1-5 words, concise)"))),
	jsonschema.Required("description", jsonschema.String(jsonschema.Description("Explanation of choice"))),
)

var questionPromptSchema = jsonschema.Object(
	jsonschema.Required("question", jsonschema.String(jsonschema.Description("Complete question"))),
	jsonschema.Required("header", jsonschema.String(jsonschema.Description("Very short label (max 30 chars)"))),
	jsonschema.Required("options", jsonschema.Array(questionOptionSchema, jsonschema.Description("Available choices"))),
	jsonschema.Optional("multiple", jsonschema.Boolean()),
)

var QuestionInputSchema = jsonschema.Object(
	jsonschema.Required("questions", jsonschema.Array(questionPromptSchema, jsonschema.Description("Questions to ask"))),
)

var QuestionOutputSchema = jsonschema.Object(
	jsonschema.Required("answers", jsonschema.Array(jsonschema.Array(jsonschema.String()))),
)

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
