package mcp

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/user/go-agent-sdk/schema"
)

// EventPublisher publishes MCP lifecycle events (for example mcp.tools.changed).
type EventPublisher interface {
	Publish(def schema.EventDefinition, data any) error
}

// Option customizes a Client.
type Option func(*Client)

// WithEvents wires an event publisher so tool-list changes and disconnects are
// observable.
func WithEvents(publisher EventPublisher) Option {
	return func(c *Client) { c.events = publisher }
}

// Client manages connections to configured MCP servers.
type Client struct {
	mu        sync.Mutex
	directory string
	version   string
	events    EventPublisher
	sessions  map[string]*sdk.ClientSession
	status    map[string]Status
	defs      map[string][]*sdk.Tool
}

// NewClient creates an MCP client rooted at directory.
func NewClient(directory, version string, options ...Option) *Client {
	client := &Client{
		directory: directory,
		version:   version,
		sessions:  map[string]*sdk.ClientSession{},
		status:    map[string]Status{},
		defs:      map[string][]*sdk.Tool{},
	}
	for _, option := range options {
		option(client)
	}
	return client
}

// Connect connects to a configured server, trying the remote transports in
// order (StreamableHTTP then SSE) as in index.ts's connectRemote.
func (c *Client) Connect(ctx context.Context, name string, cfg ServerConfig) Status {
	if !cfg.isEnabled() {
		c.setStatus(name, Status{Status: "disabled"})
		return Status{Status: "disabled"}
	}
	if _, ok := c.Session(name); ok {
		return c.StatusOf(name)
	}
	transports, err := transportsFor(cfg, c.directory)
	if err != nil {
		status := Status{Status: "failed", Error: err.Error()}
		c.setStatus(name, status)
		return status
	}
	var last Status
	for _, transport := range transports {
		status := c.ConnectTransport(ctx, name, transport, cfg.timeout())
		if status.Status == "connected" {
			return status
		}
		last = status
	}
	return last
}

// ConnectTransport connects a single transport, storing the session on success.
func (c *Client) ConnectTransport(ctx context.Context, name string, transport sdk.Transport, timeout time.Duration) Status {
	sdkClient := sdk.NewClient(&sdk.Implementation{Name: "opencode", Version: c.version}, &sdk.ClientOptions{
		ToolListChangedHandler: func(context.Context, *sdk.ToolListChangedRequest) {
			c.refreshTools(name)
			c.publishToolsChanged(name)
		},
	})
	if c.directory != "" {
		sdkClient.AddRoots(&sdk.Root{URI: fileURL(c.directory)})
	}
	connectCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	session, err := sdkClient.Connect(connectCtx, transport, nil)
	if err != nil {
		status := classifyError(err)
		c.setStatus(name, status)
		return status
	}
	c.mu.Lock()
	c.sessions[name] = session
	c.status[name] = Status{Status: "connected"}
	c.mu.Unlock()
	go c.watch(name, session)
	return Status{Status: "connected"}
}

// watch marks the session failed and publishes a change when the connection
// closes, mirroring index.ts's client.onclose handling.
func (c *Client) watch(name string, session *sdk.ClientSession) {
	_ = session.Wait()
	c.mu.Lock()
	closed := c.sessions[name] == session
	if closed {
		delete(c.sessions, name)
		delete(c.defs, name)
		c.status[name] = Status{Status: "failed", Error: "Connection closed"}
	}
	c.mu.Unlock()
	if closed {
		c.publishToolsChanged(name)
	}
}

func (c *Client) refreshTools(name string) {
	session, ok := c.Session(name)
	if !ok {
		return
	}
	tools, err := ListTools(context.Background(), session)
	if err != nil {
		return
	}
	c.mu.Lock()
	c.defs[name] = tools
	c.mu.Unlock()
}

