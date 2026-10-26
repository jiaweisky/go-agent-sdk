package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"sync"
	"time"

	config "github.com/jiaweisky/go-agent-sdk/cmd/agentd/config"
	"github.com/jiaweisky/go-agent-sdk/cmd/agentd/isolation"
	agent "github.com/jiaweisky/go-agent-sdk/pkg/agent"
)

// controlInstance is one running dynamic project.
type controlInstance struct {
	id               string
	kind             string
	serverBase       string
	root             string
	workingDirectory string
	// readonly marks a shared read-only lease; readers counts how many leases
	// share it (a writable lease has readers 1 and is exclusive).
	readonly bool
	readers  int
	// baseDir is the instance's isolation base directory ("" when none), used to
	// hide views from other instances' searches.
	baseDir  string
	cancel   context.CancelFunc
	done     chan struct{}
	client   *agent.Client
	teardown func(context.Context) error
}

// projectManager creates and destroys isolated project instances at runtime.
// Local isolation modes (none/copy/worktree/overlay) are served in-process, each
// on its own listener; container/remote modes are rejected because they run in a
// separate process provisioned by the host.
type projectManager struct {
	ctx           context.Context
	registry      config.ModelRegistry
	models        []config.ModelConfig
	profiles      map[string]*config.ProviderFileAPI
	projectListen string
	maxLeases     int
	runner        isolation.Runner

	mu        sync.Mutex
	instances map[string]*controlInstance
	roots     map[string]string // absolute root -> instance id (one Root per instance)
	reserved  map[string]bool   // ids being built (claimed before the slow build)
}

func newProjectManager(ctx context.Context, registry config.ModelRegistry, control *config.ControlConfig, models []config.ModelConfig, profiles map[string]*config.ProviderFileAPI) *projectManager {
	projectListen := control.ProjectListen
	if projectListen == "" {
		projectListen = "127.0.0.1:0"
	}
	return &projectManager{
		ctx:           ctx,
		registry:      registry,
		models:        models,
		profiles:      profiles,
		projectListen: projectListen,
		maxLeases:     control.MaxLeases,
		runner:        isolation.DefaultRunner(),
		instances:     map[string]*controlInstance{},
		roots:         map[string]string{},
		reserved:      map[string]bool{},
	}
}

// Start prepares a project, builds its client, binds a listener and serves it. A
// read-only request shares an existing read-only instance; every other case
// creates a new instance, claiming the name and root up front (so concurrent
// creates cannot race or share a root) and releasing them on any failure.
func (m *projectManager) Start(req config.ProjectRequest) (*controlInstance, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	root, err := filepath.Abs(req.Root)
	if err != nil {
		return nil, fmt.Errorf("root: %w", err)
	}
	shared, err := m.reserve(req, root)
	if err != nil || shared != nil {
		return shared, err
	}
	instance, err := m.prepare(req, root)
	if err != nil {
		m.release(req.Name, root)
		return nil, err
	}
	m.register(req.Name, instance)
	return instance, nil
}

// reserve claims the project's name and root while it is being built, or returns
// an existing read-only instance to share. It performs the whole decision under
// one lock so concurrent creates cannot race, share a root, or split a
// read-only lease from its instance.
func (m *projectManager) reserve(req config.ProjectRequest, root string) (*controlInstance, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if existing := m.instances[req.Name]; existing != nil {
		if req.Readonly && existing.readonly {
			existing.readers++
			return existing, nil
		}
		return nil, fmt.Errorf("project %q already exists", req.Name)
	}
	if m.reserved[req.Name] {
		return nil, fmt.Errorf("project %q is being created", req.Name)
	}
	if owner, taken := m.roots[root]; taken {
		return nil, fmt.Errorf("root %q is already in use by project %q", root, owner)
	}
	if m.maxLeases > 0 && len(m.instances)+len(m.reserved) >= m.maxLeases {
		return nil, fmt.Errorf("lease limit reached (%d)", m.maxLeases)
	}
	m.reserved[req.Name] = true
	m.roots[root] = req.Name
	return nil, nil
}

// release frees a claim taken by claim.
func (m *projectManager) release(name, root string) {
	m.mu.Lock()
	delete(m.reserved, name)
	delete(m.roots, root)
	m.mu.Unlock()
}

// register moves a fully prepared instance from reserved to running.
func (m *projectManager) register(name string, instance *controlInstance) {
	m.mu.Lock()
	delete(m.reserved, name)
	m.instances[name] = instance
	m.mu.Unlock()
}

// prepare builds the view, client, handler and listener for a project and starts
// serving it, tearing everything down on any failure so a partial build leaks
// nothing.
func (m *projectManager) prepare(req config.ProjectRequest, root string) (*controlInstance, error) {
	server := req.ToServer(root)
	registry, err := m.registry.Subset(server.Models, server.DefaultModel)
	if err != nil {
		return nil, err
	}
	source := server.WorkingDirectory
	project, err := m.runner.Prepare(m.ctx, projectSpec(server), isolationSpec(server))
	if err != nil {
		return nil, err
	}
	server.WorkingDirectory = project.View

	uploader, err := mediaUploaderFor(server, m.models, m.profiles)
	if err != nil {
		m.teardown(project)
		return nil, fmt.Errorf("project %q: %w", req.Name, err)
	}
	fileHeaders := mediaFileHeadersFor(server, m.models, m.profiles)
	baseDirs := append(m.activeBaseDirs(), isolationBaseDirs([]config.Server{server})...)
	client, err := buildClient(m.ctx, server, registry, uploader, fileHeaders, isolationExcludes(source, baseDirs))
	if err != nil {
		m.teardown(project)
		return nil, err
	}
	ctx, cancel := context.WithCancel(m.ctx)
	builtSrv, err := buildServer(ctx, server, client, registry, uploader)
	if err != nil {
		m.abandon(project, client, cancel)
		return nil, err
	}
	ln, err := net.Listen("tcp", m.projectListen)
	if err != nil {
		m.abandon(project, client, cancel)
		return nil, fmt.Errorf("bind project listener: %w", err)
	}
	srv := &http.Server{Handler: newRouter([]*builtServer{builtSrv}, nil, nil), ReadHeaderTimeout: 10 * time.Second}
	instance := &controlInstance{
		id:               req.Name,
		kind:             string(req.Kind),
		serverBase:       publicBaseURL(server, ln.Addr()),
		root:             root,
		workingDirectory: project.View,
		readonly:         req.Readonly,
		readers:          1,
		baseDir:          baseDirOf(req.Isolation),
		cancel:           cancel,
		done:             make(chan struct{}),
		client:           client,
		teardown:         project.Teardown,
	}
	go func() {
		_ = serve(ctx, srv, ln, nil, builtSrv.closeStreams)
		close(instance.done)
	}()
	return instance, nil
}

