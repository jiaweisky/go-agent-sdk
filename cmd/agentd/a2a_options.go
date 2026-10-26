package main

import (
	"crypto"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"

	agentdclient "github.com/jiaweisky/go-agent-sdk/cmd/agentd/client"
	config "github.com/jiaweisky/go-agent-sdk/cmd/agentd/config"
	a2aserver "github.com/jiaweisky/go-agent-sdk/pkg/a2a/server"
	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

// a2aExtendedCards maps each exposed agent with an extended-card override to its
// resolved AgentExpose, inheriting the public card's model-derived fields.
func a2aExtendedCards(cards map[string]config.CardConfig, exposed []schema.AgentInfo) map[schema.AgentID]schema.AgentExpose {
	out := map[schema.AgentID]schema.AgentExpose{}
	for _, info := range exposed {
		if info.Expose == nil {
			continue
		}
		card, ok := cards[string(info.ID)]
		if !ok || card.Extended == nil {
			continue
		}
		out[info.ID] = agentdclient.ExtendedExposeFrom(*info.Expose, card)
	}
	return out
}

// a2aPushOptions converts the configured push block, or returns nil when disabled.
func a2aPushOptions(p *config.PushNotificationsConfig) (*a2aserver.PushNotificationsConfig, error) {
	if !p.IsEnabled() {
		return nil, nil
	}
	timeout, err := config.ParseOptionalDuration(p.Timeout)
	if err != nil {
		return nil, fmt.Errorf("pushNotifications.timeout: %w", err)
	}
	return &a2aserver.PushNotificationsConfig{
		Timeout:              timeout,
		AllowPrivateNetworks: p.AllowPrivateNetworks,
		FailOnError:          p.FailOnError,
	}, nil
}

// a2aSigningOptions loads the signing key and builds the signer config.
func a2aSigningOptions(s *config.SigningConfig) (*a2aserver.SigningConfig, error) {
	if s == nil {
		return nil, nil
	}
	key, err := loadSigningKey(s.KeyFile)
	if err != nil {
		return nil, err
	}
	return &a2aserver.SigningConfig{
		PrivateKey: key,
		KeyID:      s.KeyID,
		Algorithm:  s.Algorithm,
		JWKSURL:    s.JWKSURL,
	}, nil
}

// loadSigningKey reads one PEM-encoded private key.
func loadSigningKey(path string) (crypto.Signer, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read signing key: %w", err)
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("signing key is not PEM encoded")
	}
	var key any
	switch block.Type {
	case "RSA PRIVATE KEY":
		key, err = x509.ParsePKCS1PrivateKey(block.Bytes)
	case "EC PRIVATE KEY":
		key, err = x509.ParseECPrivateKey(block.Bytes)
	default:
		key, err = x509.ParsePKCS8PrivateKey(block.Bytes)
	}
	if err != nil {
		return nil, fmt.Errorf("parse signing key: %w", err)
	}
	signer, ok := key.(crypto.Signer)
	if !ok {
		return nil, errors.New("signing key does not implement crypto.Signer")
	}
	return signer, nil
}

// a2aTransports converts configured bindings to the SDK's transport values.
func a2aTransports(transports []config.A2ATransport) []a2aserver.Transport {
	if len(transports) == 0 {
		return nil
	}
	out := make([]a2aserver.Transport, 0, len(transports))
	for _, transport := range transports {
		out = append(out, a2aserver.Transport(transport))
	}
	return out
}
