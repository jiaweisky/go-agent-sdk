package config

import "fmt"

// listenerGroup accumulates the servers that share one listen address and
// enforces the routing rules that span more than one server. Per-server routing
// fields (Host, PathPrefix, TLS shape) are checked by Server.Validate; this type
// owns the cross-server invariants: one root server, unique hosts/path prefixes,
// one default A2A entry, and consistent TLS.
type listenerGroup struct {
	root         string
	hosts        map[string]string
	pathPrefixes map[string]string
	defaultA2A   string
	tls          *TLS
	tlsOwner     string
	tlsSet       bool
}

func newListenerGroup() *listenerGroup {
	return &listenerGroup{hosts: map[string]string{}, pathPrefixes: map[string]string{}}
}

// addServer records one server in the group and rejects a cross-server conflict.
func (g *listenerGroup) addServer(server Server) error {
	if err := g.addRoot(server); err != nil {
		return err
	}

	if err := g.addHost(server); err != nil {
		return err
	}

	if err := g.addPathPrefix(server); err != nil {
		return err
	}

	if err := g.addDefault(server); err != nil {
		return err
	}

	return g.addTLS(server)
}

// addRoot records the group's root server (one without host or pathPrefix).
func (g *listenerGroup) addRoot(server Server) error {
	if server.Host != "" || server.PathPrefix != "" {
		return nil
	}

	if g.root != "" {
		return fmt.Errorf(
			"listen %q: %q and %q both serve the root; "+
				"at most one server may omit host/pathPrefix",
			server.Listen, g.root, server.Name)
	}

	g.root = server.Name

	return nil
}

// addHost records a virtual host and rejects duplicates.
func (g *listenerGroup) addHost(server Server) error {
	if server.Host == "" {
		return nil
	}

	host := NormalizeHost(server.Host)
	if prev, ok := g.hosts[host]; ok {
		return fmt.Errorf(
			"listen %q: host %q used by both %q and %q",
			server.Listen, host, prev, server.Name)
	}

	g.hosts[host] = server.Name

	return nil
}

// addPathPrefix records a URL path prefix and rejects duplicates.
func (g *listenerGroup) addPathPrefix(server Server) error {
	if server.PathPrefix == "" {
		return nil
	}

	pathPrefix := NormalizePathPrefix(server.PathPrefix)
	if prev, ok := g.pathPrefixes[pathPrefix]; ok {
		return fmt.Errorf(
			"listen %q: pathPrefix %q used by both %q and %q",
			server.Listen, pathPrefix, prev, server.Name)
	}

	g.pathPrefixes[pathPrefix] = server.Name

	return nil
}

// addDefault records the entry that owns the origin-root card. The caller has
// already rejected Default on non-A2A servers.
func (g *listenerGroup) addDefault(server Server) error {
	if !server.Default {
		return nil
	}

	if g.defaultA2A != "" {
		return fmt.Errorf(
			"listen %q: %q and %q both set default",
			server.Listen, g.defaultA2A, server.Name)
	}

	g.defaultA2A = server.Name

	return nil
}

// addTLS requires every server on a listener to use the same TLS settings.
func (g *listenerGroup) addTLS(server Server) error {
	if !g.tlsSet {
		g.tls, g.tlsOwner, g.tlsSet = server.TLS, server.Name, true
		return nil
	}

	if !tlsEqual(g.tls, server.TLS) {
		return fmt.Errorf(
			"listen %q: %q and %q must use the same tls",
			server.Listen, g.tlsOwner, server.Name)
	}

	return nil
}

// validateListenerGroups groups servers by normalized listen address and
// checks that each group is distinguishable and consistent: one root server,
// unique hosts and path prefixes, one default entry, and consistent TLS.
func validateListenerGroups(servers []Server) error {
	groups := make(map[string]*listenerGroup)

	for _, server := range servers {
		listen := normalizeListenAddress(server.Listen)
		group := groups[listen]

		if group == nil {
			group = newListenerGroup()
			groups[listen] = group
		}

		if err := group.addServer(server); err != nil {
			return err
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
