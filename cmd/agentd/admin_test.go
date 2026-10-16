package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	config "github.com/jiaweisky/go-agent-sdk/cmd/agentd/config"
)

func adminTestHandler(cfg config.Config) http.Handler {
	entries := make([]*builtServer, 0, len(cfg.Servers))
	for _, s := range cfg.Servers {
		entries = append(entries, &builtServer{cfg: s})
	}
	return newRouter(entries, &adminDoc{
		config:  cfg,
		path:    "/etc/agentd.yaml",
		env:     []string{"DEEPSEEK_API_KEY", "AGENTD_PLATFORM"},
		version: "1.2.3",
	}, nil)
}

func adminGet(t *testing.T, h http.Handler, path, bearer string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func TestAdminConfigRedactsSecrets(t *testing.T) {
	cfg := config.Config{
		Models: []config.ModelConfig{{
			ID: "m", Model: "m", Protocol: "openai-compatible-chat", ModelBaseURL: "https://api.example.com",
			InputModalities: []string{"text"},
			Credentials: config.Credentials{
				APIKey: "sk-super-secret",
				AWS:    &config.AWS{Region: "us-east-1", AccessKeyID: "AKIA-LEAK", SecretAccessKey: "aws-secret-leak", SessionToken: "aws-token-leak"},
			},
			ContextLimit: 1000, OutputLimit: 100,
		}},
		Servers: []config.Server{
			{Name: "root", Kind: "http", Listen: "127.0.0.1:8787", Root: "/tmp/r", Agents: []string{"build"}},
			{
				Name: "sec", Kind: "http", Listen: "127.0.0.1:8788", Root: "/tmp/s", Agents: []string{"build"},
				Host:        "sec.example.com",
				Auth:        &config.AuthConfig{Bearer: "bearer-leak", Username: "admin", Password: "pw-leak"},
				TLS:         &config.TLS{CertFile: "/etc/cert.pem", KeyFile: "/etc/key.pem"},
				MCP:         []config.MCPServer{{Name: "mcp1", URL: "https://mcp.example", Headers: map[string]string{"Authorization": "token-leak"}}},
				MediaUpload: &config.MediaUploadConfig{URL: "https://up.example", Header: "X-Auth", Value: "upload-leak", Fields: map[string]string{"k": "v"}},
				InboundPermissions: []config.PermissionRule{
					{Action: "bash", Resource: "*", Effect: "ask"},
				},
			},
		},
		Control: &config.ControlConfig{Listen: "127.0.0.1:9090", Auth: &config.AuthConfig{Bearer: "ctl-leak"}},
		Wrap:    &config.WrapConfig{Process: "seatbelt", FailurePolicy: "fail-closed"},
	}
	code, body := adminGet(t, adminTestHandler(cfg), "/api/config", "")
	if code != http.StatusOK {
		t.Fatalf("status = %d: %s", code, body)
	}
	for _, leak := range []string{
		"sk-super-secret", "AKIA-LEAK", "aws-secret-leak", "aws-token-leak",
		"bearer-leak", "pw-leak", "token-leak", "upload-leak", "ctl-leak",
		"/etc/cert.pem", "/etc/key.pem",
	} {
		if strings.Contains(body, leak) {
			t.Fatalf("response leaked %q:\n%s", leak, body)
		}
	}
	for _, want := range []string{
		`"schemaVersion":1`,
		`"apiKey":{"configured":true,"redacted":true}`,
		`"bearer":{"configured":true,"redacted":true}`,
		`"password":{"configured":true,"redacted":true}`,
		`"tls":{"configured":true,"redacted":true}`,
		`"username":"admin"`,
		`/etc/agentd.yaml`,
		`"DEEPSEEK_API_KEY"`,
		`"process":"seatbelt"`,
		`"action":"bash"`,
		`"name":"root"`,
		`"name":"sec"`,
		`"url":"https://up.example"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("response missing %q:\n%s", want, body)
		}
	}
}

func TestAdminConfigAuth(t *testing.T) {
	cfg := config.Config{Servers: []config.Server{{
		Name: "http", Kind: "http", Agents: []string{"build"}, Auth: &config.AuthConfig{Bearer: "s3cret"},
	}}}
	h := adminTestHandler(cfg)
	if code, _ := adminGet(t, h, "/api/config", ""); code != http.StatusUnauthorized {
		t.Fatalf("no credential = %d, want 401", code)
	}
	if code, _ := adminGet(t, h, "/api/config", "s3cret"); code != http.StatusOK {
		t.Fatalf("with credential = %d, want 200", code)
	}

	cfg.Servers[0].PublicRoutes = []string{"/api/config"}
	if code, _ := adminGet(t, adminTestHandler(cfg), "/api/config", ""); code != http.StatusOK {
		t.Fatalf("publicRoutes must skip auth, got %d", code)
	}
}

func TestAdminConfigSectionAndServerFilters(t *testing.T) {
	cfg := config.Config{
		Models: []config.ModelConfig{{ID: "m", Model: "m", Protocol: "openai-compatible-chat"}},
		Servers: []config.Server{
			{Name: "a", Kind: "http", Agents: []string{"build"}},
			{Name: "b", Kind: "a2a", Agents: []string{"build"}},
		},
	}
	h := adminTestHandler(cfg)

	if code, _ := adminGet(t, h, "/api/config?section=nope", ""); code != http.StatusBadRequest {
		t.Fatalf("unknown section = %d, want 400", code)
	}
	_, body := adminGet(t, h, "/api/config?section=models", "")
	if !strings.Contains(body, `"models"`) || strings.Contains(body, `"name":"a"`) {
		t.Fatalf("section=models should return only models:\n%s", body)
	}
	_, body = adminGet(t, h, "/api/config?section=servers&server=b", "")
	if strings.Contains(body, `"name":"a"`) || !strings.Contains(body, `"name":"b"`) {
		t.Fatalf("server filter wrong:\n%s", body)
	}
}

func TestAdminSanitizesCredentialURLs(t *testing.T) {
	cfg := config.Config{Servers: []config.Server{{
		Name: "http", Kind: "http", Agents: []string{"build"},
		References:  []config.Reference{{Name: "r", Type: "git", Repository: "https://user:git-token-leak@github.com/o/r.git"}},
		MediaUpload: &config.MediaUploadConfig{URL: "https://user:up-token-leak@up.example/api"},
		Storage:     &config.StorageConfig{Backend: "s3", S3: &config.S3Config{Endpoint: "https://user:s3-token-leak@cos.example"}},
	}}}
	code, body := adminGet(t, adminTestHandler(cfg), "/api/config", "")
	if code != http.StatusOK {
		t.Fatalf("status = %d: %s", code, body)
	}
	for _, leak := range []string{"git-token-leak", "up-token-leak", "s3-token-leak", "user:"} {
		if strings.Contains(body, leak) {
			t.Fatalf("response leaked %q:\n%s", leak, body)
		}
	}
	for _, want := range []string{
		`"repository":"https://github.com/o/r.git"`,
		`"url":"https://up.example/api"`,
		`"endpoint":"https://cos.example"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("response missing sanitized URL %q:\n%s", want, body)
		}
	}
}

