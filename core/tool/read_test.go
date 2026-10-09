package tool

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jiaweisky/go-agent-sdk/core"
	"github.com/jiaweisky/go-agent-sdk/pkg/llm/schema"
	coreschema "github.com/jiaweisky/go-agent-sdk/pkg/schema"
	"golang.org/x/text/language"
)

func newReadDeps(root string) ReadDeps {
	allow := core.PermissionRuleset{
		{Action: "*", Resource: "*", Effect: core.PermissionEffectAllow},
	}
	return ReadDeps{
		Mutation: core.NewLocationMutation(&core.Location{LocationInfo: coreschema.LocationInfo{Directory: coreschema.AbsolutePath(root)}}),
		Reader:   NewReadFileSystem(language.Und),
		Image:    core.NewImage(core.DefaultImageConfig()),
		Permission: core.NewPermission(core.PermissionConfig{
			Sessions:  testSessions{agent: "build"},
			Agents:    testAgents{rules: allow},
			Saved:     testSaved{},
			Events:    testEvents{},
			ProjectID: "proj_1",
		}),
	}
}

func settleRead(t *testing.T, deps ReadDeps, input map[string]any) *Settlement {
	t.Helper()
	registry := NewRegistry()
	if _, err := RegisterRead(registry, deps); err != nil {
		t.Fatalf("register: %v", err)
	}
	result, err := registry.Materialize(nil).Settle(context.Background(), ExecuteInput{
		SessionID:          "ses_1",
		Agent:              "build",
		AssistantMessageID: "msg_1",
		Call: schema.ToolCallPart{
			ID:    "call_1",
			Name:  ReadName,
			Input: input,
		},
	})
	if err != nil {
		t.Fatalf("settle: %v", err)
	}
	return result
}

func TestRead_TextFile(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "hello.txt"), []byte("hello\nworld\n"), 0644); err != nil {
		t.Fatal(err)
	}

	result := settleRead(t, newReadDeps(root), map[string]any{"path": "hello.txt"})
	if result.Result.Type != "json" {
		t.Fatalf("expected json result, got %q (%v)", result.Result.Type, result.Result.Value)
	}
	value, ok := result.Result.Value.(map[string]any)
	if !ok {
		t.Fatalf("expected map value, got %T", result.Result.Value)
	}
	if value["content"] != "hello\nworld\n" {
		t.Fatalf("unexpected content: %v", value["content"])
	}
	if value["encoding"] != "utf8" {
		t.Fatalf("unexpected encoding: %v", value["encoding"])
	}
	if value["mime"] != "text/plain" {
		t.Fatalf("unexpected mime: %v", value["mime"])
	}
}

func TestRead_DirectoryListing(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "b.txt"), []byte("b"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("a"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "sub"), 0755); err != nil {
		t.Fatal(err)
	}

	result := settleRead(t, newReadDeps(root), map[string]any{"path": "."})
	value, ok := result.Result.Value.(map[string]any)
	if !ok {
		t.Fatalf("expected map value, got %T", result.Result.Value)
	}
	entries, ok := value["entries"].([]any)
	if !ok {
		t.Fatalf("expected entries array, got %T", value["entries"])
	}
	var paths []string
	for _, entry := range entries {
		m := entry.(map[string]any)
		paths = append(paths, m["path"].(string))
	}
	expected := []string{"sub" + string(filepath.Separator), "a.txt", "b.txt"}
	if len(paths) != len(expected) {
		t.Fatalf("unexpected entries: %v", paths)
	}
	for i := range expected {
		if paths[i] != expected[i] {
			t.Fatalf("unexpected entries: %v", paths)
		}
	}
}

func TestRead_Paging(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "data.txt"), []byte("a\nb\nc\n"), 0644); err != nil {
		t.Fatal(err)
	}

	result := settleRead(t, newReadDeps(root), map[string]any{"path": "data.txt", "offset": 2, "limit": 1})
	value, ok := result.Result.Value.(map[string]any)
	if !ok {
		t.Fatalf("expected map value, got %T", result.Result.Value)
	}
	if value["type"] != "text-page" {
		t.Fatalf("unexpected type: %v", value["type"])
	}
	if value["content"] != "b" {
		t.Fatalf("unexpected content: %v", value["content"])
	}
	if value["truncated"] != true {
		t.Fatalf("expected truncated, got %v", value["truncated"])
	}
}

