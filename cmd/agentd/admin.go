package main

import (
	"crypto/subtle"
	"net/http"
	"net/url"
	"sort"
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
	Headers         []string        `json:"headers,omitempty"` // header names only; values are not returned
	Credentials     credentialsView `json:"credentials"`
	ContextLimit    int             `json:"contextLimit"`
	OutputLimit     int             `json:"outputLimit"`
}

type mediaView struct {
	Input []string            `json:"input,omitempty"`
	Forms map[string][]string `json:"forms,omitempty"`
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
	Media              *inboundMediaView    `json:"media,omitempty"`
	Storage            *storageView         `json:"storage,omitempty"`
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
	MaxBodyBytes        int64    `json:"maxBodyBytes,omitempty"`
	// A2A-only.
	BaseURL      string              `json:"baseURL,omitempty"`
	Cards        map[string]cardView `json:"cards,omitempty"`
	InboundMedia string              `json:"inboundMedia,omitempty"`

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
	URL          string `json:"url,omitempty"`
	IDField      string `json:"idField,omitempty"`
	ExpiresIn    int    `json:"expiresIn,omitempty"`
	ExpiresAfter int    `json:"expiresAfter,omitempty"`
	Header       secret `json:"header"`
	Value        secret `json:"value"`
	Fields       secret `json:"fields"`
}

type inboundMediaView struct {
	InlineLimitBytes *mediaSizeLimitView `json:"inlineLimitBytes,omitempty"`
	Prefer           []string            `json:"prefer,omitempty"`
	Upload           bool                `json:"upload,omitempty"`
	Downsample       bool                `json:"downsample,omitempty"`
	SpoolDir         string              `json:"spoolDir,omitempty"`
	FetchURL         bool                `json:"fetchURL,omitempty"`
	MaxBytes         *mediaSizeLimitView `json:"maxBytes,omitempty"`
}

// mediaSizeLimitView is a per-modality byte budget. It mirrors
// config.MediaSizeLimit with camelCase JSON names, and is omitted entirely when
// no budget is configured.
type mediaSizeLimitView struct {
	Default int64 `json:"default,omitempty"`
	Image   int64 `json:"image,omitempty"`
	Audio   int64 `json:"audio,omitempty"`
	Video   int64 `json:"video,omitempty"`
	File    int64 `json:"file,omitempty"`
}

// storageView is the redacted media-store config. Credential-bearing URLs have
// their userinfo stripped; every secret becomes a configured/redacted marker.
type storageView struct {
	Backend        string          `json:"backend,omitempty"`
	URL            string          `json:"url,omitempty"`
	Auth           string          `json:"auth,omitempty"`
	Username       string          `json:"username,omitempty"`
	Password       secret          `json:"password"`
	Token          secret          `json:"token"`
	Header         string          `json:"header,omitempty"`
	Value          secret          `json:"value"`
	PresignSecret  secret          `json:"presignSecret"`
	PresignURL     string          `json:"presignURL,omitempty"`
	PresignTTL     string          `json:"presignTTL,omitempty"`
	Root           string          `json:"root,omitempty"`
	ServeBaseURL   string          `json:"serveBaseURL,omitempty"`
	S3             *s3StorageView  `json:"s3,omitempty"`
	COS            *cosStorageView `json:"cos,omitempty"`
	MediaTypes     []string        `json:"mediaTypes,omitempty"`
	MaxInlineBytes int64           `json:"maxInlineBytes,omitempty"`
	MaxBytes       int64           `json:"maxBytes,omitempty"`
}

type s3StorageView struct {
	Endpoint   string `json:"endpoint,omitempty"`
	Region     string `json:"region,omitempty"`
	Bucket     string `json:"bucket,omitempty"`
	AccessKey  secret `json:"accessKey"`
	SecretKey  secret `json:"secretKey"`
	PathStyle  bool   `json:"pathStyle,omitempty"`
	PresignTTL string `json:"presignTTL,omitempty"`
}

type cosStorageView struct {
	Region       string `json:"region,omitempty"`
	Bucket       string `json:"bucket,omitempty"`
	SecretID     secret `json:"secretID"`
	SecretKey    secret `json:"secretKey"`
	SessionToken secret `json:"sessionToken"`
	Endpoint     string `json:"endpoint,omitempty"`
	PresignTTL   string `json:"presignTTL,omitempty"`
}

// cardView mirrors an operator-authored AgentCard override; cards carry no
// secrets.
type cardView struct {
	Name             string        `json:"name,omitempty"`
	Description      string        `json:"description,omitempty"`
	Version          string        `json:"version,omitempty"`
	DocumentationURL string        `json:"documentationUrl,omitempty"`
	IconURL          string        `json:"iconUrl,omitempty"`
	Provider         *providerView `json:"provider,omitempty"`
	Skills           []skillView   `json:"skills,omitempty"`
}

type providerView struct {
	Organization string `json:"organization,omitempty"`
	URL          string `json:"url,omitempty"`
}

