package core

import (
	"fmt"
	"path/filepath"

	"github.com/user/go-agent-sdk/core/effect"
	"github.com/user/go-agent-sdk/schema"
)

// RepositoryCacheResult is a ready checkout.
type RepositoryCacheResult struct {
	Repository string
	Host       string
	Remote     string
	LocalPath  string
	Status     string
	Head       *string
	Branch     *string
}

// RepositoryCacheEnsureInput describes the checkout to prepare.
type RepositoryCacheEnsureInput struct {
	Reference *RepositoryReference
	Refresh   bool
	Branch    *string
}

// RepositoryCache keeps one local checkout per remote and branch.
// tracking checkouts. Locking is in-process (cross-process flock is not ported).
type RepositoryCache struct {
	git     Git
	repos   string
	options RepositoryOptions
	locks   *effect.KeyedMutex[string]
}

func NewRepositoryCache(git Git, repos string, options ...RepositoryOption) *RepositoryCache {
	config := RepositoryOptions{}
	for _, option := range options {
		option(&config)
	}
	return &RepositoryCache{git: git, repos: repos, options: config, locks: effect.MakeUnsafe[string]()}
}

// repositoryOptions re-applies the cache's parse options; a nil cache yields
// none, so callers can forward them unconditionally.
func (c *RepositoryCache) repositoryOptions() []RepositoryOption {
	if c == nil {
		return nil
	}
	options := c.options
	return []RepositoryOption{func(target *RepositoryOptions) { *target = options }}
}

// Ensure returns the checkout, cloning or refreshing it as needed.
func (c *RepositoryCache) Ensure(input RepositoryCacheEnsureInput) (RepositoryCacheResult, error) {
	reference := input.Reference
	if reference == nil {
		return RepositoryCacheResult{}, fmt.Errorf("repository reference is required")
	}
	if input.Branch != nil {
		if err := RepositoryValidateBranch(*input.Branch); err != nil {
			return RepositoryCacheResult{}, err
		}
	}
	localPath := RepositoryCachePath(c.repos, reference, input.Branch)
	cloneTarget := RepositoryParse(reference.Remote, c.repositoryOptions()...)
	if cloneTarget == nil {
		cloneTarget = reference
	}

	var result RepositoryCacheResult
	var opErr error
	c.locks.WithLock(localPath, func() {
		result, opErr = c.ensureLocked(reference, cloneTarget, localPath, input)
	})
	return result, opErr
}

func (c *RepositoryCache) ensureLocked(reference, cloneTarget *RepositoryReference, localPath string, input RepositoryCacheEnsureInput) (RepositoryCacheResult, error) {
	if err := EnsureDir(filepath.Dir(localPath)); err != nil {
		return RepositoryCacheResult{}, err
	}

	var existing *GitRepository
	if repository, err := c.git.Repo.Discover(schema.AbsolutePath(localPath)); err == nil {
		existing = repository
	}

	origin := ""
	if existing != nil {
		origin, _ = c.git.Remote.Get(*existing)
	}
	var originReference *RepositoryReference
	if origin != "" {
		originReference = RepositoryParse(origin, c.repositoryOptions()...)
	}

	worktree := ""
	if existing != nil {
		if resolved, err := RealPath(localPath); err == nil {
			worktree = resolved
		}
	}
	reuse := existing != nil && string(existing.Worktree) == worktree &&
		originReference != nil && RepositorySame(originReference, cloneTarget)

	if !reuse {
		if exists, _ := Exists(localPath); exists {
			if err := RemoveAll(localPath); err != nil {
				return RepositoryCacheResult{}, err
			}
		}
	}

	status := "cached"
	switch {
	case !reuse:
		status = "cloned"
	case input.Refresh:
		status = "refreshed"
	}

	if status == "cloned" {
		if _, err := c.git.Repo.Clone(GitCloneInput{
			Remote: reference.Remote, Directory: schema.AbsolutePath(localPath), Branch: input.Branch,
		}); err != nil {
			return RepositoryCacheResult{}, err
		}
	}

	if status == "refreshed" {
		if existing == nil {
			return RepositoryCacheResult{}, fmt.Errorf("repository is unavailable")
		}
		if err := c.git.Sync.FetchRemotes(*existing); err != nil {
			return RepositoryCacheResult{}, err
		}
		if input.Branch != nil {
			if err := c.git.Sync.FetchBranch(*existing, GitFetchBranchInput{Branch: *input.Branch}); err != nil {
				return RepositoryCacheResult{}, err
			}
		}
		branch := ""
		if input.Branch != nil {
			branch = *input.Branch
		} else if value, err := c.git.History.DefaultRemoteBranch(*existing); err == nil {
			branch = value
		}
		if branch != "" {
			if err := c.git.Sync.CheckoutRemoteBranch(*existing, GitCheckoutRemoteBranchInput{Branch: branch}); err != nil {
				return RepositoryCacheResult{}, err
			}
		}
		target := branch
		if target == "" {
			if value, err := c.git.History.Branch(*existing); err == nil {
				target = value
			}
		}
		revision := "HEAD"
		if target != "" {
			revision = "origin/" + target
		}
		if err := c.git.Sync.ResetHard(*existing, revision); err != nil {
			return RepositoryCacheResult{}, err
		}
	}

	result := RepositoryCacheResult{
		Repository: reference.Label, Host: reference.Host, Remote: reference.Remote,
		LocalPath: localPath, Status: status,
	}
	if checkout, err := c.git.Repo.Discover(schema.AbsolutePath(localPath)); err == nil && checkout != nil {
		if head, err := c.git.History.Head(*checkout); err == nil {
			result.Head = &head
		}
		if branch, err := c.git.History.Branch(*checkout); err == nil {
			result.Branch = &branch
		}
	}
	return result, nil
}
