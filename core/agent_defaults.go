package core

import (
	"path/filepath"

	"github.com/user/go-agent-sdk/schema"
)

// Default agent system prompts, ported from core/src/plugin/agent.ts.
const agentBuildSystem = "You are an AI coding agent. Help the user accomplish software engineering tasks by inspecting the workspace, making targeted changes, and using tools according to the configured permissions."

const agentPromptExplore = `You are a file search specialist. You excel at thoroughly navigating and exploring codebases.

Your strengths:
- Rapidly finding files using glob patterns
- Searching code and text with powerful regex patterns
- Reading and analyzing file contents

Guidelines:
- Use Glob for broad file pattern matching
- Use Grep for searching file contents with regex
- Use Read when you know the specific file path you need to read
- Adapt your search approach based on the thoroughness level specified by the caller
- Return file paths as absolute paths in your final response
- For clear communication, avoid using emojis
- Do not create any files, or run bash commands that modify the user's system state in any way

Complete the user's search request efficiently and report your findings clearly.`

const agentPromptCompaction = `You are a context summarization agent. You are given a conversation between a user and an agent. Your goal is to produce a structured summary matching the format specified so another coding agent can continue the work.

Always follow the exact output structure requested by the user prompt. Keep every section, preserve exact file paths and identifiers when known, and prefer terse bullets over paragraphs.

Do not continue the conversation. Do not respond to any questions in the conversation. Only output the structured summary in the exact format requested by the user prompt. Respond in the same language as the conversation.`

const agentPromptTitle = `You are a title generator. You output ONLY a thread title. Nothing else.

<task>
Generate a brief title that would help the user find this conversation later.

Follow all rules in <rules>
Use the <examples> so you know what a good title looks like.
Your output must be:
- A single line
- <=50 characters
- No explanations
</task>

<rules>
- you MUST use the same language as the user message you are summarizing
- Title must be grammatically correct and read naturally - no word salad
- Never include tool names in the title (e.g. "read tool", "bash tool", "edit tool")
- Focus on the main topic or question the user needs to retrieve
- Vary your phrasing - avoid repetitive patterns like always starting with "Analyzing"
- When a file is mentioned, focus on WHAT the user wants to do WITH the file, not just that they shared it
- Keep exact: technical terms, numbers, filenames, HTTP codes
- Remove: the, this, my, a, an
- Never assume tech stack
- Never use tools
- NEVER respond to questions, just generate a title for the conversation
- The title should NEVER include "summarizing" or "generating" when generating a title
- DO NOT SAY YOU CANNOT GENERATE A TITLE OR COMPLAIN ABOUT THE INPUT
- Always output something meaningful, even if the input is minimal.
- If the user message is short or conversational (e.g. "hello", "lol", "what's up", "hey"):
  -> create a title that reflects the user's tone or intent (such as Greeting, Quick check-in, Light chat, Intro message, etc.)
</rules>

<examples>
"debug 500 errors in production" -> Debugging production 500 errors
"refactor user service" -> Refactoring user service
"why is app.js failing" -> app.js failure investigation
"implement rate limiting" -> Rate limiting implementation
"how do I connect postgres to my API" -> Postgres API connection
"best practices for React hooks" -> React hooks best practices
"@src/credential.ts can you add refresh token support" -> Credential refresh token support
"@utils/parser.ts this is broken" -> Parser bug fix
"look at @config.json" -> Config review
"@App.tsx add dark mode toggle" -> Dark mode toggle in App
</examples>`

const agentPromptSummary = `Summarize what was done in this conversation. Write like a pull request description.

Rules:
- 2-3 sentences max
- Describe the changes made, not the process
- Do not mention running tests, builds, or other validation steps
- Do not explain what the user asked for
- Write in first person (I added..., I fixed...)
- Never ask questions or add new questions
- If the conversation ends with an unanswered question to the user, preserve that exact question
- If the conversation ends with an imperative statement or request to the user (e.g. "Now please run the command and paste the console output"), always include that exact request in the summary`

