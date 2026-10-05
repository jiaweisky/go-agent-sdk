package agent

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/text/language"

	"github.com/jiaweisky/go-agent-sdk/core"
	"github.com/jiaweisky/go-agent-sdk/core/database"
	"github.com/jiaweisky/go-agent-sdk/core/installation"
	coreproject "github.com/jiaweisky/go-agent-sdk/core/project"
	"github.com/jiaweisky/go-agent-sdk/core/reference"
	"github.com/jiaweisky/go-agent-sdk/core/session"
	"github.com/jiaweisky/go-agent-sdk/core/session/runner"
	"github.com/jiaweisky/go-agent-sdk/core/skill"
	"github.com/jiaweisky/go-agent-sdk/core/systemcontext"
	"github.com/jiaweisky/go-agent-sdk/core/tool"
	"github.com/jiaweisky/go-agent-sdk/core/util"
	defsconfig "github.com/jiaweisky/go-agent-sdk/defs/config"
	defsskill "github.com/jiaweisky/go-agent-sdk/defs/skill"
	"github.com/jiaweisky/go-agent-sdk/llm"
	"github.com/jiaweisky/go-agent-sdk/llm/route"
	"github.com/jiaweisky/go-agent-sdk/mcp"
	"github.com/jiaweisky/go-agent-sdk/schema"
)

// runtime holds the process-wide services shared by all sessions.
type runtime struct {
	db             *database.Database
	events         core.Event
	store          *session.Store
	agents         *core.Agent
	agentSelector  *sessionAgents
	skills         *core.Skill
	outputStore    tool.OutputStore
	toolOutput     *core.ToolOutputStore
	permission     *permissionHandler
	question       *questionHandler
	runner         *runner.LLMRunner
	a2aBackendPool *a2aBackendPool
	coord          *session.Coordinator[schema.SessionID]
	snapshot       *core.Snapshot
	location       core.Location
	projectID      schema.ProjectID
	models         runner.ModelResolver
	subagentDepth  int
	name           string

	hooksMu sync.RWMutex
	hooks   map[string]*SessionHooks

	// inbound marks sessions served by the A2A inbound executor, which owns
	// their turn records (the task store maintains them), so the runner does
	// not create a competing local turn.
	inboundMu sync.Mutex
	inbound   map[schema.SessionID]struct{}

	builtin     builtinDeps
	clientTools []Tool
	dispatcher  *toolDispatcher
	mcp         *mcp.Client
	plugin      *core.Plugin
	reference   *core.Reference
	cancel      context.CancelFunc

	toolsMu     sync.Mutex
	pluginTools map[string][]pluginToolEntry

	mcpToolsMu    sync.RWMutex
	mcpTools      map[string]tool.Tool
	mcpToolsUnsub func()

	onPanic func(PanicInfo)
	onError func(error)
}

// pluginToolEntry is one runtime registration of an application tool. Entries
// are tracked per name so unregistering one registration does not drop a tool
// another plugin still provides.
type pluginToolEntry struct {
	token *int
	tool  tool.Tool
}

// toolRegistrar returns the runtime's application-tool registration surface.
func (r *runtime) toolRegistrar() *ToolRegistrar {
	return &ToolRegistrar{runtime: r}
}

// registerTools adds application tools for sessions created afterwards and for
// live sessions. The returned function removes them again.
func (r *runtime) registerTools(tools map[string]tool.Tool) (func(), error) {
	if len(tools) == 0 {
		return func() {}, nil
	}
	for name := range tools {
		if err := tool.ValidateName(name); err != nil {
			return nil, err
		}
	}

	// Hold toolsMu across both the registry update and the live-session
	// registration, and across setSessionRegistry, so a session created
	// concurrently lands on exactly one side of the registration.
	token := new(int)
	r.toolsMu.Lock()
	if r.pluginTools == nil {
		r.pluginTools = map[string][]pluginToolEntry{}
	}
	for name, item := range tools {
		r.pluginTools[name] = append(r.pluginTools[name], pluginToolEntry{token: token, tool: item})
	}
	cleanupLive, err := r.dispatcher.registerAll(tools)
	if err != nil {
		r.removePluginToolsLocked(tools, token)
		r.toolsMu.Unlock()
		return nil, err
	}
	r.toolsMu.Unlock()

	return func() {
		r.toolsMu.Lock()
		r.removePluginToolsLocked(tools, token)
		r.toolsMu.Unlock()
		cleanupLive()
	}, nil
}

// removePluginToolsLocked drops the registration identified by token. The
// caller must hold toolsMu.
func (r *runtime) removePluginToolsLocked(tools map[string]tool.Tool, token *int) {
	for name := range tools {
		var kept []pluginToolEntry
		for _, entry := range r.pluginTools[name] {
			if entry.token != token {
				kept = append(kept, entry)
			}
		}
		if len(kept) > 0 {
			r.pluginTools[name] = kept
		} else {
			delete(r.pluginTools, name)
		}
	}
}

