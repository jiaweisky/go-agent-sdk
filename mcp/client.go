package mcp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/jiaweisky/go-agent-sdk/schema"
	"github.com/modelcontextprotocol/go-sdk/auth"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
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

// LogHandler receives server logging notifications (notifications/message).
type LogHandler func(server string, params *sdk.LoggingMessageParams)

// WithLogHandler wires a handler for server logging notifications.
func WithLogHandler(handler LogHandler) Option {
	return func(c *Client) { c.logHandler = handler }
}

// Client manages connections to configured MCP servers.
type Client struct {
	mu         sync.Mutex
	directory  string
	name       string
	version    string
	events     EventPublisher
	logHandler LogHandler
	sessions   map[string]*sdk.ClientSession
	status     map[string]Status
	defs       map[string][]*sdk.Tool
	// timeouts holds the per-server call timeout, used when adapting tools and
	// when refreshing their list.
	timeouts map[string]time.Duration
}

// NewClient creates an MCP client rooted at directory that identifies itself to
// servers as name/version. name is required: it is the clientInfo the MCP
// handshake sends, so an empty name makes the handshake invalid. Callers that
// have no own name can pass DefaultName from the agent package.
func NewClient(directory, name, version string, options ...Option) *Client {
	client := &Client{
		directory: directory,
		name:      name,
		version:   version,
		sessions:  map[string]*sdk.ClientSession{},
		status:    map[string]Status{},
		defs:      map[string][]*sdk.Tool{},
		timeouts:  map[string]time.Duration{},
	}
	for _, option := range options {
		option(client)
	}
	return client
}

// Connect connects to a configured server. Remote configs are tried over
// StreamableHTTP then SSE; a local config has a single command transport.
func (c *Client) Connect(ctx context.Context, name string, cfg ServerConfig) Status {
	if !cfg.isEnabled() {
		c.setStatus(name, Status{Status: StatusDisabled})
		return Status{Status: StatusDisabled}
	}
	if _, ok := c.Session(name); ok {
		return c.StatusOf(name)
	}
	transports, err := transportsFor(cfg, c.directory)
	if err != nil {
		status := Status{Status: StatusFailed, Error: err.Error()}
		c.setStatus(name, status)
		return status
	}
	var last Status
	for _, transport := range transports {
		status := c.ConnectTransport(ctx, name, transport, cfg.timeout())
		if status.Status == StatusConnected {
			return status
		}
		last = status
		// Auth failures stop the transport fallback.
		if status.Status == StatusNeedsAuth || status.Status == StatusNeedsClientRegistration {
			break
		}
	}
	return last
}