// TestAdminConfigExposesMediaStorageAndBody checks the new server fields are
// rendered: the media delivery policy (camelCase size limits), the media upload
// lifetime, the media store (redacted), and the body cap.
func TestAdminConfigExposesMediaStorageAndBody(t *testing.T) {
	cfg := config.Config{
		Models: []config.ModelConfig{{ID: "m", Model: "m", Protocol: "openai-compatible-chat"}},
		Servers: []config.Server{{
			Name: "a", Kind: "http", Agents: []string{"build"},
			Media: &config.InboundMediaConfig{
				InlineLimitBytes: config.MediaSizeLimit{Default: 1024, Image: 2048},
				Upload:           true,
			},
			MediaUpload: &config.MediaUploadConfig{URL: "https://user:tok@up.example/api", ExpiresIn: 60, ExpiresAfter: 120},
			Storage: &config.StorageConfig{
				Backend: "http", URL: "https://user:tok@store.example/", Auth: "bearer", Token: "s3cret",
				PresignSecret: "ps",
			},
			MaxBodyBytes: 1 << 21,
		}},
	}
	code, body := adminGet(t, adminTestHandler(cfg), "/api/config?section=servers", "")
	if code != http.StatusOK {
		t.Fatalf("status = %d: %s", code, body)
	}
	for _, want := range []string{
		`"inlineLimitBytes":{"default":1024,"image":2048}`,
		`"expiresIn":60`,
		`"expiresAfter":120`,
		`"storage":{"backend":"http","url":"https://store.example/"`,
		`"presignSecret":{"configured":true,"redacted":true}`,
		`"maxBodyBytes":2097152`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("response missing %q:\n%s", want, body)
		}
	}
	for _, leak := range []string{"s3cret", "user:", "tok@"} {
		if strings.Contains(body, leak) {
			t.Errorf("response leaked %q:\n%s", leak, body)
		}
	}
}

func TestAdminInfo(t *testing.T) {
	cfg := config.Config{Servers: []config.Server{{Name: "http", Kind: "http", Agents: []string{"build"}}}}
	code, body := adminGet(t, adminTestHandler(cfg), "/api/info", "")
	if code != http.StatusOK {
		t.Fatalf("status = %d: %s", code, body)
	}
	for _, want := range []string{`"version":"1.2.3"`, `"pid":`, `"servers":["http"]`} {
		if !strings.Contains(body, want) {
			t.Errorf("info missing %q:\n%s", want, body)
		}
	}
}

// TestAdminAbsentWithoutDoc guards that a dynamic-instance router (no admin
// doc) never serves the endpoints.
func TestAdminAbsentWithoutDoc(t *testing.T) {
	entries := []*builtServer{{cfg: config.Server{Name: "d", Kind: "http"},
		handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) })}}
	h := newRouter(entries, nil, nil)
	if code, _ := adminGet(t, h, "/api/config", ""); code != http.StatusNotFound {
		t.Fatalf("dynamic router /api/config = %d, want 404", code)
	}
}