// toolDispatcher routes materialization to the session-scoped registry. Every
// session has one (set on create/resume and for subagent children), so there is
// no session-agnostic fallback: materializing an unknown session yields no tools
// rather than a registry built with the wrong permission rules.
type toolDispatcher struct {
	mu       sync.Mutex
	sessions map[string]*tool.Registry
}

func (d *toolDispatcher) MaterializeFor(
	sessionID string,
	permissions []core.PermissionRule,
) (*tool.Materialization, error) {
	d.mu.Lock()
	registry := d.sessions[sessionID]
	d.mu.Unlock()
	if registry == nil {
		return &tool.Materialization{}, nil
	}
	return registry.Materialize(permissions), nil
}

func (d *toolDispatcher) set(sessionID string, registry *tool.Registry) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.sessions[sessionID] = registry
}

func (d *toolDispatcher) remove(sessionID string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.sessions, sessionID)
}

// registerAll registers tools into every live session registry, returning a
// function that removes them again.
func (d *toolDispatcher) registerAll(tools map[string]tool.Tool) (func(), error) {
	d.mu.Lock()
	registries := make([]*tool.Registry, 0, len(d.sessions))
	for _, registry := range d.sessions {
		registries = append(registries, registry)
	}
	d.mu.Unlock()

	cleanups := make([]func(), 0, len(registries))
	for _, registry := range registries {
		cleanup, err := registry.Register(tools)
		if err != nil {
			for _, done := range cleanups {
				done()
			}
			return nil, err
		}
		cleanups = append(cleanups, cleanup)
	}
	return func() {
		for _, cleanup := range cleanups {
			cleanup()
		}
	}, nil
}

// sessionAgents resolves the agent a session runs with: the in-memory derived
// agent when the session customized its system prompt, otherwise the agent id
// stored on the session row. Keeping the derived copy here, rather than in the
// shared registry, keeps it out of agent listings and leaves the registry's
// transform stack untouched.
type sessionAgents struct {
	agents *core.Agent

	mu        sync.Mutex
	effective map[schema.SessionID]schema.AgentInfo
}

func newSessionAgents(agents *core.Agent) *sessionAgents {
	return &sessionAgents{agents: agents, effective: map[schema.SessionID]schema.AgentInfo{}}
}

func (s *sessionAgents) set(sessionID schema.SessionID, info schema.AgentInfo) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.effective[sessionID] = info
}

func (s *sessionAgents) remove(sessionID schema.SessionID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.effective, sessionID)
}

func (s *sessionAgents) Select(sessionID schema.SessionID, id *string) core.AgentSelection {
	// A session-scoped override (a derived agent) only answers for its own base
	// agent. Other ids, such as the built-in "title" agent, resolve from the
	// shared registry.
	if info, ok := s.effectiveFor(sessionID); ok && (id == nil || *id == "" || baseAgentID(string(info.ID)) == *id) {
		selected := info
		return core.AgentSelection{ID: selected.ID, Info: &selected}
	}
	return s.agents.Select(id)
}

func (s *sessionAgents) effectiveFor(sessionID schema.SessionID) (schema.AgentInfo, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	info, ok := s.effective[sessionID]
	return info, ok
}

// newSessionRegistry builds a registry with the built-ins plus the client and
// session user tools, isolating session-scoped tools from other sessions.
func (r *runtime) newSessionRegistry(
	agent *schema.AgentInfo,
	sessionRules schema.PermissionRuleset,
	sessionTools []Tool,
) (*tool.Registry, error) {
	registry := tool.NewRegistry(tool.WithOutputStore(r.outputStore))
	var permissions schema.PermissionRuleset
	if agent != nil {
		permissions = agent.Permissions
	}
	if len(sessionRules) > 0 {
		permissions = core.PermissionMerge(permissions, sessionRules)
	}
	if err := registerBuiltins(registry, r.builtin, permissions); err != nil {
		return nil, err
	}
	if err := registerIfAny(registry, r.mcpToolSet()); err != nil {
		return nil, err
	}
	tools := make([]Tool, 0, len(r.clientTools)+len(sessionTools))
	tools = append(tools, r.clientTools...)
	tools = append(tools, sessionTools...)
	for _, userTool := range tools {
		if _, err := registry.Register(map[string]tool.Tool{userTool.Name: userTool.coreTool(r.permission)}); err != nil {
			return nil, err
		}
	}
	return registry, nil
}

// registerIfAny registers tools when there are any.
func registerIfAny(registry *tool.Registry, tools map[string]tool.Tool) error {
	if len(tools) == 0 {
		return nil
	}
	_, err := registry.Register(tools)
	return err
}

