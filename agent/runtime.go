package agent

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/text/language"

	"github.com/user/go-agent-sdk/core"
	"github.com/user/go-agent-sdk/core/database"
	"github.com/user/go-agent-sdk/core/installation"
	coreproject "github.com/user/go-agent-sdk/core/project"
	"github.com/user/go-agent-sdk/core/reference"
	"github.com/user/go-agent-sdk/core/session"
	"github.com/user/go-agent-sdk/core/session/runner"
	"github.com/user/go-agent-sdk/core/skill"
	"github.com/user/go-agent-sdk/core/systemcontext"
	"github.com/user/go-agent-sdk/core/tool"
	"github.com/user/go-agent-sdk/core/util"
	defsconfig "github.com/user/go-agent-sdk/defs/config"
	defsskill "github.com/user/go-agent-sdk/defs/skill"
	"github.com/user/go-agent-sdk/llm"
	"github.com/user/go-agent-sdk/llm/route"
	"github.com/user/go-agent-sdk/mcp"
	"github.com/user/go-agent-sdk/schema"
)

// runtime holds the process-wide services shared by all sessions.
type runtime struct {
	db            *database.Database
	events        core.Event
	store         *session.Store
	agents        *core.Agent
	skills        *core.Skill
	outputStore   tool.OutputStore
	toolOutput    *core.ToolOutputStore
	mcpTools      map[string]tool.Tool
	permission    *permissionHandler
	question      *questionHandler
	runner        *runner.LLMRunner
	coord         *session.Coordinator[schema.SessionID]
	location      core.Location
	projectID     schema.ProjectID
	models        runner.ModelResolver
	subagentDepth int
	name          string

	hooksMu sync.Mutex
	hooks   map[string]*SessionHooks

	builtin     builtinDeps
	clientTools []Tool
	dispatcher  *toolDispatcher
	mcp         *mcp.Client
	plugin      *core.Plugin
	reference   *core.Reference
	cancel      context.CancelFunc
}

// toolDispatcher routes materialization to the session-scoped registry. Every
// session has one (set on create/resume and for subagent children), so there is
// no session-agnostic fallback: materializing an unknown session yields no tools
// rather than a registry built with the wrong permission rules.
type toolDispatcher struct {
	mu       sync.Mutex
	sessions map[string]*tool.Registry
}

