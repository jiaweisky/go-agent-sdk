package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// envRef matches `$$` (a literal `$`) or `${VAR}` / `${VAR:-default}`.
var envRef = regexp.MustCompile(`\$\$|\$\{([A-Za-z_][A-Za-z0-9_]*)(?::-([^}]*))?\}`)

// Load decodes a YAML file over cfg, so keys the file omits keep their prior
// (default) values. Unknown keys are rejected.
//
// String values may reference environment variables explicitly: `${VAR}` is
// replaced by VAR's value (an error if VAR is unset), `${VAR:-default}` falls
// back to `default`, and `$$` is a literal `$`. Environment is never read
// implicitly — only where the file asks for it.
func Load[T any](path string, cfg *T) error {
	_, err := LoadWithEnv(path, cfg)
	return err
}

// LoadWithEnv is Load, additionally returning the environment variable names
// that were expanded (in first-seen order), for provenance.
func LoadWithEnv[T any](path string, cfg *T) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("config %s: %w", path, err)
	}
	if doc.Kind == 0 { // empty file: keep cfg as-is
		return nil, nil
	}
	env := &envRefs{}
	if err := expandEnvNode(&doc, env); err != nil {
		return nil, fmt.Errorf("config %s: %w", path, err)
	}
	expanded, err := yaml.Marshal(&doc)
	if err != nil {
		return nil, fmt.Errorf("config %s: %w", path, err)
	}
	decoder := yaml.NewDecoder(bytes.NewReader(expanded))
	decoder.KnownFields(true)
	if err := decoder.Decode(cfg); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("config %s: %w", path, err)
	}
	return env.order, nil
}

// envRefs collects the environment variable names referenced while expanding.
type envRefs struct {
	seen  map[string]bool
	order []string
}

func (e *envRefs) add(name string) {
	if e.seen == nil {
		e.seen = map[string]bool{}
	}
	if !e.seen[name] {
		e.seen[name] = true
		e.order = append(e.order, name)
	}
}

// expandEnvNode expands environment references in every string scalar value
// (mapping keys are left untouched).
func expandEnvNode(node *yaml.Node, env *envRefs) error {
	switch node.Kind {
	case yaml.DocumentNode, yaml.SequenceNode:
		for _, child := range node.Content {
			if err := expandEnvNode(child, env); err != nil {
				return err
			}
		}
	case yaml.MappingNode:
		// Content is [key0, val0, key1, val1, ...]; expand the values.
		for i := 1; i < len(node.Content); i += 2 {
			if err := expandEnvNode(node.Content[i], env); err != nil {
				return err
			}
		}
	case yaml.ScalarNode:
		if node.Tag != "!!str" || !strings.Contains(node.Value, "$") {
			return nil
		}
		expanded, err := expandEnv(node.Value, env)
		if err != nil {
			return err
		}
		node.Value = expanded
	}
	return nil
}

// expandEnv substitutes `${VAR}` and `${VAR:-default}`; `$$` is a literal `$`. A
// referenced variable with no value and no default is an error.
func expandEnv(value string, env *envRefs) (string, error) {
	var missing []string
	out := envRef.ReplaceAllStringFunc(value, func(match string) string {
		if match == "$$" {
			return "$"
		}
		sub := envRef.FindStringSubmatch(match)
		name, def := sub[1], sub[2]
		if env != nil {
			env.add(name)
		}
		if v, ok := os.LookupEnv(name); ok {
			return v
		}
		if strings.Contains(match, ":-") {
			return def
		}
		missing = append(missing, name)
		return ""
	})
	if len(missing) > 0 {
		return "", fmt.Errorf("undefined environment variable(s): %s", strings.Join(missing, ", "))
	}
	return out, nil
}

// IsLoopback reports whether listen binds only the loopback interface.
func IsLoopback(listen string) bool {
	host, _, err := net.SplitHostPort(listen)
	if err != nil {
		return false
	}
	if host == "" {
		return false // empty host binds all interfaces
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