// setSessionRegistry installs a session's tool registry, merging the currently
// registered plugin tools under the same lock RegisterTools uses, so a
// concurrent registration cannot miss this session.
func (r *runtime) setSessionRegistry(sessionID string, registry *tool.Registry) error {
	r.toolsMu.Lock()
	defer r.toolsMu.Unlock()
	if err := registerIfAny(registry, pluginToolSnapshot(r.pluginTools)); err != nil {
		return err
	}
	r.dispatcher.set(sessionID, registry)
	return nil
}

// pluginToolSnapshot returns the newest tool for each plugin-registered name.
func pluginToolSnapshot(entries map[string][]pluginToolEntry) map[string]tool.Tool {
	if len(entries) == 0 {
		return nil
	}
	snapshot := make(map[string]tool.Tool, len(entries))
	for name, list := range entries {
		if len(list) > 0 {
			snapshot[name] = list[len(list)-1].tool
		}
	}
	return snapshot
}

// publishSessionStatus emits the live session.status event so subscribers (and
// the agent event stream) can tell when a session is busy or idle. The SDK
// publishes it from its own drain lifecycle.
func (r *runtime) publishSessionStatus(id schema.SessionID, status schema.SessionStatusInfo) {
	_, _ = r.events.Publish(schema.SessionStatusEventStatus, map[string]any{
		"sessionID": id,
		"status":    status,
	}, nil)
}

// admit enqueues a prompt for a session's next turn.
func (r *runtime) admit(sessionID schema.SessionID, prompt schema.Prompt, delivery schema.SessionDelivery) error {
	_, err := session.Admit(r.db.DB, appEvents{events: r.events}, session.AdmitInput{
		ID:        schema.NewMessageID(),
		SessionID: sessionID,
		Prompt:    prompt,
		Delivery:  delivery,
	})
	return err
}

// compact summarizes a session's history on demand. It refuses while a turn is
// active so the summary cannot race the turn's own writes.
func (r *runtime) compact(ctx context.Context, sessionID schema.SessionID) (bool, error) {
	if r.coord != nil && r.coord.IsActive(sessionID) {
		return false, ErrSessionBusy
	}
	return r.runner.Compact(ctx, sessionID)
}

// revertSnapshot adapts the runtime snapshot service to session.RevertSnapshot.
func (r *runtime) revertSnapshot() (session.RevertSnapshot, error) {
	if r.snapshot == nil {
		return nil, fmt.Errorf("snapshots are unavailable for this project")
	}
	return core.NewSessionRevertSnapshot(r.snapshot), nil
}

// revertSession returns the stored session a revert operates on.
func (r *runtime) revertSession(sessionID schema.SessionID) (*schema.SessionInfo, error) {
	stored, err := r.store.Get(sessionID)
	if err != nil {
		return nil, err
	}
	if stored == nil {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}
	return stored, nil
}

// revertInputs resolves the snapshot service and stored session a revert needs.
func (r *runtime) revertInputs(sessionID schema.SessionID) (session.RevertSnapshot, schema.SessionInfo, error) {
	snapshot, err := r.revertSnapshot()
	if err != nil {
		return nil, schema.SessionInfo{}, err
	}
	stored, err := r.revertSession(sessionID)
	if err != nil {
		return nil, schema.SessionInfo{}, err
	}
	return snapshot, *stored, nil
}

// revertStage reverts the file changes made after messageID.
func (r *runtime) revertStage(sessionID schema.SessionID, messageID schema.MessageID) (*schema.RevertState, error) {
	snapshot, stored, err := r.revertInputs(sessionID)
	if err != nil {
		return nil, err
	}
	return session.RevertStage(r.db.DB, appEvents{events: r.events}, snapshot, stored, messageID, nil)
}

// revertClear restores the files a staged revert changed.
func (r *runtime) revertClear(sessionID schema.SessionID) error {
	snapshot, stored, err := r.revertInputs(sessionID)
	if err != nil {
		return err
	}
	return session.RevertClear(appEvents{events: r.events}, snapshot, stored)
}

// revertCommit finalizes a staged revert.
func (r *runtime) revertCommit(sessionID schema.SessionID) error {
	stored, err := r.revertSession(sessionID)
	if err != nil {
		return err
	}
	return session.RevertCommit(appEvents{events: r.events}, *stored)
}

func (r *runtime) setHooks(sessionID string, hooks *SessionHooks) {
	r.hooksMu.Lock()
	defer r.hooksMu.Unlock()
	if hooks != nil {
		r.hooks[sessionID] = hooks
	}
}

func (r *runtime) removeHooks(sessionID string) {
	r.hooksMu.Lock()
	defer r.hooksMu.Unlock()
	delete(r.hooks, sessionID)
}

func (r *runtime) hooksFor(sessionID string) *SessionHooks {
	r.hooksMu.RLock()
	defer r.hooksMu.RUnlock()
	return r.hooks[sessionID]
}

// markInbound records a session whose turn is owned by the A2A task store.
func (r *runtime) markInbound(sessionID schema.SessionID) {
	r.inboundMu.Lock()
	r.inbound[sessionID] = struct{}{}
	r.inboundMu.Unlock()
}

