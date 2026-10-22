package main

import (
	"encoding/json"
	"net"
	"net/http"
	"os"
	"strings"

	"github.com/a2aproject/a2a-go/v2/a2a"

	config "github.com/jiaweisky/go-agent-sdk/cmd/agentd/config"
)

// router dispatches one listener's requests to its servers: by Host first, then
// the longest matching Mount, then the root server. It also serves the
// origin-root discovery endpoints and the read-only config/info endpoints.
type router struct {
	hosts   []*builtServer // entries with Host set
	mounts  []*builtServer // entries with Mount set
	root    *builtServer   // entry with neither Host nor Mount
	a2a     []*builtServer // every A2A entry (for the aggregate)
	defA2A  *builtServer   // the A2A entry that owns the origin-root card
	entries []*builtServer // every entry on this listener

	introspect *introspectDoc // nil on a dynamic instance router
	addr       net.Addr       // this listener's bound address
}

func newRouter(entries []*builtServer, introspect *introspectDoc, addr net.Addr) http.Handler {
	r := &router{entries: entries, introspect: introspect, addr: addr}
	for _, e := range entries {
		if e.cfg.Host != "" {
			r.hosts = append(r.hosts, e)
		}
		if e.cfg.Mount != "" {
			r.mounts = append(r.mounts, e)
		}
		if e.cfg.Host == "" && e.cfg.Mount == "" {
			r.root = e
		}
		if e.cfg.Kind == "a2a" {
			r.a2a = append(r.a2a, e)
			if e.cfg.Default {
				r.defA2A = e
			}
		}
	}
	if r.defA2A == nil && len(r.a2a) > 0 {
		r.defA2A = r.a2a[0]
	}
	return r
}

func (r *router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if r.serveDiscovery(w, req) {
		return
	}
	if r.serveIntrospection(w, req) {
		return
	}
	if e := r.matchHost(req); e != nil {
		e.handler.ServeHTTP(w, req)
		return
	}
	if e, mount := r.matchMount(req.URL.Path); e != nil {
		http.StripPrefix(mount, e.handler).ServeHTTP(w, req)
		return
	}
	if r.root != nil {
		r.root.handler.ServeHTTP(w, req)
		return
	}
	http.NotFound(w, req)
}

func (r *router) matchHost(req *http.Request) *builtServer {
	host := requestHost(req)
	if host == "" {
		return nil
	}
	for _, e := range r.hosts {
		if config.NormalizeHost(e.cfg.Host) == host {
			return e
		}
	}
	return nil
}

func (r *router) matchMount(path string) (*builtServer, string) {
	best := (*builtServer)(nil)
	bestMount := ""
	for _, e := range r.mounts {
		mount := config.NormalizeMount(e.cfg.Mount)
		if !pathMatchesMount(path, mount) {
			continue
		}
		if len(mount) > len(bestMount) {
			best, bestMount = e, mount
		}
	}
	return best, bestMount
}

func pathMatchesMount(path, mount string) bool {
	return path == mount || strings.HasPrefix(path, mount+"/")
}

// serveDiscovery handles the origin-root discovery endpoints, reporting whether
// it served the request.
func (r *router) serveDiscovery(w http.ResponseWriter, req *http.Request) bool {
	if req.Method != http.MethodGet {
		return false
	}
	switch req.URL.Path {
	case "/.well-known/agent-card.json":
		if r.defA2A == nil {
			return false
		}
		cards := r.defA2A.cards(req)
		if len(cards) == 0 {
			return false
		}
		writeJSON(w, cards[0])
		return true
	case "/.well-known/agents.json":
		if len(r.a2a) == 0 {
			return false
		}
		cards := []*a2a.AgentCard{}
		for _, e := range r.a2a {
			cards = append(cards, e.cards(req)...)
		}
		writeJSON(w, cards)
		return true
	}
	return false
}

// serveIntrospection handles the read-only config/info endpoints
// (/api/config, /api/info), reporting whether it served the request. It is
// served on the listener's origin-root server and uses that server's
// credential; a publicRoutes entry is the only way to make it anonymous.
// Dynamic-instance routers have no introspection doc, so they never serve it.
func (r *router) serveIntrospection(w http.ResponseWriter, req *http.Request) bool {
	if r.introspect == nil {
		return false
	}
	path := req.URL.Path
	if path != "/api/config" && path != "/api/info" {
		return false
	}
	if req.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeIntrospectError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET is supported")
		return true
	}
	if !r.authorizeIntrospection(w, req) {
		return true
	}
	if path == "/api/config" {
		r.serveConfig(w, req)
	} else {
		r.serveInfo(w)
	}
	return true
}

// authorizeIntrospection enforces the origin-root server's credential (an
// explicit publicRoutes entry is the only way to skip it), writing 404/401 on
// failure.
func (r *router) authorizeIntrospection(w http.ResponseWriter, req *http.Request) bool {
	entry := r.root
	if entry == nil {
		writeIntrospectError(w, http.StatusNotFound, "not_found", "no server serves this listener")
		return false
	}
	if entry.cfg.Auth == nil || publicPath(entry.cfg.PublicRoutes, req.URL.Path) || authorized(req, entry.cfg.Auth) {
		return true
	}
	w.Header().Set("WWW-Authenticate", "Bearer")
	writeIntrospectError(w, http.StatusUnauthorized, "unauthorized", "authentication required")
	return false
}

func (r *router) serveConfig(w http.ResponseWriter, req *http.Request) {
	query := req.URL.Query()
	section := query.Get("section")
	if !validSection(section) {
		writeIntrospectError(w, http.StatusBadRequest, "bad_request", "section must be models, servers, control or wrap")
		return
	}
	sources := []sourceView{{Kind: "file", Path: r.introspect.path}}
	if len(r.introspect.env) > 0 {
		sources = append(sources, sourceView{Kind: "env", Expanded: r.introspect.env})
	}
	writeJSONStatus(w, http.StatusOK, configResponse{
		SchemaVersion: configSchemaVersion,
		Sources:       sources,
		Config:        buildConfigDocument(r.introspect.config, section, query.Get("server")),
	})
}

func (r *router) serveInfo(w http.ResponseWriter) {
	info := infoResponse{Version: r.introspect.version, PID: os.Getpid()}
	if r.addr != nil {
		info.Listen = r.addr.String()
	}
	for _, e := range r.entries {
		info.Servers = append(info.Servers, e.cfg.Name)
		if e.cfg.Kind != "a2a" || r.addr == nil {
			continue
		}
		base := publicBaseURL(e.cfg, r.addr)
		for _, path := range e.cardPaths {
			info.A2A = append(info.A2A, base+path)
		}
	}
	writeJSONStatus(w, http.StatusOK, info)
}

// requestHost returns the request's host (preferring X-Forwarded-Host behind a
// proxy), lowercased and without the port.
func requestHost(req *http.Request) string {
	host := req.Header.Get("X-Forwarded-Host")
	if host == "" {
		host = req.Host
	}
	if i := strings.IndexByte(host, ','); i >= 0 {
		host = host[:i]
	}
	return config.NormalizeHost(host)
}

func writeJSON(w http.ResponseWriter, v any) { writeJSONStatus(w, http.StatusOK, v) }

// writeJSONStatus writes v as JSON with an explicit status code.
func writeJSONStatus(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
