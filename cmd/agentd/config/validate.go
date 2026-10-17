package config

import (
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"slices"
	"strings"
)

// Validate reports configuration that cannot produce a running set of servers.
func (c Config) Validate() error {
	if len(c.Models) == 0 {
		return errors.New("models is required")
	}
	catalog, err := c.Catalog()
	if err != nil {
		return fmt.Errorf("models: %w", err)
	}
	if len(c.Servers) == 0 {
		return errors.New("at least one server is required")
	}
	names := map[string]bool{}
	for _, s := range c.Servers {
		if s.Name == "" {
			return errors.New("server name is required")
		}
		if names[s.Name] {
			return fmt.Errorf("duplicate server name %q", s.Name)
		}
		names[s.Name] = true
		if err := validateServer(s, catalog); err != nil {
			return err
		}
	}
	if err := validateControl(c.Control); err != nil {
		return err
	}
	if err := validateWrap(c.Wrap); err != nil {
		return err
	}
	if err := validateRouting(c.Servers); err != nil {
		return err
	}
	return validateInstanceSeparation(c.Servers)
}

// validateInstanceSeparation enforces the hard constraint that each server has
// its own state root: a Root cannot be shared (sqlite and snapshots would
// collide, and one server's session would be invisible to the other).
func validateInstanceSeparation(servers []Server) error {
	roots := map[string]string{}
	for _, s := range servers {
		root := resolvePath(s.Root)
		if prev, ok := roots[root]; ok {
			return fmt.Errorf("servers %q and %q share the state root %q; each server needs its own root", prev, s.Name, s.Root)
		}
		roots[root] = s.Name
	}
	return nil
}

// resolvePath normalizes a path for comparison; an empty path stays empty.
func resolvePath(path string) string {
	if path == "" {
		return ""
	}
	if abs, err := filepath.Abs(path); err == nil {
		return filepath.Clean(abs)
	}
	return filepath.Clean(path)
}

// validateWrap checks the optional whole-process sandbox configuration.
func validateWrap(w *WrapConfig) error {
	if w == nil {
		return nil
	}
	if w.Process == "" {
		return errors.New(`wrap.process is required (use "none" to disable)`)
	}
	switch w.FailurePolicy {
	case "", "fail-closed", "fail-open":
	default:
		return fmt.Errorf("wrap.failurePolicy must be fail-closed or fail-open, got %q", w.FailurePolicy)
	}
	return ValidateSandbox(&SandboxConfig{
		Process:       w.Process,
		FailurePolicy: w.FailurePolicy,
		SandboxPolicy: w.SandboxPolicy,
	})
}

// validateControl checks the optional admin API configuration.
func validateControl(c *ControlConfig) error {
	if c == nil {
		return nil
	}
	if c.Listen == "" {
		return errors.New("control.listen is required")
	}
	if _, _, err := net.SplitHostPort(c.Listen); err != nil {
		return fmt.Errorf("control.listen: %w", err)
	}
	// The admin API can copy arbitrary directories and spawn agents, so a
	// non-loopback bind must be authenticated (same guard as a served endpoint).
	if !IsLoopback(c.Listen) && (c.Auth == nil || c.Auth.Bearer == "") {
		return errors.New("control.listen on a non-loopback address requires control.auth.bearer")
	}
	if c.MaxLeases < 0 {
		return errors.New("control.maxLeases must not be negative")
	}
	if c.ProjectListen != "" {
		if _, _, err := net.SplitHostPort(c.ProjectListen); err != nil {
			return fmt.Errorf("control.projectListen: %w", err)
		}
		// Dynamic project instances are served without auth, so they must stay
		// loopback (the host reaches them locally or proxies them).
		if !IsLoopback(c.ProjectListen) {
			return errors.New("control.projectListen must be loopback; dynamic instances are unauthenticated")
		}
	}
	return nil
}

// validateServer checks one server's fields against the global catalog.
func validateServer(s Server, catalog Catalog) error {
	if err := validateServerCommon(s, catalog); err != nil {
		return err
	}
	if s.Kind == "http" {
		return validateHTTPFields(s)
	}
	return validateA2AFields(s)
}

