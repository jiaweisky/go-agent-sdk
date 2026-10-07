// Package serveconfig holds the configuration pieces shared by the serve
// commands (cmd/a2aserve and cmd/agentserve): the credential, permission and
// TLS shapes, YAML loading, and the small provider/safety helpers.
package serveconfig

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
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

// CredentialProvider returns an SDK credential provider for creds.
func CredentialProvider(creds Credentials) agent.CredentialProvider {
	return func(schema.ProviderID) (schema.Credential, error) {
		credential := schema.Credential{APIKey: creds.APIKey}
		if creds.AWS != nil {
			credential.AWS = &schema.AWSCredentials{
				Region:          creds.AWS.Region,
				AccessKeyID:     creds.AWS.AccessKeyID,
				SecretAccessKey: creds.AWS.SecretAccessKey,
				SessionToken:    creds.AWS.SessionToken,
			}
		}
		return credential, nil
	}
}

// ModelInfoProvider returns an SDK model-info provider. When baseURL is empty,
// Bedrock derives the endpoint from the AWS region and every other protocol
// uses its route's built-in endpoint.
func ModelInfoProvider(protocol, baseURL string, aws *AWS) agent.ModelInfoProvider {
	if baseURL == "" && protocol == "bedrock-converse" && aws != nil {
		baseURL = fmt.Sprintf("https://bedrock-runtime.%s.amazonaws.com", aws.Region)
	}
	return func(id string) (schema.ModelInfo, error) {
		return agent.NewModelInfo(schema.ProtocolID(protocol), id, baseURL, schema.ModelLimit{})
	}
}

// StringList is a repeatable string flag.
type StringList []string

func (l *StringList) String() string { return strings.Join(*l, ",") }

func (l *StringList) Set(value string) error {
	*l = append(*l, value)
	return nil
}