// unmarkInbound drops the inbound marker when a session is released.
func (r *runtime) unmarkInbound(sessionID schema.SessionID) {
	r.inboundMu.Lock()
	delete(r.inbound, sessionID)
	r.inboundMu.Unlock()
}

// isInbound reports whether a session is served by the A2A inbound executor.
func (r *runtime) isInbound(sessionID schema.SessionID) bool {
	r.inboundMu.Lock()
	defer r.inboundMu.Unlock()
	_, ok := r.inbound[sessionID]
	return ok
}

// detach drops the per-session runtime state registered by attachSession: the
// tool registry, hooks, permission/question handlers and the derived agent
// override. The event listener and the Client.sessions entry belong to
// Session.Close.
func (r *runtime) detach(sessionID string) {
	r.dispatcher.remove(sessionID)
	r.removeHooks(sessionID)
	r.permission.removeHandler(sessionID)
	r.question.removeHandler(sessionID)
	r.agentSelector.remove(schema.SessionID(sessionID))
}

// inheritSession copies a parent session's hooks and per-session permission and
// question handlers to a child (subagent) session, so they apply to subagent
// tool calls too.
func (r *runtime) inheritSession(parentID, childID schema.SessionID) {
	r.setHooks(string(childID), r.hooksFor(string(parentID)))
	r.permission.setHandler(string(childID), r.permission.handlerFor(string(parentID)))
	r.question.setHandler(string(childID), r.question.handlerFor(string(parentID)))
}

// clearSession drops the per-session state inheritSession installed.
func (r *runtime) clearSession(sessionID schema.SessionID) {
	r.removeHooks(string(sessionID))
	r.permission.removeHandler(string(sessionID))
	r.question.removeHandler(string(sessionID))
}

// resolveRuntimePaths resolves the working directory plus the home and state
// root the runtime derives its global directories from.
func resolveRuntimePaths(opts *ClientOptions) (directory, home, root string, err error) {
	directory, err = resolveWorkingDirectory(opts.WorkingDirectory)
	if err != nil {
		return "", "", "", err
	}
	home, root, err = resolveHomeAndRoot(opts)
	if err != nil {
		return "", "", "", err
	}
	return directory, home, root, nil
}

// initRuntimeState creates the event log, registers its projectors and records
// the project location the session tables reference.
func initRuntimeState(
	db *database.Database,
	directory string,
	onError func(error),
) (core.Event, core.Git, *core.Location, error) {
	events := core.NewEvent(core.EventConfig{
		DB:      db.DB,
		Durable: schema.Durable(schema.SessionEventDurableDefinitions),
		OnError: onError,
	})
	if err := core.RegisterSessionProjectors(events); err != nil {
		return nil, core.Git{}, nil, err
	}
	git := core.NewGit(core.NewAppProcess())
	location, err := ensureProject(db, git, directory)
	if err != nil {
		return nil, core.Git{}, nil, err
	}
	return events, git, location, nil
}

// newRuntime builds the process-wide services: the database and event log, the
// project location, the agent/skill/reference registries, the permission and
// question handlers, and the runner the coordinator drives.
func newRuntime(ctx context.Context, opts *ClientOptions) (*runtime, error) {
	directory, home, root, err := resolveRuntimePaths(opts)
	if err != nil {
		return nil, err
	}
	global := core.NewGlobal(root, home)

	db, err := openDatabase(global, opts.DatabaseMaxConns)
	if err != nil {
		return nil, err
	}
	// fail releases the database when a later step cannot start.
	fail := func(err error) (*runtime, error) {
		db.Close()
		return nil, err
	}
	events, git, location, err := initRuntimeState(db, directory, opts.OnError)
	if err != nil {
		return fail(err)
	}

	agents, err := buildAgents(opts, location, global)
	if err != nil {
		return fail(err)
	}
	store := session.NewStore(db)
	permission := buildPermission(opts, db, events, agents, store, location.Project.ID)
	question := newQuestionHandler(core.NewQuestion(core.QuestionConfig{Events: events}), opts.Question)
	skills, err := buildSkills(opts, global)
	if err != nil {
		return fail(err)
	}
	references, err := buildReferences(opts, global, events, git)
	if err != nil {
		return fail(err)
	}
	models, err := buildModelResolver(opts)
	if err != nil {
		return fail(err)
	}

	// Bound oversized tool results (see core.ToolOutputStore) before they are
	// sent to the model; without this a single large grep/read can overflow the
	// provider context window.
	toolOutput := core.NewToolOutputStore(global, toolOutputSettings(opts))

	locale := util.ResolveLocale(opts.Locale)
	lifetime, cancel := context.WithCancel(context.Background())
	rt := &runtime{
		db: db, events: events, store: store, agents: agents, skills: skills,
		outputStore: tool.NewCoreOutputStore(toolOutput), toolOutput: toolOutput,
		permission: permission, question: question, location: *location,
		projectID: location.Project.ID, models: models,
		subagentDepth: subagentDepth(opts.SubagentDepth),
		name:          applicationName(opts.Name),
		hooks:         map[string]*SessionHooks{}, clientTools: opts.Tools,
		inbound:       map[schema.SessionID]struct{}{},
		dispatcher:    &toolDispatcher{sessions: map[string]*tool.Registry{}},
		agentSelector: newSessionAgents(agents),
		reference:     references,
		cancel:        cancel,
		onPanic:       opts.OnPanic,
		onError:       opts.OnError,
	}
	rt.buildBuiltins(opts, global, locale)
	rt.buildRunner(opts, git, global, locale)
	if err := rt.loadPlugins(opts); err != nil {
		cancel()
		return fail(err)
	}
	// Connect MCP last: it creates the only client whose lifetime rt.close
	// owns, so an earlier startup failure never leaves one connected.
	rt.connectMCP(ctx, opts.MCP)
	go rt.runToolOutputCleanup(lifetime)
	return rt, nil
}

