package config

import (
	"errors"
	"fmt"
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
	return validateRouting(c.Servers)
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
	if s.SubagentDepth < 0 || s.SubscriberCapacity < 0 {
		return fmt.Errorf("server %q: limits must not be negative", s.Name)
	}
	if s.TLS != nil && (s.TLS.CertFile == "" || s.TLS.KeyFile == "") {
		return fmt.Errorf("server %q: tls requires both certFile and keyFile", s.Name)
	}
	if err := ValidateMCP(s.MCP); err != nil {
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
	if s.Auth != nil {
		if s.Auth.Bearer == "" && s.Auth.Password == "" {
			return errors.New("auth requires bearer or a password (username defaults to opencode)")
		}
		if s.Kind == "a2a" && s.Auth.Bearer == "" {
			return errors.New("a2a auth is bearer-only")
		}
	}
	if !IsLoopback(s.Listen) && !s.AllowInsecure && s.Auth == nil {
		return errors.New("refusing to bind a non-loopback address without auth; set auth, bind loopback, or allowInsecure")
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
