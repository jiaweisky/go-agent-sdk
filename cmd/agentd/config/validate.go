package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
)

// Validate reports configuration that cannot produce a running set of servers.
func (c Config) Validate() error {
	if err := validateProviderFileAPIs(c.ProviderFileAPIs); err != nil {
		return err
	}
	if err := validateModelProviderFileAPIs(c.Models, c.ProviderFileAPIs); err != nil {
		return err
	}
	registry, err := c.ModelRegistry()
	if errors.Is(err, errModelsRequired) {
		return err
	}
	if err != nil {
		return fmt.Errorf("models: %w", err)
	}

	if len(c.Servers) == 0 {
		return errors.New("at least one server is required")
	}
	if err := validateServers(c.Servers, registry, c.ProviderFileAPIs); err != nil {
		return err
	}

	if err := c.Control.Validate(); err != nil {
		return err
	}
	if err := validateControlListenOverlap(c.Control, c.Servers); err != nil {
		return err
	}
	if err := c.Wrap.Validate(); err != nil {
		return err
	}

	if err := validateListenerGroups(c.Servers); err != nil {
		return err
	}
	return validateInstanceSeparation(c.Servers)
}

// validateServers validates every server and rejects duplicate names.
func validateServers(servers []Server, registry ModelRegistry, profiles map[string]*ProviderFileAPI) error {
	names := make(map[string]bool, len(servers))
	for _, s := range servers {
		if err := validateServerName(s, names); err != nil {
			return err
		}
		if err := s.Validate(registry); err != nil {
			return err
		}
		if err := validateServerProviderFiles(s, registry, profiles); err != nil {
			return err
		}
		if err := validateServerDeliverySources(s); err != nil {
			return err
		}
	}
	return nil
}

// validateServerProviderFiles enforces the runtime providerID -> profile map.
func validateServerProviderFiles(s Server, registry ModelRegistry, profiles map[string]*ProviderFileAPI) error {
	ids := s.Models
	if len(ids) == 0 {
		ids = registry.IDs()
	}
	byProvider := map[string]string{}
	for _, id := range ids {
		entry, ok := registry.Entry(id)
		if !ok {
			continue
		}
		if entry.ProviderFileAPI == "" {
			if modelFileCapable(entry) {
				return fmt.Errorf("server %q: model %q declares a file source but has no providerFileAPI", s.Name, entry.ID)
			}
			continue
		}
		if entry.ProviderID == "" {
			return fmt.Errorf("server %q: model %q uses providerFileAPI %q but has no providerID", s.Name, entry.ID, entry.ProviderFileAPI)
		}
		if _, ok := profiles[entry.ProviderFileAPI]; !ok {
			return fmt.Errorf("server %q: model %q references unknown providerFileAPI %q", s.Name, entry.ID, entry.ProviderFileAPI)
		}
		if prev, ok := byProvider[entry.ProviderID]; ok && prev != entry.ProviderFileAPI {
			return fmt.Errorf("server %q: providerID %q is used with providerFileAPI %q and %q; assign a distinct providerID", s.Name, entry.ProviderID, prev, entry.ProviderFileAPI)
		}
		byProvider[entry.ProviderID] = entry.ProviderFileAPI
	}
	return nil
}

// validateServerDeliverySources rejects a preferred source that cannot run and
// has no fallback. Missing optional sources are otherwise reported at startup.
func validateServerDeliverySources(s Server) error {
	if s.Media == nil || s.Media.Delivery == nil {
		return nil
	}
	d := s.Media.Delivery
	if len(d.Prefer) != 1 {
		return nil
	}
	switch d.Prefer[0] {
	case "providerFile":
		if d.Spool == nil {
			return fmt.Errorf("server %q: media.delivery.prefer is file-only but there is no inline/url fallback or spool", s.Name)
		}
	case "url":
		if !d.AllowUploads || !urlHostFetchable(s.Media.Uploads) {
			return fmt.Errorf("server %q: media.delivery.prefer is url-only but media.uploads.urlHost is not provider-fetchable", s.Name)
		}
	}
	return nil
}

func modelFileCapable(m ModelConfig) bool {
	if m.Media == nil {
		return false
	}
	for _, forms := range m.Media.Forms {
		if slices.Contains(forms, "file") {
			return true
		}
	}
	return false
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
	type owner struct {
		name string
		path string
		info os.FileInfo
	}

	roots := map[string]string{}
	var existing []owner

	for _, s := range servers {
		root := resolvePath(s.Root)
		if prev, ok := roots[root]; ok {
			return fmt.Errorf("servers %q and %q share the state root %q; each server needs its own root", prev, s.Name, s.Root)
		}

		if info, err := os.Stat(s.Root); err == nil {
			for _, prev := range existing {
				if os.SameFile(info, prev.info) {
					return fmt.Errorf(
						"servers %q and %q share the state root (%q and %q); each server needs its own root",
						prev.name, s.Name, prev.path, s.Root)
				}
			}
			existing = append(existing, owner{name: s.Name, path: s.Root, info: info})
		}

		roots[root] = s.Name
	}
	return nil
}

// resolvePath normalizes a path for comparison: it absolutizes and cleans the
// path, resolves symlinks when possible, and folds case on Windows. An empty
// path stays empty.
func resolvePath(path string) string {
	if path == "" {
		return ""
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	path = filepath.Clean(path)
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = filepath.Clean(resolved)
	}
	if runtime.GOOS == "windows" {
		path = strings.ToLower(path)
	}
	return path
}
