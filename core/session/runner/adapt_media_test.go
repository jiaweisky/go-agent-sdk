package runner

import (
	"context"
	"encoding/base64"
	"strconv"
	"testing"

	"github.com/jiaweisky/go-agent-sdk/pkg/llm/protocols"
	llmschema "github.com/jiaweisky/go-agent-sdk/pkg/llm/schema"
	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

type fakeUploader struct {
	ref    schema.MediaRef
	called int
}

func (f *fakeUploader) Upload(context.Context, schema.MediaUpload) (schema.MediaRef, error) {
	f.called++
	return f.ref, nil
}

func adaptFile(uri string) schema.PromptFileAttachment {
	return schema.PromptFileAttachment{URI: uri, Mime: "image/png"}
}

func TestAdaptAttachmentUploadsInlineToFile(t *testing.T) {
	entry, ok := protocols.LookupProtocol("gemini")
	if !ok {
		t.Fatal("gemini protocol not registered")
	}
	uri := "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte{1, 2, 3})
	uploader := &fakeUploader{ref: schema.MediaRef{Source: "file", ID: "file-123"}}
	var decisions []string
	runner := &LLMRunner{
		OnError:         func(error) {},
		OnMediaDecision: func(d schema.MediaDecision) { decisions = append(decisions, d.Action) },
	}
	adapter := mediaAdapter{
		runner: runner, entry: entry, provider: "gemini",
		input: []string{"text", "image"}, sources: []llmschema.MediaSource{llmschema.MediaSourceFile},
		uploader: uploader,
	}

	// Model accepts only the file source: inline is infeasible, file feasible.
	got := adapter.adapt(context.Background(), "ses_1", adaptFile(uri))
	if uploader.called != 1 {
		t.Fatalf("upload called %d times, want 1", uploader.called)
	}
	if got.MediaSource != "file" || got.FileID != "file-123" || got.URI != "" {
		t.Fatalf("adapted file = %+v", got)
	}

	// A second identical part is served from the content-hash cache.
	again := adapter.adapt(context.Background(), "ses_1", adaptFile(uri))
	if uploader.called != 1 {
		t.Fatalf("upload called %d times after a cache hit, want 1", uploader.called)
	}
	if again.FileID != "file-123" {
		t.Fatalf("cached file = %+v", again)
	}
	if len(decisions) != 1 || decisions[0] != "upload" {
		t.Fatalf("decisions = %v, want one upload", decisions)
	}
}

func TestAdaptAttachmentKeepsFeasibleInline(t *testing.T) {
	entry, _ := protocols.LookupProtocol("gemini")
	uri := "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte{1, 2, 3})
	uploader := &fakeUploader{}
	adapter := mediaAdapter{runner: &LLMRunner{}, entry: entry, input: []string{"text", "image"}, uploader: uploader}

	got := adapter.adapt(context.Background(), "ses_1", adaptFile(uri))
	if uploader.called != 0 {
		t.Fatalf("upload should not be called for a feasible inline part")
	}
	if got.URI != uri || got.FileID != "" {
		t.Fatalf("file = %+v, want unchanged", got)
	}
}

// TestAttachmentFeasibleRoleGate covers the role axis: a protocol that
// only allows media in the "user" role rejects a "tool" part.
func TestAttachmentFeasibleRoleGate(t *testing.T) {
	entry := protocols.ProtocolEntry{Media: protocols.ProtocolMediaSupport{
		MIMEs:   map[string]struct{}{"image/png": {}},
		Sources: []protocols.MediaSource{protocols.MediaSourceInline},
		Roles:   []string{"user"},
	}}
	if mediaFeasibilityReason(entry.Media, nil, nil, "user", "image/png", protocols.MediaSourceInline) != "" {
		t.Fatal("a user part should be feasible")
	}
	if mediaFeasibilityReason(entry.Media, nil, nil, "tool", "image/png", protocols.MediaSourceInline) == "" {
		t.Fatal("a tool part should be rejected when only the user role is allowed")
	}
}

// TestFileWithRefHonorsSource verifies the adaptation result carries the source
// the uploader returned, not a hardcoded "file".
func TestFileWithRefHonorsSource(t *testing.T) {
	url := fileWithRef(schema.PromptFileAttachment{URI: "data:image/png;base64,x"}, schema.MediaRef{Source: "url", ID: "https://host/a.png"})
	if url.MediaSource != "url" || url.URI != "https://host/a.png" || url.FileID != "" {
		t.Fatalf("url ref = %+v", url)
	}
	file := fileWithRef(schema.PromptFileAttachment{URI: "data:image/png;base64,x"}, schema.MediaRef{ID: "file-1"})
	if file.MediaSource != "file" || file.FileID != "file-1" || file.URI != "" {
		t.Fatalf("file ref = %+v", file)
	}
}

// TestMediaUploadCacheEvicts verifies the dedup cache is FIFO-bounded.
func TestMediaUploadCacheEvicts(t *testing.T) {
	runner := &LLMRunner{}
	for i := 0; i < maxMediaCacheEntries+10; i++ {
		runner.storeUpload(strconv.Itoa(i), schema.MediaRef{Source: "file", ID: strconv.Itoa(i)})
	}
	runner.media.mu.Lock()
	size := len(runner.media.index)
	runner.media.mu.Unlock()
	if size != maxMediaCacheEntries {
		t.Fatalf("cache size = %d, want %d", size, maxMediaCacheEntries)
	}
	if _, ok := runner.cachedUpload("0"); ok {
		t.Fatal("the oldest entry should have been evicted")
	}
	if _, ok := runner.cachedUpload(strconv.Itoa(maxMediaCacheEntries + 9)); !ok {
		t.Fatal("the newest entry should be retained")
	}
}