// ApplyDefaultAgents registers the built-in agents (build, plan, general,
// explore, compaction, title, summary) and their default permission rulesets.
func ApplyDefaultAgents(agents *Agent, location *Location, global Global) error {
	worktree := string(location.Directory)
	truncationGlob := filepath.Join(global.Data, "tool-output", "*")

	readonlyExternalDirectory := PermissionRuleset{
		{Action: "external_directory", Resource: "*", Effect: PermissionEffectAsk},
	}
	for _, resource := range []string{truncationGlob, filepath.Join(global.Tmp, "*")} {
		readonlyExternalDirectory = append(readonlyExternalDirectory, PermissionRule{
			Action: "external_directory", Resource: resource, Effect: PermissionEffectAllow,
		})
	}

	defaults := PermissionRuleset{{Action: "*", Resource: "*", Effect: PermissionEffectAllow}}
	defaults = append(defaults, readonlyExternalDirectory...)
	defaults = append(defaults,
		PermissionRule{Action: "question", Resource: "*", Effect: PermissionEffectDeny},
		PermissionRule{Action: "plan_enter", Resource: "*", Effect: PermissionEffectDeny},
		PermissionRule{Action: "plan_exit", Resource: "*", Effect: PermissionEffectDeny},
		PermissionRule{Action: "read", Resource: "*", Effect: PermissionEffectAllow},
		PermissionRule{Action: "read", Resource: "*.env", Effect: PermissionEffectAsk},
		PermissionRule{Action: "read", Resource: "*.env.*", Effect: PermissionEffectAsk},
		PermissionRule{Action: "read", Resource: "*.env.example", Effect: PermissionEffectAllow},
	)

	planRelative, err := filepath.Rel(worktree, filepath.Join(global.Data, "plans", "*.md"))
	if err != nil {
		planRelative = filepath.Join(global.Data, "plans", "*.md")
	}

	_, err = agents.Transform(func(draft AgentDraft) error {
		draft.Update(AgentDefaultID, func(item *schema.AgentInfo) {
			description := "The default agent. Executes tools based on configured permissions."
			item.Description = &description
			if item.System == nil {
				system := agentBuildSystem
				item.System = &system
			}
			item.Mode = schema.AgentModePrimary
			item.Permissions = append(item.Permissions, PermissionMerge(defaults, PermissionRuleset{
				{Action: "question", Resource: "*", Effect: PermissionEffectAllow},
				{Action: "plan_enter", Resource: "*", Effect: PermissionEffectAllow},
			})...)
		})

		draft.Update(schema.AgentID("plan"), func(item *schema.AgentInfo) {
			description := "Plan mode. Disallows all edit tools."
			item.Description = &description
			item.Mode = schema.AgentModePrimary
			item.Permissions = append(item.Permissions, PermissionMerge(defaults, PermissionRuleset{
				{Action: "question", Resource: "*", Effect: PermissionEffectAllow},
				{Action: "plan_exit", Resource: "*", Effect: PermissionEffectAllow},
				{Action: "external_directory", Resource: filepath.Join(global.Data, "plans", "*"), Effect: PermissionEffectAllow},
				{Action: "edit", Resource: "*", Effect: PermissionEffectDeny},
				{Action: "edit", Resource: filepath.Join(".opencode", "plans", "*.md"), Effect: PermissionEffectAllow},
				{Action: "edit", Resource: planRelative, Effect: PermissionEffectAllow},
			})...)
		})

		draft.Update(schema.AgentID("general"), func(item *schema.AgentInfo) {
			description := "General-purpose agent for researching complex questions and executing multi-step tasks. Use this agent to execute multiple units of work in parallel."
			item.Description = &description
			item.Mode = schema.AgentModeSubagent
			item.Permissions = append(item.Permissions, PermissionMerge(defaults, PermissionRuleset{
				{Action: "todowrite", Resource: "*", Effect: PermissionEffectDeny},
			})...)
		})

		draft.Update(schema.AgentID("explore"), func(item *schema.AgentInfo) {
			description := `Fast agent specialized for exploring codebases. Use this when you need to quickly find files by patterns (eg. "src/components/**/*.tsx"), search code for keywords (eg. "API endpoints"), or answer questions about the codebase (eg. "how do API endpoints work?"). When calling this agent, specify the desired thoroughness level: "quick" for basic searches, "medium" for moderate exploration, or "very thorough" for comprehensive analysis across multiple locations and naming conventions.`
			item.Description = &description
			system := agentPromptExplore
			item.System = &system
			item.Mode = schema.AgentModeSubagent
			item.Permissions = append(item.Permissions, PermissionMerge(defaults, PermissionRuleset{
				{Action: "*", Resource: "*", Effect: PermissionEffectDeny},
				{Action: "grep", Resource: "*", Effect: PermissionEffectAllow},
				{Action: "glob", Resource: "*", Effect: PermissionEffectAllow},
				{Action: "webfetch", Resource: "*", Effect: PermissionEffectAllow},
				{Action: "read", Resource: "*", Effect: PermissionEffectAllow},
			}, readonlyExternalDirectory)...)
		})

		draft.Update(schema.AgentID("compaction"), func(item *schema.AgentInfo) {
			item.Mode = schema.AgentModePrimary
			item.Hidden = true
			system := agentPromptCompaction
			item.System = &system
			item.Permissions = append(item.Permissions, PermissionMerge(defaults, PermissionRuleset{
				{Action: "*", Resource: "*", Effect: PermissionEffectDeny},
			})...)
		})

		draft.Update(schema.AgentID("title"), func(item *schema.AgentInfo) {
			item.Mode = schema.AgentModePrimary
			item.Hidden = true
			system := agentPromptTitle
			item.System = &system
			item.Permissions = append(item.Permissions, PermissionMerge(defaults, PermissionRuleset{
				{Action: "*", Resource: "*", Effect: PermissionEffectDeny},
			})...)
		})

		draft.Update(schema.AgentID("summary"), func(item *schema.AgentInfo) {
			item.Mode = schema.AgentModePrimary
			item.Hidden = true
			system := agentPromptSummary
			item.System = &system
			item.Permissions = append(item.Permissions, PermissionMerge(defaults, PermissionRuleset{
				{Action: "*", Resource: "*", Effect: PermissionEffectDeny},
			})...)
		})
		return nil
	})
	return err
}
