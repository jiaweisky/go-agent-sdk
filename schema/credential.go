package schema

// Credential is a provider secret. The protocol decides how it is applied
// (bearer, header or SigV4), so callers supply only the secret.
type Credential struct {
	// APIKey authenticates bearer- and header-based protocols.
	APIKey string
	// AWS is the SigV4 credential for Amazon Bedrock.
	AWS *AWSCredentials
}

// AWSCredentials is the SigV4 credential for Amazon Bedrock.
type AWSCredentials struct {
	Region          string
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
}
