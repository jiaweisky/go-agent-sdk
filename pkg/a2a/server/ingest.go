package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"

	"github.com/jiaweisky/go-agent-sdk/pkg/a2a/client"
	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

// StorageExtensionURI identifies the file-exchange extension advertised on every
// exposed agent's card (shared with the outbound client, which reads it to learn
// where to upload). A2A has no standard "source" field or upload API, so the
// ingest contract (schemes / media types / size limits / modes) lives there.
const StorageExtensionURI = client.StorageExtensionURI

// Ingest configures the A2A file-exchange edge: the contract advertised on each
// card plus the endpoints that accept a peer's out-of-band upload. A nil Ingest
// (or a nil Store) disables file exchange.
type Ingest struct {
	// Store backs the endpoints and the advertised reference target.
	Store schema.MediaStore
	// Schemes a peer may reference (e.g. "https").
	Schemes []string
	// MediaTypes allowed on ingest (patterns such as "image/*").
	MediaTypes []string
	// MaxInlineBytes is the largest inline A2A file part this server accepts (a
	// sender should reference anything larger).
	MaxInlineBytes int64
	// MaxBytes is the largest referenced file accepted.
	MaxBytes int64
}

// extension builds the card extension for one agent; ingestURL is the agent's own
// ingest endpoint (receiver-driven: the peer uploads here, then references it).
func (g *Ingest) extension(ingestURL string) a2a.AgentExtension {
	return a2a.AgentExtension{
		URI:         StorageExtensionURI,
		Description: "Large-file exchange over the server's media store",
		Params: map[string]any{
			"ingest": map[string]any{
				"schemes":        g.Schemes,
				"mediaTypes":     g.MediaTypes,
				"maxInlineBytes": g.MaxInlineBytes,
				"maxBytes":       g.MaxBytes,
				"modes": []map[string]any{
					{"mode": "reference", "upload": map[string]any{"url": ingestURL, "method": http.MethodPut}},
					{"mode": "broker", "request": ingestURL},
				},
			},
		},
	}
}

// ingestPath is where one agent's ingest endpoint is mounted.
func (s *Server) ingestPath(info schema.AgentInfo) string {
	return "/agents/" + string(info.ID) + "/storage/in/"
}

// ingestHandler accepts a peer's out-of-band upload (PUT → a url reference) and
// streams a stored object back (GET with Range support).
func (s *Server) ingestHandler(info schema.AgentInfo) http.Handler {
	store := s.opts.Ingest.Store
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPut:
			mime := contentType(r.Header.Get("Content-Type"))
			if !mediaTypeAllowed(s.opts.Ingest.MediaTypes, mime) {
				http.Error(w, fmt.Sprintf("unsupported media type %q; allowed: %s",
					mime, strings.Join(s.opts.Ingest.MediaTypes, ", ")), http.StatusUnsupportedMediaType)
				return
			}
			var body io.Reader = r.Body
			if limit := s.opts.Ingest.MaxBytes; limit > 0 {
				body = http.MaxBytesReader(w, r.Body, limit)
			}
			data, err := io.ReadAll(body)
			if err != nil {
				http.Error(w, maxBytesMessage(err, s.opts.Ingest.MaxBytes), http.StatusRequestEntityTooLarge)
				return
			}
			ref, err := store.Put(r.Context(), schema.MediaUpload{Mime: mime, Data: data})
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadGateway)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"reference": ref.Value})
		case http.MethodGet:
			ref := r.URL.Query().Get("ref")
			rc, err := store.Get(r.Context(), schema.Reference{Source: "url", Value: ref})
			if err != nil {
				status := http.StatusBadGateway
				if errors.Is(err, schema.ErrUnsupportedReference) {
					status = http.StatusNotFound
				}
				http.Error(w, err.Error(), status)
				return
			}
			defer rc.Close()
			data, err := io.ReadAll(rc)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadGateway)
				return
			}
			http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(data))
		case http.MethodPost:
			// Broker mode: issue a per-file upload target (a presigned slot).
			q := r.URL.Query()
			size, _ := strconv.ParseInt(q.Get("size"), 10, 64)
			target, err := store.Reserve(r.Context(), q.Get("name"), q.Get("mime"), size)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadGateway)
				return
			}
			if target.URL == "" {
				http.Error(w, "broker not supported", http.StatusNotImplemented)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"url": target.URL, "method": target.Method, "headers": target.Headers,
				"reference": target.Reference, "expires": target.Expires.UTC().Format(time.RFC3339),
			})
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
}

// contentType strips parameters (e.g. "; charset=...") from a Content-Type.
func contentType(value string) string {
	base, _, _ := strings.Cut(value, ";")
	return strings.ToLower(strings.TrimSpace(base))
}

// mediaTypeAllowed reports whether a media type matches one of the patterns
// ("image/*", "application/pdf", "*/*"). No patterns means "accept anything".
func mediaTypeAllowed(patterns []string, mime string) bool {
	if len(patterns) == 0 {
		return true
	}
	for _, pattern := range patterns {
		pattern = strings.ToLower(strings.TrimSpace(pattern))
		if pattern == "*/*" || pattern == mime {
			return true
		}
		if prefix, ok := strings.CutSuffix(pattern, "*"); ok && strings.HasPrefix(mime, prefix) {
			return true
		}
	}
	return false
}

// maxBytesMessage renders an over-limit error with the exact limit, so a client
// immediately learns the accepted size.
func maxBytesMessage(err error, limit int64) string {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		limit = tooLarge.Limit
	}
	return fmt.Sprintf("request body exceeds the %d-byte limit", limit)
}
