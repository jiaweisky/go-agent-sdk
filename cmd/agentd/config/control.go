package config

import (
	"errors"
	"fmt"
	"net"
)

// ControlConfig configures the optional control API: a project-instance
// lifecycle API that creates and destroys isolated instances at runtime.
type ControlConfig struct {
	// Listen is the control API bind address (required to enable it).
	Listen string `yaml:"listen"`
	// ProjectListen is the host:port dynamic project instances bind to; the
	// default (127.0.0.1:0) picks a free loopback port per instance.
	ProjectListen string `yaml:"projectListen"`
	// MaxLeases caps the number of live project instances; 0 (the default) is
	// unlimited. It bounds a control-API caller from exhausting resources.
	MaxLeases int `yaml:"maxLeases"`
	// Auth, when set, guards the control API.
	Auth *AuthConfig `yaml:"auth"`
}

// Validate checks the optional control API block. Nil is valid.
func (c *ControlConfig) Validate() error {
	if c == nil {
		return nil
	}

	if err := validateControlListen(c.Listen); err != nil {
		return err
	}

	// The control API can copy arbitrary directories and spawn agents, so a
	// non-loopback bind must be authenticated (same guard as a served endpoint).
	if err := c.Auth.Validate(); err != nil {
		return fmt.Errorf("control: %w", err)
	}

	if !IsLoopback(c.Listen) && c.Auth == nil {
		return errors.New("control.listen on a non-loopback address requires control.auth")
	}

	if c.MaxLeases < 0 {
		return errors.New("control.maxLeases must not be negative")
	}

	return validateProjectListen(c.ProjectListen)
}

// validateControlListen checks the control API's bind address.
func validateControlListen(listen string) error {
	if listen == "" {
		return errors.New("control.listen is required")
	}

	if _, _, err := net.SplitHostPort(listen); err != nil {
		return fmt.Errorf("control.listen: %w", err)
	}

	return nil
}

// validateProjectListen checks where dynamic project instances bind. They are
// served without auth, so they must stay loopback (the host reaches them
// locally or proxies them).
func validateProjectListen(listen string) error {
	if listen == "" {
		return nil
	}

	if _, _, err := net.SplitHostPort(listen); err != nil {
		return fmt.Errorf("control.projectListen: %w", err)
	}

	if !IsLoopback(listen) {
		return errors.New("control.projectListen must be loopback; dynamic instances are unauthenticated")
	}

	return nil
}
