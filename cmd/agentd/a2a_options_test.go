package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
	"time"

	config "github.com/jiaweisky/go-agent-sdk/cmd/agentd/config"
)

func TestA2ASigningOptionsLoadsPEM(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	path := filepath.Join(t.TempDir(), "card.pem")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	options, err := a2aSigningOptions(&config.SigningConfig{KeyFile: path, KeyID: "key-1"})
	if err != nil {
		t.Fatalf("signing options: %v", err)
	}
	if options.PrivateKey == nil || options.KeyID != "key-1" {
		t.Fatalf("options = %+v", options)
	}
}

func TestA2APushOptions(t *testing.T) {
	options, err := a2aPushOptions(&config.PushNotificationsConfig{Enabled: true, Timeout: "5s"})
	if err != nil {
		t.Fatalf("push options: %v", err)
	}
	if options == nil || options.Timeout != 5*time.Second {
		t.Fatalf("options = %+v", options)
	}
	disabled, err := a2aPushOptions(&config.PushNotificationsConfig{})
	if err != nil || disabled != nil {
		t.Fatalf("disabled options = %+v, err = %v", disabled, err)
	}
}