func TestRead_LargeFilePages(t *testing.T) {
	root := t.TempDir()
	var b strings.Builder
	for i := 0; i < 10000; i++ {
		b.WriteString("0123456789\n")
	}
	if err := os.WriteFile(filepath.Join(root, "big.txt"), []byte(b.String()), 0644); err != nil {
		t.Fatal(err)
	}

	result := settleRead(t, newReadDeps(root), map[string]any{"path": "big.txt"})
	value, ok := result.Result.Value.(map[string]any)
	if !ok {
		t.Fatalf("expected map value, got %T", result.Result.Value)
	}
	if value["type"] != "text-page" {
		t.Fatalf("unexpected type: %v", value["type"])
	}
	if value["truncated"] != true || value["next"] == nil {
		t.Fatalf("expected truncation with a next page, got %v / %v", value["truncated"], value["next"])
	}
	content, _ := value["content"].(string)
	if !strings.HasPrefix(content, "0123456789\n") {
		t.Fatalf("unexpected content start: %q", content)
	}
}

func TestRead_OffsetOutOfRange(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "data.txt"), []byte("a\nb\n"), 0644); err != nil {
		t.Fatal(err)
	}

	result := settleRead(t, newReadDeps(root), map[string]any{"path": "data.txt", "offset": 10})
	if result.Result.Type != "error" {
		t.Fatalf("expected error result, got %q", result.Result.Type)
	}
	if result.Result.Value != "Unable to read data.txt" {
		t.Fatalf("unexpected error: %v", result.Result.Value)
	}
}

func TestRead_BinaryFile(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "blob.bin"), []byte{0x00, 0x01, 0x02}, 0644); err != nil {
		t.Fatal(err)
	}

	result := settleRead(t, newReadDeps(root), map[string]any{"path": "blob.bin"})
	if result.Result.Type != "error" {
		t.Fatalf("expected error result, got %q", result.Result.Type)
	}
	if result.Result.Value != "Cannot read binary file: blob.bin" {
		t.Fatalf("unexpected error: %v", result.Result.Value)
	}
}

func TestRead_Image(t *testing.T) {
	root := t.TempDir()
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "pic.png"), buf.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}

	result := settleRead(t, newReadDeps(root), map[string]any{"path": "pic.png"})
	if result.Result.Type != "content" {
		t.Fatalf("expected content result, got %q (%v)", result.Result.Type, result.Result.Value)
	}
	content, ok := result.Result.Value.([]schema.ToolContent)
	if !ok {
		t.Fatalf("expected content slice, got %T", result.Result.Value)
	}
	if len(content) != 2 {
		t.Fatalf("expected 2 content parts, got %d", len(content))
	}
	text, ok := content[0].(schema.ToolTextContent)
	if !ok || text.Text != "Image read successfully" {
		t.Fatalf("unexpected text part: %#v", content[0])
	}
	file, ok := content[1].(schema.ToolFileContent)
	if !ok {
		t.Fatalf("expected file part, got %T", content[1])
	}
	if file.Mime != "image/png" {
		t.Fatalf("unexpected mime: %s", file.Mime)
	}
	if file.Name == nil || *file.Name != "pic.png" {
		t.Fatalf("unexpected name: %v", file.Name)
	}
	if _, err := base64.StdEncoding.DecodeString(file.URI[len("data:image/png;base64,"):]); err != nil {
		t.Fatalf("file data is not valid base64: %v", err)
	}
}

func TestRead_Audio(t *testing.T) {
	root := t.TempDir()
	wav := append([]byte("RIFF"), make([]byte, 64)...)
	if err := os.WriteFile(filepath.Join(root, "clip.wav"), wav, 0644); err != nil {
		t.Fatal(err)
	}

	result := settleRead(t, newReadDeps(root), map[string]any{"path": "clip.wav"})
	if result.Result.Type != "content" {
		t.Fatalf("expected content result, got %q (%v)", result.Result.Type, result.Result.Value)
	}
	content, ok := result.Result.Value.([]schema.ToolContent)
	if !ok || len(content) != 2 {
		t.Fatalf("expected 2 content parts, got %T %v", result.Result.Value, result.Result.Value)
	}
	if text, ok := content[0].(schema.ToolTextContent); !ok || text.Text != "Media read successfully" {
		t.Fatalf("unexpected text part: %#v", content[0])
	}
	file, ok := content[1].(schema.ToolFileContent)
	if !ok {
		t.Fatalf("expected file part, got %T", content[1])
	}
	if file.Mime != "audio/wav" {
		t.Fatalf("unexpected mime: %s", file.Mime)
	}
	if !strings.HasPrefix(file.URI, "data:audio/wav;base64,") {
		t.Fatalf("unexpected uri: %s", file.URI)
	}
}

