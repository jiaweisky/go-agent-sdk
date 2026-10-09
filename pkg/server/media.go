package server

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"time"

	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

type mediaRefDTO struct {
	Source    string  `json:"source"`
	ID        string  `json:"id"`
	ExpiresAt *string `json:"expiresAt,omitempty"`
}

// handleSessionFile uploads inline media to the session model's provider via the
// configured host MediaUploader and returns the resolved reference, which the
// caller then sends as a file-source attachment. The provider defaults to
// the session's model provider; a caller may override it.
func (s *Server) handleSessionFile(w http.ResponseWriter, r *http.Request) {
	if s.opts.MediaUploader == nil {
		writeError(w, http.StatusNotImplemented, "unsupported", "no media uploader is configured")
		return
	}
	info, err := s.client.GetSession(r.Context(), r.PathValue("sessionID"))
	if err != nil {
		s.writeSDKError(w, err)
		return
	}
	var body struct {
		ProviderID string `json:"providerID"`
		Mime       string `json:"mime"`
		Name       string `json:"name"`
		Data       string `json:"data"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	provider := schema.ProviderID(body.ProviderID)
	if provider == "" && info.Model != nil {
		provider = info.Model.ProviderID
	}
	uploader := s.opts.MediaUploader(provider)
	if uploader == nil {
		writeError(w, http.StatusBadRequest, "unsupported", fmt.Sprintf("no uploader for provider %q", provider))
		return
	}
	data, err := base64.StdEncoding.DecodeString(body.Data)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "data must be base64")
		return
	}
	var name *string
	if body.Name != "" {
		name = &body.Name
	}
	ref, err := uploader.Upload(r.Context(), schema.MediaUpload{Mime: body.Mime, Name: name, Data: data})
	if err != nil {
		s.writeSDKError(w, err)
		return
	}
	dto := mediaRefDTO{Source: ref.Source, ID: ref.ID}
	if ref.ExpiresAt != nil {
		expires := ref.ExpiresAt.Format(time.RFC3339)
		dto.ExpiresAt = &expires
	}
	writeData(w, http.StatusOK, dto)
}
