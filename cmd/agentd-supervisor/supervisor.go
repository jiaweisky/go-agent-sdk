package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/jiaweisky/go-agent-sdk/cmd/agentd/config"
)

// request is one supervisor_command invocation body (the wire contract enforced
// by Tether's SupervisorProjectProvider).
type request struct {
	Action    string `json:"action"`
	ProjectID string `json:"project_id"`
	Kind      string `json:"kind"`
	Agent     string `json:"agent"`
	Readonly  bool   `json:"readonly"`
	Transport string `json:"transport"`
}

// lease is the acquire response body (the ProjectLease fields).
type lease struct {
	ProjectID        string `json:"project_id"`
	Transport        string `json:"transport"`
	ServerBase       string `json:"server_base"`
	WorkingDirectory string `json:"working_directory,omitempty"`
	Ephemeral        bool   `json:"ephemeral"`
}

// supervisor translates the supervisor_command protocol into calls on a running
// agentd admin (control) API.
type supervisor struct {
	cfg    supervisorConfig
	client *http.Client
}

func newSupervisor(cfg supervisorConfig) *supervisor {
	return &supervisor{cfg: cfg, client: &http.Client{Timeout: 30 * time.Second}}
}

// handle dispatches one request; a non-nil lease is printed for acquire.
func (s *supervisor) handle(req request) (*lease, error) {
	switch req.Action {
	case "acquire":
		return s.acquire(req)
	case "release":
		return nil, s.release(req)
	default:
		return nil, fmt.Errorf("unknown action %q", req.Action)
	}
}

func (s *supervisor) acquire(req request) (*lease, error) {
	if req.ProjectID == "" {
		return nil, errors.New("acquire requires project_id")
	}
	if s.cfg.Agentd.BaseURL == "" {
		return nil, errors.New("agentd.baseURL is required")
	}
	t := s.cfg.Project
	kind := t.Kind
	if kind == "" {
		kind = "http"
	}
	transport := req.Transport
	if transport == "" {
		transport = kind
	}
	// A read-only conversation shares the source directory (mode none); a
	// writable one gets the template's isolated view.
	isolation := t.Isolation
	if req.Readonly {
		isolation = nil
	}
	body := config.ProjectRequest{
		Name:             req.ProjectID,
		Kind:             kind,
		Root:             filepath.Join(t.RootBase, sanitizeID(req.ProjectID)),
		WorkingDirectory: t.WorkingDirectory,
		ConfigDirs:       t.ConfigDirs,
		Models:           t.Models,
		DefaultModel:     t.DefaultModel,
		Agents:           t.Agents,
		Isolation:        isolation,
		Auth:             t.Auth,
	}
	var response struct {
		Data struct {
			ProjectID        string `json:"project_id"`
			ServerBase       string `json:"server_base"`
			Transport        string `json:"transport"`
			WorkingDirectory string `json:"working_directory"`
		} `json:"data"`
	}
	if err := s.do(http.MethodPost, "/admin/projects", body, &response); err != nil {
		return nil, err
	}
	if response.Data.ServerBase == "" {
		return nil, errors.New("agentd returned no server_base")
	}
	// Prefer the view agentd actually prepared; fall back to the source.
	workingDirectory := response.Data.WorkingDirectory
	if workingDirectory == "" {
		workingDirectory = t.WorkingDirectory
	}
	return &lease{
		ProjectID:        req.ProjectID,
		Transport:        transport,
		ServerBase:       response.Data.ServerBase,
		WorkingDirectory: workingDirectory,
		Ephemeral:        true,
	}, nil
}

func (s *supervisor) release(req request) error {
	if req.ProjectID == "" {
		return errors.New("release requires project_id")
	}
	if err := s.do(http.MethodDelete, "/admin/projects/"+url.PathEscape(req.ProjectID), nil, nil); err != nil {
		// An already-released project is not an error.
		if errors.Is(err, errNotFound) {
			return nil
		}
		return err
	}
	return nil
}

// errNotFound reports a 404 from the control API.
var errNotFound = errors.New("not found")

func (s *supervisor) do(method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(encoded)
	}
	request, err := http.NewRequest(method, strings.TrimRight(s.cfg.Agentd.BaseURL, "/")+path, reader)
	if err != nil {
		return err
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if s.cfg.Agentd.Token != "" {
		request.Header.Set("Authorization", "Bearer "+s.cfg.Agentd.Token)
	}
	response, err := s.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return errNotFound
	}
	if response.StatusCode/100 != 2 {
		detail, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return fmt.Errorf("agentd %s %s: %s: %s", method, path, response.Status, strings.TrimSpace(string(detail)))
	}
	if out != nil {
		return json.NewDecoder(response.Body).Decode(out)
	}
	return nil
}

// sanitizeID makes a project id safe as a single path element.
func sanitizeID(id string) string {
	replaced := strings.NewReplacer("/", "_", "\\", "_", "..", "_").Replace(id)
	return strings.Trim(replaced, " .")
}