// buildBuiltins assembles the dependency set the built-in tools register with.
func (r *runtime) buildBuiltins(opts *ClientOptions, global core.Global, locale language.Tag) {
	r.builtin = builtinDeps{
		db: r.db, events: r.events, permission: r.permission, question: r.question,
		location: r.location, global: global, shell: opts.Shell, skills: r.skills,
		locale: locale, agents: r.agents, task: &deferredTaskRunner{rt: r}, name: r.name,
	}
}

// buildRunner wires the LLM runner and the coordinator that drains sessions.
func (r *runtime) buildRunner(opts *ClientOptions, git core.Git, global core.Global, locale language.Tag) {
	r.snapshot = newSnapshot(git, global, &r.location)
	r.a2aBackendPool = newA2ABackendPool(r.askBackendQuestion)
	r.a2aBackendPool.persistTask = r.persistRemoteTask
	r.a2aBackendPool.loadTask = r.loadRemoteTask
	r.runner = runner.NewLLMRunner(runner.LLMRunner{
		Events:            appEvents{events: r.events},
		LLM:               chooseLLM(opts),
		Backends:          r.a2aBackendPool.forAgent,
		Agents:            r.agentSelector,
		Tools:             hookedRegistry{inner: r.dispatcher, hooksFor: r.hooksFor},
		Models:            r.models,
		Store:             r.store,
		Location:          r.location,
		SystemContext:     systemcontext.NewRegistry(),
		SkillGuidance:     skill.NewGuidance(r.skills, ruleEvaluator{}, locale),
		ReferenceGuidance: reference.NewGuidance(referenceLister{reference: r.reference}, locale),
		Compaction:        compactionSettings(opts),
		Title:             titleSettings(opts),
		Snapshots:         snapshotterFor(r.snapshot),
		DB:                r.db,
		InboundSessions:   r.isInbound,
		OnError:           r.onError,
	})
	r.coord = session.NewCoordinator(func(ctx context.Context, id schema.SessionID, force bool) error {
		r.publishSessionStatus(id, schema.SessionStatusBusy{Type: "busy"})
		defer r.publishSessionStatus(id, schema.SessionStatusIdle{Type: "idle"})
		return r.runner.Run(ctx, runner.RunInput{SessionID: id, Force: force})
	})
}

// askBackendQuestion asks the local user for input a remote agent requested and
// joins the answers into one relay string. It runs while the calling turn is
// parked, so a held turn blocks until the user replies.
func (r *runtime) askBackendQuestion(
	ctx context.Context,
	sessionID string,
	questions []schema.QuestionInfo,
) (string, error) {
	answers, err := r.question.Ask(ctx, core.QuestionAskInput{SessionID: sessionID, Questions: questions})
	if err != nil {
		return "", err
	}
	parts := make([]string, 0, len(answers))
	for _, answer := range answers {
		parts = append(parts, strings.Join(answer, ", "))
	}
	return strings.Join(parts, "\n"), nil
}

// loadPlugins registers the configured plugins. A plugin that cannot start
// leaves the runtime half-built; the caller releases it.
func (r *runtime) loadPlugins(opts *ClientOptions) error {
	r.plugin = core.NewPlugin(r.events, r.agents, r.skills, r.reference)
	for _, config := range opts.Plugins {
		r.clientTools = append(r.clientTools, config.Tools...)
		if err := r.plugin.Add(schema.PluginID(config.ID), r.adaptPlugin(config)); err != nil {
			return err
		}
	}
	return nil
}

