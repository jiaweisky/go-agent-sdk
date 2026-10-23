package config

import "testing"

func TestValidateS3(t *testing.T) {
	if err := (&StorageConfig{Backend: StorageS3}).Validate(); err == nil {
		t.Fatal("s3 without a config must be rejected")
	}
	if err := (&StorageConfig{Backend: StorageS3, S3: &S3Config{Endpoint: "https://s3", Region: "r", Bucket: "b"}}).Validate(); err != nil {
		t.Fatalf("anonymous s3 rejected: %v", err)
	}
	if err := (&StorageConfig{Backend: StorageS3, S3: &S3Config{Endpoint: "https://s3", Region: "r", Bucket: "b", AccessKey: "a"}}).Validate(); err == nil {
		t.Fatal("s3 with half a credential must be rejected")
	}
	if err := (&StorageConfig{Backend: StorageS3, S3: &S3Config{Endpoint: "https://s3", Region: "r", Bucket: "b", AccessKey: "a", SecretKey: "s"}}).Validate(); err != nil {
		t.Fatalf("valid s3 rejected: %v", err)
	}
}
func TestValidateS3RejectsNegativePresignTTL(t *testing.T) {
	cfg := &StorageConfig{Backend: StorageS3, S3: &S3Config{
		Endpoint: "https://s3", Region: "r", Bucket: "b", PresignTTL: "-1m",
	}}
	if err := cfg.Validate(); err == nil {
		t.Fatal("a negative presignTTL must be rejected")
	}
}
