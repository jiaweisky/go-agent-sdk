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

// Load decodes a single YAML document over cfg, so keys the file omits keep
// their prior (default) values. An empty file keeps cfg unchanged. Unknown keys
// and additional YAML documents are rejected.
//
// The document is expanded with envsubst before decoding, so the whole file may
// reference environment variables: `$VAR`, `${VAR}`, `${VAR:-default}` /
// `${VAR:=default}`, and `$$` for a literal `$`. A referenced variable without
// a fallback must be set, otherwise loading fails. Names beginning with a digit
// stay literal, and only explicitly referenced variables are expanded. A value
// containing YAML-special characters (`#`, `: `) needs its placeholder inside a
// quoted scalar; that is a YAML style choice, not escaping.
func Load[T any](path string, cfg *T) error {
	if cfg == nil {
		return fmt.Errorf("config %s: nil destination", path)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return configError(path, err)
	}

	// noUnset=true fails on a missing variable without a fallback; noEmpty
	// stays false; noDigit=true keeps `$1` literal.
	expanded, err := envsubst.BytesRestrictedNoDigit(data, true, false, true)
	if err != nil {
		return configError(path, err)
	}

	decoder := yaml.NewDecoder(bytes.NewReader(expanded))
	decoder.KnownFields(true)
	if err := decoder.Decode(cfg); err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return configError(path, err)
	}

	var extra yaml.Node
	if err := decoder.Decode(&extra); err == nil {
		return fmt.Errorf("config %s: multiple YAML documents are not supported", path)
	} else if !errors.Is(err, io.EOF) {
		return configError(path, err)
	}
	return nil
}

// configError adds the config path to a read, expansion or decode error.
func configError(path string, err error) error {
	return fmt.Errorf("config %s: %w", path, err)
}
