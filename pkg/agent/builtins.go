package agent

import (
	"net/http"

	"golang.org/x/text/language"

	"github.com/jiaweisky/go-agent-sdk/internal/core/database"
	"github.com/jiaweisky/go-agent-sdk/internal/core/ripgrep"
	"github.com/jiaweisky/go-agent-sdk/internal/core/wire"
	"github.com/jiaweisky/go-agent-sdk/pkg/core"
	"github.com/jiaweisky/go-agent-sdk/pkg/core/tool"
	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

// builtinDeps are the services the built-in tools register with.
type builtinDeps struct {
	db         *database.Database
	events     core.Event
	permission core.Permission
	question   core.Question
	location   core.Location
	global     core.Global
	shell      string
	skills     tool.SkillLister
	locale     language.Tag
	agents     *core.Agent
	task       TaskRunner
	name       string
	process    core.AppProcess
	fileGuard  core.FileWriteGuard
	excludes   []string
}

// registerBuiltins registers the shipped built-in tools into a registry.
// agentPermissions are the calling agent's rules, used to filter the task
// tool's advertised subagent types.
func registerBuiltins(tools tool.Tools, deps builtinDeps, agentPermissions schema.PermissionRuleset) error {
	mutation := core.NewLocationMutation(&deps.location)
	files := core.NewFileMutation()
	process := deps.process
	if process == nil {
		process = core.NewAppProcess()
	}
	image := core.NewImage(core.DefaultImageConfig())
	ripgrepService := core.NewRipgrep(process, ripgrep.NewBinary(deps.global), deps.excludes...)
	todos := wire.NewSessionTodo(deps.db, deps.events)
	directory := string(deps.location.Directory)

	registrations := []func() error{
		func() error {
			_, err := tool.RegisterRead(tools, tool.ReadDeps{
				Mutation: mutation, Reader: tool.NewReadFileSystem(deps.locale), Image: image, Permission: deps.permission,
			})
			return err
		},
		func() error {
			_, err := tool.RegisterWrite(tools, tool.WriteDeps{Mutation: mutation, Files: files, Permission: deps.permission, Guard: deps.fileGuard})
			return err
		},
		func() error {
			_, err := tool.RegisterEdit(tools, tool.EditDeps{Mutation: mutation, Files: files, Permission: deps.permission, Guard: deps.fileGuard})
			return err
		},
		func() error {
			_, err := tool.RegisterApplyPatch(
				tools,
				tool.ApplyPatchDeps{Mutation: mutation, Files: files, Permission: deps.permission, Guard: deps.fileGuard},
			)
			return err
		},
		func() error {
			_, err := tool.RegisterBash(
				tools,
				tool.BashDeps{Mutation: mutation, Process: process, Permission: deps.permission, Shell: deps.shell},
			)
			return err
		},
		func() error {
			_, err := tool.RegisterGlob(
				tools,
				tool.GlobDeps{Ripgrep: ripgrepService, Directory: directory, Permission: deps.permission},
			)
			return err
		},
		func() error {
			_, err := tool.RegisterGrep(
				tools,
				tool.GrepDeps{Ripgrep: ripgrepService, Directory: directory, Permission: deps.permission},
			)
			return err
		},
		func() error {
			_, err := tool.RegisterTodoWrite(tools, tool.TodoWriteDeps{Todos: todos, Permission: deps.permission})
			return err
		},
		func() error {
			_, err := tool.RegisterQuestion(tools, tool.QuestionDeps{Permission: deps.permission, Question: deps.question})
			return err
		},
		func() error {
			_, err := tool.RegisterWebFetch(
				tools,
				tool.WebFetchDeps{HTTPClient: http.DefaultClient, Permission: deps.permission, UserAgent: deps.name},
			)
			return err
		},
		func() error {
			_, err := tool.RegisterSkill(tools, tool.SkillToolDeps{Skills: deps.skills, Permission: deps.permission})
			return err
		},
		func() error {
			_, err := RegisterTask(tools, TaskToolDeps{
				Permission: deps.permission, Agents: deps.agents, Runner: deps.task,
				AgentPermissions: agentPermissions, Locale: deps.locale,
			})
			return err
		},
	}
	for _, register := range registrations {
		if err := register(); err != nil {
			return err
		}
	}
	return nil
}
