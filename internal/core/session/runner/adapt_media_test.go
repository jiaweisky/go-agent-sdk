package runner

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"image"
	"image/png"
	"io"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/jiaweisky/go-agent-sdk/pkg/core"
	"github.com/jiaweisky/go-agent-sdk/pkg/llm/protocols"
	llmschema "github.com/jiaweisky/go-agent-sdk/pkg/llm/schema"
	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

type fakeUploader struct {
	ref    schema.Reference
	called int
	last   schema.MediaUpload
}

func (f *fakeUploader) Upload(_ context.Context, media schema.MediaUpload) (schema.Reference, error) {
	f.called++
	f.last = media
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
	uploader := &fakeUploader{ref: schema.Reference{Source: "file", Value: "file-123"}}
	var decisions []string
	runner := &LLMRunner{
		OnError:         func(error) {},
		OnMediaDecision: func(d schema.MediaDecision) { decisions = append(decisions, d.Action) },
	}
	adapter := mediaAdapter{
		runner: runner, entry: entry, provider: "gemini",
		input: []string{"text", "image"}, forms: map[string][]llmschema.MediaSource{"image": {llmschema.MediaSourceFile}},
		uploader: uploader,
	}

	// Model accepts only the file source: inline is infeasible, file feasible.
	got, _ := adapter.adapt(context.Background(), "ses_1", adaptFile(uri))
	if uploader.called != 1 {
		t.Fatalf("upload called %d times, want 1", uploader.called)
	}
	if got.MediaSource != "file" || got.FileID != "file-123" || got.URI != "" {
		t.Fatalf("adapted file = %+v", got)
	}

	// A second identical part is served from the content-hash cache.
	again, _ := adapter.adapt(context.Background(), "ses_1", adaptFile(uri))
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

	got, _ := adapter.adapt(context.Background(), "ses_1", adaptFile(uri))
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
	url := fileWithRef(schema.PromptFileAttachment{URI: "data:image/png;base64,x"}, schema.Reference{Source: "url", Value: "https://host/a.png"})
	if url.MediaSource != "url" || url.URI != "https://host/a.png" || url.FileID != "" {
		t.Fatalf("url ref = %+v", url)
	}
	file := fileWithRef(schema.PromptFileAttachment{URI: "data:image/png;base64,x"}, schema.Reference{Value: "file-1"})
	if file.MediaSource != "file" || file.FileID != "file-1" || file.URI != "" {
		t.Fatalf("file ref = %+v", file)
	}
}

