package config

import "fmt"

// S3Config configures an S3-compatible object store (AWS S3, Tencent COS, MinIO,
// ...). Requests are signed with AWS Signature Version 4.
type S3Config struct {
	// Endpoint is the base URL, e.g. https://s3.us-east-1.amazonaws.com or a COS
	// endpoint (https://cos.ap-guangzhou.myqcloud.com).
	Endpoint string `yaml:"endpoint"`
	// Region is the signing region (e.g. us-east-1, ap-guangzhou).
	Region string `yaml:"region"`
	// Bucket is the target bucket.
	Bucket string `yaml:"bucket"`
	// AccessKey / SecretKey are the signing credentials.
	AccessKey string `yaml:"accessKey"`
	SecretKey string `yaml:"secretKey"`
	// PathStyle addresses objects as endpoint/bucket/key (MinIO/older COS) rather
	// than bucket.endpoint/key.
	PathStyle bool `yaml:"pathStyle"`
	// PresignTTL bounds a returned presigned GET url's lifetime.
	// Empty defaults to 15m.
	PresignTTL string `yaml:"presignTTL"`
}

// HasCredentials reports whether the bucket has a signing key pair; without one
// it is public and requests are unsigned.
func (c S3Config) HasCredentials() bool { return c.AccessKey != "" && c.SecretKey != "" }

// Validate rejects a malformed S3 backend.
func (c *S3Config) Validate() error {
	switch {
	case c.Endpoint == "":
		return fmt.Errorf("storage.s3.endpoint is required")
	case c.Region == "":
		return fmt.Errorf("storage.s3.region is required")
	case c.Bucket == "":
		return fmt.Errorf("storage.s3.bucket is required")
	case (c.AccessKey == "") != (c.SecretKey == ""):
		return fmt.Errorf("storage.s3.accessKey and secretKey must be set together, or both empty for an anonymous bucket")
	}
	if c.PresignTTL != "" {
		if err := validateNonNegativeDuration("storage.s3.presignTTL", c.PresignTTL); err != nil {
			return err
		}
	}
	return nil
}