func TestRead_Video(t *testing.T) {
	root := t.TempDir()
	mp4 := append([]byte("\x00\x00\x00\x18ftypmp42"), bytes.Repeat([]byte("x"), 64)...)
	if err := os.WriteFile(filepath.Join(root, "clip.mp4"), mp4, 0644); err != nil {
		t.Fatal(err)
	}

	result := settleRead(t, newReadDeps(root), map[string]any{"path": "clip.mp4"})
	if result.Result.Type != "content" {
		t.Fatalf("expected content result, got %q (%v)", result.Result.Type, result.Result.Value)
	}
	content, ok := result.Result.Value.([]schema.ToolContent)
	if !ok || len(content) != 2 {
		t.Fatalf("expected 2 content parts, got %T %v", result.Result.Value, result.Result.Value)
	}
	if text, ok := content[0].(schema.ToolTextContent); !ok || text.Text != "Media read successfully" {
		t.Fatalf("unexpected text part: %#v", content[0])
	}
	file, ok := content[1].(schema.ToolFileContent)
	if !ok {
		t.Fatalf("expected file part, got %T", content[1])
	}
	if file.Mime != "video/mp4" {
		t.Fatalf("unexpected mime: %s", file.Mime)
	}
	if !strings.HasPrefix(file.URI, "data:video/mp4;base64,") {
		t.Fatalf("unexpected uri: %s", file.URI)
	}
}

func TestRead_PDF(t *testing.T) {
	root := t.TempDir()
	pdf := append([]byte("%PDF-1.4\n"), bytes.Repeat([]byte("x"), 64)...)
	if err := os.WriteFile(filepath.Join(root, "doc.pdf"), pdf, 0644); err != nil {
		t.Fatal(err)
	}

	result := settleRead(t, newReadDeps(root), map[string]any{"path": "doc.pdf"})
	if result.Result.Type != "content" {
		t.Fatalf("expected content result, got %q (%v)", result.Result.Type, result.Result.Value)
	}
	content, ok := result.Result.Value.([]schema.ToolContent)
	if !ok || len(content) != 2 {
		t.Fatalf("expected 2 content parts, got %T %v", result.Result.Value, result.Result.Value)
	}
	if text, ok := content[0].(schema.ToolTextContent); !ok || text.Text != "Media read successfully" {
		t.Fatalf("unexpected text part: %#v", content[0])
	}
	file, ok := content[1].(schema.ToolFileContent)
	if !ok {
		t.Fatalf("expected file part, got %T", content[1])
	}
	if file.Mime != "application/pdf" {
		t.Fatalf("unexpected mime: %s", file.Mime)
	}
	if !strings.HasPrefix(file.URI, "data:application/pdf;base64,") {
		t.Fatalf("unexpected uri: %s", file.URI)
	}
}

func TestRead_StripsUtf8Bom(t *testing.T) {
	root := t.TempDir()
	content := append([]byte{0xef, 0xbb, 0xbf}, []byte("hello")...)
	if err := os.WriteFile(filepath.Join(root, "bom.txt"), content, 0644); err != nil {
		t.Fatal(err)
	}

	result := settleRead(t, newReadDeps(root), map[string]any{"path": "bom.txt"})
	value, ok := result.Result.Value.(map[string]any)
	if !ok {
		t.Fatalf("expected map value, got %T", result.Result.Value)
	}
	if value["content"] != "hello" {
		t.Fatalf("expected BOM stripped, got %q", value["content"])
	}
}

func TestRead_PagedStripsUtf8Bom(t *testing.T) {
	root := t.TempDir()
	content := append([]byte{0xef, 0xbb, 0xbf}, []byte("a\nb\n")...)
	if err := os.WriteFile(filepath.Join(root, "bom.txt"), content, 0644); err != nil {
		t.Fatal(err)
	}

	result := settleRead(t, newReadDeps(root), map[string]any{"path": "bom.txt", "offset": 1})
	value, ok := result.Result.Value.(map[string]any)
	if !ok {
		t.Fatalf("expected map value, got %T", result.Result.Value)
	}
	if value["content"] != "a\nb" {
		t.Fatalf("expected BOM stripped, got %q", value["content"])
	}
}