// TestMediaUploadCacheEvicts verifies the dedup cache is FIFO-bounded.
func TestMediaUploadCacheEvicts(t *testing.T) {
	runner := &LLMRunner{}
	for i := 0; i < maxMediaCacheEntries+10; i++ {
		runner.storeUpload(strconv.Itoa(i), schema.Reference{Source: "file", Value: strconv.Itoa(i)})
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

// noisePNG builds an incompressible image so downsampling must actually resize.
func noisePNG(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	rng := rand.New(rand.NewSource(7))
	for i := range img.Pix {
		img.Pix[i] = byte(rng.Intn(256))
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

func dataURL(mime string, data []byte) string {
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data)
}

// policyAdapter is a gemini adapter (which carries inline, url and file) with a
// delivery policy and no model-source restriction.
func policyAdapter(policy *core.MediaPolicy, uploader schema.MediaUploader) mediaAdapter {
	entry, _ := protocols.LookupProtocol("gemini")
	return mediaAdapter{
		runner:   &LLMRunner{OnError: func(error) {}},
		entry:    entry,
		provider: "gemini",
		input:    []string{"text", "image"},
		uploader: uploader,
		policy:   policy,
	}
}

func TestPolicyPrefersFileUpload(t *testing.T) {
	uploader := &fakeUploader{ref: schema.Reference{Source: "file", Value: "file-9"}}
	adapter := policyAdapter(&core.MediaPolicy{Upload: true, Prefer: []core.MediaPolicySource{core.MediaPolicySourceFile}}, uploader)

	got, note := adapter.adapt(context.Background(), "ses_1", adaptFile(dataURL("image/png", []byte{1, 2, 3})))
	if uploader.called != 1 {
		t.Fatalf("upload called %d times, want 1", uploader.called)
	}
	if note != "" || got == nil || got.MediaSource != "file" || got.FileID != "file-9" {
		t.Fatalf("adapted = %+v note=%q", got, note)
	}
}

func TestPolicyHonorsDeclaredInlineWhenItFits(t *testing.T) {
	uri := dataURL("image/png", []byte{1, 2, 3})
	uploader := &fakeUploader{ref: schema.Reference{Source: "file", Value: "file-9"}}
	// prefer file, but the caller declared inline and it fits -> keep inline.
	adapter := policyAdapter(&core.MediaPolicy{Upload: true, Prefer: []core.MediaPolicySource{core.MediaPolicySourceFile}}, uploader)
	file := adaptFile(uri)
	file.MediaSource = "inline"

	got, _ := adapter.adapt(context.Background(), "ses_1", file)
	if uploader.called != 0 {
		t.Fatal("a feasible declared inline should not be uploaded")
	}
	if got == nil || got.URI != uri {
		t.Fatalf("adapted = %+v, want unchanged inline", got)
	}
}

func TestPolicyDownsamplesOversizedImage(t *testing.T) {
	raw := noisePNG(t, 256, 256)
	const limit = int64(4096)
	adapter := policyAdapter(&core.MediaPolicy{
		InlineLimitBytes: core.MediaSizeLimits{Default: limit},
		Downsample:       true,
		Prefer:           []core.MediaPolicySource{core.MediaPolicySourceInline},
	}, nil)

	got, note := adapter.adapt(context.Background(), "ses_1", adaptFile(dataURL("image/png", raw)))
	if note != "" || got == nil {
		t.Fatalf("adapted = %+v note=%q", got, note)
	}
	decoded, err := inlineBytes(got.URI)
	if err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if int64(len(decoded)) > limit {
		t.Fatalf("downsampled to %d bytes, want <= %d", len(decoded), limit)
	}
}

// TestPolicyDownsamplesBeforeUpload: with the budget exceeded and downsample on,
// the payload is shrunk before it is uploaded, independent of prefer.
func TestPolicyDownsamplesBeforeUpload(t *testing.T) {
	raw := noisePNG(t, 256, 256)
	const limit = int64(4096)
	uploader := &fakeUploader{ref: schema.Reference{Source: "file", Value: "file-1"}}
	adapter := policyAdapter(&core.MediaPolicy{
		InlineLimitBytes: core.MediaSizeLimits{Default: limit},
		Downsample:       true,
		Upload:           true,
		Prefer:           []core.MediaPolicySource{core.MediaPolicySourceFile},
	}, uploader)

	got, _ := adapter.adapt(context.Background(), "ses_1", adaptFile(dataURL("image/png", raw)))
	if uploader.called != 1 || got == nil || got.MediaSource != "file" {
		t.Fatalf("upload called %d, got %+v", uploader.called, got)
	}
	if int64(len(uploader.last.Data)) > limit {
		t.Fatalf("uploaded %d bytes, want <= %d (shrunk before upload)", len(uploader.last.Data), limit)
	}
	if int64(len(uploader.last.Data)) >= int64(len(raw)) {
		t.Fatalf("uploaded data was not shrunk: %d >= %d", len(uploader.last.Data), len(raw))
	}
}

// TestPolicyNoDownsampleUploadsFullSize: with downsample off, an over-budget
// payload is uploaded at full size.
func TestPolicyNoDownsampleUploadsFullSize(t *testing.T) {
	raw := noisePNG(t, 256, 256)
	uploader := &fakeUploader{ref: schema.Reference{Source: "file", Value: "file-1"}}
	adapter := policyAdapter(&core.MediaPolicy{
		InlineLimitBytes: core.MediaSizeLimits{Default: 4096},
		Upload:           true,
		Prefer:           []core.MediaPolicySource{core.MediaPolicySourceFile},
	}, uploader)

	if _, _ = adapter.adapt(context.Background(), "ses_1", adaptFile(dataURL("image/png", raw))); uploader.called != 1 {
		t.Fatalf("upload called %d, want 1", uploader.called)
	}
	if int64(len(uploader.last.Data)) != int64(len(raw)) {
		t.Fatalf("uploaded %d bytes, want the full %d", len(uploader.last.Data), len(raw))
	}
}

func TestPolicySpoolsUndeliverable(t *testing.T) {
	dir := t.TempDir()
	adapter := policyAdapter(&core.MediaPolicy{
		InlineLimitBytes: core.MediaSizeLimits{Default: 1},
		Prefer:           []core.MediaPolicySource{core.MediaPolicySourceInline},
		Spool:            &core.MediaSpoolPolicy{Dir: dir},
	}, nil)

	got, note := adapter.adapt(context.Background(), "ses_1", adaptFile(dataURL("image/png", []byte{1, 2, 3})))
	if got != nil {
		t.Fatalf("a spooled attachment should be dropped, got %+v", got)
	}
	if !strings.HasPrefix(note, "[spooled attachment: ") {
		t.Fatalf("note = %q", note)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("spool dir entries = %v, err = %v", entries, err)
	}
}

func TestPolicyMaxBytesLeavesForOmission(t *testing.T) {
	uploader := &fakeUploader{ref: schema.Reference{Source: "file", Value: "file-9"}}
	adapter := policyAdapter(&core.MediaPolicy{Upload: true, MaxBytes: core.MediaSizeLimits{Default: 2}, Prefer: []core.MediaPolicySource{core.MediaPolicySourceFile}}, uploader)

	got, note := adapter.adapt(context.Background(), "ses_1", adaptFile(dataURL("image/png", []byte{1, 2, 3})))
	if uploader.called != 0 || note != "" || got == nil {
		t.Fatalf("over-maxBytes attachment should be left for the planner: got=%+v note=%q called=%d", got, note, uploader.called)
	}
}

// TestPolicyGatesInThePlanner covers the omissions the delivery loop cannot make
// itself: a disallowed url and an over-limit inline payload.
func TestPolicyGatesInThePlanner(t *testing.T) {
	info := runnerModelInfo(schema.ModelAPI{Protocol: "openai-chat", URL: runnerStrPtr("https://openai.example/v1")}, nil)
	info.Capabilities.Input = []string{"text", "image"}
	model, err := FromCatalogModel(info, schema.Credential{})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	policy := mediaPolicyForModel(model, core.AgentSelection{}, &core.MediaPolicy{InlineLimitBytes: core.MediaSizeLimits{Default: 4}, FetchURL: false})
	if got := policy.reason("user", "image/jpeg", "url", "https://example.com/a.jpg"); got != string(schema.AttachmentOmitURLNotAllowed) {
		t.Fatalf("disallowed url reason = %q, want url_not_allowed", got)
	}
	if got := policy.reason("user", "image/png", "inline", dataURL("image/png", []byte{1, 2, 3, 4, 5})); got != string(schema.AttachmentOmitTooLarge) {
		t.Fatalf("oversized inline reason = %q, want too_large", got)
	}
	if got := policy.reason("user", "image/png", "inline", dataURL("image/png", []byte{1, 2, 3})); got != "" {
		t.Fatalf("small inline reason = %q, want empty", got)
	}

	allowed := mediaPolicyForModel(model, core.AgentSelection{}, &core.MediaPolicy{FetchURL: true})
	if got := allowed.reason("user", "image/jpeg", "url", "https://example.com/a.jpg"); got != "" {
		t.Fatalf("allowed url reason = %q, want empty", got)
	}

	// With no policy the caller-declared behavior is unchanged.
	legacy := mediaPolicyForModel(model, core.AgentSelection{}, nil)
	if got := legacy.reason("user", "image/jpeg", "url", "https://example.com/a.jpg"); got != "" {
		t.Fatalf("no-policy url reason = %q, want empty", got)
	}
}

type fakeStore struct {
	ref    schema.Reference
	called int
}

func (f *fakeStore) Put(context.Context, schema.MediaUpload) (schema.Reference, error) {
	f.called++
	return f.ref, nil
}

func (f *fakeStore) Get(context.Context, schema.Reference) (io.ReadCloser, error) { return nil, nil }
func (f *fakeStore) Target() schema.UploadTarget                                  { return schema.UploadTarget{} }
func (f *fakeStore) Reserve(context.Context, string, string, int64) (schema.UploadTarget, error) {
	return schema.UploadTarget{}, nil
}

// TestPolicyHostsURL: with `url` preferred and a store configured, a byte-backed
// attachment is hosted and rewritten as a url reference.
func TestPolicyHostsURL(t *testing.T) {
	store := &fakeStore{ref: schema.Reference{Source: "url", Value: "https://cdn/a.png"}}
	adapter := policyAdapter(&core.MediaPolicy{Upload: true, Prefer: []core.MediaPolicySource{core.MediaPolicySourceURL}}, nil)
	adapter.store = store

	got, note := adapter.adapt(context.Background(), "ses_1", adaptFile(dataURL("image/png", []byte{1, 2, 3})))
	if store.called != 1 || note != "" {
		t.Fatalf("store called %d, note=%q", store.called, note)
	}
	if got == nil || got.MediaSource != "url" || got.URI != "https://cdn/a.png" || got.FileID != "" {
		t.Fatalf("hosted = %+v", got)
	}
}

// TestPolicyURLNeedsTheModelToAcceptIt: a model that does not accept `url`
// falls through (no hosting).
func TestPolicyURLNeedsTheModelToAcceptIt(t *testing.T) {
	store := &fakeStore{ref: schema.Reference{Source: "url", Value: "https://cdn/a.png"}}
	entry, _ := protocols.LookupProtocol("gemini")
	adapter := mediaAdapter{
		runner: &LLMRunner{OnError: func(error) {}},
		entry:  entry, provider: "gemini", input: []string{"text", "image"},
		forms:  map[string][]llmschema.MediaSource{"image": {llmschema.MediaSourceInline}},
		store:  store,
		policy: &core.MediaPolicy{Upload: true, Prefer: []core.MediaPolicySource{core.MediaPolicySourceURL}},
	}
	if _, _ = adapter.adapt(context.Background(), "ses_1", adaptFile(dataURL("image/png", []byte{1, 2, 3}))); store.called != 0 {
		t.Fatal("url must not be hosted when the model does not accept it")
	}
}

// TestOmissionCarriesLimit: an over-limit inline attachment reports the typed
// reason plus the accepted limit, in both the note and the event detail.
func TestOmissionCarriesLimit(t *testing.T) {
	info := runnerModelInfo(schema.ModelAPI{Protocol: "openai-responses", URL: runnerStrPtr("https://x")}, nil)
	info.Capabilities.Input = []string{"text", "image"}
	model, err := FromCatalogModel(info, schema.Credential{})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	mp := &core.MediaPolicy{InlineLimitBytes: core.MediaSizeLimits{Default: 4}}
	policy := mediaPolicyForModel(model, core.AgentSelection{}, mp)

	big := dataURL("image/png", []byte{1, 2, 3, 4, 5})
	if got := policy.reason("user", "image/png", "inline", big); got != string(schema.AttachmentOmitTooLarge) {
		t.Fatalf("reason = %q, want too_large", got)
	}
	if got := policy.detail("user", "image/png", "inline", big); got != "limit 4 bytes" {
		t.Fatalf("detail = %q, want the limit", got)
	}
	note := policy.omitNote("m1", nil, "user", "image/png", "inline", big)
	if !strings.Contains(note, "too_large") || !strings.Contains(note, "limit 4 bytes") {
		t.Fatalf("note = %q, want reason + limit", note)
	}
}

// failingUploader always fails, to exercise the prefer-loop fall-through.
type failingUploader struct{ called int }

func (f *failingUploader) Upload(context.Context, schema.MediaUpload) (schema.Reference, error) {
	f.called++
	return schema.Reference{}, errors.New("upload failed")
}

// TestPolicyFallsThroughWhenFileUploadFails: a failed file upload does not end
// the prefer loop; the next option (spool) is used.
func TestPolicyFallsThroughWhenFileUploadFails(t *testing.T) {
	dir := t.TempDir()
	uploader := &failingUploader{}
	adapter := policyAdapter(&core.MediaPolicy{
		InlineLimitBytes: core.MediaSizeLimits{Default: 1},
		Upload:           true,
		Prefer:           []core.MediaPolicySource{core.MediaPolicySourceFile},
		Spool:            &core.MediaSpoolPolicy{Dir: dir},
	}, uploader)

	got, note := adapter.adapt(context.Background(), "ses_1", adaptFile(dataURL("image/png", []byte{1, 2, 3})))
	if uploader.called != 1 {
		t.Fatalf("upload called %d times, want 1", uploader.called)
	}
	if got != nil || !strings.HasPrefix(note, "[spooled attachment: ") {
		t.Fatalf("a failed file upload should fall through to spool: got=%+v note=%q", got, note)
	}
}

// TestMediaModalityUsesFileTaxonomy: the policy's per-modality budgets use the
// image|audio|video|file taxonomy, so a non-av MIME keys the "file" budget.
func TestMediaModalityUsesFileTaxonomy(t *testing.T) {
	for mime, want := range map[string]string{
		"image/png": "image", "audio/wav": "audio", "video/mp4": "video",
		"text/plain": "file", "application/pdf": "file", "application/octet-stream": "file",
	} {
		if got := mediaModality(mime); got != want {
			t.Errorf("mediaModality(%q) = %q, want %q", mime, got, want)
		}
	}
	mp := &core.MediaPolicy{InlineLimitBytes: core.MediaSizeLimits{ByModality: map[string]int64{"file": 4}}}
	big := dataURL("text/plain", []byte{1, 2, 3, 4, 5})
	if got := policyGateReason(mp, llmschema.MediaSourceInline, mediaModality("text/plain"), big); got != string(schema.AttachmentOmitTooLarge) {
		t.Fatalf("file budget reason = %q, want too_large", got)
	}
}

// TestMediaFeasibilityFormsUseFileModality: media.forms is keyed by the
// image|audio|video|file taxonomy, so a document keys the "file" entry (not the
// input-modality "pdf").
func TestMediaFeasibilityFormsUseFileModality(t *testing.T) {
	support := protocols.ProtocolMediaSupport{MIMEs: map[string]struct{}{"application/pdf": {}}}
	forms := map[string][]llmschema.MediaSource{"file": {llmschema.MediaSourceInline}}
	if got := mediaFeasibilityReason(support, []string{"text", "pdf"}, forms, "user", "application/pdf", llmschema.MediaSourceInline); got != "" {
		t.Fatalf("pdf with a file form = %q, want feasible", got)
	}
	other := map[string][]llmschema.MediaSource{"image": {llmschema.MediaSourceInline}}
	if got := mediaFeasibilityReason(support, []string{"text", "pdf"}, other, "user", "application/pdf", llmschema.MediaSourceInline); got != string(schema.AttachmentOmitUnsupportedSource) {
		t.Fatalf("pdf with an image-only form = %q, want unsupported_source", got)
	}
}
