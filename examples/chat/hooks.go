package main

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/jiaweisky/go-agent-sdk/pkg/agent"
)

// newSessionHooks builds the SessionHooks the chat attaches to every session it
// creates or resumes:
//
//   - OnSessionStart / OnSessionEnd log the lifecycle; OnSessionStart also adds
//     the current date to the system context.
//   - OnUserPromptSubmitted rewrites a "?..." prompt into a request for a
//     concise answer.
//   - OnPreToolUse logs each call and denies an obvious "rm -rf".
//   - OnPostToolUse / OnPostToolUseFailure run around tool settlement; the
//     failure path is logged.
//   - OnToolDefinition logs once per session.
//   - OnErrorOccurred logs a failed turn.
//
// Task subagents inherit these hooks, so their tool calls are covered too.
func newSessionHooks(console *console) *agent.SessionHooks {
	// A parent and its task subagents share this hook set and can materialize
	// their tools concurrently, so the one-time log is guarded.
	var annotated sync.Once
	return &agent.SessionHooks{
		OnSessionStart: func(in agent.SessionStartInput) (*agent.SessionStartOutput, error) {
			console.write(hookLine("session start %s (%s/%s)", in.SessionID, in.Agent, in.Model))
			return &agent.SessionStartOutput{
				AdditionalContext: "The current date is " + time.Now().Format("2006-01-02") + ".",
			}, nil
		},
		OnSessionEnd: func(in agent.SessionEndInput) (*agent.SessionEndOutput, error) {
			console.write(hookLine("session end %s (%s)", in.SessionID, in.Reason))
			return nil, nil
		},
		OnUserPromptSubmitted: func(in agent.UserPromptSubmittedInput) (*agent.UserPromptSubmittedOutput, error) {
			if !strings.HasPrefix(in.Prompt, "?") {
				return nil, nil
			}
			console.write(hookLine("prompt rewritten (stripped \"?\" prefix)"))
			return &agent.UserPromptSubmittedOutput{
				ModifiedPrompt: "Answer concisely: " + strings.TrimSpace(strings.TrimPrefix(in.Prompt, "?")),
			}, nil
		},
		OnPreToolUse: func(in agent.PreToolUseInput) (*agent.PreToolUseOutput, error) {
			console.write(hookLine("pre-tool-use %s", in.ToolName))
			if in.ToolName == "bash" && strings.Contains(fmt.Sprint(in.Args), "rm -rf") {
				console.write(hookLine("denied %s: rm -rf blocked by hook", in.ToolName))
				return &agent.PreToolUseOutput{PermissionDecision: "deny"}, nil
			}
			return nil, nil
		},
		OnPostToolUse: func(agent.PostToolUseInput) (*agent.PostToolUseOutput, error) {
			return nil, nil
		},
		OnPostToolUseFailure: func(in agent.PostToolUseFailureInput) (*agent.PostToolUseFailureOutput, error) {
			console.write(hookLine("tool failed %s: %s", in.ToolName, firstLine(in.Error)))
			return nil, nil
		},
		OnToolDefinition: func(agent.ToolDefinitionInput) (*agent.ToolDefinitionOutput, error) {
			annotated.Do(func() {
				console.write(hookLine("tool definitions passed through OnToolDefinition"))
			})
			return nil, nil
		},
		OnErrorOccurred: func(in agent.ErrorOccurredInput) (*agent.ErrorOccurredOutput, error) {
			console.write(hookLine("error: %s", firstLine(in.Error)))
			return nil, nil
		},
	}
}

// hookLine renders a dim "[hook] ..." line, matching the other bracketed
// lifecycle lines the chat prints.
func hookLine(format string, args ...any) string {
	return fmt.Sprintf("%s[hook] %s%s\n", dim, fmt.Sprintf(format, args...), reset)
}
