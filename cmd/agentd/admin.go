package main

import (
	"crypto/subtle"
	"net/http"
	"net/url"
	"strings"

	config "github.com/jiaweisky/go-agent-sdk/cmd/agentd/config"
)

// adminDoc carries the loaded configuration and its provenance for the admin
// read-only endpoints (/api/config, /api/info). It is shared by every listener;
// each listener reports its own address in /api/info.
type adminDoc struct {
	config  config.Config
	path    string
	env     []string
	version string
}

// infoResponse is the GET /api/info body.
type infoResponse struct {
	Version string   `json:"version"`
	PID     int      `json:"pid"`
	Listen  string   `json:"listen,omitempty"`
	Servers []string `json:"servers"`
	A2A     []string `json:"a2a,omitempty"`
}

func validSection(section string) bool {
	switch section {
	case "", "models", "servers", "control", "wrap":
		return true
	}
	return false
}

// writeAdminError writes the uniform error envelope, matching the /api/* shape.
func writeAdminError(w http.ResponseWriter, status int, code, message string) {
	writeJSONStatus(w, status, map[string]string{"error": code, "message": message})
}

// authorized reports whether the request carries the server's configured
// credential. A nil auth (loopback server) authorizes every request.
func authorized(req *http.Request, auth *config.AuthConfig) bool {
	if auth == nil {
		return true
	}
	if auth.Bearer != "" {
		token := ""
		if h := req.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
			token = strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
		}
		if token == "" {
			token = req.URL.Query().Get("auth_token")
		}
		return constEq(token, auth.Bearer)
	}
	if auth.Password != "" {
		user, pass, ok := req.BasicAuth()
		if !ok {
			return false
		}
		username := auth.Username
		if username == "" {
			username = "opencode"
		}
		return constEq(user, username) && constEq(pass, auth.Password)
	}
	return true
}

