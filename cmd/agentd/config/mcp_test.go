package config

import (
	"testing"
	"time"
)

func TestValidateMCP(t *testing.T) {
	cases := []struct {
		name   string
		server MCPServer
		ok     bool
	}{
		{"local", MCPServer{Name: "a", Type: MCPLocal, Command: []string{"srv"}}, true},
		{"remote", MCPServer{Name: "a", Type: MCPRemote, URL: "https://mcp.example"}, true},
		{"missing name", MCPServer{Type: MCPLocal, Command: []string{"srv"}}, false},
		{"missing type", MCPServer{Name: "a"}, false},
		{"unknown type", MCPServer{Name: "a", Type: "stdio"}, false},
		{"local without command", MCPServer{Name: "a", Type: MCPLocal}, false},
		{"remote without url", MCPServer{Name: "a", Type: MCPRemote}, false},
	}
	for _, tc := range cases {
		err := ValidateMCP([]MCPServer{tc.server})
		if tc.ok && err != nil {
			t.Errorf("%s: unexpected error: %v", tc.name, err)
		}
		if !tc.ok && err == nil {
			t.Errorf("%s: expected an error", tc.name)
		}
	}
}
func TestValidateMCPRejectsNegativeTimeout(t *testing.T) {
	err := ValidateMCP([]MCPServer{{Name: "a", Type: MCPLocal, Command: []string{"srv"}, Timeout: "-1s"}})
	if err == nil {
		t.Fatal("a negative timeout must be rejected")
	}
}
func TestMCPTimeouts(t *testing.T) {
	timeouts, err := MCPTimeouts([]MCPServer{
		{Name: "a", Type: MCPLocal, Command: []string{"srv"}, Timeout: "5s"},
		{Name: "b", Type: MCPRemote, URL: "https://mcp.example"},
	})
	if err != nil {
		t.Fatalf("MCPTimeouts: %v", err)
	}
	if len(timeouts) != 2 || timeouts[0] != 5*time.Second || timeouts[1] != 0 {
		t.Fatalf("timeouts = %v", timeouts)
	}
}