func TestRead_ShortRiffFile(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "riff.txt"), []byte("RIFF"), 0644); err != nil {
		t.Fatal(err)
	}

	result := settleRead(t, newReadDeps(root), map[string]any{"path": "riff.txt"})
	if result.Result.Type == "error" {
		t.Fatalf("unexpected error: %v", result.Result.Value)
	}
}

func TestRead_TruncatesLongLine(t *testing.T) {
	root := t.TempDir()
	long := strings.Repeat("x", 3000)
	if err := os.WriteFile(filepath.Join(root, "long.txt"), []byte(long), 0644); err != nil {
		t.Fatal(err)
	}

	result := settleRead(t, newReadDeps(root), map[string]any{"path": "long.txt", "offset": 1})
	value, ok := result.Result.Value.(map[string]any)
	if !ok {
		t.Fatalf("expected map value, got %T", result.Result.Value)
	}
	content, _ := value["content"].(string)
	if !strings.HasSuffix(content, readMaxLineSuffix) {
		t.Fatalf("expected truncated line suffix, got %q", content)
	}
	if readCharLength(content) != readMaxLineLength+readCharLength(readMaxLineSuffix) {
		t.Fatalf("unexpected truncated length: %d", readCharLength(content))
	}
}

func TestRead_DirectoryListingLocaleSort(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"B.txt", "a.txt"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	result := settleRead(t, newReadDeps(root), map[string]any{"path": "."})
	value, ok := result.Result.Value.(map[string]any)
	if !ok {
		t.Fatalf("expected map value, got %T", result.Result.Value)
	}
	entries, ok := value["entries"].([]any)
	if !ok {
		t.Fatalf("expected entries array, got %T", value["entries"])
	}
	var paths []string
	for _, entry := range entries {
		paths = append(paths, entry.(map[string]any)["path"].(string))
	}
	if len(paths) != 2 || paths[0] != "a.txt" || paths[1] != "B.txt" {
		t.Fatalf("expected locale-aware order [a.txt B.txt], got %v", paths)
	}
}

func TestRead_MalformedUtf8(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "bad.txt"), []byte{0xff, 0xfe, 0xfd}, 0644); err != nil {
		t.Fatal(err)
	}

	result := settleRead(t, newReadDeps(root), map[string]any{"path": "bad.txt"})
	if result.Result.Type != "error" {
		t.Fatalf("expected error result, got %q", result.Result.Type)
	}
	if result.Result.Value != "Unable to read bad.txt" {
		t.Fatalf("unexpected error: %v", result.Result.Value)
	}
}

func TestRead_EmptyFile(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "empty.txt"), nil, 0644); err != nil {
		t.Fatal(err)
	}

	result := settleRead(t, newReadDeps(root), map[string]any{"path": "empty.txt"})
	value, ok := result.Result.Value.(map[string]any)
	if !ok {
		t.Fatalf("expected map value, got %T", result.Result.Value)
	}
	if value["content"] != "" {
		t.Fatalf("expected empty content, got %q", value["content"])
	}
}

func TestRead_ExternalAbsolutePath(t *testing.T) {
	root := t.TempDir()
	external := t.TempDir()
	target := filepath.Join(external, "outside.txt")
	if err := os.WriteFile(target, []byte("outside"), 0644); err != nil {
		t.Fatal(err)
	}

	result := settleRead(t, newReadDeps(root), map[string]any{"path": target})
	if result.Result.Type != "json" {
		t.Fatalf("expected json result, got %q (%v)", result.Result.Type, result.Result.Value)
	}
	value, ok := result.Result.Value.(map[string]any)
	if !ok {
		t.Fatalf("expected map value, got %T", result.Result.Value)
	}
	if value["content"] != "outside" {
		t.Fatalf("unexpected content: %v", value["content"])
	}
}

func TestRead_DeniedPermission(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "secret.txt"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	deny := core.PermissionRuleset{
		{Action: "*", Resource: "*", Effect: core.PermissionEffectDeny},
	}
	deps := newReadDeps(root)
	deps.Permission = core.NewPermission(core.PermissionConfig{
		Sessions:  testSessions{agent: "build"},
		Agents:    testAgents{rules: deny},
		Saved:     testSaved{},
		Events:    testEvents{},
		ProjectID: "proj_1",
	})

	result := settleRead(t, deps, map[string]any{"path": "secret.txt"})
	if result.Result.Type != "error" {
		t.Fatalf("expected error result, got %q", result.Result.Type)
	}
	if result.Result.Value != "Unable to read secret.txt" {
		t.Fatalf("unexpected error: %v", result.Result.Value)
	}
}