func constEq(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// publicPath reports whether path is an explicitly-configured public route.
func publicPath(patterns []string, path string) bool {
	for _, pattern := range patterns {
		if pattern == path {
			return true
		}
	}
	return false
}

// adminSchemaVersion versions the /api/config document; bump on a breaking change.
const adminSchemaVersion = 1

// secret reports whether a sensitive value is configured without revealing it.
type secret struct {
	Configured bool `json:"configured"`
	Redacted   bool `json:"redacted,omitempty"`
}

// redact builds the marker for a sensitive scalar value.
func redact(value string) secret {
	return secret{Configured: value != "", Redacted: value != ""}
}

// redactSet builds the marker for a sensitive collection or opaque block.
func redactSet(present bool) secret {
	return secret{Configured: present, Redacted: present}
}

// sanitizeURL strips any userinfo (a possible embedded credential) from a URL,
// so a git repository or upload endpoint like https://user:token@host/... cannot
// leak its secret. A value that is not a URL is returned unchanged.
func sanitizeURL(raw string) string {
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.User == nil {
		return raw
	}
	parsed.User = nil
	return parsed.String()
}

// configResponse is the GET /api/config body.
type configResponse struct {
	SchemaVersion int            `json:"schemaVersion"`
	Sources       []sourceView   `json:"sources"`
	Config        configDocument `json:"config"`
}

// configDocument mirrors config.Config with sensitive fields redacted.
type configDocument struct {
	Models  []modelView  `json:"models"`
	Servers []serverView `json:"servers"`
	Control *controlView `json:"control"`
	Wrap    *wrapView    `json:"wrap"`
}

// sourceView reports where the effective configuration came from.
type sourceView struct {
	Kind string `json:"kind"`
	Path string `json:"path,omitempty"`
	// Expanded lists the `${VAR}` references the loader substituted (their
	// values are treated as sensitive; only the names are reported).
	Expanded []string `json:"expanded,omitempty"`
}

type modelView struct {
	ID              string          `json:"id"`
	Model           string          `json:"model"`
	ProviderID      string          `json:"providerID,omitempty"`
	Protocol        string          `json:"protocol"`
	ModelBaseURL    string          `json:"modelBaseURL,omitempty"`
	InputModalities []string        `json:"inputModalities,omitempty"`
	Media           *mediaView      `json:"media,omitempty"`
	Credentials     credentialsView `json:"credentials"`
	ContextLimit    int             `json:"contextLimit"`
	OutputLimit     int             `json:"outputLimit"`
}

type mediaView struct {
	Input   []string `json:"input,omitempty"`
	Sources []string `json:"sources,omitempty"`
}

type credentialsView struct {
	APIKey secret   `json:"apiKey"`
	AWS    *awsView `json:"aws,omitempty"`
}

type awsView struct {
	Region          string `json:"region,omitempty"`
	AccessKeyID     secret `json:"accessKeyId"`
	SecretAccessKey secret `json:"secretAccessKey"`
	SessionToken    secret `json:"sessionToken"`
}

type serverView struct {
	Name               string               `json:"name"`
	Kind               string               `json:"kind"`
	Listen             string               `json:"listen"`
	Root               string               `json:"root"`
	WorkingDirectory   string               `json:"workingDirectory"`
	ConfigDirs         []string             `json:"configDirs,omitempty"`
	Isolation          *isolationView       `json:"isolation,omitempty"`
	Sandbox            *sandboxView         `json:"sandbox,omitempty"`
	Models             []string             `json:"models,omitempty"`
	DefaultModel       string               `json:"defaultModel,omitempty"`
	Agents             []string             `json:"agents,omitempty"`
	Host               string               `json:"host,omitempty"`
	Mount              string               `json:"mount,omitempty"`
	Default            bool                 `json:"default,omitempty"`
	MediaUpload        *mediaUploadView     `json:"mediaUpload,omitempty"`
	MCP                []mcpView            `json:"mcp,omitempty"`
	References         []referenceView      `json:"references,omitempty"`
	InboundPermissions []permissionRuleView `json:"inboundPermissions,omitempty"`
	Questions          string               `json:"questions,omitempty"`
	SubagentDepth      int                  `json:"subagentDepth,omitempty"`
	TitleAuto          bool                 `json:"titleAuto,omitempty"`
	DropErroredTurns   *bool                `json:"dropErroredTurns,omitempty"`
	Auth               *authView            `json:"auth,omitempty"`
	TLS                *secret              `json:"tls,omitempty"`
	AllowInsecure      bool                 `json:"allowInsecure,omitempty"`
	// HTTP-only.
	InteractionTimeout  string   `json:"interactionTimeout,omitempty"`
	InteractionFallback string   `json:"interactionFallback,omitempty"`
	SubscriberCapacity  int      `json:"subscriberCapacity,omitempty"`
	PublicRoutes        []string `json:"publicRoutes,omitempty"`
	CORS                []string `json:"cors,omitempty"`
	// A2A-only.
	BaseURL      string `json:"baseURL,omitempty"`
	InboundMedia string `json:"inboundMedia,omitempty"`

	KeepAlive string `json:"keepAlive,omitempty"`
}

type isolationView struct {
	Mode         string   `json:"mode"`
	BaseDir      string   `json:"baseDir,omitempty"`
	BaseReadOnly bool     `json:"baseReadOnly,omitempty"`
	BaseRef      string   `json:"baseRef,omitempty"`
	Cow          *cowView `json:"cow,omitempty"`
}

type cowView struct {
	Mode string `json:"mode"`
}

type sandboxView struct {
	Process       string `json:"process"`
	FailurePolicy string `json:"failurePolicy,omitempty"`
}

type mediaUploadView struct {
	URL       string `json:"url,omitempty"`
	IDField   string `json:"idField,omitempty"`
	ExpiresIn int    `json:"expiresIn,omitempty"`
	Header    secret `json:"header"`
	Value     secret `json:"value"`
	Fields    secret `json:"fields"`
}

type mcpView struct {
	Name     string `json:"name"`
	Type     string `json:"type,omitempty"`
	Redacted bool   `json:"redacted,omitempty"` // command/url/environment/headers omitted
}

type referenceView struct {
	Name        string `json:"name"`
	Type        string `json:"type,omitempty"`
	Path        string `json:"path,omitempty"`
	Repository  string `json:"repository,omitempty"`
	Branch      string `json:"branch,omitempty"`
	Description string `json:"description,omitempty"`
	Hidden      *bool  `json:"hidden,omitempty"`
}

type permissionRuleView struct {
	Action   string `json:"action"`
	Resource string `json:"resource"`
	Effect   string `json:"effect"`
}

type authView struct {
	Bearer   secret `json:"bearer"`
	Username string `json:"username,omitempty"`
	Password secret `json:"password"`
}

type controlView struct {
	Listen        string    `json:"listen"`
	ProjectListen string    `json:"projectListen,omitempty"`
	MaxLeases     int       `json:"maxLeases,omitempty"`
	Auth          *authView `json:"auth,omitempty"`
}

type wrapView struct {
	Process       string `json:"process"`
	FailurePolicy string `json:"failurePolicy,omitempty"`
}

// buildConfigDocument renders the redacted, optionally filtered document.
func buildConfigDocument(cfg config.Config, section, server string) configDocument {
	doc := configDocument{
		Models:  modelViews(cfg.Models),
		Servers: serverViews(cfg.Servers, server),
	}
	if cfg.Control != nil {
		doc.Control = &controlView{
			Listen:        cfg.Control.Listen,
			ProjectListen: cfg.Control.ProjectListen,
			MaxLeases:     cfg.Control.MaxLeases,
			Auth:          authViewOf(cfg.Control.Auth),
		}
	}
	if cfg.Wrap != nil {
		doc.Wrap = &wrapView{Process: cfg.Wrap.Process, FailurePolicy: cfg.Wrap.FailurePolicy}
	}
	switch section {
	case "models":
		doc = configDocument{Models: doc.Models}
	case "servers":
		doc = configDocument{Servers: doc.Servers}
	case "control":
		doc = configDocument{Control: doc.Control}
	case "wrap":
		doc = configDocument{Wrap: doc.Wrap}
	}
	return doc
}

func modelViews(models []config.ModelConfig) []modelView {
	views := make([]modelView, 0, len(models))
	for _, m := range models {
		views = append(views, modelViewOf(m))
	}
	return views
}

func modelViewOf(m config.ModelConfig) modelView {
	v := modelView{
		ID:              m.ID,
		Model:           m.Model,
		ProviderID:      m.ProviderID,
		Protocol:        m.Protocol,
		ModelBaseURL:    m.ModelBaseURL,
		InputModalities: m.InputModalities,
		ContextLimit:    m.ContextLimit,
		OutputLimit:     m.OutputLimit,
		Credentials:     credentialsView{APIKey: redact(m.Credentials.APIKey)},
	}
	if m.Media != nil {
		v.Media = &mediaView{Input: m.Media.Input, Sources: m.Media.Sources}
	}
	if a := m.Credentials.AWS; a != nil {
		v.Credentials.AWS = &awsView{
			Region:          a.Region,
			AccessKeyID:     redact(a.AccessKeyID),
			SecretAccessKey: redact(a.SecretAccessKey),
			SessionToken:    redact(a.SessionToken),
		}
	}
	return v
}

func serverViewOf(s config.Server) serverView {
	return serverView{
		Name:                s.Name,
		Kind:                s.Kind,
		Listen:              s.Listen,
		Root:                s.Root,
		WorkingDirectory:    s.WorkingDirectory,
		ConfigDirs:          s.ConfigDirs,
		Models:              s.Models,
		DefaultModel:        s.DefaultModel,
		Agents:              s.Agents,
		Host:                s.Host,
		Mount:               s.Mount,
		Default:             s.Default,
		Questions:           s.Questions,
		SubagentDepth:       s.SubagentDepth,
		TitleAuto:           s.TitleAuto,
		DropErroredTurns:    s.DropErroredTurns,
		AllowInsecure:       s.AllowInsecure,
		InteractionTimeout:  s.InteractionTimeout,
		InteractionFallback: s.InteractionFallback,
		SubscriberCapacity:  s.SubscriberCapacity,
		PublicRoutes:        s.PublicRoutes,
		CORS:                s.CORS,
		BaseURL:             s.BaseURL,
		InboundMedia:        s.InboundMedia,
		KeepAlive:           s.KeepAlive,
		Auth:                authViewOf(s.Auth),
		TLS:                 tlsView(s.TLS),
		Isolation:           isolationViewOf(s.Isolation),
		Sandbox:             sandboxViewOf(s.Sandbox),
		MediaUpload:         mediaUploadViewOf(s.MediaUpload),
		MCP:                 mcpViews(s.MCP),
		References:          referenceViews(s.References),
		InboundPermissions:  permissionRuleViews(s.InboundPermissions),
	}
}

func isolationViewOf(iso *config.IsolationConfig) *isolationView {
	if iso == nil {
		return nil
	}
	v := &isolationView{Mode: iso.Mode, BaseDir: iso.BaseDir, BaseReadOnly: iso.BaseReadOnly, BaseRef: iso.BaseRef}
	if iso.Cow != nil {
		v.Cow = &cowView{Mode: iso.Cow.Mode}
	}
	return v
}

func sandboxViewOf(sb *config.SandboxConfig) *sandboxView {
	if sb == nil {
		return nil
	}
	return &sandboxView{Process: sb.Process, FailurePolicy: sb.FailurePolicy}
}

func mediaUploadViewOf(up *config.MediaUploadConfig) *mediaUploadView {
	if up == nil {
		return nil
	}
	return &mediaUploadView{
		URL:       sanitizeURL(up.URL),
		IDField:   up.IDField,
		ExpiresIn: up.ExpiresIn,
		Header:    redact(up.Header),
		Value:     redact(up.Value),
		Fields:    redactSet(len(up.Fields) > 0),
	}
}

func tlsView(tls *config.TLS) *secret {
	if tls == nil {
		return nil
	}
	value := redactSet(true)
	return &value
}

func mcpViews(servers []config.MCPServer) []mcpView {
	views := make([]mcpView, 0, len(servers))
	for _, m := range servers {
		views = append(views, mcpView{
			Name:     m.Name,
			Type:     m.Type,
			Redacted: len(m.Command) > 0 || m.URL != "" || len(m.Environment) > 0 || len(m.Headers) > 0,
		})
	}
	return views
}

func referenceViews(references []config.Reference) []referenceView {
	views := make([]referenceView, 0, len(references))
	for _, r := range references {
		views = append(views, referenceView{
			Name: r.Name, Type: r.Type, Path: r.Path, Repository: sanitizeURL(r.Repository),
			Branch: r.Branch, Description: r.Description, Hidden: r.Hidden,
		})
	}
	return views
}

func permissionRuleViews(rules []config.PermissionRule) []permissionRuleView {
	views := make([]permissionRuleView, 0, len(rules))
	for _, p := range rules {
		views = append(views, permissionRuleView{Action: p.Action, Resource: p.Resource, Effect: p.Effect})
	}
	return views
}

func serverViews(servers []config.Server, only string) []serverView {
	views := make([]serverView, 0, len(servers))
	for _, s := range servers {
		if only != "" && s.Name != only {
			continue
		}
		views = append(views, serverViewOf(s))
	}
	return views
}

func authViewOf(a *config.AuthConfig) *authView {
	if a == nil {
		return nil
	}
	return &authView{Bearer: redact(a.Bearer), Username: a.Username, Password: redact(a.Password)}
}
