package main

import (
	"strings"
	"testing"

	"github.com/jiaweisky/go-agent-sdk"
	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

// newPluginClient builds a client with the given plugins, without the catalog.
func newPluginClient(t *testing.T, plugins []agent.PluginConfig) *agent.Client {
	t.Helper()
	client, err := agent.NewClient(&agent.ClientOptions{
		Protocol: "openai-responses", Credentials: agent.APIKey("test"),
		Model: "test-model", Root: t.TempDir(), Plugins: plugins,
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

// TestGreeterPluginLoadsAndUnloads guards the plugin lifecycle: its Effect runs at
// startup (registering the tool) and its cleanup runs on Remove.
func TestGreeterPluginLoadsAndUnloads(t *testing.T) {
	var out strings.Builder
	client := newPluginClient(t, []agent.PluginConfig{greeterPlugin(&console{out: &out, lineStart: true})})
	if !strings.Contains(out.String(), greeterPluginID+" loaded") {
		t.Fatalf("out = %q, want the load line", out.String())
	}
	if err := client.Plugin().Remove(schema.PluginID(greeterPluginID)); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if !strings.Contains(out.String(), greeterPluginID+" unloaded") {
		t.Fatalf("out = %q, want the unload line", out.String())
	}
}

// TestPermissionPluginAddsAskRule guards the migrated permission plugin.
func TestPermissionPluginAddsAskRule(t *testing.T) {
	client := newPluginClient(t, []agent.PluginConfig{permissionPlugin()})
	info := client.Agents().Get(schema.AgentID(agentName))
	if info == nil {
		t.Fatalf("agent %s missing", agentName)
	}
	want := map[string]bool{"bash": false, convertLengthName: false}
	for _, rule := range info.Permissions {
		if rule.Effect == schema.PermissionEffectAsk {
			if _, ok := want[rule.Action]; ok {
				want[rule.Action] = true
			}
		}
	}
	for action, found := range want {
		if !found {
			t.Fatalf("permissions = %+v, want a %s ask rule", info.Permissions, action)
		}
	}
}

// TestPluginCommandUsage guards the no-argument case.
func TestPluginCommandUsage(t *testing.T) {
	out := runCommand(t, nil, func(c *chat) {
		if !handleCommand(c, "/plugin") {
			t.Error("/plugin was not recognised")
		}
	})
	if !strings.Contains(out, "usage: /plugin remove") {
		t.Fatalf("out = %q, want the usage hint", out)
	}
}

// TestPluginCommandUnknownID guards that removing an id the chat never loaded is
// reported instead of claiming success.
func TestPluginCommandUnknownID(t *testing.T) {
	out := runCommand(t, nil, func(c *chat) {
		if !handleCommand(c, "/plugin remove nope") {
			t.Error("/plugin was not recognised")
		}
	})
	if !strings.Contains(out, "plugin nope is not active") {
		t.Fatalf("out = %q, want the not-active hint", out)
	}
}

// TestPluginCommandRemovesKnownID guards that a loaded id is removed once, then
// reported as inactive.
func TestPluginCommandRemovesKnownID(t *testing.T) {
	out := runCommand(t, nil, func(c *chat) {
		c.plugins = newPluginRegistry([]agent.PluginConfig{greeterPlugin(c.console)})
		handleCommand(c, "/plugin remove "+greeterPluginID)
		c.console.write("--again--\n")
		handleCommand(c, "/plugin remove "+greeterPluginID)
	})
	for _, want := range []string{
		"removed plugin " + greeterPluginID,
		"--again--",
		"plugin " + greeterPluginID + " is not active",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("out = %q, missing %q", out, want)
		}
	}
}
