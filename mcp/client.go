package mcp

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Client manages connections to configured MCP servers.
type Client struct {
	mu        sync.Mutex
	directory string
	version   string
	sessions  map[string]*sdk.ClientSession
	status    map[string]Status
}

// NewClient creates an MCP client rooted at directory.
func NewClient(directory, version string) *Client {
	return &Client{
		directory: directory,
		version:   version,
		sessions:  map[string]*sdk.ClientSession{},
		status:    map[string]Status{},
	}
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
	client := sdk.NewClient(&sdk.Implementation{Name: "opencode", Version: c.version}, nil)
	if c.directory != "" {
		client.AddRoots(&sdk.Root{URI: fileURL(c.directory)})
	}
	connectCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	session, err := client.Connect(connectCtx, transport, nil)
	if err != nil {
		status := Status{Status: "failed", Error: err.Error()}
		c.setStatus(name, status)
		return status
	}
	c.mu.Lock()
	c.sessions[name] = session
	c.status[name] = Status{Status: "connected"}
	c.mu.Unlock()
	return Status{Status: "connected"}
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
		tools, err := ListTools(ctx, session)
		if err != nil {
			return nil, err
		}
		for _, def := range tools {
			result[ToolName(server, def.Name)] = AdaptedTool{Def: def, Session: session, Server: server}
		}
	}
	return result, nil
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
		command.Dir = filepath.Join(directory, cfg.Cwd)
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