func (c *Client) publishToolsChanged(name string) {
	if c.events == nil {
		return
	}
	_ = c.events.Publish(schema.McpEventToolsChanged, map[string]any{"server": name})
}

// classifyError maps a connect error to a Status, mirroring connectRemote's
// auth handling (needs_auth / needs_client_registration).
func classifyError(err error) Status {
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "registration") || strings.Contains(message, "client_id"):
		return Status{Status: "needs_client_registration", Error: err.Error()}
	case strings.Contains(message, "oauth") || strings.Contains(message, "authorization") ||
		strings.Contains(message, "unauthorized"):
		return Status{Status: "needs_auth", Error: err.Error()}
	default:
		return Status{Status: "failed", Error: err.Error()}
	}
}

// Disconnect closes and forgets a server session.
func (c *Client) Disconnect(name string) {
	c.mu.Lock()
	session := c.sessions[name]
	delete(c.sessions, name)
	delete(c.status, name)
	c.mu.Unlock()
	if session != nil {
		_ = session.Close()
	}
}

// Close disconnects every server.
func (c *Client) Close() {
	c.mu.Lock()
	sessions := c.sessions
	c.sessions = map[string]*sdk.ClientSession{}
	c.status = map[string]Status{}
	c.mu.Unlock()
	for _, session := range sessions {
		_ = session.Close()
	}
}

// Status returns a copy of the per-server status map.
func (c *Client) Status() map[string]Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	result := make(map[string]Status, len(c.status))
	for name, status := range c.status {
		result[name] = status
	}
	return result
}

// StatusOf returns a single server's status.
func (c *Client) StatusOf(name string) Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.status[name]
}

// Session returns the session for a connected server.
func (c *Client) Session(name string) (*sdk.ClientSession, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	session, ok := c.sessions[name]
	return session, ok
}

// Tools lists the tools of every connected server, keyed by "<server>_<tool>".
func (c *Client) Tools(ctx context.Context) (map[string]AdaptedTool, error) {
	result := map[string]AdaptedTool{}
	c.mu.Lock()
	sessions := make(map[string]*sdk.ClientSession, len(c.sessions))
	for name, session := range c.sessions {
		sessions[name] = session
	}
	c.mu.Unlock()
	for server, session := range sessions {
		c.mu.Lock()
		cached := c.defs[server]
		c.mu.Unlock()
		tools := cached
		if tools == nil {
			var err error
			tools, err = ListTools(ctx, session)
			if err != nil {
				return nil, err
			}
			c.mu.Lock()
			c.defs[server] = tools
			c.mu.Unlock()
		}
		for _, def := range tools {
			result[ToolName(server, def.Name)] = AdaptedTool{Def: def, Session: session, Server: server}
		}
	}
	return result, nil
}

// PromptInfo is a discovered prompt plus its server.
type PromptInfo struct {
	Name        string
	Description string
	Client      string
}

// ResourceInfo is a discovered resource plus its server.
type ResourceInfo struct {
	Name        string
	URI         string
	Description string
	MIMEType    string
	Client      string
}

// ResourceTemplateInfo is a discovered resource template plus its server.
type ResourceTemplateInfo struct {
	Name        string
	URITemplate string
	Description string
	MIMEType    string
	Client      string
}

// Prompts lists the prompts of every connected server.
func (c *Client) Prompts(ctx context.Context) (map[string]PromptInfo, error) {
	result := map[string]PromptInfo{}
	for server, session := range c.snapshotSessions() {
		prompts, err := ListPrompts(ctx, session)
		if err != nil {
			return nil, err
		}
		for _, prompt := range prompts {
			result[entryKey(server, prompt.Name)] = PromptInfo{
				Name: prompt.Name, Description: prompt.Description, Client: server,
			}
		}
	}
	return result, nil
}