// adaptPlugin bridges a PluginFunc to the core lifecycle manager, extending
// the core context with per-plugin options and the runtime tool registrar.
func (r *runtime) adaptPlugin(config PluginConfig) core.PluginFunc {
	return func(ctx *core.PluginContext) (func(), error) {
		return config.Func(&PluginContext{
			PluginContext: *ctx,
			Options:       config.Options,
			Tools:         r.toolRegistrar(),
			onPanic:       r.onPanic,
		})
	}
}

// resolveWorkingDirectory returns the absolute project directory.
func resolveWorkingDirectory(dir string) (string, error) {
	if dir == "" {
		dir, _ = os.Getwd()
	}
	return filepath.Abs(dir)
}

// resolveHomeAndRoot resolves the home directory and the required state root.
func resolveHomeAndRoot(opts *ClientOptions) (string, string, error) {
	home := opts.Home
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	if opts.Root == "" {
		return "", "", fmt.Errorf("ClientOptions.Root is required: the SDK does not choose a data directory")
	}
	return home, opts.Root, nil
}

// openDatabase opens the session database under the state root.
func openDatabase(global core.Global, maxConns int) (*database.Database, error) {
	dbPath := filepath.Join(global.Data, "agent.db")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, err
	}
	return database.Open(dbPath, maxConns)
}

// ensureProject resolves the project location and records it, so the row the
// session tables reference exists.
func ensureProject(db *database.Database, git core.Git, directory string) (*core.Location, error) {
	projectService := core.NewProject(core.ProjectConfig{
		Git:         git,
		Directories: coreproject.NewDirectories(db),
	})
	location, err := core.NewLocation(schema.LocationRef{Directory: schema.AbsolutePath(directory)}, projectService)
	if err != nil {
		return nil, err
	}
	now := time.Now().UnixMilli()
	if _, err := db.DB.Exec(
		"INSERT INTO `project` (`id`,`worktree`,`sandboxes`,`time_created`,`time_updated`) VALUES (?,?,?,?,?) "+
			"ON CONFLICT(`id`) DO NOTHING",
		string(location.Project.ID), string(location.Project.Directory), "[]", now, now,
	); err != nil {
		return nil, err
	}
	return location, nil
}

// buildAgents registers the built-in agents, then the configured directories.
func buildAgents(opts *ClientOptions, location *core.Location, global core.Global) (*core.Agent, error) {
	agents := core.NewAgent()
	if err := core.ApplyDefaultAgents(agents, location, global); err != nil {
		return nil, err
	}
	// Config agents inherit the default ruleset (a configured agent never
	// inherits a built-in's permissions), so merge it in as the base.
	if err := defsconfig.ApplyDirAgents(
		agents,
		opts.ConfigDirs,
		global,
		core.DefaultAgentPermissions(global),
	); err != nil {
		return nil, err
	}
	return agents, nil
}

// buildPermission wires the permission service to the session store, the agent
// registry and the saved-rules table.
func buildPermission(
	opts *ClientOptions,
	db *database.Database,
	events core.Event,
	agents *core.Agent,
	store *session.Store,
	projectID schema.ProjectID,
) *permissionHandler {
	inner := core.NewPermission(core.PermissionConfig{
		Sessions:  permissionSessionStore{store: store},
		Agents:    permissionAgents{agents: agents},
		Saved:     &permissionSaved{db: db},
		Events:    permissionEvents{events: events},
		ProjectID: string(projectID),
	})
	return newPermissionHandler(inner, opts.Permission)
}

