package config

import (
	"errors"
	"fmt"
	"path/filepath"
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
	if err := validateServers(c.Servers, catalog); err != nil {
		return err
	}
	if err := c.Control.Validate(); err != nil {
		return err
	}
	if err := c.Wrap.Validate(); err != nil {
		return err
	}
	if err := validateRouting(c.Servers); err != nil {
		return err
	}
	return validateInstanceSeparation(c.Servers)
}

// validateServers checks each server and rejects duplicate names.
func validateServers(servers []Server, catalog Catalog) error {
	names := make(map[string]bool, len(servers))
	for _, s := range servers {
		if err := validateServerName(s, names); err != nil {
			return err
		}
		if err := s.Validate(catalog); err != nil {
			return err
		}
	}
	return nil
}

// validateServerName checks one server's name and records it for duplicate
// detection.
func validateServerName(s Server, seen map[string]bool) error {
	if s.Name == "" {
		return errors.New("server name is required")
	}
	if seen[s.Name] {
		return fmt.Errorf("duplicate server name %q", s.Name)
	}
	seen[s.Name] = true
	return nil
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
