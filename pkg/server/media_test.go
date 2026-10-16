package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

type fakeMediaUploader struct{ ref schema.Reference }

func (f fakeMediaUploader) Upload(context.Context, schema.MediaUpload) (schema.Reference, error) {
	return f.ref, nil
}

func TestSessionFileUpload(t *testing.T) {
	ts := newTestServer(t, Options{
		MediaUploader: func(schema.ProviderID) schema.MediaUploader {
			return fakeMediaUploader{ref: schema.Reference{Source: "file", Value: "file-1"}}
		},
	})
	id := createSession(t, ts)

	body := `{"providerID":"p","mime":"image/png","name":"a.png","data":"` +
		base64.StdEncoding.EncodeToString([]byte{1, 2, 3}) + `"}`
	resp := doJSON(t, http.MethodPost, ts.URL+"/api/session/"+id+"/file", body, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var out struct {
		Data struct {
			Source string `json:"source"`
			ID     string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Data.Source != "file" || out.Data.ID != "file-1" {
		t.Fatalf("ref = %+v", out.Data)
	}
}

func TestSessionFileUploadUnconfigured(t *testing.T) {
	ts := newTestServer(t, Options{})
	id := createSession(t, ts)
	resp := doJSON(t, http.MethodPost, ts.URL+"/api/session/"+id+"/file", `{"data":""}`, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotImplemented {
		t.Fatalf("status = %d, want 501", resp.StatusCode)
	}
}
