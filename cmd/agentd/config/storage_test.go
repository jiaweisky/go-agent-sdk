package config

import "testing"

func TestValidateHTTPAuth(t *testing.T) {
	for _, c := range []StorageConfig{
		{URL: "https://x/", Auth: "basic"},
		{URL: "https://x/", Auth: "bearer"},
		{URL: "https://x/", Auth: "digest"},
		{URL: "https://x/", Header: "X-K"},
	} {
		if err := c.Validate(); err == nil {
			t.Fatalf("expected rejection: %+v", c)
		}
	}
	for _, c := range []StorageConfig{
		{URL: "https://x/"},
		{URL: "https://x/", Auth: "none"},
		{URL: "https://x/", Auth: "basic", Username: "u", Password: "p"},
		{URL: "https://x/", Auth: "bearer", Token: "t"},
		{URL: "https://x/", Header: "X-K", Value: "v"},
	} {
		if err := c.Validate(); err != nil {
			t.Fatalf("expected valid: %+v: %v", c, err)
		}
	}
}

func TestValidateStorage(t *testing.T) {
	var nilStorage *StorageConfig
	if err := nilStorage.Validate(); err != nil {
		t.Fatalf("nil storage should be valid: %v", err)
	}
	if err := (&StorageConfig{URL: "https://store.example/x/"}).Validate(); err != nil {
		t.Fatalf("http storage with url should be valid: %v", err)
	}
	if err := (&StorageConfig{}).Validate(); err == nil {
		t.Fatal("storage without a url must be rejected")
	}
	if err := (&StorageConfig{Backend: StorageLocal}).Validate(); err == nil {
		t.Fatal("local storage without a root must be rejected")
	}
	if err := (&StorageConfig{Backend: StorageLocal, Root: "/shared/media"}).Validate(); err != nil {
		t.Fatalf("local storage with a root should be valid: %v", err)
	}
	if err := (&StorageConfig{Backend: "ftp", URL: "https://x"}).Validate(); err == nil {
		t.Fatal("an unknown backend must be rejected")
	}
}
func TestValidateStorageRejectsNegativePresignTTL(t *testing.T) {
	cfg := &StorageConfig{URL: "https://x/", PresignTTL: "-1m"}
	if err := cfg.Validate(); err == nil {
		t.Fatal("a negative presignTTL must be rejected")
	}
}
