package core

import (
	"net/url"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/user/go-agent-sdk/core/project"
	"github.com/user/go-agent-sdk/core/util"
	"github.com/user/go-agent-sdk/schema"
)

type ProjectResolved struct {
	Previous  *schema.ProjectID
	ID        schema.ProjectID
	Directory AbsolutePath
	Vcs       *project.Vcs
}

type Project interface {
	Directories(projectID schema.ProjectID) ([]project.Directory, error)
	Resolve(input AbsolutePath) (*ProjectResolved, error)
	Commit(store AbsolutePath, id schema.ProjectID) error
}

type ProjectConfig struct {
	Git         Git
	Directories project.Directories
}

func NewProject(cfg ProjectConfig) Project {
	return &projectService{git: cfg.Git, directories: cfg.Directories}
}

type projectService struct {
	git         Git
	directories project.Directories
}

func (p *projectService) Directories(projectID schema.ProjectID) ([]project.Directory, error) {
	return p.directories.List(projectID)
}

func (p *projectService) Resolve(input AbsolutePath) (*ProjectResolved, error) {
	repo, err := p.git.Repo.Discover(input)
	if err != nil {
		return nil, err
	}
	if repo == nil {
		root := filepath.VolumeName(string(input)) + string(filepath.Separator)
		return &ProjectResolved{
			ID:        schema.ProjectIDGlobal,
			Directory: AbsolutePath(root),
		}, nil
	}

	previous := p.cached(string(repo.CommonDirectory))
	id := p.remote(repo)
	if id == nil {
		id = previous
	}
	if id == nil {
		id = p.root(repo)
	}
	resolvedID := schema.ProjectIDGlobal
	if id != nil {
		resolvedID = *id
	}
	return &ProjectResolved{
		Previous:  previous,
		ID:        resolvedID,
		Directory: repo.Worktree,
		Vcs:       &project.Vcs{Type: project.VcsTypeGit, Store: repo.CommonDirectory},
	}, nil
}

func (p *projectService) Commit(store AbsolutePath, id schema.ProjectID) error {
	_ = WriteFileString(filepath.Join(string(store), "opencode"), string(id))
	return nil
}

func (p *projectService) cached(dir string) *schema.ProjectID {
	value, err := ReadFileString(filepath.Join(dir, "opencode"))
	if err != nil {
		return nil
	}
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	id := schema.ProjectID(trimmed)
	return &id
}

func (p *projectService) remote(repo *GitRepository) *schema.ProjectID {
	origin, err := p.git.Remote.Get(*repo)
	if err != nil || origin == "" {
		return nil
	}
	normalized := parseGitURL(origin)
	if normalized == "" {
		return nil
	}
	id := schema.ProjectID(util.HashFastString("git-remote:" + normalized))
	return &id
}

func (p *projectService) root(repo *GitRepository) *schema.ProjectID {
	commits, err := p.git.History.RootCommits(*repo)
	if err != nil || len(commits) == 0 {
		return nil
	}
	id := schema.ProjectID(commits[0])
	return &id
}

var scpLikePattern = regexp.MustCompile(`^([^@/:]+@)?([^/:]+):(.+)$`)

func parseGitURL(input string) string {
	value := strings.TrimSpace(input)
	if value == "" {
		return ""
	}
	if parsed, err := url.Parse(value); err == nil && parsed.Host != "" {
		if parsed.Scheme == "file" {
			return ""
		}
		return gitURLParts(parsed.Hostname(), parsed.Path)
	}
	if match := scpLikePattern.FindStringSubmatch(value); match != nil {
		return gitURLParts(match[2], match[3])
	}
	return ""
}

var gitSuffixPattern = regexp.MustCompile(`\.git/?$`)

func gitURLParts(host, name string) string {
	pathname := strings.TrimLeft(name, "/")
	pathname = gitSuffixPattern.ReplaceAllString(pathname, "")
	pathname = strings.TrimRight(pathname, "/")
	if host == "" || pathname == "" {
		return ""
	}
	return strings.ToLower(host) + "/" + pathname
}
