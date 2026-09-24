package main

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	root, err := os.MkdirTemp("", "go-agent-sdk-chat-test")
	if err != nil {
		panic(err)
	}
	for _, key := range []string{"XDG_DATA_HOME", "XDG_CACHE_HOME", "XDG_CONFIG_HOME", "XDG_STATE_HOME"} {
		os.Setenv(key, root)
	}
	code := m.Run()
	os.RemoveAll(root)
	os.Exit(code)
}
