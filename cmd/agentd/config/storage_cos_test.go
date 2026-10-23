package config

import "testing"

func TestValidateCOS(t *testing.T) {
	if err := (&StorageConfig{Backend: StorageCOS}).Validate(); err == nil {
		t.Fatal("cos without a config must be rejected")
	}
	if err := (&StorageConfig{Backend: StorageCOS, COS: &COSConfig{Region: "ap-guangzhou", Bucket: "b"}}).Validate(); err != nil {
		t.Fatalf("anonymous cos rejected: %v", err)
	}
	if err := (&StorageConfig{Backend: StorageCOS, COS: &COSConfig{Region: "ap-guangzhou", Bucket: "b", SecretID: "id"}}).Validate(); err == nil {
		t.Fatal("cos with half a credential must be rejected")
	}
	if err := (&StorageConfig{Backend: StorageCOS, COS: &COSConfig{Region: "ap-guangzhou", Bucket: "b", SecretID: "a", SecretKey: "s"}}).Validate(); err != nil {
		t.Fatalf("valid cos rejected: %v", err)
	}
}
func TestValidateCOSRejectsNegativePresignTTL(t *testing.T) {
	cfg := &StorageConfig{Backend: StorageCOS, COS: &COSConfig{
		Region: "ap-guangzhou", Bucket: "b", PresignTTL: "-1m",
	}}
	if err := cfg.Validate(); err == nil {
		t.Fatal("a negative presignTTL must be rejected")
	}
}
