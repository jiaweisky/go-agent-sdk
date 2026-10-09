package tool

import (
	"context"
	"testing"

	"github.com/jiaweisky/go-agent-sdk/core"
	"github.com/jiaweisky/go-agent-sdk/pkg/llm/schema"
)

type testQuestion struct {
	answers []core.QuestionAnswer
	asked   *core.QuestionAskInput
}

func (q *testQuestion) Ask(_ context.Context, input core.QuestionAskInput) ([]core.QuestionAnswer, error) {
	q.asked = &input
	return q.answers, nil
}

func (q *testQuestion) Reply(core.QuestionReplyInput) error { return nil }
func (q *testQuestion) Reject(core.QuestionID) error        { return nil }
func (q *testQuestion) List() []core.QuestionRequest        { return nil }
func (q *testQuestion) Close() error                        { return nil }

func newQuestionDeps(question core.Question) QuestionDeps {
	allow := core.PermissionRuleset{
		{Action: "*", Resource: "*", Effect: core.PermissionEffectAllow},
	}
	return QuestionDeps{
		Permission: core.NewPermission(core.PermissionConfig{
			Sessions:  testSessions{agent: "build"},
			Agents:    testAgents{rules: allow},
			Saved:     testSaved{},
			Events:    testEvents{},
			ProjectID: "proj_1",
		}),
		Question: question,
	}
}

func questionCall() schema.ToolCallPart {
	return schema.ToolCallPart{
		ID:   "call_1",
		Name: QuestionName,
		Input: map[string]any{
			"questions": []any{
				map[string]any{
					"question": "Which direction?",
					"header":   "Direction",
					"custom":   true,
					"options": []any{
						map[string]any{"label": "Left", "description": "Go left"},
						map[string]any{"label": "Right", "description": "Go right"},
					},
				},
			},
		},
	}
}

func TestQuestion_AnswersToModelOutput(t *testing.T) {
	question := &testQuestion{answers: []core.QuestionAnswer{{"Left", "Right"}}}
	registry := NewRegistry()
	if _, err := RegisterQuestion(registry, newQuestionDeps(question)); err != nil {
		t.Fatalf("register: %v", err)
	}

	mat := registry.Materialize(nil)
	if len(mat.Definitions) != 1 {
		t.Fatalf("expected 1 definition, got %d", len(mat.Definitions))
	}
	if mat.Definitions[0].Name != QuestionName {
		t.Fatalf("expected definition name %q, got %q", QuestionName, mat.Definitions[0].Name)
	}

	result, err := mat.Settle(context.Background(), ExecuteInput{
		SessionID:          "ses_1",
		Agent:              "build",
		AssistantMessageID: "msg_1",
		Call:               questionCall(),
	})
	if err != nil {
		t.Fatalf("settle: %v", err)
	}
	expected := `User has answered your questions: "Which direction?"="Left, Right". ` +
		"You can now continue with the user's answers in mind."
	if result.Result.Value != expected {
		t.Fatalf("unexpected model output: %v", result.Result.Value)
	}
	if question.asked == nil || question.asked.Tool == nil {
		t.Fatalf("expected question tool metadata to be forwarded")
	}
	if question.asked.Tool.MessageID != "msg_1" || question.asked.Tool.CallID != "call_1" {
		t.Fatalf("unexpected tool metadata: %+v", question.asked.Tool)
	}
	if len(question.asked.Questions) != 1 || question.asked.Questions[0].Custom != nil {
		t.Fatalf("expected tool input to use Prompt (custom dropped), got %+v", question.asked.Questions)
	}
}

func TestQuestion_Unanswered(t *testing.T) {
	question := &testQuestion{answers: []core.QuestionAnswer{{}}}
	registry := NewRegistry()
	if _, err := RegisterQuestion(registry, newQuestionDeps(question)); err != nil {
		t.Fatalf("register: %v", err)
	}

	result, err := registry.Materialize(nil).Settle(context.Background(), ExecuteInput{
		SessionID:          "ses_1",
		Agent:              "build",
		AssistantMessageID: "msg_1",
		Call:               questionCall(),
	})
	if err != nil {
		t.Fatalf("settle: %v", err)
	}
	expected := `User has answered your questions: "Which direction?"="Unanswered". ` +
		"You can now continue with the user's answers in mind."
	if result.Result.Value != expected {
		t.Fatalf("unexpected model output: %v", result.Result.Value)
	}
}

func TestQuestion_DeniedPermission(t *testing.T) {
	deny := core.PermissionRuleset{
		{Action: "*", Resource: "*", Effect: core.PermissionEffectDeny},
	}
	deps := QuestionDeps{
		Permission: core.NewPermission(core.PermissionConfig{
			Sessions:  testSessions{agent: "build"},
			Agents:    testAgents{rules: deny},
			Saved:     testSaved{},
			Events:    testEvents{},
			ProjectID: "proj_1",
		}),
		Question: &testQuestion{answers: []core.QuestionAnswer{{"Left"}}},
	}

	registry := NewRegistry()
	if _, err := RegisterQuestion(registry, deps); err != nil {
		t.Fatalf("register: %v", err)
	}

	result, err := registry.Materialize(nil).Settle(context.Background(), ExecuteInput{
		SessionID:          "ses_1",
		Agent:              "build",
		AssistantMessageID: "msg_1",
		Call:               questionCall(),
	})
	if err != nil {
		t.Fatalf("settle: %v", err)
	}
	if result.Result.Type != "error" {
		t.Fatalf("expected error result, got %q", result.Result.Type)
	}
	if result.Result.Value != "Permission denied: question" {
		t.Fatalf("unexpected error: %v", result.Result.Value)
	}
}
