package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/a8m/envsubst"
	"gopkg.in/yaml.v3"
)

// Load decodes a YAML file over cfg, so keys the file omits keep their prior
// (default) values. Unknown keys are rejected.
//
// The document is expanded with envsubst before decoding, so the whole file may
// reference environment variables: `$VAR`, `${VAR}`, `${VAR:-default}` /
// `${VAR:=default}`, and `$$` for a literal `$`. A referenced variable without
// a fallback must be set, otherwise loading fails. Names beginning with a digit
// stay literal, and no environment is read implicitly. A value containing
// YAML-special characters (`#`, `: `) needs its placeholder inside a quoted
// scalar; that is a YAML style choice, not escaping.
func Load[T any](path string, cfg *T) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("config %s: %w", path, err)
	}
	// noUnset=true fails on a missing variable without a fallback; noEmpty
	// stays false; noDigit=true keeps `$1` literal.
	expanded, err := envsubst.BytesRestrictedNoDigit(data, true, false, true)
	if err != nil {
		return fmt.Errorf("config %s: %w", path, err)
	}
	decoder := yaml.NewDecoder(bytes.NewReader(expanded))
	decoder.KnownFields(true)
	if err := decoder.Decode(cfg); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("config %s: %w", path, err)
	}
	return nil
}
