package config

import "github.com/jiaweisky/go-agent-sdk/pkg/schema"

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
