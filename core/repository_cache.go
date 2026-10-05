package core

import (
	"fmt"
	"path/filepath"

	"github.com/jiaweisky/go-agent-sdk/core/effect"
	"github.com/jiaweisky/go-agent-sdk/schema"
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

// RepositoryCache keeps one local checkout per remote and branch. Locking is
// in-process only; it does not coordinate across processes.
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

func (c *RepositoryCache) ensureLocked(
	reference, cloneTarget *RepositoryReference,
	localPath string,
	input RepositoryCacheEnsureInput,
) (RepositoryCacheResult, error) {
	if err := EnsureDir(filepath.Dir(localPath)); err != nil {
		return RepositoryCacheResult{}, err
	}

	existing, originReference := c.discoverExisting(localPath)
	reuse := c.canReuse(existing, originReference, cloneTarget, localPath)

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

	switch status {
	case "cloned":
		if _, err := c.git.Repo.Clone(GitCloneInput{
			Remote: reference.Remote, Directory: schema.AbsolutePath(localPath), Branch: input.Branch,
		}); err != nil {
			return RepositoryCacheResult{}, err
		}
	case "refreshed":
		if err := c.refreshCheckout(existing, input); err != nil {
			return RepositoryCacheResult{}, err
		}
	}

	return c.result(reference, localPath, status), nil
}

// discoverExisting opens the cached checkout and parses its origin remote.
func (c *RepositoryCache) discoverExisting(localPath string) (*GitRepository, *RepositoryReference) {
	repository, err := c.git.Repo.Discover(schema.AbsolutePath(localPath))
	if err != nil || repository == nil {
		return nil, nil
	}
	origin, _ := c.git.Remote.Get(*repository)
	if origin == "" {
		return repository, nil
	}
	return repository, RepositoryParse(origin, c.repositoryOptions()...)
}

// canReuse reports whether the cached checkout already points at cloneTarget.
func (c *RepositoryCache) canReuse(
	existing *GitRepository,
	origin, cloneTarget *RepositoryReference,
	localPath string,
) bool {
	if existing == nil || origin == nil || !RepositorySame(origin, cloneTarget) {
		return false
	}
	resolved, err := RealPath(localPath)
	if err != nil {
		return false
	}
	return string(existing.Worktree) == resolved
}

// refreshCheckout fetches and hard-resets an existing checkout to its branch.
func (c *RepositoryCache) refreshCheckout(existing *GitRepository, input RepositoryCacheEnsureInput) error {
	if existing == nil {
		return fmt.Errorf("repository is unavailable")
	}
	if err := c.git.Sync.FetchRemotes(*existing); err != nil {
		return err
	}
	if input.Branch != nil {
		if err := c.git.Sync.FetchBranch(*existing, GitFetchBranchInput{Branch: *input.Branch}); err != nil {
			return err
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
			return err
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
	return c.git.Sync.ResetHard(*existing, revision)
}

// result describes the checkout, resolving its current head and branch.
func (c *RepositoryCache) result(reference *RepositoryReference, localPath, status string) RepositoryCacheResult {
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
	return result
}