// validateServerCommon checks the fields both kinds share.
func validateServerCommon(s Server, catalog Catalog) error {
	switch {
	case s.Kind != "http" && s.Kind != "a2a":
		return fmt.Errorf("server %q: kind must be http or a2a", s.Name)
	case s.Listen == "":
		return fmt.Errorf("server %q: listen is required", s.Name)
	case s.Root == "":
		return fmt.Errorf("server %q: root is required", s.Name)
	case len(s.Agents) == 0:
		return fmt.Errorf("server %q: at least one agent is required", s.Name)
	case s.Mount != "" && !strings.HasPrefix(s.Mount, "/"):
		return fmt.Errorf("server %q: mount must start with /", s.Name)
	}
	if _, err := catalog.Subset(s.Models, s.DefaultModel); err != nil {
		return fmt.Errorf("server %q: %w", s.Name, err)
	}
	if s.SubagentDepth < 0 || s.SubscriberCapacity < 0 || s.MaxBodyBytes < 0 {
		return fmt.Errorf("server %q: limits must not be negative", s.Name)
	}
	if s.TLS != nil && (s.TLS.CertFile == "" || s.TLS.KeyFile == "") {
		return fmt.Errorf("server %q: tls requires both certFile and keyFile", s.Name)
	}
	if err := ValidateMCP(s.MCP); err != nil {
		return fmt.Errorf("server %q: %w", s.Name, err)
	}
	if err := ValidateSandbox(s.Sandbox); err != nil {
		return fmt.Errorf("server %q: %w", s.Name, err)
	}
	if err := ValidatePeers(s.Peers); err != nil {
		return fmt.Errorf("server %q: %w", s.Name, err)
	}
	if err := ValidateInboundMedia(s.Media); err != nil {
		return fmt.Errorf("server %q: %w", s.Name, err)
	}
	if err := ValidateStorage(s.Storage); err != nil {
		return fmt.Errorf("server %q: %w", s.Name, err)
	}
	if _, err := ReferenceSources(s.References); err != nil {
		return fmt.Errorf("server %q: %w", s.Name, err)
	}
	if err := validateAuth(s); err != nil {
		return fmt.Errorf("server %q: %w", s.Name, err)
	}
	if _, err := KeepAliveDuration(s.KeepAlive); err != nil {
		return fmt.Errorf("server %q: %w", s.Name, err)
	}
	switch s.Questions {
	case "", "park", "decline":
	default:
		return fmt.Errorf("server %q: questions must be park or decline, got %q", s.Name, s.Questions)
	}
	if err := ValidateIsolation(s.Name, s.WorkingDirectory, s.Isolation); err != nil {
		return err
	}
	return validateSandbox(s)
}

// validateSandbox checks the command-execution seam selection.
func validateSandbox(s Server) error {
	if err := validateSandboxConfig(s.Sandbox); err != nil {
		return fmt.Errorf("server %q: %w", s.Name, err)
	}
	return nil
}

// validateSandboxConfig checks a command-execution seam selection; it is shared
// by a configured server and a control-API project request.
func validateSandboxConfig(sandbox *SandboxConfig) error {
	if sandbox == nil {
		return nil
	}
	if sandbox.Process == "" {
		return errors.New("sandbox.process is required")
	}
	switch sandbox.FailurePolicy {
	case "", "fail-closed", "fail-open":
	default:
		return fmt.Errorf("sandbox.failurePolicy must be fail-closed or fail-open, got %q", sandbox.FailurePolicy)
	}
	return nil
}

// ValidateIsolation checks a server's per-view isolation configuration. It is
// exported so the control API validates a dynamic project's isolation exactly
// like a configured server's.
func ValidateIsolation(name, workingDirectory string, iso *IsolationConfig) error {
	if iso == nil {
		return nil
	}
	mode := iso.Mode
	switch mode {
	case "", "none":
		return nil
	case "copy", "worktree":
		if iso.BaseDir == "" {
			return fmt.Errorf("server %q: isolation.baseDir is required for mode %s", name, mode)
		}
	case "overlay":
		if iso.BaseDir == "" {
			return fmt.Errorf("server %q: isolation.baseDir is required for mode overlay", name)
		}
		if !iso.BaseReadOnly {
			return fmt.Errorf("server %q: isolation.baseReadOnly is required for mode overlay", name)
		}
		if iso.Cow != nil {
			switch iso.Cow.Mode {
			case "", "auto", "reflink", "clonefile":
			default:
				return fmt.Errorf("server %q: isolation.cow.mode must be auto, reflink or clonefile, got %q", name, iso.Cow.Mode)
			}
		}
	case "container", "remote":
		return fmt.Errorf("server %q: isolation.mode %s is external; such an instance runs its own agentd and is provisioned by the host", name, mode)
	default:
		return fmt.Errorf("server %q: isolation.mode must be none, copy, worktree or overlay, got %q", name, mode)
	}
	if workingDirectory == "" {
		return fmt.Errorf("server %q: isolation mode %s requires workingDirectory", name, mode)
	}
	return nil
}

// validateHTTPFields rejects A2A-only fields and checks the HTTP tuning.
func validateHTTPFields(s Server) error {
	if s.BaseURL != "" || len(s.Cards) > 0 || s.InboundMedia != "" || s.Default {
		return fmt.Errorf("server %q: baseURL, cards, inboundMedia and default are a2a-only", s.Name)
	}
	if s.InteractionFallback != "" && s.InteractionFallback != "reject" && s.InteractionFallback != "allow" {
		return fmt.Errorf("server %q: interactionFallback must be reject or allow", s.Name)
	}
	if _, err := InteractionTimeout(s.InteractionTimeout); err != nil {
		return fmt.Errorf("server %q: %w", s.Name, err)
	}
	return nil
}

