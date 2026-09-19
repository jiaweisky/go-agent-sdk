package config

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/user/go-agent-sdk/schema"
)

// V1PermissionAction mirrors ConfigPermissionV1.Action.
type V1PermissionAction string

const (
	V1PermissionAsk   V1PermissionAction = "ask"
	V1PermissionAllow V1PermissionAction = "allow"
	V1PermissionDeny  V1PermissionAction = "deny"
)

// V1PermissionInfo mirrors ConfigPermissionV1.Info. The raw JSON is retained so
// migration can preserve the authored key order (Effect decodes with
// propertyOrder: "original").
type V1PermissionInfo struct {
	Raw json.RawMessage
}

func (p *V1PermissionInfo) UnmarshalJSON(data []byte) error {
	p.Raw = append(json.RawMessage(nil), data...)
	return nil
}

func (p V1PermissionInfo) MarshalJSON() ([]byte, error) {
	if len(p.Raw) == 0 {
		return []byte("null"), nil
	}
	return p.Raw, nil
}

func (p V1PermissionInfo) Present() bool {
	return len(p.Raw) > 0
}

type orderedKV struct {
	Key string
	Raw json.RawMessage
}

func parseOrderedObject(raw []byte) ([]orderedKV, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	token, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if delim, ok := token.(json.Delim); !ok || delim != '{' {
		return nil, fmt.Errorf("expected object")
	}
	var result []orderedKV
	for dec.More() {
		keyToken, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, _ := keyToken.(string)
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return nil, err
		}
		result = append(result, orderedKV{Key: key, Raw: value})
	}
	if _, err := dec.Token(); err != nil {
		return nil, err
	}
	return result, nil
}

func normalizeV1Action(action string) string {
	if action == "write" || action == "patch" {
		return "edit"
	}
	return action
}

// v1Permissions mirrors migrate.ts `permissions`.
func v1Permissions(permission V1PermissionInfo, tools json.RawMessage) []schema.PermissionRule {
	var rules []schema.PermissionRule
	if len(tools) > 0 {
		if entries, err := parseOrderedObject(tools); err == nil {
			for _, entry := range entries {
				var enabled bool
				if err := json.Unmarshal(entry.Raw, &enabled); err != nil {
					continue
				}
				effect := schema.PermissionEffectDeny
				if enabled {
					effect = schema.PermissionEffectAllow
				}
				rules = append(rules, schema.PermissionRule{
					Action:   normalizeV1Action(entry.Key),
					Resource: "*",
					Effect:   effect,
				})
			}
		}
	}
	if len(permission.Raw) == 0 {
		return rules
	}
	trimmed := bytes.TrimSpace(permission.Raw)
	if len(trimmed) > 0 && trimmed[0] == '"' {
		var action string
		if err := json.Unmarshal(trimmed, &action); err == nil {
			rules = append(rules, schema.PermissionRule{
				Action:   "*",
				Resource: "*",
				Effect:   schema.PermissionEffect(action),
			})
		}
		return rules
	}
	entries, err := parseOrderedObject(permission.Raw)
	if err != nil {
		return rules
	}
	for _, entry := range entries {
		value := bytes.TrimSpace(entry.Raw)
		if len(value) > 0 && value[0] == '"' {
			var action string
			if err := json.Unmarshal(value, &action); err == nil {
				rules = append(rules, schema.PermissionRule{
					Action:   entry.Key,
					Resource: "*",
					Effect:   schema.PermissionEffect(action),
				})
			}
			continue
		}
		nested, err := parseOrderedObject(entry.Raw)
		if err != nil {
			continue
		}
		for _, rule := range nested {
			var action string
			if err := json.Unmarshal(rule.Raw, &action); err != nil {
				continue
			}
			rules = append(rules, schema.PermissionRule{
				Action:   entry.Key,
				Resource: rule.Key,
				Effect:   schema.PermissionEffect(action),
			})
		}
	}
	return rules
}
