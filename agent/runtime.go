package agent

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/user/go-agent-sdk/core"
	"github.com/user/go-agent-sdk/core/database"
	"github.com/user/go-agent-sdk/core/reference"
	"github.com/user/go-agent-sdk/core/session"
	"github.com/user/go-agent-sdk/core/session/runner"
	"github.com/user/go-agent-sdk/core/skill"
	"github.com/user/go-agent-sdk/core/systemcontext"
	"github.com/user/go-agent-sdk/core/tool"
	"github.com/user/go-agent-sdk/llm"
	"github.com/user/go-agent-sdk/llm/protocols"
	"github.com/user/go-agent-sdk/llm/provider"
	"github.com/user/go-agent-sdk/llm/route"
	llmschema "github.com/user/go-agent-sdk/llm/schema"
	"github.com/user/go-agent-sdk/schema"
)

// runtime holds the process-wide services shared by all sessions.
type runtime struct {
	db         *database.Database
	events     core.Event
	store      *session.Store
	agents     *core.Agent
	tools      *tool.Registry
	permission core.Permission
	question   core.Question
	runner     *runner.LLMRunner
	coord      *session.Coordinator[schema.SessionID]
	location   core.Location
	projectID  schema.ProjectID
	models     runner.ModelResolver
	sessions   *permissionSessions

	hooksMu sync.Mutex
	hooks   map[string]*SessionHooks

	builtin     builtinDeps
	clientTools []Tool
	dispatcher  *toolDispatcher
}

// toolDispatcher routes materialization to a session-scoped registry when one
// exists, falling back to the base registry.
type toolDispatcher struct {
	base     *tool.Registry
	mu       sync.Mutex
	sessions map[string]*tool.Registry
}