// validateA2AFields rejects HTTP-only fields and checks inbound media and cards.
func validateA2AFields(s Server) error {
	if s.InteractionTimeout != "" || s.InteractionFallback != "" || s.SubscriberCapacity != 0 || len(s.PublicRoutes) > 0 || len(s.CORS) > 0 {
		return fmt.Errorf("server %q: interactionTimeout, interactionFallback, subscriberCapacity, publicRoutes and cors are http-only", s.Name)
	}
	switch s.InboundMedia {
	case "", "spool", "inline":
	default:
		return fmt.Errorf("server %q: inboundMedia must be spool or inline, got %q", s.Name, s.InboundMedia)
	}
	return validateAgentCards(s)
}

// validateAgentCards enforces that every exposed A2A agent carries an
// operator-authored card. The card is the discovery contract a remote client
// fetches first, so its name and description must be explicit rather than
// derived from the agent's internal definition; skills stay optional.
func validateAgentCards(s Server) error {
	if err := validateCards(s.Cards); err != nil {
		return fmt.Errorf("server %q: %w", s.Name, err)
	}
	for agent := range s.Cards {
		if !slices.Contains(s.Agents, agent) {
			return fmt.Errorf("server %q: card for %q, which is not an exposed agent", s.Name, agent)
		}
	}
	for _, agent := range s.Agents {
		card, ok := s.Cards[agent]
		if !ok {
			return fmt.Errorf("server %q: agent %q requires a card with a name and description", s.Name, agent)
		}
		if card.Name == "" {
			return fmt.Errorf("server %q: agent %q card requires a name", s.Name, agent)
		}
		if card.Description == "" {
			return fmt.Errorf("server %q: agent %q card requires a description", s.Name, agent)
		}
	}
	return nil
}

func validateAuth(s Server) error {
	if err := validateAuthConfig(s.Kind, s.Auth); err != nil {
		return err
	}
	if !IsLoopback(s.Listen) && !s.AllowInsecure && s.Auth == nil {
		return errors.New("refusing to bind a non-loopback address without auth; set auth, bind loopback, or allowInsecure")
	}
	return nil
}

// validateAuthConfig checks an auth block's shape for a transport kind: a
// bearer token or a username/password, bearer-only for a2a. Nil is valid.
func validateAuthConfig(kind string, auth *AuthConfig) error {
	if auth == nil {
		return nil
	}
	if auth.Bearer == "" && auth.Password == "" {
		return errors.New("auth requires bearer or a password (username defaults to opencode)")
	}
	if kind == "a2a" && auth.Bearer == "" {
		return errors.New("a2a auth is bearer-only")
	}
	return nil
}

// validateRouting checks that servers sharing a listener are distinguishable
// and consistent.
func validateRouting(servers []Server) error {
	type listener struct {
		root     string
		hosts    map[string]string
		mounts   map[string]string
		defA2A   string
		tls      *TLS
		tlsOwner string
		tlsSet   bool
	}
	byListen := map[string]*listener{}
	for _, s := range servers {
		l := byListen[s.Listen]
		if l == nil {
			l = &listener{hosts: map[string]string{}, mounts: map[string]string{}}
			byListen[s.Listen] = l
		}
		switch {
		case s.Host == "" && s.Mount == "":
			if l.root != "" {
				return fmt.Errorf("listen %q: %q and %q both serve the root; at most one server may omit host/mount", s.Listen, l.root, s.Name)
			}
			l.root = s.Name
		}
		if s.Host != "" {
			host := NormalizeHost(s.Host)
			if prev, ok := l.hosts[host]; ok {
				return fmt.Errorf("listen %q: host %q used by both %q and %q", s.Listen, host, prev, s.Name)
			}
			l.hosts[host] = s.Name
		}
		if s.Mount != "" {
			mount := NormalizeMount(s.Mount)
			if prev, ok := l.mounts[mount]; ok {
				return fmt.Errorf("listen %q: mount %q used by both %q and %q", s.Listen, mount, prev, s.Name)
			}
			l.mounts[mount] = s.Name
		}
		if s.Default {
			if l.defA2A != "" {
				return fmt.Errorf("listen %q: %q and %q both set default", s.Listen, l.defA2A, s.Name)
			}
			l.defA2A = s.Name
		}
		if !l.tlsSet {
			l.tls, l.tlsOwner, l.tlsSet = s.TLS, s.Name, true
		} else if !tlsEqual(l.tls, s.TLS) {
			return fmt.Errorf("listen %q: %q and %q must use the same tls", s.Listen, l.tlsOwner, s.Name)
		}
	}
	return nil
}

// tlsEqual reports whether two listener TLS settings are identical.
func tlsEqual(a, b *TLS) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.CertFile == b.CertFile && a.KeyFile == b.KeyFile
}
