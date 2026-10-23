package config

import "testing"

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