// buildSkills registers the skill directories the client was configured with.
func buildSkills(opts *ClientOptions, global core.Global) (*core.Skill, error) {
	discovery := core.NewSkillDiscovery(filepath.Join(global.Cache, "skills"), http.DefaultClient)
	skills := core.NewSkill(discovery)
	_, err := skills.Transform(func(draft core.SkillDraft) error {
		for _, dir := range defsskill.DiscoverSkills(opts.ConfigDirs).Roots {
			draft.Source(schema.SkillDirectorySource{Type: "directory", Path: schema.AbsolutePath(dir)})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return skills, nil
}

// buildReferences registers the named reference sources the client was
// configured with.
func buildReferences(
	opts *ClientOptions,
	global core.Global,
	events core.Event,
	git core.Git,
) (*core.Reference, error) {
	cache := core.NewRepositoryCache(git, global.Repos)
	references := core.NewReference(global, events, cache)
	if len(opts.References) == 0 {
		return references, nil
	}
	_, err := references.Transform(func(draft core.ReferenceDraft) error {
		for name, source := range opts.References {
			draft.Add(name, source)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return references, nil
}

// subagentDepth is the configured nesting limit, at least one.
func subagentDepth(configured int) int {
	if configured <= 0 {
		return 1
	}
	return configured
}

// newSnapshot builds the git-backed snapshot service, or nil when the project
// has no git backing.
func newSnapshot(git core.Git, global core.Global, location *core.Location) *core.Snapshot {
	snapshot, err := core.NewSnapshot(git, global, location)
	if err != nil {
		return nil
	}
	return snapshot
}

// snapshotterFor adapts a possibly-nil snapshot service to the runner's
// snapshotter.
func snapshotterFor(snapshot *core.Snapshot) runner.Snapshotter {
	if snapshot == nil {
		return noopSnapshots{}
	}
	return snapshot
}

const toolOutputCleanupInterval = time.Hour

// runToolOutputCleanup periodically prunes expired tool-output files until the
// context is cancelled.
func (r *runtime) runToolOutputCleanup(ctx context.Context) {
	if r.toolOutput == nil {
		return
	}
	_ = r.toolOutput.Cleanup()
	ticker := time.NewTicker(toolOutputCleanupInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = r.toolOutput.Cleanup()
		}
	}
}

// applicationName resolves the MCP client identity, defaulting to DefaultName.
func applicationName(name string) string {
	if name == "" {
		return DefaultName
	}
	return name
}

// connectMCP connects the configured MCP servers in parallel and warms their
// tool cache; session registries read the cache through mcpToolSet. Servers
// that fail to connect or list tools simply contribute none.
func (r *runtime) connectMCP(ctx context.Context, configs []MCPConfig) {
	if len(configs) == 0 {
		return
	}
	client := mcp.NewClient(
		string(r.location.Directory),
		r.name,
		installation.InstallationVersion,
		mcp.WithEvents(mcpEventPublisher{events: r.events}),
	)
	var connected sync.WaitGroup
	for _, cfg := range configs {
		connected.Add(1)
		go func(cfg MCPConfig) {
			defer connected.Done()
			client.Connect(ctx, cfg.Name, cfg.Server)
		}(cfg)
	}
	connected.Wait()
	r.mcp = client
	// Refresh the converted tool set whenever a server's list changes.
	r.mcpToolsUnsub, _ = r.events.Listen(func(payload schema.EventPayload) error {
		if payload.Type == schema.McpEventToolsChanged.Type {
			r.refreshMCPTools()
		}
		return nil
	})
	r.refreshMCPTools()
}

// mcpToolListTimeout bounds a cache-miss tool listing during session setup.
const mcpToolListTimeout = 5 * time.Second

// refreshMCPTools rebuilds the converted MCP tool set from the client cache.
func (r *runtime) refreshMCPTools() {
	if r.mcp == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), mcpToolListTimeout)
	defer cancel()
	adapted := r.mcp.Tools(ctx)
	tools := make(map[string]tool.Tool, len(adapted))
	for name, item := range adapted {
		tools[name] = item.CoreTool()
	}
	r.mcpToolsMu.Lock()
	r.mcpTools = tools
	r.mcpToolsMu.Unlock()
}

// mcpToolSet returns the cached MCP tool set. The map is replaced (never
// mutated) on refresh, so callers may read it without holding the lock.
func (r *runtime) mcpToolSet() map[string]tool.Tool {
	r.mcpToolsMu.RLock()
	defer r.mcpToolsMu.RUnlock()
	return r.mcpTools
}

type mcpEventPublisher struct{ events core.Event }

func (p mcpEventPublisher) Publish(def schema.EventDefinition, data any) error {
	_, err := p.events.Publish(def, data, nil)
	return err
}

// chooseLLM returns the caller's LLM client, or one over the default HTTP
// transport.
func chooseLLM(opts *ClientOptions) route.LLMClient {
	if opts.LLM != nil {
		return opts.LLM
	}
	return llm.NewClient(route.TransportRuntime{HTTP: route.NewExecutor(newLLMHTTPClient())})
}

// defaultLLMResponseHeaderTimeout bounds how long a provider may take to send
// response headers, so a hung connection fails instead of blocking a turn
// forever. The streaming body is not affected. Callers needing a different
// policy can inject ClientOptions.LLM.
const defaultLLMResponseHeaderTimeout = 2 * time.Minute

// newLLMHTTPClient builds the default HTTP client for provider requests. It
// clones the default transport so it does not mutate the process-wide one.
func newLLMHTTPClient() *http.Client {
	transport := http.DefaultTransport
	if base, ok := http.DefaultTransport.(*http.Transport); ok {
		clone := base.Clone()
		clone.ResponseHeaderTimeout = defaultLLMResponseHeaderTimeout
		transport = clone
	}
	return &http.Client{Transport: transport}
}

// shutdownGrace bounds how long Close waits for in-flight turns to stop.
const shutdownGrace = 5 * time.Second

// stopTurns interrupts and waits for every active session turn so teardown does
// not close the database under a running turn.
func (r *runtime) stopTurns() {
	if r.coord == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
	defer cancel()
	r.coord.StopAll(ctx)
}

func (r *runtime) close() error {
	if r.cancel != nil {
		r.cancel()
	}
	if r.plugin != nil {
		_ = r.plugin.Close()
	}
	if r.mcpToolsUnsub != nil {
		r.mcpToolsUnsub()
	}
	if r.mcp != nil {
		r.mcp.Close()
	}
	if r.permission != nil {
		_ = r.permission.Close()
	}
	if r.question != nil {
		_ = r.question.Close()
	}
	if r.reference != nil {
		ctx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
		r.reference.WaitContext(ctx)
		cancel()
	}
	if r.a2aBackendPool != nil {
		r.a2aBackendPool.close()
	}
	return r.db.Close()
}

// appEvents adapts core.Event to the session publisher interfaces.
type appEvents struct{ events core.Event }

func (e appEvents) Publish(def schema.EventDefinition, data any) (*schema.EventPayloadDurable, error) {
	payload, err := e.events.Publish(def, data, nil)
	if err != nil {
		return nil, err
	}
	return payload.Durable, nil
}

func (e appEvents) PublishWithCommit(
	def schema.EventDefinition,
	data any,
	commit func(tx session.DB, seq int) error,
) (*schema.EventPayloadDurable, error) {
	payload, err := e.events.Publish(def, data, &core.EventPublishOptions{
		Commit: func(tx core.EventDB, seq int) error { return commit(tx, seq) },
	})
	if err != nil {
		return nil, err
	}
	return payload.Durable, nil
}

// toolOutputSettings maps ClientOptions.ToolOutput to the core settings.
func toolOutputSettings(opts *ClientOptions) core.ToolOutputSettings {
	if opts.ToolOutput == nil {
		return core.ToolOutputSettings{}
	}
	return core.ToolOutputSettings{MaxLines: opts.ToolOutput.MaxLines, MaxBytes: opts.ToolOutput.MaxBytes}
}

// compactionSettings maps ClientOptions.Compaction to the session settings,
// applying the built-in defaults for a nil config.
func compactionSettings(opts *ClientOptions) session.CompactionSettings {
	settings := session.DefaultCompactionSettings()
	if opts.Compaction == nil {
		return settings
	}
	if opts.Compaction.Auto != nil {
		settings.Auto = *opts.Compaction.Auto
	}
	if opts.Compaction.Reserved > 0 {
		settings.Buffer = opts.Compaction.Reserved
	}
	if opts.Compaction.PreserveRecentTokens > 0 {
		settings.Tokens = opts.Compaction.PreserveRecentTokens
	}
	return settings
}

// titleSettings maps ClientOptions.Title to the title settings, applying the
// built-in default (disabled) for a nil config.
func titleSettings(opts *ClientOptions) session.TitleSettings {
	settings := session.DefaultTitleSettings()
	if opts.Title == nil {
		return settings
	}
	if opts.Title.Auto != nil {
		settings.Auto = *opts.Title.Auto
	}
	settings.Model = opts.Title.Model
	return settings
}

// buildModelResolver maps the client's protocol (or ModelInfo) and credentials
// to a model resolver.
func buildModelResolver(opts *ClientOptions) (runner.ModelResolver, error) {
	if opts.ModelInfo == nil {
		if opts.Protocol == "" {
			return nil, fmt.Errorf("ClientOptions requires ModelInfo or Protocol")
		}
		// Fail fast on an unknown protocol instead of at the first request.
		if _, err := NewModelInfo(opts.Protocol, opts.Model, "", schema.ModelLimit{}); err != nil {
			return nil, err
		}
	}
	return func(stored schema.SessionInfo) (route.Model, error) {
		return resolveStoredModel(opts, stored)
	}, nil
}

// resolveStoredModel resolves a stored session's model metadata and credential.
func resolveStoredModel(opts *ClientOptions, stored schema.SessionInfo) (route.Model, error) {
	id := opts.Model
	if stored.Model != nil && stored.Model.ID != "" {
		id = string(stored.Model.ID)
	}
	info, err := resolveModelInfo(opts, id)
	if err != nil {
		return route.Model{}, err
	}
	var credential schema.Credential
	if opts.Credentials != nil {
		credential, err = opts.Credentials(info.ProviderID)
		if err != nil {
			return route.Model{}, err
		}
	}
	return runner.ResolveModel(stored, info, credential)
}

// resolveModelInfo returns the model metadata: the caller's ModelInfo when set,
// otherwise a ModelInfo synthesized from Protocol. ContextLimit and OutputLimit
// override the resolved limits so callers can supply them without a full
// ModelInfoProvider.
func resolveModelInfo(opts *ClientOptions, id string) (schema.ModelInfo, error) {
	var info schema.ModelInfo
	var err error
	if opts.ModelInfo != nil {
		info, err = opts.ModelInfo(id)
	} else {
		info, err = NewModelInfo(opts.Protocol, id, "", schema.ModelLimit{})
	}
	if err != nil {
		return info, err
	}
	if opts.ContextLimit > 0 {
		info.Limit.Context = opts.ContextLimit
	}
	if opts.OutputLimit > 0 {
		info.Limit.Output = opts.OutputLimit
	}
	return info, nil
}