// Resources lists the resources of every connected server.
func (c *Client) Resources(ctx context.Context) (map[string]ResourceInfo, error) {
	result := map[string]ResourceInfo{}
	for server, session := range c.snapshotSessions() {
		resources, err := ListResources(ctx, session)
		if err != nil {
			return nil, err
		}
		for _, resource := range resources {
			result[entryKey(server, resource.Name)] = ResourceInfo{
				Name: resource.Name, URI: resource.URI, Description: resource.Description,
				MIMEType: resource.MIMEType, Client: server,
			}
		}
	}
	return result, nil
}

// ResourceTemplates lists the resource templates of every connected server.
func (c *Client) ResourceTemplates(ctx context.Context) (map[string]ResourceTemplateInfo, error) {
	result := map[string]ResourceTemplateInfo{}
	for server, session := range c.snapshotSessions() {
		templates, err := ListResourceTemplates(ctx, session)
		if err != nil {
			return nil, err
		}
		for _, template := range templates {
			result[entryKey(server, template.Name)] = ResourceTemplateInfo{
				Name: template.Name, URITemplate: template.URITemplate, Description: template.Description,
				MIMEType: template.MIMEType, Client: server,
			}
		}
	}
	return result, nil
}

func (c *Client) snapshotSessions() map[string]*sdk.ClientSession {
	c.mu.Lock()
	defer c.mu.Unlock()
	result := make(map[string]*sdk.ClientSession, len(c.sessions))
	for name, session := range c.sessions {
		result[name] = session
	}
	return result
}

func entryKey(server, name string) string {
	return Sanitize(server) + ":" + Sanitize(name)
}

// CallTool invokes a tool on the named server.
func (c *Client) CallTool(ctx context.Context, server, toolName string, args any) (*sdk.CallToolResult, error) {
	session, ok := c.Session(server)
	if !ok {
		return nil, fmt.Errorf("mcp server not connected: %s", server)
	}
	return session.CallTool(ctx, &sdk.CallToolParams{Name: toolName, Arguments: args})
}

func (c *Client) setStatus(name string, status Status) {
	c.mu.Lock()
	c.status[name] = status
	c.mu.Unlock()
}

// transportsFor builds the candidate transports for a server config.
func transportsFor(cfg ServerConfig, directory string) ([]sdk.Transport, error) {
	if cfg.Type == "remote" {
		httpClient := clientWithHeaders(cfg.Headers)
		return []sdk.Transport{
			&sdk.StreamableClientTransport{Endpoint: cfg.URL, HTTPClient: httpClient},
			&sdk.SSEClientTransport{Endpoint: cfg.URL, HTTPClient: httpClient},
		}, nil
	}
	if len(cfg.Command) == 0 {
		return nil, fmt.Errorf("mcp local server has no command")
	}
	command := exec.Command(cfg.Command[0], cfg.Command[1:]...)
	if cfg.Cwd != "" {
		if filepath.IsAbs(cfg.Cwd) {
			command.Dir = cfg.Cwd
		} else {
			command.Dir = filepath.Join(directory, cfg.Cwd)
		}
	} else {
		command.Dir = directory
	}
	command.Env = os.Environ()
	for key, value := range cfg.Environment {
		command.Env = append(command.Env, key+"="+value)
	}
	return []sdk.Transport{&sdk.CommandTransport{Command: command}}, nil
}

func clientWithHeaders(headers map[string]string) *http.Client {
	if len(headers) == 0 {
		return nil
	}
	return &http.Client{Transport: &headerTransport{base: http.DefaultTransport, headers: headers}}
}

type headerTransport struct {
	base    http.RoundTripper
	headers map[string]string
}

func (t *headerTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	clone := request.Clone(request.Context())
	for key, value := range t.headers {
		clone.Header.Set(key, value)
	}
	return t.base.RoundTrip(clone)
}

func fileURL(path string) string {
	absolute, err := filepath.Abs(path)
	if err != nil {
		absolute = path
	}
	return (&url.URL{Scheme: "file", Path: absolute}).String()
}
