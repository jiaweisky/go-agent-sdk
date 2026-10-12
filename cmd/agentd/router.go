package main

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/a2aproject/a2a-go/v2/a2a"

	config "github.com/jiaweisky/go-agent-sdk/cmd/agentd/config"
)

// router dispatches one listener's requests to its servers: by Host first, then
// the longest matching Mount, then the root server. It also serves the
// origin-root discovery endpoints.
type router struct {
	hosts  []*builtServer // entries with Host set
	mounts []*builtServer // entries with Mount set
	root   *builtServer   // entry with neither Host nor Mount
	a2a    []*builtServer // every A2A entry (for the aggregate)
	defA2A *builtServer   // the A2A entry that owns the origin-root card
}

func newRouter(entries []*builtServer) http.Handler {
	r := &router{}
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
