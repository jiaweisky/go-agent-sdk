package config

import "fmt"

// COSConfig configures a Tencent COS bucket reached over its native REST API. It
// is signed with the COS SHA1 scheme (q-sign-algorithm=sha1), not S3/SigV4.
type COSConfig struct {
	// Region is the bucket's region (e.g. ap-guangzhou).
	Region string `yaml:"region"`
	// Bucket is the target bucket (e.g. examplebucket-1250000000).
	Bucket string `yaml:"bucket"`
	// SecretID / SecretKey are the COS credentials.
	SecretID  string `yaml:"secretID"`
	SecretKey string `yaml:"secretKey"`
	// SessionToken is an optional temporary-credential token (x-cos-security-token).
	SessionToken string `yaml:"sessionToken"`
	// Endpoint overrides the default host (<bucket>.cos.<region>.myqcloud.com).
	Endpoint string `yaml:"endpoint"`
	// PresignTTL bounds a returned presigned GET url's lifetime.
	// Empty defaults to 15m.
	PresignTTL string `yaml:"presignTTL"`
}

// HasCredentials reports whether the bucket has a signing key pair; without one
// it is public and requests are unsigned.
func (c COSConfig) HasCredentials() bool { return c.SecretID != "" && c.SecretKey != "" }

// Validate rejects a malformed COS backend.
func (c *COSConfig) Validate() error {
	switch {
	case c.Region == "":
		return fmt.Errorf("storage.cos.region is required")
	case c.Bucket == "":
		return fmt.Errorf("storage.cos.bucket is required")
	case (c.SecretID == "") != (c.SecretKey == ""):
		return fmt.Errorf("storage.cos.secretID and secretKey must be set together, or both empty for an anonymous bucket")
	}

	if c.PresignTTL != "" {
		if err := validateNonNegativeDuration("storage.cos.presignTTL", c.PresignTTL); err != nil {
			return err
		}
	}

	return nil
}
