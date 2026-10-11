package config

import "errors"

// Config is the agentd configuration: a global model catalog plus a list of
// servers, each exposing one model subset. String values may reference
// environment variables explicitly (`${VAR}` / `${VAR:-default}`); none are read
// implicitly.
type Config struct {
	Models  []ModelConfig `yaml:"models"`
	Servers []Server      `yaml:"servers"`
}

// Catalog builds the global model catalog from the configured entries.
func (c Config) Catalog() (Catalog, error) {
	if len(c.Models) == 0 {
		return Catalog{}, errors.New("models is required")
	}
	return NewCatalog(c.Models[0], c.Models[1:])
}
