package config

import (
	"errors"
	"fmt"

	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

// A2APeer declares a remote A2A agent (an A2A peer/server) that this server
// reaches as a client. It is registered as an agent, so the local model can
// delegate to it with the task tool (a subagent) or run it as a primary agent.
type A2APeer struct {
	// Name is the agent id it is registered under.
	Name string `yaml:"name"`
	// Endpoint is the remote A2A base URL (card discovery root). With no path
	// (or "/") the SDK fetches /.well-known/agent-card.json; a non-root path is
	// treated by the card resolver as a complete card URL, so use CardURL for
	// that case instead. It is ignored when CardURL is set.
	Endpoint string `yaml:"endpoint"`
	// CardURL overrides the card location with a full card URL (custom path,
	// proxy, or file://). When set, discovery uses this URL and Endpoint is
	// ignored.
	CardURL string `yaml:"cardURL"`
	// Headers are extra request headers (for example authorization).
	Headers map[string]string `yaml:"headers"`
	// Description is advertised to the model as the subagent's purpose.
	Description string `yaml:"description"`
	// Mode is primary, subagent or all. Empty defaults to subagent, so the peer
	// is delegatable via the task tool.
	Mode string `yaml:"mode"`
}

// ModeOrDefault is the peer's mode, defaulting to subagent so it is delegatable
// via the task tool.
func (p A2APeer) ModeOrDefault() string {
	if p.Mode == "" {
		return schema.AgentModeSubagent
	}
	return p.Mode
}

// validate checks the peer's discovery target and mode. Name uniqueness is a
// collection-level concern handled by ValidateA2APeers.
func (p A2APeer) validate() error {
	if err := p.validateTarget(); err != nil {
		return err
	}
	return p.validateMode()
}

// validateTarget checks where the remote card is discovered: an endpoint (the
// SDK fetches /.well-known/agent-card.json under it) or a direct card URL. Both
// may be set; CardURL is preferred and Endpoint is then ignored.
func (p A2APeer) validateTarget() error {
	if p.Endpoint == "" && p.CardURL == "" {
		return fmt.Errorf("peer %q: endpoint or cardURL is required", p.Name)
	}
	return nil
}

// validateMode checks the optional primary/subagent/all mode.
func (p A2APeer) validateMode() error {
	switch p.Mode {
	case "", schema.AgentModePrimary, schema.AgentModeSubagent, schema.AgentModeAll:
		return nil
	}
	return fmt.Errorf("peer %q: mode must be primary, subagent or all, got %q", p.Name, p.Mode)
}

// ValidateA2APeers checks each A2A peer's name, discovery target and mode, and
// rejects duplicate names.
func ValidateA2APeers(peers []A2APeer) error {
	seen := make(map[string]bool, len(peers))

	for _, peer := range peers {
		if err := validateA2APeerName(peer.Name, seen); err != nil {
			return err
		}
		if err := peer.validate(); err != nil {
			return err
		}
	}

	return nil
}

// validateA2APeerName checks an A2A peer name and records it for duplicate
// detection.
func validateA2APeerName(name string, seen map[string]bool) error {
	if name == "" {
		return errors.New("peers: each A2A peer requires a name")
	}

	if seen[name] {
		return fmt.Errorf("peer %q: duplicate name", name)
	}

	seen[name] = true

	return nil
}
