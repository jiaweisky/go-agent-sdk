// Package serveconfig holds the configuration pieces shared by the serve
// commands (cmd/a2aserve and cmd/agentserve): the credential, permission and
// TLS shapes, YAML loading, and the small provider/safety helpers.
package serveconfig

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"os"
	"slices"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	agent "github.com/jiaweisky/go-agent-sdk"
	"github.com/jiaweisky/go-agent-sdk/schema"
)

// ShutdownTimeout bounds graceful shutdown before connections are force-closed.
const ShutdownTimeout = 10 * time.Second

// Serve runs the HTTP server until ctx is canceled, then shuts down gracefully:
// it waits up to ShutdownTimeout for in-flight requests and force-closes on
// timeout. beforeShutdown, when set, runs first so long-lived streams (SSE) end
// and Shutdown can complete. The caller closes the SDK client afterwards.
func Serve(ctx context.Context, server *http.Server, tls *TLS, beforeShutdown func()) error {
	serveErr := make(chan error, 1)
	go func() {
		if tls != nil {
			serveErr <- server.ListenAndServeTLS(tls.CertFile, tls.KeyFile)
			return
		}
		serveErr <- server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		if beforeShutdown != nil {
			beforeShutdown()
		}
		shutdownCtx, cancel := context.WithTimeout(context.Background(), ShutdownTimeout)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			_ = server.Close()
			return err
		}
		return nil
	case err := <-serveErr:
		// The listener failed (e.g. the address is in use): release the server
		// too, so it is not left with live goroutines.
		if beforeShutdown != nil {
			beforeShutdown()
		}
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

// Credentials holds the credential for the configured protocol: an API key for
// bearer/header protocols, or AWS SigV4 for Bedrock.
type Credentials struct {
	APIKey string `yaml:"apiKey"`
	AWS    *AWS   `yaml:"aws"`
}

// AWS is the SigV4 credential for Amazon Bedrock.
type AWS struct {
	Region          string `yaml:"region"`
	AccessKeyID     string `yaml:"accessKeyId"`
	SecretAccessKey string `yaml:"secretAccessKey"`
	SessionToken    string `yaml:"sessionToken"`
}

// PermissionRule is one entry of an inbound permission ruleset.
type PermissionRule struct {
	Action   string `yaml:"action"`
	Resource string `yaml:"resource"`
	Effect   string `yaml:"effect"`
}

// TLS enables HTTPS when both files are set.
type TLS struct {
	CertFile string `yaml:"certFile"`
	KeyFile  string `yaml:"keyFile"`
}

// Load decodes a YAML file over cfg, so keys the file omits keep their prior
// (default) values. Unknown keys are rejected.
func Load[T any](path string, cfg *T) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(cfg); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("config %s: %w", path, err)
	}
	return nil
}

