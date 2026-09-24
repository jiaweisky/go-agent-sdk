package agent

import (
	"net/http"

	"golang.org/x/text/language"

	"github.com/user/go-agent-sdk/core"
	"github.com/user/go-agent-sdk/core/database"
	"github.com/user/go-agent-sdk/core/ripgrep"
	"github.com/user/go-agent-sdk/core/tool"
	"github.com/user/go-agent-sdk/schema"
)

type builtinDeps struct {
	db         *database.Database
	events     core.Event
	permission core.Permission
	question   core.Question
	location   core.Location
	global     core.Global
	shell      string
	skills     tool.SkillLister
	webSearch  *tool.WebSearchConfig
	locale     language.Tag
	agents     *core.Agent
	task       TaskRunner
}

// registerBuiltins registers the shipped built-in tools into a registry.
// agentPermissions are the calling agent's rules (used to filter the task
// tool's advertised subagent types); pass nil for the session-agnostic base
// registry.
func registerBuiltins(tools tool.Tools, deps builtinDeps, agentPermissions schema.PermissionRuleset) error {
	mutation := core.NewLocationMutation(&deps.location)
	files := core.NewFileMutation()
	process := core.NewAppProcess()
	image := core.NewImage(core.DefaultImageConfig())
	ripgrepService := core.NewRipgrep(process, ripgrep.NewBinary(deps.global))
	todos := core.NewSessionTodo(deps.db, deps.events)
	directory := string(deps.location.Directory)

	registrations := []func() (func(), error){
		func() (func(), error) {
			return tool.RegisterRead(tools, tool.ReadDeps{
				Mutation: mutation, Reader: tool.NewReadFileSystem(deps.locale), Image: image, Permission: deps.permission,
			})
		},
		func() (func(), error) {
			return tool.RegisterWrite(tools, tool.WriteDeps{Mutation: mutation, Files: files, Permission: deps.permission})
		},
		func() (func(), error) {
			return tool.RegisterEdit(tools, tool.EditDeps{Mutation: mutation, Files: files, Permission: deps.permission})
		},
		func() (func(), error) {
			return tool.RegisterApplyPatch(tools, tool.ApplyPatchDeps{Mutation: mutation, Files: files, Permission: deps.permission})
		},
		func() (func(), error) {
			return tool.RegisterBash(tools, tool.BashDeps{Mutation: mutation, Process: process, Permission: deps.permission, Shell: deps.shell})
		},
		func() (func(), error) {
			return tool.RegisterGlob(tools, tool.GlobDeps{Ripgrep: ripgrepService, Directory: directory, Permission: deps.permission})
		},
		func() (func(), error) {
			return tool.RegisterGrep(tools, tool.GrepDeps{Ripgrep: ripgrepService, Directory: directory, Permission: deps.permission})
		},
		func() (func(), error) {
			return tool.RegisterTodoWrite(tools, tool.TodoWriteDeps{Todos: todos, Permission: deps.permission})
		},
		func() (func(), error) {
			return tool.RegisterQuestion(tools, tool.QuestionDeps{Permission: deps.permission, Question: deps.question})
		},
		func() (func(), error) {
			return tool.RegisterWebFetch(tools, tool.WebFetchDeps{HTTPClient: http.DefaultClient, Permission: deps.permission})
		},
		func() (func(), error) {
			return tool.RegisterSkill(tools, tool.SkillToolDeps{Skills: deps.skills, Permission: deps.permission})
		},
		func() (func(), error) {
			return RegisterTask(tools, TaskToolDeps{
				Permission: deps.permission, Agents: deps.agents, Runner: deps.task,
				AgentPermissions: agentPermissions, Locale: deps.locale,
			})
		},
	}
	// websearch is registered only when the host supplies a config, because the
	// SDK does not read the provider keys from the environment.
	if deps.webSearch != nil {
		registrations = append(registrations, func() (func(), error) {
			return tool.RegisterWebSearch(tools, tool.WebSearchDeps{
				HTTPClient: http.DefaultClient, Permission: deps.permission, Config: *deps.webSearch,
			})
		})
	}
	for _, register := range registrations {
		if _, err := register(); err != nil {
			return err
		}
	}
	return nil
}
