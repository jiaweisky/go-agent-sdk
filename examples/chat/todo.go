package main

// todosCommand prints the parent session's task list, cached from todo.updated
// events.
func (c *chat) todosCommand() {
	c.mu.Lock()
	todos := c.todos[c.parent]
	c.mu.Unlock()
	if len(todos) == 0 {
		c.console.write("(no todos; the model updates them with the todowrite tool)\n")
		return
	}
	c.writeTodos(todos)
}
