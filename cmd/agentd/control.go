package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	config "github.com/jiaweisky/go-agent-sdk/cmd/agentd/config"
	"github.com/jiaweisky/go-agent-sdk/cmd/agentd/isolation"
	agent "github.com/jiaweisky/go-agent-sdk/pkg/agent"
)

// controlRequest is the body of POST /admin/projects: a project instance to
// create. Root and WorkingDirectory must be absolute host paths; Isolation is
// optional (default: none).
type controlRequest struct {
	Name             string                  `json:"name"`
	Kind             string                  `json:"kind"`
	Root             string                  `json:"root"`
	WorkingDirectory string                  `json:"workingDirectory"`
	ConfigDirs       []string                `json:"configDirs"`
	Models           []string                `json:"models"`
	DefaultModel     string                  `json:"defaultModel"`
	Agents           []string                `json:"agents"`
	Isolation        *config.IsolationConfig `json:"isolation"`
	// Auth, when set, guards the instance's transport endpoints.
	Auth *config.AuthConfig `json:"auth"`
}

// controlInstance is one running dynamic project.
type controlInstance struct {
	id               string
	kind             string
	serverBase       string
	root             string
	workingDirectory string
	cancel           context.CancelFunc
	done             chan struct{}
	client           *agent.Client
	teardown         func(context.Context) error
}

// projectManager creates and destroys isolated project instances at runtime.
// Local isolation modes (none/copy/worktree/overlay) are served in-process, each
// on its own listener; container/remote modes are rejected because they run in a
// separate process provisioned by the host.
type projectManager struct {
	ctx           context.Context
	catalog       config.Catalog
	projectListen string
	runner        isolation.Runner

	mu        sync.Mutex
	instances map[string]*controlInstance
	roots     map[string]string // absolute root -> instance id (one Root per instance)
	reserved  map[string]bool   // ids being built (claimed before the slow build)
}

func newProjectManager(ctx context.Context, catalog config.Catalog, control *config.ControlConfig) *projectManager {
	projectListen := control.ProjectListen
	if projectListen == "" {
		projectListen = "127.0.0.1:0"
	}
	return &projectManager{
		ctx:           ctx,
		catalog:       catalog,
		projectListen: projectListen,
		runner:        isolation.DefaultRunner(),
		instances:     map[string]*controlInstance{},
		roots:         map[string]string{},
		reserved:      map[string]bool{},
	}
}

// Start prepares a project, builds its client, binds a listener and serves it. It
// claims the name and root up front (so concurrent creates cannot race or share a
// root) and releases them on any failure.
func (m *projectManager) Start(req controlRequest) (*controlInstance, error) {
	if err := req.validate(); err != nil {
		return nil, err
	}
	root, err := filepath.Abs(req.Root)
	if err != nil {
		return nil, fmt.Errorf("root: %w", err)
	}
	if err := m.claim(req.Name, root); err != nil {
		return nil, err
	}
	instance, err := m.prepare(req, root)
	if err != nil {
		m.release(req.Name, root)
		return nil, err
	}
	m.register(req.Name, instance)
	return instance, nil
}

