package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

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
	"github.com/user/go-agent-sdk/llm"
	"github.com/user/go-agent-sdk/llm/route"
	"github.com/user/go-agent-sdk/mcp"
	opencodeconfig "github.com/user/go-agent-sdk/opencode/config"
	opencodeskill "github.com/user/go-agent-sdk/opencode/skill"
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
	permission    *handlerPermission
	question      *handlerQuestion
	runner        *runner.LLMRunner
	coord         *session.Coordinator[schema.SessionID]
	location      core.Location
	projectID     schema.ProjectID
	models        runner.ModelResolver
	subagentDepth int

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
		if _, err := registry.Register(map[string]tool.Tool{userTool.Name: userTool.build(r.permission)}); err != nil {
			return nil, err
		}
	}
	return registry, nil
}

// publishSessionStatus emits the live session.status event so subscribers (and
// the agent event stream) can tell when a session is busy or idle. Mirrors
// SessionStatus.set; the SDK publishes it from its own drain lifecycle.
func (r *runtime) publishSessionStatus(id schema.SessionID, status schema.SessionStatusInfo) {
	_, _ = r.events.Publish(schema.SessionStatusEventStatus, map[string]any{
		"sessionID": id,
		"status":    status,
	}, nil)
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

func newRuntime(ctx context.Context, opts *ClientOptions) (*runtime, error) {
	workingDir := opts.WorkingDirectory
	if workingDir == "" {
		workingDir, _ = os.Getwd()
	}
	directory, err := filepath.Abs(workingDir)
	if err != nil {
		return nil, err
	}

	dbPath := opts.DataDir
	if dbPath == "" {
		dbPath = database.Path()
	} else {
		if err := os.MkdirAll(dbPath, 0o755); err != nil {
			return nil, err
		}
		dbPath = filepath.Join(dbPath, "agent.db")
	}
	db, err := database.Open(dbPath)
	if err != nil {
		return nil, err
	}

	events := core.NewEvent(core.EventConfig{
		DB:      db.DB,
		Durable: schema.Durable(schema.SessionEventDurableDefinitions),
	})
	if err := core.RegisterSessionProjectors(events); err != nil {
		db.Close()
		return nil, err
	}

	var globalOverrides []func(*core.Global)
	if opts.DataRoot != "" {
		globalOverrides = append(globalOverrides, core.WithDataRoot(opts.DataRoot))
	}
	global := core.NewGlobal(globalOverrides...)

	git := core.NewGit(core.NewAppProcess())
	projectService := core.NewProject(core.ProjectConfig{
		Git:         git,
		Directories: coreproject.NewDirectories(db),
	})
	location, err := core.NewLocation(schema.LocationRef{Directory: schema.AbsolutePath(directory)}, projectService)
	if err != nil {
		db.Close()
		return nil, err
	}
	projectID := location.Project.ID
	now := time.Now().UnixMilli()
	if _, err := db.DB.Exec(
		"INSERT INTO `project` (`id`,`worktree`,`sandboxes`,`time_created`,`time_updated`) VALUES (?,?,?,?,?) "+
			"ON CONFLICT(`id`) DO NOTHING",
		string(projectID), string(location.Project.Directory), "[]", now, now,
	); err != nil {
		db.Close()
		return nil, err
	}

	policy := core.NewPolicy()
	policy.Load(opts.Policies)

	// Bound oversized tool results (see core.ToolOutputStore) before they are
	// sent to the model; without this a single large grep/read can overflow the
	// provider context window.
	toolOutput := core.NewToolOutputStore(global, toolOutputSettings(opts))
	outputStore := tool.NewCoreOutputStore(toolOutput)

	subagentDepth := opts.SubagentDepth
	if subagentDepth <= 0 {
		subagentDepth = 1
	}

	agents := core.NewAgent()
	if err := core.ApplyDefaultAgents(agents, location, global); err != nil {
		db.Close()
		return nil, err
	}
	if err := opencodeconfig.ApplyDirAgents(agents, opts.ConfigDirs, global); err != nil {
		db.Close()
		return nil, err
	}

	store := session.NewStore(db)
	agentSessions := permissionSessionStore{store: store}
	permission := &handlerPermission{
		inner: core.NewPermission(core.PermissionConfig{
			Sessions:  agentSessions,
			Agents:    permissionAgents{agents: agents},
			Saved:     &permissionSaved{db: db},
			Events:    permissionEvents{events: events},
			ProjectID: string(projectID),
		}),
		def:      opts.Permission,
		handlers: map[string]PermissionHandlerFunc{},
	}
	question := newHandlerQuestion(core.NewQuestion(core.QuestionConfig{Events: events}), opts.Question)

	discovery := core.NewSkillDiscovery(filepath.Join(global.Cache, "skills"), http.DefaultClient)
	skills := core.NewSkill(discovery)
	if _, err := skills.Transform(func(draft core.SkillDraft) error {
		discovered := opencodeskill.DiscoverSkills(opts.ConfigDirs)
		for _, dir := range discovered.Roots {
			draft.Source(schema.SkillDirectorySource{Type: "directory", Path: schema.AbsolutePath(dir)})
		}
		return nil
	}); err != nil {
		db.Close()
		return nil, err
	}

	referenceCache := core.NewRepositoryCache(git, global.Repos)
	referenceService := core.NewReference(global, events, referenceCache)
	if len(opts.References) > 0 {
		if _, err := referenceService.Transform(func(draft core.ReferenceDraft) error {
			for name, source := range opts.References {
				draft.Add(name, source)
			}
			return nil
		}); err != nil {
			db.Close()
			return nil, err
		}
	}

	models, err := buildModelResolver(opts)
	if err != nil {
		db.Close()
		return nil, err
	}

	task := &taskRunner{}
	builtin := builtinDeps{
		db: db, events: events, permission: permission, question: question,
		location: *location, shell: opts.Shell, skills: skills,
		agents: agents, task: task,
	}
	snapshots := runner.Snapshotter(noopSnapshots{})
	if snapshot, err := core.NewSnapshot(git, global, location); err == nil {
		snapshots = snapshot
	}

	lifetime, cancel := context.WithCancel(context.Background())
	rt := &runtime{
		db: db, events: events, store: store, agents: agents, skills: skills,
		outputStore: outputStore, toolOutput: toolOutput,
		permission: permission, question: question, location: *location,
		projectID: projectID, models: models,
		subagentDepth: subagentDepth,
		hooks:         map[string]*SessionHooks{}, builtin: builtin, clientTools: opts.Tools,
		dispatcher: &toolDispatcher{sessions: map[string]*tool.Registry{}},
		reference:  referenceService,
		cancel:     cancel,
	}
	task.rt = rt
	rt.runner = runner.NewLLMRunner(runner.LLMRunner{
		Events:            appEvents{events: events},
		LLM:               chooseLLM(opts),
		Agents:            agents,
		Tools:             hookedRegistry{inner: rt.dispatcher, hooksFor: rt.hooksFor},
		Models:            models,
		Store:             store,
		Location:          *location,
		SystemContext:     systemcontext.NewRegistry(),
		SkillGuidance:     skill.NewGuidance(skills, allowAllEvaluator{}),
		ReferenceGuidance: reference.NewGuidance(referenceLister{reference: referenceService}),
		Compaction:        compactionSettings(opts),
		Snapshots:         snapshots,
		DB:                db,
	})
	rt.coord = session.NewCoordinator(func(ctx context.Context, id schema.SessionID, force bool) error {
		rt.publishSessionStatus(id, schema.SessionStatusBusy{Type: "busy"})
		defer rt.publishSessionStatus(id, schema.SessionStatusIdle{Type: "idle"})
		return rt.runner.Run(ctx, runner.RunInput{SessionID: id, Force: force})
	})
	if err := rt.connectMCP(ctx, opts.MCP); err != nil {
		cancel()
		db.Close()
		return nil, err
	}
	rt.plugin = core.NewPlugin(events, agents, skills)
	for _, config := range opts.Plugins {
		if err := rt.plugin.Add(schema.PluginID(config.ID), core.PluginEffect(config.Effect)); err != nil {
			cancel()
			db.Close()
			return nil, err
		}
	}
	go rt.runToolOutputCleanup(lifetime)
	return rt, nil
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

// connectMCP connects the configured MCP servers and collects their tools; each
// session registry picks them up from r.mcpTools.
func (r *runtime) connectMCP(ctx context.Context, configs []MCPConfig) error {
	if len(configs) == 0 {
		return nil
	}
	client := mcp.NewClient(string(r.location.Directory), installation.InstallationVersion, mcp.WithEvents(mcpEventPublisher{events: r.events}))
	for _, cfg := range configs {
		client.Connect(ctx, cfg.Name, cfg.Server)
	}
	tools, err := client.Tools(ctx)
	if err != nil {
		client.Close()
		return err
	}
	if r.mcpTools == nil {
		r.mcpTools = map[string]tool.Tool{}
	}
	for name, adapted := range tools {
		r.mcpTools[name] = adapted.CoreTool()
	}
	r.mcp = client
	return nil
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

// taskRunner defers to the runtime once it is constructed; registerBuiltins
// runs before the runtime value exists.
type taskRunner struct{ rt *runtime }

func (t *taskRunner) RunTask(ctx context.Context, input TaskRunInput) (TaskRunOutput, error) {
	return t.rt.runTask(ctx, input)
}

func (r *runtime) runTask(ctx context.Context, input TaskRunInput) (out TaskRunOutput, err error) {
	parent, err := r.store.Get(schema.SessionID(input.ParentSessionID))
	if err != nil {
		return TaskRunOutput{}, err
	}
	if parent == nil {
		return TaskRunOutput{}, fmt.Errorf("parent session not found: %s", input.ParentSessionID)
	}
	if err := r.checkSubagentDepth(parent); err != nil {
		return TaskRunOutput{}, err
	}
	subagent := r.agents.Get(schema.AgentID(input.SubagentType))
	if subagent == nil {
		return TaskRunOutput{}, fmt.Errorf("Unknown agent type: %s is not a valid agent type", input.SubagentType)
	}
	var parentPermission schema.PermissionRuleset
	if parent.Permission != nil {
		parentPermission = *parent.Permission
	}
	derived := DeriveSubagentSessionPermission(parentPermission, *subagent)

	childID, reused, err := r.resolveChildSession(input, parent, subagent, derived)
	if err != nil {
		return TaskRunOutput{}, err
	}
	// A resumed session keeps its stored agent and permission ruleset.
	childAgent := input.SubagentType
	childRules := derived
	if reused {
		if existing, getErr := r.store.Get(childID); getErr == nil && existing != nil {
			if existing.Agent != nil {
				childAgent = string(*existing.Agent)
			}
			if existing.Permission != nil {
				childRules = *existing.Permission
			}
		}
	}
	registry, err := r.newSessionRegistry(childAgent, childRules, nil)
	if err != nil {
		return TaskRunOutput{}, err
	}
	r.dispatcher.set(string(childID), registry)
	defer r.dispatcher.remove(string(childID))

	// Publish the running task state (durable) on the parent session, mirroring
	// the tool's progress metadata. The child session id links the subagent's
	// events to this task.
	if input.Progress != nil {
		_ = input.Progress(map[string]any{
			"sessionId":       string(childID),
			"parentSessionId": input.ParentSessionID,
			"subagentType":    input.SubagentType,
			"description":     input.Description,
			"prompt":          input.Prompt,
			"model":           childModelJSON(subagent, parent),
		}, nil)
	}

	if _, err := session.Admit(r.db.DB, appEvents{events: r.events}, session.AdmitInput{
		ID:        schema.NewMessageID(),
		SessionID: childID,
		Prompt:    schema.Prompt{Text: input.Prompt},
		Delivery:  schema.SessionDeliveryQueue,
	}); err != nil {
		return TaskRunOutput{}, err
	}
	runErr := r.coord.Run(ctx, childID)
	if ctx.Err() != nil {
		r.coord.Interrupt(childID)
	}
	if runErr != nil {
		return TaskRunOutput{}, runErr
	}
	text, err := r.lastAssistantText(childID)
	if err != nil {
		return TaskRunOutput{}, err
	}
	return TaskRunOutput{SessionID: string(childID), Text: text}, nil
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
	permissionJSON := "null"
	if len(derived) > 0 {
		if encoded, err := json.Marshal(derived); err == nil {
			permissionJSON = string(encoded)
		}
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
	if ref == nil || ref.ID == "" {
		return "null"
	}
	return fmt.Sprintf(`{"id":%q,"providerID":%q}`, string(ref.ID), string(ref.ProviderID))
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

type allowAllEvaluator struct{}

func (allowAllEvaluator) Evaluate(action, resource string, rules schema.PermissionRuleset) schema.PermissionRule {
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
