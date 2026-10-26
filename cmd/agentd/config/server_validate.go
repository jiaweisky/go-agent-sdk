package config

import (
	"errors"
	"fmt"
	"net"
	"path"
	"slices"
	"strconv"
	"strings"
	"unicode"
)

// Validate checks one server's fields against the global registry.
func (s Server) Validate(registry ModelRegistry) error {
	if err := validateServerCommon(s, registry); err != nil {
		return err
	}
	if s.Kind == KindHTTP {
		return validateHTTPFields(s)
	}
	return validateA2AFields(s)
}

// validateRoutingFields checks the per-server fields that select a route on a
// listener. Cross-server rules (duplicate hosts or path prefixes, one root
// server, and consistent TLS) are checked by validateListenerGroups.
func (s Server) validateRoutingFields() error {
	if err := s.validateHostField(); err != nil {
		return err
	}
	if err := s.validatePathPrefixField(); err != nil {
		return err
	}
	if s.TLS != nil && (s.TLS.CertFile == "" || s.TLS.KeyFile == "") {
		return fmt.Errorf("server %q: tls requires both certFile and keyFile", s.Name)
	}
	return nil
}

// validateHostField checks a virtual host used for request routing.
func (s Server) validateHostField() error {
	if s.Host == "" {
		return nil
	}
	if NormalizeHost(s.Host) == "" {
		return fmt.Errorf("server %q: host must not be empty", s.Name)
	}
	if invalidHostPort(s.Host) ||
		strings.ContainsAny(s.Host, `/\?#,@`) ||
		containsSpaceOrControl(s.Host) {
		return fmt.Errorf("server %q: host %q is invalid", s.Name, s.Host)
	}
	return nil
}

// validatePathPrefixField checks a URL path prefix used for request routing.
func (s Server) validatePathPrefixField() error {
	prefix := s.PathPrefix
	if prefix == "" {
		return nil
	}
	if !strings.HasPrefix(prefix, "/") {
		return fmt.Errorf("server %q: pathPrefix must start with /", s.Name)
	}
	normalized := NormalizePathPrefix(prefix)
	if normalized == "/" {
		return fmt.Errorf("server %q: pathPrefix must not be /; omit it to serve the listener root", s.Name)
	}
	if strings.ContainsAny(prefix, `?#\%`) || containsSpaceOrControl(prefix) {
		return fmt.Errorf("server %q: pathPrefix %q is invalid", s.Name, prefix)
	}
	if path.Clean(normalized) != normalized {
		return fmt.Errorf("server %q: pathPrefix %q is not a clean path", s.Name, prefix)
	}
	return nil
}

// validateServerCommon checks the fields every server shares, regardless of
// kind. Kind-specific fields are validated separately.
func validateServerCommon(s Server, registry ModelRegistry) error {
	if err := validateServerShape(s); err != nil {
		return err
	}

	if err := validateServerModels(s, registry); err != nil {
		return err
	}

	if err := validateServerComponents(s); err != nil {
		return err
	}

	return validateServerPolicy(s)
}

// validateServerShape checks the fields that describe the server itself.
func validateServerShape(s Server) error {
	switch {
	case s.Name == "":
		return errors.New("server name is required")
	case !s.Kind.Valid():
		return fmt.Errorf("server %q: kind must be %s or %s", s.Name, KindHTTP, KindA2A)
	case s.Listen == "":
		return fmt.Errorf("server %q: listen is required", s.Name)
	case s.Root == "":
		return fmt.Errorf("server %q: root is required", s.Name)
	case len(s.Agents) == 0:
		return fmt.Errorf("server %q: at least one agent is required", s.Name)
	case s.SubagentDepth < 0:
		return fmt.Errorf("server %q: subagentDepth must not be negative, got %d", s.Name, s.SubagentDepth)
	case s.SubscriberCapacity < 0:
		return fmt.Errorf("server %q: subscriberCapacity must not be negative, got %d", s.Name, s.SubscriberCapacity)
	case s.MaxBodyBytes < 0:
		return fmt.Errorf("server %q: maxBodyBytes must not be negative, got %d", s.Name, s.MaxBodyBytes)
	}
	if _, _, err := net.SplitHostPort(s.Listen); err != nil {
		return fmt.Errorf("server %q: listen: %w", s.Name, err)
	}
	return s.validateRoutingFields()
}