// IsLoopback reports whether listen binds only the loopback interface.
func IsLoopback(listen string) bool {
	host, _, err := net.SplitHostPort(listen)
	if err != nil {
		return false
	}
	if host == "" {
		return false // empty host binds all interfaces
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// PermissionRuleset converts configured rules; nil means "use the SDK default".
func PermissionRuleset(rules []PermissionRule) schema.PermissionRuleset {
	if len(rules) == 0 {
		return nil
	}
	result := make(schema.PermissionRuleset, 0, len(rules))
	for _, rule := range rules {
		result = append(result, schema.PermissionRule{
			Action:   rule.Action,
			Resource: rule.Resource,
			Effect:   schema.PermissionEffect(rule.Effect),
		})
	}
	return result
}

// toSchemaCredential converts configured credentials to the SDK form.
func toSchemaCredential(creds Credentials) schema.Credential {
	credential := schema.Credential{APIKey: creds.APIKey}
	if creds.AWS != nil {
		credential.AWS = &schema.AWSCredentials{
			Region:          creds.AWS.Region,
			AccessKeyID:     creds.AWS.AccessKeyID,
			SecretAccessKey: creds.AWS.SecretAccessKey,
			SessionToken:    creds.AWS.SessionToken,
		}
	}
	return credential
}

// MediaConfig declares a model's media capability: input modalities and
// the sources (inline/url/file) it accepts. Input is sugar over
// ModelCapabilities.Input.
type MediaConfig struct {
	Input   []string `yaml:"input"`
	Sources []string `yaml:"sources"`
}

// ModelConfig is one per-model catalog entry.
type ModelConfig struct {
	// ID keys the catalog and names the model in agent configs.
	ID string `yaml:"id"`
	// Model is the wire model name sent to the provider; empty means ID.
	Model string `yaml:"model"`
	// ProviderID keys credentials; empty means Protocol.
	ProviderID string `yaml:"providerID"`
	// Protocol selects the wire route (e.g. openai-compatible-chat).
	Protocol string `yaml:"protocol"`
	// ModelBaseURL overrides the route's default endpoint.
	ModelBaseURL string `yaml:"modelBaseURL"`
	// InputModalities is shorthand for Media.Input.
	InputModalities []string `yaml:"inputModalities"`
	// Media declares modalities and accepted sources.
	Media *MediaConfig `yaml:"media"`
	// Credentials is the model's provider credential.
	Credentials Credentials `yaml:"credentials"`
	// ContextLimit / OutputLimit are the model's token limits.
	ContextLimit int `yaml:"contextLimit"`
	OutputLimit  int `yaml:"outputLimit"`
}

// providerID returns the credential key: ProviderID, or the Protocol.
func (m ModelConfig) providerID() schema.ProviderID {
	if m.ProviderID != "" {
		return schema.ProviderID(m.ProviderID)
	}
	return schema.ProviderID(m.Protocol)
}

// inputs returns the declared input modalities (Media.Input wins over the
// InputModalities shorthand).
func (m ModelConfig) inputs() []string {
	if m.Media != nil && len(m.Media.Input) > 0 {
		return m.Media.Input
	}
	return m.InputModalities
}

// baseURL returns the model's endpoint: an explicit ModelBaseURL, a
// region-derived Amazon Bedrock endpoint when only AWS credentials are set, or
// "" to use the protocol's default endpoint.
func (m ModelConfig) baseURL() string {
	if m.ModelBaseURL != "" {
		return m.ModelBaseURL
	}
	if m.Protocol == "bedrock-converse" && m.Credentials.AWS != nil && m.Credentials.AWS.Region != "" {
		return fmt.Sprintf("https://bedrock-runtime.%s.amazonaws.com", m.Credentials.AWS.Region)
	}
	return ""
}

// Catalog resolves model metadata and credentials per model id. The first
// entry is the default; an unknown id falls back to it.
type Catalog struct {
	entries    map[string]ModelConfig
	order      []string
	byProvider map[schema.ProviderID]schema.Credential
	defaultID  string
}

// NewCatalog builds a catalog from a default entry plus additional models.
func NewCatalog(defaultEntry ModelConfig, models []ModelConfig) (Catalog, error) {
	catalog := Catalog{
		entries:    make(map[string]ModelConfig),
		byProvider: make(map[schema.ProviderID]schema.Credential),
	}
	for _, entry := range append([]ModelConfig{defaultEntry}, models...) {
		id := entry.ID
		if id == "" {
			id = entry.Model
		}
		if id == "" {
			return Catalog{}, fmt.Errorf("model entry requires an id")
		}
		if entry.Protocol == "" {
			return Catalog{}, fmt.Errorf("model %q requires a protocol", id)
		}
		if _, dup := catalog.entries[id]; dup {
			return Catalog{}, fmt.Errorf("duplicate model id %q", id)
		}
		entry.ID = id
		catalog.entries[id] = entry
		catalog.order = append(catalog.order, id)
		catalog.recordCredential(entry.providerID(), toSchemaCredential(entry.Credentials))
	}
	if len(catalog.order) == 0 {
		return Catalog{}, fmt.Errorf("catalog requires at least one model")
	}
	catalog.defaultID = catalog.order[0]
	return catalog, nil
}

// recordCredential keeps the first non-empty credential seen for a provider, so
// a later entry sharing the provider cannot blank it.
func (c *Catalog) recordCredential(provider schema.ProviderID, credential schema.Credential) {
	existing, seen := c.byProvider[provider]
	if !seen || (credentialEmpty(existing) && !credentialEmpty(credential)) {
		c.byProvider[provider] = credential
	}
}

// DefaultID is the catalog's default model id.
func (c Catalog) DefaultID() string { return c.defaultID }

// IDs returns the catalog's model ids in registration order.
func (c Catalog) IDs() []string { return append([]string(nil), c.order...) }

// ModelInfo resolves a model id to SDK metadata, applying the entry's provider
// id, endpoint, token limits, modalities and media sources. An unknown id
// resolves through the default entry but keeps the requested id.
func (c Catalog) ModelInfo(id string) (schema.ModelInfo, error) {
	entry, ok := c.entries[id]
	if !ok {
		entry = c.entries[c.defaultID]
	}
	wire := entry.Model
	if wire == "" {
		wire = id
	}
	info, err := agent.NewModelInfo(
		schema.ProtocolID(entry.Protocol),
		wire,
		entry.baseURL(),
		schema.ModelLimit{Context: entry.ContextLimit, Output: entry.OutputLimit},
	)
	if err != nil {
		return schema.ModelInfo{}, err
	}
	info.ProviderID = entry.providerID()
	if entry.Model != "" {
		modelID := schema.ModelID(entry.Model)
		info.API.ID = &modelID
	}
	if inputs := entry.inputs(); len(inputs) > 0 {
		info.Capabilities.Input = inputs
	}
	if entry.Media != nil && len(entry.Media.Sources) > 0 {
		info.Capabilities.Media = &schema.ModelMediaCapability{Sources: entry.Media.Sources}
	}
	return info, nil
}

// Credentials returns the credential for a provider id.
func (c Catalog) Credentials(providerID schema.ProviderID) (schema.Credential, error) {
	if credential, ok := c.byProvider[providerID]; ok {
		return credential, nil
	}
	return schema.Credential{}, fmt.Errorf("unknown provider: %s", providerID)
}

// credentialEmpty reports whether a credential carries no secret.
func credentialEmpty(credential schema.Credential) bool {
	return credential.APIKey == "" && credential.AWS == nil
}

// MediaUploadConfig configures a generic provider Files API uploader: a
// multipart POST with a "file" part (plus any extra fields) whose JSON response
// names the stored file. Provider lifecycle (list/delete/expiry) stays with the
// host.
type MediaUploadConfig struct {
	URL     string            `yaml:"url"`
	Header  string            `yaml:"header"`
	Value   string            `yaml:"value"`
	Fields  map[string]string `yaml:"fields"`
	IDField string            `yaml:"idField"`
	// ExpiresIn, when > 0, is the reference lifetime in seconds.
	ExpiresIn int `yaml:"expiresIn"`
}

// Uploader builds an SDK MediaUploader from the config.
func (c MediaUploadConfig) Uploader() schema.MediaUploader {
	return mediaUploadClient{config: c}
}

type mediaUploadClient struct{ config MediaUploadConfig }

// Upload posts the media as multipart/form-data and returns the file reference.
func (c mediaUploadClient) Upload(ctx context.Context, media schema.MediaUpload) (schema.MediaRef, error) {
	if c.config.URL == "" {
		return schema.MediaRef{}, errors.New("media upload: url is required")
	}
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	name := "file"
	if media.Name != nil && *media.Name != "" {
		name = *media.Name
	}
	part, err := writer.CreateFormFile("file", name)
	if err != nil {
		return schema.MediaRef{}, err
	}
	if _, err := part.Write(media.Data); err != nil {
		return schema.MediaRef{}, err
	}
	for key, value := range c.config.Fields {
		if err := writer.WriteField(key, value); err != nil {
			return schema.MediaRef{}, err
		}
	}
	if err := writer.Close(); err != nil {
		return schema.MediaRef{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.config.URL, &buf)
	if err != nil {
		return schema.MediaRef{}, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if c.config.Header != "" && c.config.Value != "" {
		req.Header.Set(c.config.Header, c.config.Value)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return schema.MediaRef{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return schema.MediaRef{}, fmt.Errorf("media upload: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var decoded map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return schema.MediaRef{}, err
	}
	idField := c.config.IDField
	if idField == "" {
		idField = "id"
	}
	id, _ := decoded[idField].(string)
	if id == "" {
		return schema.MediaRef{}, fmt.Errorf("media upload: response has no %q", idField)
	}
	ref := schema.MediaRef{Source: "file", ID: id}
	if c.config.ExpiresIn > 0 {
		expires := time.Now().Add(time.Duration(c.config.ExpiresIn) * time.Second)
		ref.ExpiresAt = &expires
	}
	return ref, nil
}

// InputModalityNames are the model input modalities a host may declare.
var InputModalityNames = []string{"text", "image", "audio", "video", "pdf"}

// ValidateInputModalities rejects unknown modality names.
func ValidateInputModalities(modalities []string) error {
	for _, modality := range modalities {
		if !slices.Contains(InputModalityNames, modality) {
			return fmt.Errorf("unknown input modality %q (text, image, audio, video, pdf)", modality)
		}
	}
	return nil
}

// MediaSourceNames are the media sources a model may declare.
var MediaSourceNames = []string{"inline", "url", "file"}

// ValidateMediaSources rejects unknown media source names.
func ValidateMediaSources(sources []string) error {
	for _, source := range sources {
		if !slices.Contains(MediaSourceNames, source) {
			return fmt.Errorf("unknown media source %q (inline, url, file)", source)
		}
	}
	return nil
}

// Validate checks one catalog entry's media declaration.
func (m ModelConfig) Validate() error {
	if m.ID == "" && m.Model == "" {
		return fmt.Errorf("model entry requires an id")
	}
	if m.Protocol == "" {
		return fmt.Errorf("model entry requires a protocol")
	}
	if err := ValidateInputModalities(m.inputs()); err != nil {
		return err
	}
	if m.Media != nil {
		if err := ValidateMediaSources(m.Media.Sources); err != nil {
			return err
		}
	}
	return nil
}

// StringList is a repeatable string flag.
type StringList []string

func (l *StringList) String() string { return strings.Join(*l, ",") }

func (l *StringList) Set(value string) error {
	*l = append(*l, value)
	return nil
}