// ConnectTransport connects a single transport, storing the session on success.
func (c *Client) ConnectTransport(
	ctx context.Context,
	name string,
	transport sdk.Transport,
	timeout time.Duration,
) Status {
	sdkClient := sdk.NewClient(&sdk.Implementation{Name: c.name, Version: c.version}, &sdk.ClientOptions{
		ToolListChangedHandler: func(context.Context, *sdk.ToolListChangedRequest) {
			c.refreshTools(name)
			c.publishToolsChanged(name)
		},
		LoggingMessageHandler: func(_ context.Context, request *sdk.LoggingMessageRequest) {
			if c.logHandler != nil {
				c.logHandler(name, request.Params)
			}
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
	c.status[name] = Status{Status: StatusConnected}
	c.timeouts[name] = timeout
	c.mu.Unlock()
	go c.watchSession(name, session)
	return Status{Status: StatusConnected}
}

// watchSession marks the session failed and publishes a change when the
// connection closes.
func (c *Client) watchSession(name string, session *sdk.ClientSession) {
	_ = session.Wait()
	c.mu.Lock()
	closed := c.sessions[name] == session
	if closed {
		delete(c.sessions, name)
		delete(c.defs, name)
		delete(c.timeouts, name)
		c.status[name] = Status{Status: StatusFailed, Error: "Connection closed"}
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
	ctx, cancel := context.WithTimeout(context.Background(), c.timeoutOf(name))
	defer cancel()
	tools, err := ListTools(ctx, session)
	if err != nil {
		return
	}
	c.mu.Lock()
	if c.sessions[name] == session {
		c.defs[name] = tools
	}
	c.mu.Unlock()
}

func (c *Client) publishToolsChanged(name string) {
	if c.events == nil {
		return
	}
	_ = c.events.Publish(schema.McpEventToolsChanged, map[string]any{"server": name})
}

// registrationErrorHints and authErrorHints are substrings that identify a
// failure when the SDK does not return a typed error.
var (
	registrationErrorHints = []string{"registration", "client_id"}
	authErrorHints         = []string{"oauth", "authorization", "unauthorized"}
)

// classifyError maps a connect error to a Status. It checks the SDK's typed
// errors first and falls back to the error text, because this client does not
// wire interactive OAuth and most failures arrive as plain transport messages.
func classifyError(err error) Status {
	var registration *oauthex.ClientRegistrationError
	switch {
	case errors.As(err, &registration):
		return Status{Status: StatusNeedsClientRegistration, Error: err.Error()}
	case errors.Is(err, auth.ErrOAuth), errors.Is(err, auth.ErrInvalidToken):
		return Status{Status: StatusNeedsAuth, Error: err.Error()}
	}
	message := strings.ToLower(err.Error())
	switch {
	case containsAny(message, registrationErrorHints):
		return Status{Status: StatusNeedsClientRegistration, Error: err.Error()}
	case containsAny(message, authErrorHints):
		return Status{Status: StatusNeedsAuth, Error: err.Error()}
	default:
		return Status{Status: StatusFailed, Error: err.Error()}
	}
}

func containsAny(message string, hints []string) bool {
	for _, hint := range hints {
		if strings.Contains(message, hint) {
			return true
		}
	}
	return false
}

// Disconnect closes and forgets a server session.
func (c *Client) Disconnect(name string) {
	c.mu.Lock()
	session := c.sessions[name]
	delete(c.sessions, name)
	delete(c.status, name)
	delete(c.defs, name)
	delete(c.timeouts, name)
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
	c.defs = map[string][]*sdk.Tool{}
	c.timeouts = map[string]time.Duration{}
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
// A server that lacks the tools capability or fails to list contributes
// nothing, matching Prompts, Resources and ResourceTemplates.
func (c *Client) Tools(ctx context.Context) map[string]AdaptedTool {
	result := map[string]AdaptedTool{}
	sessions := c.snapshotSessions()
	timeouts := c.snapshotTimeouts()
	for server, session := range sessions {
		for _, def := range c.serverTools(ctx, server, session) {
			result[ToolName(server, def.Name)] = AdaptedTool{
				Def: def, Session: session, Server: server, Timeout: timeouts[server],
			}
		}
	}
	return result
}

// serverTools returns a server's cached tools, listing and caching them on a
// miss. A server whose tools cannot be listed contributes none.
func (c *Client) serverTools(ctx context.Context, server string, session *sdk.ClientSession) []*sdk.Tool {
	c.mu.Lock()
	cached := c.defs[server]
	c.mu.Unlock()
	if cached != nil {
		return cached
	}
	tools, err := ListTools(ctx, session)
	if err != nil {
		return nil
	}
	c.mu.Lock()
	// Drop the result if the server disconnected while we listed, so a stale
	// cache cannot outlive its session.
	if c.sessions[server] == session {
		c.defs[server] = tools
	}
	c.mu.Unlock()
	return tools
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

// Prompts lists the prompts of every connected server, keyed by
// "<sanitized server>:<sanitized prompt>". A server that lacks the prompts
// capability or fails to list contributes nothing.
func (c *Client) Prompts(ctx context.Context, server ...string) map[string]PromptInfo {
	return collectCatalog(ctx, c.filteredSessions(server), ListPrompts,
		func(server string, prompt *sdk.Prompt) string { return sanitizedPromptKey(server, prompt.Name) },
		func(server string, prompt *sdk.Prompt) PromptInfo {
			return PromptInfo{Name: prompt.Name, Description: prompt.Description, Client: server}
		})
}

// Resources lists the resources of every connected server, keyed by
// "<escaped server>:<resource URI>". A server that lacks the resources
// capability or fails to list contributes nothing.
func (c *Client) Resources(ctx context.Context, server ...string) map[string]ResourceInfo {
	return collectCatalog(ctx, c.filteredSessions(server), ListResources,
		func(server string, resource *sdk.Resource) string { return escapedResourceKey(server, resource.URI) },
		func(server string, resource *sdk.Resource) ResourceInfo {
			return ResourceInfo{
				Name: resource.Name, URI: resource.URI, Description: resource.Description,
				MIMEType: resource.MIMEType, Client: server,
			}
		})
}

// ResourceTemplates lists the resource templates of every connected server,
// keyed by "<escaped server>:<URI template>". A server that lacks the resources
// capability or fails to list contributes nothing.
func (c *Client) ResourceTemplates(ctx context.Context, server ...string) map[string]ResourceTemplateInfo {
	return collectCatalog(ctx, c.filteredSessions(server), ListResourceTemplates,
		func(server string, template *sdk.ResourceTemplate) string {
			return escapedResourceKey(server, template.URITemplate)
		},
		func(server string, template *sdk.ResourceTemplate) ResourceTemplateInfo {
			return ResourceTemplateInfo{
				Name: template.Name, URITemplate: template.URITemplate, Description: template.Description,
				MIMEType: template.MIMEType, Client: server,
			}
		})
}

// collectCatalog lists every server's entries and keys them, skipping servers
// that lack the capability or fail to list.
func collectCatalog[E any, T any](
	ctx context.Context,
	sessions map[string]*sdk.ClientSession,
	list func(context.Context, *sdk.ClientSession) ([]E, error),
	key func(server string, entry E) string,
	info func(server string, entry E) T,
) map[string]T {
	result := map[string]T{}
	for server, session := range sessions {
		entries, err := list(ctx, session)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			result[key(server, entry)] = info(server, entry)
		}
	}
	return result
}

// filteredSessions returns every session, or only the named server's when a
// non-empty server name is given.
func (c *Client) filteredSessions(server []string) map[string]*sdk.ClientSession {
	sessions := c.snapshotSessions()
	if len(server) == 0 || server[0] == "" {
		return sessions
	}
	filtered := map[string]*sdk.ClientSession{}
	if session, ok := sessions[server[0]]; ok {
		filtered[server[0]] = session
	}
	return filtered
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

func (c *Client) snapshotTimeouts() map[string]time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	result := make(map[string]time.Duration, len(c.timeouts))
	for name, timeout := range c.timeouts {
		result[name] = timeout
	}
	return result
}

func (c *Client) snapshotDefs() map[string][]*sdk.Tool {
	c.mu.Lock()
	defer c.mu.Unlock()
	result := make(map[string][]*sdk.Tool, len(c.defs))
	for name, tools := range c.defs {
		result[name] = tools
	}
	return result
}

// timeoutOf returns the connect timeout a server was registered with.
func (c *Client) timeoutOf(name string) time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	if timeout, ok := c.timeouts[name]; ok {
		return timeout
	}
	return DefaultTimeout
}

// sanitizedPromptKey is "<sanitized server>:<sanitized prompt>".
func sanitizedPromptKey(server, name string) string {
	return Sanitize(server) + ":" + Sanitize(name)
}

// escapedResourceKey is "<escaped server>:<uri>", escaping "%" and ":" so a
// "server:uri" key stays unambiguous.
func escapedResourceKey(server, uri string) string {
	return escapeKeyPart(server) + ":" + uri
}

// escapeKeyPart percent-escapes the characters that would make a "server:uri"
// key ambiguous.
func escapeKeyPart(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, "%", "%25"), ":", "%3A")
}

// sessionFor returns the session for a connected server.
func (c *Client) sessionFor(server string) (*sdk.ClientSession, error) {
	session, ok := c.Session(server)
	if !ok {
		return nil, fmt.Errorf("mcp server not connected: %s", server)
	}
	return session, nil
}

// CallTool invokes a tool on the named server.
func (c *Client) CallTool(ctx context.Context, server, toolName string, args any) (*sdk.CallToolResult, error) {
	session, err := c.sessionFor(server)
	if err != nil {
		return nil, err
	}
	return session.CallTool(ctx, &sdk.CallToolParams{Name: toolName, Arguments: args})
}

// GetPrompt fetches a prompt from the named server.
func (c *Client) GetPrompt(
	ctx context.Context,
	server, name string,
	args map[string]string,
) (*sdk.GetPromptResult, error) {
	session, err := c.sessionFor(server)
	if err != nil {
		return nil, err
	}
	return session.GetPrompt(ctx, &sdk.GetPromptParams{Name: name, Arguments: args})
}

// ReadResource reads a resource from the named server.
func (c *Client) ReadResource(ctx context.Context, server, uri string) (*sdk.ReadResourceResult, error) {
	session, err := c.sessionFor(server)
	if err != nil {
		return nil, err
	}
	return session.ReadResource(ctx, &sdk.ReadResourceParams{URI: uri})
}

// ServerInstructions describes a server's usage instructions and tools.
type ServerInstructions struct {
	Name         string
	Instructions string
	Tools        []string
}

// Instructions returns the usage instructions of every server that provides
// them.
func (c *Client) Instructions() []ServerInstructions {
	sessions := c.snapshotSessions()
	defs := c.snapshotDefs()

	var result []ServerInstructions
	for name, session := range sessions {
		initialized := session.InitializeResult()
		if initialized == nil || strings.TrimSpace(initialized.Instructions) == "" {
			continue
		}
		tools := make([]string, 0, len(defs[name]))
		for _, def := range defs[name] {
			tools = append(tools, def.Name)
		}
		result = append(result, ServerInstructions{
			Name:         name,
			Instructions: strings.TrimSpace(initialized.Instructions),
			Tools:        tools,
		})
	}
	return result
}

// ServerMetadata is a connected server's initialize metadata (the protocol's
// serverInfo / Implementation).
type ServerMetadata struct {
	Name        string
	Title       string
	Description string
	Version     string
}

// ServerInfo returns the initialize metadata of a connected server. The second
// result is false when the server is not connected or reported no serverInfo.
func (c *Client) ServerInfo(name string) (ServerMetadata, bool) {
	session, ok := c.Session(name)
	if !ok {
		return ServerMetadata{}, false
	}
	initialized := session.InitializeResult()
	if initialized == nil || initialized.ServerInfo == nil {
		return ServerMetadata{}, false
	}
	info := initialized.ServerInfo
	return ServerMetadata{
		Name:        info.Name,
		Title:       info.Title,
		Description: info.Description,
		Version:     info.Version,
	}, true
}

func (c *Client) setStatus(name string, status Status) {
	c.mu.Lock()
	c.status[name] = status
	c.mu.Unlock()
}

// transportsFor builds the candidate transports for a server config. A remote
// config yields both HTTP transports to try in order; a local config yields a
// single command transport.
func transportsFor(cfg ServerConfig, directory string) ([]sdk.Transport, error) {
	if cfg.Type == "remote" {
		return remoteTransports(cfg), nil
	}
	transport, err := localTransport(cfg, directory)
	if err != nil {
		return nil, err
	}
	return []sdk.Transport{transport}, nil
}

func remoteTransports(cfg ServerConfig) []sdk.Transport {
	httpClient := clientWithHeaders(cfg.Headers)
	return []sdk.Transport{
		&sdk.StreamableClientTransport{Endpoint: cfg.URL, HTTPClient: httpClient},
		&sdk.SSEClientTransport{Endpoint: cfg.URL, HTTPClient: httpClient},
	}
}

func localTransport(cfg ServerConfig, directory string) (sdk.Transport, error) {
	if len(cfg.Command) == 0 {
		return nil, fmt.Errorf("mcp local server has no command")
	}
	command := exec.Command(cfg.Command[0], cfg.Command[1:]...)
	command.Dir = commandDir(cfg.Cwd, directory)
	command.Env = append(os.Environ(), environmentPairs(cfg.Environment)...)
	return &sdk.CommandTransport{Command: command}, nil
}

// commandDir resolves a configured working directory against the client root.
func commandDir(cwd, directory string) string {
	switch {
	case cwd == "":
		return directory
	case filepath.IsAbs(cwd):
		return cwd
	default:
		return filepath.Join(directory, cwd)
	}
}

func environmentPairs(environment map[string]string) []string {
	pairs := make([]string, 0, len(environment))
	for key, value := range environment {
		pairs = append(pairs, key+"="+value)
	}
	return pairs
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
