package config

import (
	"fmt"
	"net"
	"strings"
)

// NormalizePathPrefix trims a trailing slash, keeping a single leading one.
func NormalizePathPrefix(pathPrefix string) string {
	if pathPrefix == "" {
		return ""
	}
	return "/" + strings.Trim(pathPrefix, "/")
}

// NormalizeHost lowercases a host and drops any port, so configured and request
// hosts compare consistently.
func NormalizeHost(host string) string {
	host = strings.TrimSpace(host)
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return strings.ToLower(host)
}

// IsLoopback reports whether listen binds only the loopback interface.
func IsLoopback(listen string) bool {
	host, _, err := net.SplitHostPort(listen)
	if err != nil {
		return false
	}
	if host == "" {
		return false // empty host binds all interfaces
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// listenGroup accumulates the servers that share one listen address.
type listenGroup struct {
	root         string
	hosts        map[string]string
	pathPrefixes map[string]string
	defaultA2A   string
	tls          *TLS
	tlsOwner     string
	tlsSet       bool
}

func newListenGroup() *listenGroup {
	return &listenGroup{hosts: map[string]string{}, pathPrefixes: map[string]string{}}
}

// validateRouting checks that servers sharing a listener are distinguishable
// and consistent.
func validateRouting(servers []Server) error {
	groups := make(map[string]*listenGroup)
	for _, s := range servers {
		g := groups[s.Listen]
		if g == nil {
			g = newListenGroup()
			groups[s.Listen] = g
		}
		if err := g.add(s); err != nil {
			return err
		}
	}
	return nil
}

// add records one server in the group.
func (g *listenGroup) add(s Server) error {
	if err := g.addRoot(s); err != nil {
		return err
	}
	if err := g.addHost(s); err != nil {
		return err
	}
	if err := g.addPathPrefix(s); err != nil {
		return err
	}
	if err := g.addDefault(s); err != nil {
		return err
	}
	return g.addTLS(s)
}

// addRoot records the group's root server (one without host or pathPrefix).
func (g *listenGroup) addRoot(s Server) error {
	if s.Host != "" || s.PathPrefix != "" {
		return nil
	}
	if g.root != "" {
		return fmt.Errorf(
			"listen %q: %q and %q both serve the root; "+
				"at most one server may omit host/pathPrefix",
			s.Listen, g.root, s.Name)
	}
	g.root = s.Name
	return nil
}

// addHost records a virtual host and rejects duplicates.
func (g *listenGroup) addHost(s Server) error {
	if s.Host == "" {
		return nil
	}
	host := NormalizeHost(s.Host)
	if prev, ok := g.hosts[host]; ok {
		return fmt.Errorf("listen %q: host %q used by both %q and %q", s.Listen, host, prev, s.Name)
	}
	g.hosts[host] = s.Name
	return nil
}

// addPathPrefix records a URL path prefix and rejects duplicates.
func (g *listenGroup) addPathPrefix(s Server) error {
	if s.PathPrefix == "" {
		return nil
	}
	pathPrefix := NormalizePathPrefix(s.PathPrefix)
	if prev, ok := g.pathPrefixes[pathPrefix]; ok {
		return fmt.Errorf("listen %q: pathPrefix %q used by both %q and %q", s.Listen, pathPrefix, prev, s.Name)
	}
	g.pathPrefixes[pathPrefix] = s.Name
	return nil
}

// addDefault records the A2A entry that owns the origin-root card.
func (g *listenGroup) addDefault(s Server) error {
	if !s.Default {
		return nil
	}
	if g.defaultA2A != "" {
		return fmt.Errorf("listen %q: %q and %q both set default", s.Listen, g.defaultA2A, s.Name)
	}
	g.defaultA2A = s.Name
	return nil
}

// addTLS requires every server on a listener to use the same TLS settings.
func (g *listenGroup) addTLS(s Server) error {
	if !g.tlsSet {
		g.tls, g.tlsOwner, g.tlsSet = s.TLS, s.Name, true
		return nil
	}
	if !tlsEqual(g.tls, s.TLS) {
		return fmt.Errorf("listen %q: %q and %q must use the same tls", s.Listen, g.tlsOwner, s.Name)
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