func (d *toolDispatcher) MaterializeFor(sessionID string, permissions []core.PermissionRule) *tool.Materialization {
	d.mu.Lock()
	registry := d.sessions[sessionID]
	d.mu.Unlock()
	if registry == nil {
		registry = d.base
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
func (r *runtime) newSessionRegistry(sessionTools []Tool) (*tool.Registry, error) {
	registry := tool.NewRegistry()
	if err := registerBuiltins(registry, r.builtin); err != nil {
		return nil, err
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

func newRuntime(opts *ClientOptions) (*runtime, error) {
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

	projectID := schema.ProjectIDGlobal
	if _, err := db.DB.Exec(
		"INSERT INTO `project` (`id`,`worktree`,`sandboxes`,`time_created`,`time_updated`) VALUES (?,?,?,?,?)",
		string(projectID), directory, "[]", time.Now().UnixMilli(), time.Now().UnixMilli(),
	); err != nil {
		db.Close()
		return nil, err
	}

	location := core.Location{LocationInfo: schema.LocationInfo{
		Directory: schema.AbsolutePath(directory),
		Project:   schema.LocationProject{ID: projectID, Directory: schema.AbsolutePath(directory)},
	}}

	agents := core.NewAgent()
	agentSessions := newPermissionSessions()
	permission := core.NewPermission(core.PermissionConfig{
		Sessions:  agentSessions,
		Agents:    permissionAgents{agents: agents},
		Saved:     &permissionSaved{},
		Events:    permissionEvents{events: events},
		ProjectID: string(projectID),
	})
	question := core.NewQuestion(core.QuestionConfig{Events: events})

	global := core.NewGlobal()
	skills := core.NewSkill(core.NewSkillDiscovery(filepath.Join(global.Cache, "skills"), http.DefaultClient))

	models, err := buildModelResolver(opts)
	if err != nil {
		db.Close()
		return nil, err
	}

	builtin := builtinDeps{
		db: db, events: events, permission: permission, question: question,
		location: location, config: emptyConfig{}, skills: skills,
	}
	registry := tool.NewRegistry()
	if err := registerBuiltins(registry, builtin); err != nil {
		db.Close()
		return nil, err
	}
	for _, userTool := range opts.Tools {
		if _, err := registry.Register(map[string]tool.Tool{userTool.Name: userTool.build(permission)}); err != nil {
			db.Close()
			return nil, err
		}
	}

	snapshots := runner.Snapshotter(noopSnapshots{})
	if snapshot, err := core.NewSnapshot(core.NewGit(core.NewAppProcess()), emptyConfig{}, global, &location); err == nil {
		snapshots = snapshot
	}

	store := session.NewStore(db)
	rt := &runtime{
		db: db, events: events, store: store, agents: agents, tools: registry,
		permission: permission, question: question, location: location,
		projectID: projectID, models: models, sessions: agentSessions,
		hooks: map[string]*SessionHooks{}, builtin: builtin, clientTools: opts.Tools,
		dispatcher: &toolDispatcher{base: registry, sessions: map[string]*tool.Registry{}},
	}
	rt.runner = runner.NewLLMRunner(runner.LLMRunner{
		Events:            appEvents{events: events},
		LLM:               chooseLLM(opts),
		Agents:            agents,
		Tools:             hookedRegistry{inner: rt.dispatcher, hooksFor: rt.hooksFor},
		Models:            models,
		Store:             store,
		Location:          location,
		SystemContext:     systemcontext.NewRegistry(),
		SkillGuidance:     skill.NewGuidance(skills, allowAllEvaluator{}),
		ReferenceGuidance: reference.NewGuidance(noopReferences{}),
		Config:            emptyConfig{},
		Snapshots:         snapshots,
		DB:                db,
	})
	rt.coord = session.NewCoordinator(func(ctx context.Context, id schema.SessionID, force bool) error {
		return rt.runner.Run(runner.RunInput{SessionID: id, Force: force})
	})
	return rt, nil
}

func chooseLLM(opts *ClientOptions) route.LLMClient {
	if opts.llm != nil {
		return opts.llm
	}
	return llm.NewClient(route.TransportRuntime{HTTP: route.NewExecutor(http.DefaultClient)})
}

func (r *runtime) close() error {
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

// buildModelResolver maps a provider config to a model resolver.
func buildModelResolver(opts *ClientOptions) (runner.ModelResolver, error) {
	var model route.Model
	apiKey := opts.Provider.APIKey
	switch opts.Provider.Type {
	case "", "openai":
		facade := provider.NewOpenAI(
			protocols.OpenAIResponsesHTTPRoute,
			protocols.OpenAIResponsesHTTPRoute,
			protocols.OpenAIChatHTTPRoute,
			provider.OpenAIConfig{APIKey: &apiKey, BaseURL: opts.Provider.BaseURL},
		)
		model = facade.Model(opts.Model)
	case "anthropic":
		facade := provider.NewAnthropic(
			protocols.AnthropicMessagesHTTPRoute,
			provider.AnthropicConfig{APIKey: &apiKey, BaseURL: opts.Provider.BaseURL},
		)
		model = facade.Model(opts.Model)
	case "openai-compatible":
		facade := provider.NewOpenAICompatible(
			protocols.OpenAICompatibleChatHTTPRoute,
			provider.OpenAICompatibleGenericOptions{
				APIKey:   &apiKey,
				BaseURL:  opts.Provider.BaseURL,
				Provider: opts.Provider.Provider,
			},
		)
		model = facade.Model(opts.Model)
	default:
		facade := provider.NewOpenAI(
			protocols.OpenAIResponsesHTTPRoute,
			protocols.OpenAIResponsesHTTPRoute,
			protocols.OpenAIChatHTTPRoute,
			provider.OpenAIConfig{APIKey: &apiKey, BaseURL: opts.Provider.BaseURL},
		)
		model = facade.Model(opts.Model)
	}
	return func(stored schema.SessionInfo) (route.Model, error) {
		return model, nil
	}, nil
}

// --- narrow no-op adapters ---

type emptyConfig struct{}

func (emptyConfig) Entries() []core.ConfigEntry { return nil }

type noopSnapshots struct{}

func (noopSnapshots) Capture() (*core.SnapshotID, error) { return nil, nil }
func (noopSnapshots) Files(core.SnapshotCompareInput) ([]schema.RelativePath, error) {
	return nil, nil
}

type noopReferences struct{}

func (noopReferences) List() ([]reference.Info, error) { return nil, nil }

type allowAllEvaluator struct{}

func (allowAllEvaluator) Evaluate(action, resource string, rules schema.PermissionRuleset) schema.PermissionRule {
	return core.PermissionEvaluate(action, resource, rules)
}

var _ = llmschema.ToolResultValue{}
