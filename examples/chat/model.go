package main

import (
	"fmt"

	"github.com/jiaweisky/go-agent-sdk/examples/internal/util"
	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

// models is the shared model catalog the chat runs with.
var models = util.DefaultCatalog

// resolveDefaultModel picks the model to start with: the persisted selection
// when available, otherwise the catalog default.
func resolveDefaultModel(persisted string) (string, error) {
	if persisted != "" && models.Available(persisted) {
		return persisted, nil
	}
	return models.DefaultModel()
}

// modelCommand handles "/model" (list) and "/model <id>" (switch). It changes
// the active session's model in place, so the choice applies from the next turn
// without starting a new session.
func (c *chat) modelCommand(input string) {
	name := commandArgument(input, "/model")
	if name == "" {
		c.writeModels()
		return
	}
	if c.session == nil {
		c.console.write("(no active session)\n")
		return
	}
	entry, ok := models.Entry(name)
	if !ok || !models.Available(name) {
		c.console.write(fmt.Sprintf("unknown or unavailable model: %s\n", name))
		return
	}
	ref := schema.ModelRef{ID: schema.ModelID(name), ProviderID: entry.ProviderID}
	if err := c.session.SetModel(ref); err != nil {
		c.console.write(fmt.Sprintf("%s%v%s\n", yellow, err, reset))
		return
	}
	c.setModel(name)
	c.console.write(fmt.Sprintf("model set to %s%s%s\n", cyan, name, reset))
}

// writeModels lists the available models, marking the active one.
func (c *chat) writeModels() {
	ids := models.AvailableIDs()
	c.mu.Lock()
	current := c.model
	c.mu.Unlock()
	c.console.write(fmt.Sprintf("%sModels (%d):%s\n", bold, len(ids), reset))
	for _, id := range ids {
		marker := " "
		if id == current {
			marker = "▶"
		}
		entry, _ := models.Entry(id)
		c.console.write(fmt.Sprintf("  %s %s %s(%s · %s)%s\n",
			marker, id, dim, entry.ProviderID, entry.Protocol, reset))
	}
}

// setModel records the active model and persists it for the next launch.
func (c *chat) setModel(id string) {
	c.mu.Lock()
	c.model = id
	c.mu.Unlock()
	c.state.Model = id
	c.persist()
}