// invalidHostPort reports whether a host with a port has a malformed port, or
// whether a bracketed host is not an IP literal.
func invalidHostPort(host string) bool {
	if !strings.Contains(host, ":") {
		return false
	}
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		return net.ParseIP(host[1:len(host)-1]) == nil
	}
	if net.ParseIP(host) != nil {
		return false
	}
	_, port, err := net.SplitHostPort(host)
	if err != nil {
		return true
	}
	_, err = strconv.Atoi(port)
	return err != nil
}

// containsSpaceOrControl reports whether a value contains whitespace or a
// control character.
func containsSpaceOrControl(value string) bool {
	for _, r := range value {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return true
		}
	}
	return false
}

// validateServerModels checks the server's model subset against the registry.
func validateServerModels(s Server, registry ModelRegistry) error {
	if _, err := registry.Subset(s.Models, s.DefaultModel); err != nil {
		return fmt.Errorf("server %q: %w", s.Name, err)
	}
	return nil
}

// validateServerComponents checks the optional blocks a server can attach.
func validateServerComponents(s Server) error {
	if err := ValidateMCP(s.MCP); err != nil {
		return fmt.Errorf("server %q: %w", s.Name, err)
	}
	if err := s.Sandbox.Validate(); err != nil {
		return fmt.Errorf("server %q: %w", s.Name, err)
	}
	if err := ValidateA2APeers(s.Peers); err != nil {
		return fmt.Errorf("server %q: %w", s.Name, err)
	}

	if err := s.Media.Validate(); err != nil {
		return fmt.Errorf("server %q: %w", s.Name, err)
	}

	if err := ValidateReferences(s.References); err != nil {
		return fmt.Errorf("server %q: %w", s.Name, err)
	}
	return nil
}

// validateServerPolicy checks auth, lifetime, interaction policy and isolation.
func validateServerPolicy(s Server) error {
	if err := validateServerAuth(s); err != nil {
		return fmt.Errorf("server %q: %w", s.Name, err)
	}
	if _, err := KeepAliveDuration(s.KeepAlive); err != nil {
		return fmt.Errorf("server %q: %w", s.Name, err)
	}
	if !s.Questions.Valid() {
		return fmt.Errorf("server %q: questions must be park or decline, got %q", s.Name, s.Questions)
	}
	if err := s.Isolation.Validate(s.WorkingDirectory); err != nil {
		return fmt.Errorf("server %q: %w", s.Name, err)
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

// hasHTTPOnlyFields reports whether an A2A server sets a field that belongs to
// the HTTP transport.
func hasHTTPOnlyFields(s Server) bool {
	return s.InteractionTimeout != "" || s.InteractionFallback != "" ||
		s.SubscriberCapacity != 0 || len(s.PublicRoutes) > 0 || len(s.CORS) > 0
}

// validateA2AFields rejects HTTP-only fields and checks inbound media and cards.
func validateA2AFields(s Server) error {
	if hasHTTPOnlyFields(s) {
		return fmt.Errorf(
			"server %q: interactionTimeout, interactionFallback, "+
				"subscriberCapacity, publicRoutes and cors are http-only", s.Name)
	}
	if !s.InboundMedia.Valid() {
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
	if err := validateCardOwners(s); err != nil {
		return err
	}
	return validateExposedAgentCards(s)
}

// validateCardOwners rejects cards for agents the server does not expose.
func validateCardOwners(s Server) error {
	for agent := range s.Cards {
		if !slices.Contains(s.Agents, agent) {
			return fmt.Errorf("server %q: card for %q, which is not an exposed agent", s.Name, agent)
		}
	}
	return nil
}

// validateExposedAgentCards requires every exposed agent to have a named and
// described card.
func validateExposedAgentCards(s Server) error {
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

// validateServerAuth checks a server's auth block and the non-loopback bind guard.
func validateServerAuth(s Server) error {
	if err := s.Auth.Validate(); err != nil {
		return err
	}
	if !IsLoopback(s.Listen) && !s.AllowInsecure && s.Auth == nil {
		return errors.New("refusing to bind a non-loopback address without auth; set auth, bind loopback, or allowInsecure")
	}
	return nil
}