// claim reserves a project's name and root while it is being built, so
// concurrent creates cannot race or share a root.
func (m *projectManager) claim(name, root string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.instances[name]; exists || m.reserved[name] {
		return fmt.Errorf("project %q already exists", name)
	}
	if owner, taken := m.roots[root]; taken {
		return fmt.Errorf("root %q is already in use by project %q", root, owner)
	}
	m.reserved[name] = true
	m.roots[root] = name
	return nil
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
func (m *projectManager) prepare(req controlRequest, root string) (*controlInstance, error) {
	server := config.Server{
		Name:             req.Name,
		Kind:             req.Kind,
		Root:             root,
		WorkingDirectory: req.WorkingDirectory,
		ConfigDirs:       req.ConfigDirs,
		Models:           req.Models,
		DefaultModel:     req.DefaultModel,
		Agents:           req.Agents,
		Isolation:        req.Isolation,
		Auth:             req.Auth,
	}
	catalog, err := m.catalog.Subset(server.Models, server.DefaultModel)
	if err != nil {
		return nil, err
	}
	project, err := m.runner.Prepare(m.ctx, projectSpec(server), isolationSpec(server))
	if err != nil {
		return nil, err
	}
	server.WorkingDirectory = project.View

	uploader := mediaUploader(server)
	client, err := buildClient(server, catalog, uploader)
	if err != nil {
		m.teardown(project)
		return nil, err
	}
	ctx, cancel := context.WithCancel(m.ctx)
	built, err := buildServer(ctx, server, client, catalog, uploader)
	if err != nil {
		m.abandon(project, client, cancel)
		return nil, err
	}
	ln, err := net.Listen("tcp", m.projectListen)
	if err != nil {
		m.abandon(project, client, cancel)
		return nil, fmt.Errorf("bind project listener: %w", err)
	}
	srv := &http.Server{Handler: newRouter([]*builtServer{built}), ReadHeaderTimeout: 10 * time.Second}
	instance := &controlInstance{
		id:               req.Name,
		kind:             req.Kind,
		serverBase:       publicBaseURL(server, ln.Addr()),
		root:             root,
		workingDirectory: project.View,
		cancel:           cancel,
		done:             make(chan struct{}),
		client:           client,
		teardown:         project.Teardown,
	}
	go func() {
		_ = serve(ctx, srv, ln, nil, built.closeStreams)
		close(instance.done)
	}()
	return instance, nil
}

// Stop shuts down a project instance and releases its view.
func (m *projectManager) Stop(id string) bool {
	m.mu.Lock()
	instance, ok := m.instances[id]
	if ok {
		delete(m.instances, id)
		delete(m.roots, instance.root)
	}
	m.mu.Unlock()
	if !ok {
		return false
	}
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

// Close stops every instance (used on shutdown).
func (m *projectManager) Close() {
	for _, instance := range m.List() {
		m.Stop(instance.id)
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

func (r controlRequest) validate() error {
	switch {
	case r.Name == "":
		return errors.New("name is required")
	case r.Kind != "http" && r.Kind != "a2a":
		return errors.New("kind must be http or a2a")
	case r.Root == "":
		return errors.New("root is required")
	case len(r.Agents) == 0:
		return errors.New("at least one agent is required")
	}
	if r.Auth != nil {
		if r.Auth.Bearer == "" && r.Auth.Password == "" {
			return errors.New("auth requires bearer or a password")
		}
		if r.Kind == "a2a" && r.Auth.Bearer == "" {
			return errors.New("a2a auth is bearer-only")
		}
	}
	return config.ValidateIsolation(r.Name, r.WorkingDirectory, r.Isolation)
}

// controlHandler serves the admin API.
type controlHandler struct {
	manager *projectManager
	auth    *config.AuthConfig
}

func newControlHandler(manager *projectManager, auth *config.AuthConfig) http.Handler {
	handler := &controlHandler{manager: manager, auth: auth}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /admin/projects", handler.list)
	mux.HandleFunc("POST /admin/projects", handler.create)
	mux.HandleFunc("DELETE /admin/projects/{id}", handler.delete)
	return mux
}

func (h *controlHandler) authorized(r *http.Request) bool {
	if h.auth == nil || h.auth.Bearer == "" {
		return true
	}
	provided := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	return subtle.ConstantTimeCompare([]byte(provided), []byte(h.auth.Bearer)) == 1
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
	}
	instances := h.manager.List()
	items := make([]item, 0, len(instances))
	for _, instance := range instances {
		items = append(items, item{
			ProjectID:        instance.id,
			Kind:             instance.kind,
			ServerBase:       instance.serverBase,
			WorkingDirectory: instance.workingDirectory,
		})
	}
	writeJSONStatus(w, http.StatusOK, map[string]any{"data": items})
}

func (h *controlHandler) create(w http.ResponseWriter, r *http.Request) {
	if !h.authorized(r) {
		writeControlError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var req controlRequest
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
		"project_id":  instance.id,
		"kind":        instance.kind,
		"server_base": instance.serverBase,
		"transport":   instance.kind,
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