func (d *toolDispatcher) MaterializeFor(sessionID string, permissions []core.PermissionRule) *tool.Materialization {
	d.mu.Lock()
	registry := d.sessions[sessionID]
	d.mu.Unlock()
	if registry == nil {
		return &tool.Materialization{}
	}
	return registry.Materialize(permissions)
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

// newSessionRegistry builds a registry with the built-ins plus the client and
// session user tools, isolating session-scoped tools from other sessions.
func (r *runtime) newSessionRegistry(agentID string, sessionRules schema.PermissionRuleset, sessionTools []Tool) (*tool.Registry, error) {
	registry := tool.NewRegistry(tool.WithOutputStore(r.outputStore))
	var permissions schema.PermissionRuleset
	if agent := r.agents.Get(schema.AgentID(agentID)); agent != nil {
		permissions = agent.Permissions
	}
	if len(sessionRules) > 0 {
		permissions = core.PermissionMerge(permissions, sessionRules)
	}
	if err := registerBuiltins(registry, r.builtin, permissions); err != nil {
		return nil, err
	}
	if len(r.mcpTools) > 0 {
		if _, err := registry.Register(r.mcpTools); err != nil {
			return nil, err
		}
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
func (r *runtime) admit(sessionID schema.SessionID, prompt string, delivery schema.SessionDelivery) error {
	_, err := session.Admit(r.db.DB, appEvents{events: r.events}, session.AdmitInput{
		ID:        schema.NewMessageID(),
		SessionID: sessionID,
		Prompt:    schema.Prompt{Text: prompt},
		Delivery:  delivery,
	})
	return err
}

func (r *runtime) setHooks(sessionID string, hooks *SessionHooks) {
	r.hooksMu.Lock()
	defer r.hooksMu.Unlock()
	if hooks != nil {
		r.hooks[sessionID] = hooks
	}
}

func (r *runtime) hooksFor(sessionID string) *SessionHooks {
	r.hooksMu.Lock()
	defer r.hooksMu.Unlock()
	return r.hooks[sessionID]
}

// newRuntime builds the process-wide services: the database and event log, the
// project location, the agent/skill/reference registries, the permission and
// question handlers, and the runner the coordinator drives.
func newRuntime(ctx context.Context, opts *ClientOptions) (*runtime, error) {
	directory, err := resolveWorkingDirectory(opts.WorkingDirectory)
	if err != nil {
		return nil, err
	}
	home, root, err := resolveHomeAndRoot(opts)
	if err != nil {
		return nil, err
	}
	global := core.NewGlobal(root, home)

	db, err := openDatabase(global)
	if err != nil {
		return nil, err
	}
	// fail releases the database when a later step cannot start.
	fail := func(err error) (*runtime, error) {
		db.Close()
		return nil, err
	}

	events := core.NewEvent(core.EventConfig{
		DB:      db.DB,
		Durable: schema.Durable(schema.SessionEventDurableDefinitions),
	})
	if err := core.RegisterSessionProjectors(events); err != nil {
		return fail(err)
	}

	git := core.NewGit(core.NewAppProcess())
	location, err := ensureProject(db, git, directory)
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
		dispatcher: &toolDispatcher{sessions: map[string]*tool.Registry{}},
		reference:  references,
		cancel:     cancel,
	}
	rt.buildBuiltins(opts, global, locale)
	rt.buildRunner(opts, git, global, locale)
	rt.connectMCP(ctx, opts.MCP)
	if err := rt.loadPlugins(opts); err != nil {
		cancel()
		return fail(err)
	}
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
	r.runner = runner.NewLLMRunner(runner.LLMRunner{
		Events:            appEvents{events: r.events},
		LLM:               chooseLLM(opts),
		Agents:            r.agents,
		Tools:             hookedRegistry{inner: r.dispatcher, hooksFor: r.hooksFor},
		Models:            r.models,
		Store:             r.store,
		Location:          r.location,
		SystemContext:     systemcontext.NewRegistry(),
		SkillGuidance:     skill.NewGuidance(r.skills, ruleEvaluator{}, locale),
		ReferenceGuidance: reference.NewGuidance(referenceLister{reference: r.reference}, locale),
		Compaction:        compactionSettings(opts),
		Snapshots:         buildSnapshots(git, global, &r.location),
		DB:                r.db,
	})
	r.coord = session.NewCoordinator(func(ctx context.Context, id schema.SessionID, force bool) error {
		r.publishSessionStatus(id, schema.SessionStatusBusy{Type: "busy"})
		defer r.publishSessionStatus(id, schema.SessionStatusIdle{Type: "idle"})
		return r.runner.Run(ctx, runner.RunInput{SessionID: id, Force: force})
	})
}

// loadPlugins registers the configured plugins. A plugin that cannot start
// leaves the runtime half-built; the caller releases it.
func (r *runtime) loadPlugins(opts *ClientOptions) error {
	r.plugin = core.NewPlugin(r.events, r.agents, r.skills, r.reference)
	for _, config := range opts.Plugins {
		if err := r.plugin.Add(schema.PluginID(config.ID), core.PluginEffect(config.Effect)); err != nil {
			return err
		}
	}
	return nil
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
func openDatabase(global core.Global) (*database.Database, error) {
	dbPath := filepath.Join(global.Data, "agent.db")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, err
	}
	return database.Open(dbPath)
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
	if err := defsconfig.ApplyDirAgents(agents, opts.ConfigDirs, global); err != nil {
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
func buildReferences(opts *ClientOptions, global core.Global, events core.Event, git core.Git) (*core.Reference, error) {
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

// buildSnapshots uses the git-backed snapshotter when the project supports it,
// and a no-op one otherwise.
func buildSnapshots(git core.Git, global core.Global, location *core.Location) runner.Snapshotter {
	snapshot, err := core.NewSnapshot(git, global, location)
	if err != nil {
		return noopSnapshots{}
	}
	return snapshot
}

const toolOutputCleanupInterval = time.Hour

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

// connectMCP connects the configured MCP servers and collects their tools; each
// session registry picks them up from r.mcpTools. Servers that fail to connect
// or list tools simply contribute none.
func (r *runtime) connectMCP(ctx context.Context, configs []MCPConfig) {
	if len(configs) == 0 {
		return
	}
	client := mcp.NewClient(string(r.location.Directory), r.name, installation.InstallationVersion, mcp.WithEvents(mcpEventPublisher{events: r.events}))
	for _, cfg := range configs {
		client.Connect(ctx, cfg.Name, cfg.Server)
	}
	if r.mcpTools == nil {
		r.mcpTools = map[string]tool.Tool{}
	}
	for name, adapted := range client.Tools(ctx) {
		r.mcpTools[name] = adapted.CoreTool()
	}
	r.mcp = client
}

type mcpEventPublisher struct{ events core.Event }

func (p mcpEventPublisher) Publish(def schema.EventDefinition, data any) error {
	_, err := p.events.Publish(def, data, nil)
	return err
}

func chooseLLM(opts *ClientOptions) route.LLMClient {
	if opts.LLM != nil {
		return opts.LLM
	}
	return llm.NewClient(route.TransportRuntime{HTTP: route.NewExecutor(http.DefaultClient)})
}

func (r *runtime) close() error {
	if r.cancel != nil {
		r.cancel()
	}
	if r.plugin != nil {
		_ = r.plugin.Close()
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
	}, nil
}

// resolveModelInfo returns the model metadata: the caller's ModelInfo when set,
// otherwise a ModelInfo synthesized from Protocol.
func resolveModelInfo(opts *ClientOptions, id string) (schema.ModelInfo, error) {
	if opts.ModelInfo != nil {
		return opts.ModelInfo(id)
	}
	return NewModelInfo(opts.Protocol, id, "", schema.ModelLimit{})
}

// --- task tool (subagent delegation) ---

// deferredTaskRunner defers to the runtime once it is constructed;
// registerBuiltins runs before the runtime value exists.
type deferredTaskRunner struct{ rt *runtime }

func (t *deferredTaskRunner) RunTask(ctx context.Context, input TaskRunInput) (TaskRunOutput, error) {
	return t.rt.runTask(ctx, input)
}

func (r *runtime) runTask(ctx context.Context, input TaskRunInput) (TaskRunOutput, error) {
	parent, err := r.store.Get(schema.SessionID(input.ParentSessionID))
	if err != nil {
		return TaskRunOutput{}, err
	}
	if parent == nil {
		return TaskRunOutput{}, fmt.Errorf("parent session not found: %s", input.ParentSessionID)
	}
	subagent, derived, err := r.resolveSubagent(parent, input)
	if err != nil {
		return TaskRunOutput{}, err
	}

	childID, reused, err := r.resolveChildSession(input, parent, subagent, derived)
	if err != nil {
		return TaskRunOutput{}, err
	}
	childAgent, childRules := r.childRunSettings(childID, reused, input.SubagentType, derived)
	registry, err := r.newSessionRegistry(childAgent, childRules, nil)
	if err != nil {
		return TaskRunOutput{}, err
	}
	r.dispatcher.set(string(childID), registry)
	defer r.dispatcher.remove(string(childID))

	r.publishTaskProgress(input, childID, subagent, parent)
	if err := r.runChildSession(ctx, childID, input.Prompt); err != nil {
		return TaskRunOutput{}, err
	}
	text, err := r.lastAssistantText(childID)
	if err != nil {
		return TaskRunOutput{}, err
	}
	return TaskRunOutput{SessionID: string(childID), Text: text}, nil
}

// resolveSubagent checks the nesting limit and returns the subagent to run,
// together with the permission ruleset its session starts from.
func (r *runtime) resolveSubagent(parent *schema.SessionInfo, input TaskRunInput) (*schema.AgentInfo, schema.PermissionRuleset, error) {
	if err := r.checkSubagentDepth(parent); err != nil {
		return nil, nil, err
	}
	subagent := r.agents.Get(schema.AgentID(input.SubagentType))
	if subagent == nil {
		return nil, nil, fmt.Errorf("Unknown agent type: %s is not a valid agent type", input.SubagentType)
	}
	return subagent, DeriveSubagentSessionPermission(permissionRulesOf(parent), *subagent), nil
}

// childRunSettings is the agent and permission ruleset a child session runs with.
// A resumed session keeps the agent and ruleset it was created with.
func (r *runtime) childRunSettings(childID schema.SessionID, reused bool, subagentType string, derived schema.PermissionRuleset) (string, schema.PermissionRuleset) {
	if !reused {
		return subagentType, derived
	}
	existing, err := r.store.Get(childID)
	if err != nil || existing == nil {
		return subagentType, derived
	}
	agentID, rules := subagentType, derived
	if existing.Agent != nil {
		agentID = string(*existing.Agent)
	}
	if existing.Permission != nil {
		rules = *existing.Permission
	}
	return agentID, rules
}

// publishTaskProgress reports the running task state on the parent session,
// mirroring the tool's progress metadata. The child session id links the
// subagent's events to this task.
func (r *runtime) publishTaskProgress(input TaskRunInput, childID schema.SessionID, subagent *schema.AgentInfo, parent *schema.SessionInfo) {
	if input.Progress == nil {
		return
	}
	_ = input.Progress(map[string]any{
		"sessionId":       string(childID),
		"parentSessionId": input.ParentSessionID,
		"subagentType":    input.SubagentType,
		"description":     input.Description,
		"prompt":          input.Prompt,
		"model":           childModelJSON(subagent, parent),
	}, nil)
}

// runChildSession admits the prompt and drains the child session's turn.
func (r *runtime) runChildSession(ctx context.Context, childID schema.SessionID, prompt string) error {
	if err := r.admit(childID, prompt, schema.SessionDeliveryQueue); err != nil {
		return err
	}
	runErr := r.coord.Run(ctx, childID)
	if ctx.Err() != nil {
		r.coord.Interrupt(childID)
	}
	return runErr
}

// checkSubagentDepth counts the parent chain; nesting beyond the limit fails.
func (r *runtime) checkSubagentDepth(parent *schema.SessionInfo) error {
	depth := 0
	current := parent
	for current.ParentID != nil {
		depth++
		next, err := r.store.Get(*current.ParentID)
		if err != nil {
			return err
		}
		if next == nil {
			break
		}
		current = next
	}
	if depth >= r.subagentDepth {
		return fmt.Errorf("Subagent depth limit reached (%d). Increase subagent depth to allow nested subagents", r.subagentDepth)
	}
	return nil
}

// resolveChildSession reuses the session named by task_id, or inserts a new
// child session carrying parent_id and the derived permission ruleset.
func (r *runtime) resolveChildSession(
	input TaskRunInput,
	parent *schema.SessionInfo,
	subagent *schema.AgentInfo,
	derived schema.PermissionRuleset,
) (schema.SessionID, bool, error) {
	if input.TaskID != nil && *input.TaskID != "" {
		existing, err := r.store.Get(schema.SessionID(*input.TaskID))
		if err == nil && existing != nil {
			return existing.ID, true, nil
		}
	}
	childID := schema.NewSessionID()
	now := time.Now().UnixMilli()
	title := input.Description + " (@" + input.SubagentType + " subagent)"
	permissionJSON, err := encodePermissionRules(derived)
	if err != nil {
		return "", false, err
	}
	if _, err := r.db.DB.Exec(
		"INSERT INTO `session` (`id`,`project_id`,`parent_id`,`slug`,`directory`,`title`,`version`,`agent`,`model`,`permission`,`time_created`,`time_updated`) "+
			"VALUES (?,?,?,?,?,?,?,?,?,?,?,?)",
		string(childID), string(r.projectID), input.ParentSessionID, "session", string(r.location.Directory),
		title, "1", input.SubagentType, childModelJSON(subagent, parent), permissionJSON, now, now,
	); err != nil {
		return "", false, err
	}
	return childID, false, nil
}

func childModelJSON(subagent *schema.AgentInfo, parent *schema.SessionInfo) string {
	ref := subagent.Model
	if ref == nil {
		ref = parent.Model
	}
	if ref == nil {
		return "null"
	}
	return encodeModelRef(ref.ID, ref.ProviderID)
}

// lastAssistantText returns the last text part of the last assistant message.
func (r *runtime) lastAssistantText(sessionID schema.SessionID) (string, error) {
	messages, err := r.store.Context(sessionID)
	if err != nil {
		return "", err
	}
	for i := len(messages) - 1; i >= 0; i-- {
		assistant, ok := messages[i].(schema.SessionMessageAssistant)
		if !ok {
			continue
		}
		for j := len(assistant.Content) - 1; j >= 0; j-- {
			if text, ok := assistant.Content[j].(schema.SessionMessageAssistantText); ok {
				return text.Text, nil
			}
		}
	}
	return "", nil
}

// --- narrow no-op adapters ---

type noopSnapshots struct{}

func (noopSnapshots) Capture() (*core.SnapshotID, error) { return nil, nil }
func (noopSnapshots) Files(core.SnapshotCompareInput) ([]schema.RelativePath, error) {
	return nil, nil
}

// ruleEvaluator adapts core.PermissionEvaluate to skill's PermissionEvaluator.
type ruleEvaluator struct{}

func (ruleEvaluator) Evaluate(action, resource string, rules schema.PermissionRuleset) schema.PermissionRule {
	return core.PermissionEvaluate(action, resource, rules)
}

// referenceLister adapts *core.Reference to reference.Guidance's Lister.
type referenceLister struct{ reference *core.Reference }

func (l referenceLister) List() ([]reference.Info, error) {
	infos := l.reference.List()
	result := make([]reference.Info, 0, len(infos))
	for _, info := range infos {
		result = append(result, reference.Info{Name: info.Name, Path: string(info.Path), Description: info.Description})
	}
	return result, nil
}