// Stop releases one lease on a project instance and, when it is the last, shuts
// the instance down and releases its view. A shared read-only instance is only
// torn down once every reader has released.
func (m *projectManager) Stop(id string) bool {
	return m.stop(id, false)
}

// stop releases a lease. force ignores the reader count (used on shutdown so a
// shared read-only instance is always torn down).
func (m *projectManager) stop(id string, force bool) bool {
	m.mu.Lock()
	instance, ok := m.instances[id]
	if !ok {
		m.mu.Unlock()
		return false
	}
	if !force && instance.readonly && instance.readers > 1 {
		instance.readers--
		m.mu.Unlock()
		return true
	}
	delete(m.instances, id)
	delete(m.roots, instance.root)
	m.mu.Unlock()
	instance.cancel()
	select {
	case <-instance.done:
	case <-time.After(shutdownTimeout):
	}
	_ = instance.client.Close()
	m.teardown(isolation.IsolatedProject{Teardown: instance.teardown})
	return true
}

// List returns the running instances.
func (m *projectManager) List() []*controlInstance {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*controlInstance, 0, len(m.instances))
	for _, instance := range m.instances {
		out = append(out, instance)
	}
	return out
}

// activeBaseDirs returns the isolation base directories of the running
// instances, so a new instance can hide their views from its own searches.
func (m *projectManager) activeBaseDirs() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var dirs []string
	for _, instance := range m.instances {
		if instance.baseDir != "" {
			dirs = append(dirs, instance.baseDir)
		}
	}
	return dirs
}

// Close stops every instance (used on shutdown), regardless of the reader count.
func (m *projectManager) Close() {
	for _, instance := range m.List() {
		m.stop(instance.id, true)
	}
}

// abandon releases the resources of a project whose startup failed: it cancels
// the instance context, closes the client, and tears down the view.
func (m *projectManager) abandon(project isolation.IsolatedProject, client *agent.Client, cancel context.CancelFunc) {
	cancel()
	_ = client.Close()
	m.teardown(project)
}

func (m *projectManager) teardown(project isolation.IsolatedProject) {
	if project.Teardown == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	_ = project.Teardown(ctx)
}

// controlHandler serves the control API: project-instance lifecycle endpoints.
type controlHandler struct {
	manager *projectManager
	auth    *config.AuthConfig
}

func newControlHandler(manager *projectManager, auth *config.AuthConfig) http.Handler {
	handler := &controlHandler{manager: manager, auth: auth}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /admin/projects", handler.list)
	mux.HandleFunc("POST /admin/projects", handler.create)
	mux.HandleFunc("DELETE /admin/projects/{id...}", handler.delete)
	return mux
}

func (h *controlHandler) authorized(r *http.Request) bool {
	if h.auth == nil {
		return true
	}
	if h.auth.Token == "" {
		return false
	}
	return constEq(bearerToken(r), h.auth.Token)
}

func (h *controlHandler) list(w http.ResponseWriter, r *http.Request) {
	if !h.authorized(r) {
		writeControlError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	type item struct {
		ProjectID        string `json:"project_id"`
		Kind             string `json:"kind"`
		ServerBase       string `json:"server_base"`
		WorkingDirectory string `json:"working_directory"`
		Readonly         bool   `json:"readonly"`
	}
	instances := h.manager.List()
	items := make([]item, 0, len(instances))
	for _, instance := range instances {
		items = append(items, item{
			ProjectID:        instance.id,
			Kind:             instance.kind,
			ServerBase:       instance.serverBase,
			WorkingDirectory: instance.workingDirectory,
			Readonly:         instance.readonly,
		})
	}
	writeJSONStatus(w, http.StatusOK, map[string]any{"data": items})
}

func (h *controlHandler) create(w http.ResponseWriter, r *http.Request) {
	if !h.authorized(r) {
		writeControlError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var req config.ProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeControlError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	instance, err := h.manager.Start(req)
	if err != nil {
		writeControlError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSONStatus(w, http.StatusCreated, map[string]any{"data": map[string]any{
		"project_id":        instance.id,
		"kind":              instance.kind,
		"server_base":       instance.serverBase,
		"transport":         instance.kind,
		"working_directory": instance.workingDirectory,
		"readonly":          instance.readonly,
	}})
}

func (h *controlHandler) delete(w http.ResponseWriter, r *http.Request) {
	if !h.authorized(r) {
		writeControlError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if !h.manager.Stop(r.PathValue("id")) {
		writeControlError(w, http.StatusNotFound, "no such project")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeControlError(w http.ResponseWriter, status int, message string) {
	writeJSONStatus(w, status, map[string]any{"error": message})
}