type skillView struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Tags        []string `json:"tags,omitempty"`
	Examples    []string `json:"examples,omitempty"`
	InputModes  []string `json:"inputModes,omitempty"`
	OutputModes []string `json:"outputModes,omitempty"`
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
		v.Media = &mediaView{Input: m.Media.Input, Forms: m.Media.Forms}
	}
	if len(m.Headers) > 0 {
		v.Headers = make([]string, 0, len(m.Headers))
		for name := range m.Headers {
			v.Headers = append(v.Headers, name)
		}
		sort.Strings(v.Headers)
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
		MaxBodyBytes:        s.MaxBodyBytes,
		BaseURL:             s.BaseURL,
		Cards:               cardViews(s.Cards),
		InboundMedia:        s.InboundMedia,
		KeepAlive:           s.KeepAlive,
		Auth:                authViewOf(s.Auth),
		TLS:                 tlsView(s.TLS),
		Isolation:           isolationViewOf(s.Isolation),
		Sandbox:             sandboxViewOf(s.Sandbox),
		MediaUpload:         mediaUploadViewOf(s.MediaUpload),
		Media:               inboundMediaViewOf(s.Media),
		Storage:             storageViewOf(s.Storage),
		MCP:                 mcpViews(s.MCP),
		References:          referenceViews(s.References),
		InboundPermissions:  permissionRuleViews(s.InboundPermissions),
	}
}

func storageViewOf(s *config.StorageConfig) *storageView {
	if s == nil {
		return nil
	}
	return &storageView{
		Backend:        s.Backend,
		URL:            sanitizeURL(s.URL),
		Auth:           s.Auth,
		Username:       s.Username,
		Password:       redact(s.Password),
		Token:          redact(s.Token),
		Header:         s.Header,
		Value:          redact(s.Value),
		PresignSecret:  redact(s.PresignSecret),
		PresignURL:     sanitizeURL(s.PresignURL),
		PresignTTL:     s.PresignTTL,
		Root:           s.Root,
		ServeBaseURL:   sanitizeURL(s.ServeBaseURL),
		S3:             s3StorageViewOf(s.S3),
		COS:            cosStorageViewOf(s.COS),
		MediaTypes:     s.MediaTypes,
		MaxInlineBytes: s.MaxInlineBytes,
		MaxBytes:       s.MaxBytes,
	}
}

func s3StorageViewOf(s *config.S3Config) *s3StorageView {
	if s == nil {
		return nil
	}
	return &s3StorageView{
		Endpoint:   sanitizeURL(s.Endpoint),
		Region:     s.Region,
		Bucket:     s.Bucket,
		AccessKey:  redact(s.AccessKey),
		SecretKey:  redact(s.SecretKey),
		PathStyle:  s.PathStyle,
		PresignTTL: s.PresignTTL,
	}
}

func cosStorageViewOf(c *config.COSConfig) *cosStorageView {
	if c == nil {
		return nil
	}
	return &cosStorageView{
		Region:       c.Region,
		Bucket:       c.Bucket,
		SecretID:     redact(c.SecretID),
		SecretKey:    redact(c.SecretKey),
		SessionToken: redact(c.SessionToken),
		Endpoint:     sanitizeURL(c.Endpoint),
		PresignTTL:   c.PresignTTL,
	}
}

func cardViews(cards map[string]config.CardConfig) map[string]cardView {
	if len(cards) == 0 {
		return nil
	}
	views := make(map[string]cardView, len(cards))
	for agent, card := range cards {
		views[agent] = cardViewOf(card)
	}
	return views
}

func cardViewOf(c config.CardConfig) cardView {
	v := cardView{
		Name:             c.Name,
		Description:      c.Description,
		Version:          c.Version,
		DocumentationURL: sanitizeURL(c.DocumentationURL),
		IconURL:          sanitizeURL(c.IconURL),
	}
	if c.Provider != nil {
		v.Provider = &providerView{Organization: c.Provider.Organization, URL: sanitizeURL(c.Provider.URL)}
	}
	if len(c.Skills) > 0 {
		v.Skills = make([]skillView, 0, len(c.Skills))
		for _, s := range c.Skills {
			v.Skills = append(v.Skills, skillView{
				ID: s.ID, Name: s.Name, Description: s.Description,
				Tags: s.Tags, Examples: s.Examples, InputModes: s.InputModes, OutputModes: s.OutputModes,
			})
		}
	}
	return v
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
		URL:          sanitizeURL(up.URL),
		IDField:      up.IDField,
		ExpiresIn:    up.ExpiresIn,
		ExpiresAfter: up.ExpiresAfter,
		Header:       redact(up.Header),
		Value:        redact(up.Value),
		Fields:       redactSet(len(up.Fields) > 0),
	}
}

func inboundMediaViewOf(m *config.InboundMediaConfig) *inboundMediaView {
	if m == nil {
		return nil
	}
	view := &inboundMediaView{
		InlineLimitBytes: mediaSizeLimitViewOf(m.InlineLimitBytes),
		Prefer:           m.Prefer,
		Upload:           m.Upload,
		Downsample:       m.Downsample,
		FetchURL:         m.FetchURL,
		MaxBytes:         mediaSizeLimitViewOf(m.MaxBytes),
	}
	if m.Spool != nil {
		view.SpoolDir = m.Spool.Dir
	}
	return view
}

// mediaSizeLimitViewOf mirrors a media size budget, or nil when it is unset.
func mediaSizeLimitViewOf(m config.MediaSizeLimit) *mediaSizeLimitView {
	if m == (config.MediaSizeLimit{}) {
		return nil
	}
	return &mediaSizeLimitView{Default: m.Default, Image: m.Image, Audio: m.Audio, Video: m.Video, File: m.File}
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
