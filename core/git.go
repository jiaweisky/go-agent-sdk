package core

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/jiaweisky/go-agent-sdk/core/effect"
)

type GitRepository struct {
	Worktree        AbsolutePath
	GitDirectory    AbsolutePath
	CommonDirectory AbsolutePath
}

type GitChangeSet string

type GitTreeID string

type GitOperationKind string

const (
	GitOperationClone     GitOperationKind = "clone"
	GitOperationFetch     GitOperationKind = "fetch"
	GitOperationCheckout  GitOperationKind = "checkout"
	GitOperationReset     GitOperationKind = "reset"
	GitOperationCreate    GitOperationKind = "create"
	GitOperationRefresh   GitOperationKind = "refresh"
	GitOperationWriteTree GitOperationKind = "write_tree"
	GitOperationListFiles GitOperationKind = "list_files"
	GitOperationDiff      GitOperationKind = "diff"
	GitOperationRestore   GitOperationKind = "restore"
)

type GitOperationError struct {
	Operation GitOperationKind
	Message   string
	Directory *AbsolutePath
	Cause     error
}

func (e *GitOperationError) Error() string {
	return fmt.Sprintf("git %s failed: %s", e.Operation, e.Message)
}

func (e *GitOperationError) Unwrap() error {
	return e.Cause
}

type GitWorktreeKind string

const (
	GitWorktreeMain   GitWorktreeKind = "main"
	GitWorktreeLinked GitWorktreeKind = "linked"
)

type GitWorktree struct {
	Directory AbsolutePath
	Kind      GitWorktreeKind
}

type GitWorktreeOperation string

const (
	GitWorktreeCreateOp GitWorktreeOperation = "create"
	GitWorktreeRemoveOp GitWorktreeOperation = "remove"
	GitWorktreeListOp   GitWorktreeOperation = "list"
)

type GitWorktreeError struct {
	Operation     GitWorktreeOperation
	Message       string
	Directory     *AbsolutePath
	ForceRequired bool
	Cause         error
}

func (e *GitWorktreeError) Error() string {
	return fmt.Sprintf("git worktree %s failed: %s", e.Operation, e.Message)
}

type GitPatchOperation string

const (
	GitPatchCapture GitPatchOperation = "capture"
	GitPatchApply   GitPatchOperation = "apply"
	GitPatchReset   GitPatchOperation = "reset"
)

type GitPatchError struct {
	Operation GitPatchOperation
	Directory AbsolutePath
	Message   string
	Cause     error
}

func (e *GitPatchError) Error() string {
	return fmt.Sprintf("git patch %s failed: %s", e.Operation, e.Message)
}

type GitCloneInput struct {
	Remote    string
	Directory AbsolutePath
	Branch    *string
	Depth     *int
}

type GitCreateInput struct {
	Worktree     AbsolutePath
	GitDirectory AbsolutePath
	Seed         *GitRepository
}

type GitFetchBranchInput struct {
	Remote *string
	Branch string
	Force  *bool
}

type GitCheckoutRemoteBranchInput struct {
	Remote *string
	Branch string
	Reset  *bool
}

type GitChangeCaptureInput struct {
	Repository GitRepository
	Path       AbsolutePath
}

type GitChangeApplyInput struct {
	Repository GitRepository
	Path       AbsolutePath
	Changes    GitChangeSet
}

type GitChangeDiscardInput struct {
	Repository GitRepository
	Path       AbsolutePath
	Index      string
	Untracked  string
}

type GitWorktreeCreateInput struct {
	Repository GitRepository
	Directory  AbsolutePath
}

type GitWorktreeRemoveInput struct {
	Repository GitRepository
	Directory  AbsolutePath
	Force      bool
}

type GitIndexRefreshInput struct {
	Repository                GitRepository
	Scope                     RelativePath
	Ignores                   *GitRepository
	MaximumUntrackedFileBytes *int
}

type GitIndexRefreshResult struct {
	Skipped []RelativePath
}

type GitIndexIgnoredInput struct {
	Repository GitRepository
	Paths      []RelativePath
}

type GitTreeCaptureInput struct {
	Repository                GitRepository
	Scopes                    []RelativePath
	Ignores                   *GitRepository
	MaximumUntrackedFileBytes *int
}

type GitTreeFilesInput struct {
	Repository GitRepository
	From       GitTreeID
	To         GitTreeID
}

type GitTreeDiffInput struct {
	Repository GitRepository
	From       GitTreeID
	To         GitTreeID
	Context    *int
	Paths      []RelativePath
}

type GitTreePreviewInput struct {
	Repository GitRepository
	Current    GitTreeID
	Files      map[RelativePath]GitTreeID
	Context    *int
}

type GitTreeRestoreInput struct {
	Repository GitRepository
	Files      map[RelativePath]GitTreeID
}

type GitTreeCheckoutInput struct {
	Repository GitRepository
	Tree       GitTreeID
}

type GitRepoAPI interface {
	Discover(input AbsolutePath) (*GitRepository, error)
	Clone(input GitCloneInput) (*GitRepository, error)
	Create(input GitCreateInput) (*GitRepository, error)
}

type GitRemoteAPI interface {
	Get(repository GitRepository, name ...string) (string, error)
}

type GitHistoryAPI interface {
	Head(repository GitRepository) (string, error)
	Branch(repository GitRepository) (string, error)
	DefaultRemoteBranch(repository GitRepository, remote ...string) (string, error)
	RootCommits(repository GitRepository) ([]string, error)
}

type GitSyncAPI interface {
	FetchRemotes(repository GitRepository, input ...struct{ Prune *bool }) error
	FetchBranch(repository GitRepository, input GitFetchBranchInput) error
	CheckoutRemoteBranch(repository GitRepository, input GitCheckoutRemoteBranchInput) error
	ResetHard(repository GitRepository, revision string) error
}

type GitChangeAPI interface {
	CaptureChange(input GitChangeCaptureInput) (GitChangeSet, error)
	Apply(input GitChangeApplyInput) error
	Discard(input GitChangeDiscardInput) error
}

type GitWorktreeAPI interface {
	CreateWorktree(input GitWorktreeCreateInput) (*GitRepository, error)
	RemoveWorktree(input GitWorktreeRemoveInput) error
	ListWorktrees(repository GitRepository) ([]GitWorktree, error)
}

type GitIndexAPI interface {
	Refresh(input GitIndexRefreshInput) (*GitIndexRefreshResult, error)
	Ignored(input GitIndexIgnoredInput) (map[RelativePath]bool, error)
}

type GitTreeAPI interface {
	Capture(input GitTreeCaptureInput) (GitTreeID, error)
	Write(repository GitRepository) (GitTreeID, error)
	Files(input GitTreeFilesInput) ([]RelativePath, error)
	Diff(input GitTreeDiffInput) ([]FileDiff, error)
	Preview(input GitTreePreviewInput) ([]FileDiff, error)
	Restore(input GitTreeRestoreInput) error
	Checkout(input GitTreeCheckoutInput) error
}

type Git struct {
	Repo     GitRepoAPI
	Remote   GitRemoteAPI
	History  GitHistoryAPI
	Sync     GitSyncAPI
	Change   GitChangeAPI
	Worktree GitWorktreeAPI
	Index    GitIndexAPI
	Tree     GitTreeAPI
}

func NewGit(proc AppProcess) Git {
	impl := &gitImpl{proc: proc, locks: effect.MakeUnsafe[string]()}
	return Git{
		Repo:     impl,
		Remote:   impl,
		History:  impl,
		Sync:     impl,
		Change:   impl,
		Worktree: impl,
		Index:    impl,
		Tree:     impl,
	}
}

type gitResult struct {
	ExitCode int
	Text     string
	Stderr   string
}

type gitImpl struct {
	proc  AppProcess
	locks *effect.KeyedMutex[string]
}

func gitRun(proc AppProcess, cwd string, args []string) gitResult {
	result, err := proc.Run(context.Background(), "git", args, RunOptions{Dir: cwd, ExtendEnv: true})
	if err != nil {
		return gitResult{ExitCode: 1}
	}
	return gitResult{
		ExitCode: result.ExitCode,
		Text:     string(result.Stdout),
		Stderr:   string(result.Stderr),
	}
}

// gitErrorMessage picks the most useful failure text from a git run, falling
// back to the supplied message when the run produced no output.
func gitErrorMessage(stderr, stdout, fallback string) string {
	if message := strings.TrimSpace(stderr); message != "" {
		return message
	}
	if message := strings.TrimSpace(stdout); message != "" {
		return message
	}
	return fallback
}

func resolveGitPath(cwd, value string) string {
	trimmed := strings.TrimRight(value, "\r\n")
	if trimmed == "" {
		return cwd
	}
	if filepath.IsAbs(trimmed) {
		return filepath.Clean(trimmed)
	}
	return filepath.Join(cwd, trimmed)
}

func (g *gitImpl) Discover(input AbsolutePath) (*GitRepository, error) {
	matches := FindUp([]string{".git"}, string(input))
	if len(matches) == 0 {
		return nil, nil
	}
	dotgit := matches[0]
	cwd := filepath.Dir(dotgit)

	topLevel := gitRun(g.proc, cwd, []string{"rev-parse", "--show-toplevel"})
	gitDir := gitRun(g.proc, cwd, []string{"rev-parse", "--git-dir"})
	commonDir := gitRun(g.proc, cwd, []string{"rev-parse", "--git-common-dir"})
	if gitDir.ExitCode != 0 || commonDir.ExitCode != 0 {
		return nil, nil
	}

	worktree := cwd
	if topLevel.ExitCode == 0 {
		worktree = resolveGitPath(cwd, topLevel.Text)
	}
	return &GitRepository{
		Worktree:        AbsolutePath(worktree),
		GitDirectory:    AbsolutePath(resolveGitPath(cwd, gitDir.Text)),
		CommonDirectory: AbsolutePath(resolveGitPath(cwd, commonDir.Text)),
	}, nil
}

func (g *gitImpl) Get(repository GitRepository, name ...string) (string, error) {
	remoteName := "origin"
	if len(name) > 0 && name[0] != "" {
		remoteName = name[0]
	}
	result := gitRun(g.proc, string(repository.Worktree), []string{"remote", "get-url", remoteName})
	if result.ExitCode != 0 {
		return "", nil
	}
	return strings.TrimSpace(result.Text), nil
}

func (g *gitImpl) RootCommits(repository GitRepository) ([]string, error) {
	result := gitRun(g.proc, string(repository.Worktree), []string{"rev-list", "--max-parents=0", "HEAD"})
	if result.ExitCode != 0 {
		return nil, nil
	}
	var commits []string
	for _, item := range strings.Split(result.Text, "\n") {
		trimmed := strings.TrimSpace(item)
		if trimmed != "" {
			commits = append(commits, trimmed)
		}
	}
	sort.Strings(commits)
	return commits, nil
}

func (g *gitImpl) Head(repository GitRepository) (string, error) {
	result := gitRun(g.proc, string(repository.Worktree), []string{"rev-parse", "HEAD"})
	if result.ExitCode != 0 {
		return "", nil
	}
	return strings.TrimSpace(result.Text), nil
}

func (g *gitImpl) Branch(repository GitRepository) (string, error) {
	result := gitRun(g.proc, string(repository.Worktree), []string{"symbolic-ref", "--quiet", "--short", "HEAD"})
	if result.ExitCode != 0 {
		return "", nil
	}
	return strings.TrimSpace(result.Text), nil
}

func (g *gitImpl) DefaultRemoteBranch(repository GitRepository, remote ...string) (string, error) {
	remoteName := "origin"
	if len(remote) > 0 && remote[0] != "" {
		remoteName = remote[0]
	}
	result := gitRun(g.proc, string(repository.Worktree), []string{"symbolic-ref", "refs/remotes/" + remoteName + "/HEAD"})
	if result.ExitCode != 0 {
		return "", nil
	}
	value := strings.TrimSpace(result.Text)
	value = regexp.MustCompile("^refs/remotes/"+regexp.QuoteMeta(remoteName)+"/").ReplaceAllString(value, "")
	return value, nil
}

func (g *gitImpl) operation(kind GitOperationKind, directory AbsolutePath, args []string) error {
	result := gitRun(g.proc, string(directory), args)
	if result.ExitCode == 0 {
		return nil
	}
	message := gitErrorMessage(result.Stderr, result.Text, fmt.Sprintf("Git %s failed", kind))
	dir := directory
	return &GitOperationError{Operation: kind, Directory: &dir, Message: message}
}

func (g *gitImpl) repositoryArgs(repository GitRepository, args []string) []string {
	result := []string{"--git-dir", string(repository.GitDirectory), "--work-tree", string(repository.Worktree)}
	return append(result, args...)
}

func (g *gitImpl) repositoryOperation(kind GitOperationKind, repository GitRepository, args []string, stdin ...string) (*gitResult, error) {
	var input []byte
	if len(stdin) > 0 {
		input = []byte(stdin[0])
	}
	result, err := g.proc.Run(context.Background(), "git", g.repositoryArgs(repository, args), RunOptions{
		Dir:       string(repository.Worktree),
		ExtendEnv: true,
		Stdin:     input,
	})
	if err != nil {
		dir := repository.Worktree
		return nil, &GitOperationError{Operation: kind, Directory: &dir, Message: err.Error(), Cause: err}
	}
	text := string(result.Stdout)
	stderr := string(result.Stderr)
	if result.ExitCode == 0 {
		return &gitResult{Text: text, Stderr: stderr}, nil
	}
	message := gitErrorMessage(stderr, text, fmt.Sprintf("Git %s failed", kind))
	dir := repository.Worktree
	return nil, &GitOperationError{Operation: kind, Directory: &dir, Message: message}
}

func (g *gitImpl) Clone(input GitCloneInput) (*GitRepository, error) {
	depth := 100
	if input.Depth != nil {
		depth = *input.Depth
	}
	args := []string{"clone", "--depth", fmt.Sprintf("%d", depth)}
	if input.Branch != nil {
		args = append(args, "--branch", *input.Branch)
	}
	args = append(args, "--", input.Remote, string(input.Directory))
	if err := g.operation(GitOperationClone, AbsolutePath(filepath.Dir(string(input.Directory))), args); err != nil {
		return nil, err
	}
	repository, err := g.Discover(input.Directory)
	if err != nil {
		return nil, err
	}
	if repository != nil {
		return repository, nil
	}
	return nil, &GitOperationError{
		Operation: GitOperationClone,
		Directory: &input.Directory,
		Message:   "Cloned repository could not be opened",
	}
}

func (g *gitImpl) FetchRemotes(repository GitRepository, input ...struct{ Prune *bool }) error {
	args := []string{"fetch", "--all"}
	prune := true
	if len(input) > 0 && input[0].Prune != nil {
		prune = *input[0].Prune
	}
	if prune {
		args = append(args, "--prune")
	}
	return g.operation(GitOperationFetch, repository.Worktree, args)
}

func (g *gitImpl) FetchBranch(repository GitRepository, input GitFetchBranchInput) error {
	remoteName := "origin"
	if input.Remote != nil && *input.Remote != "" {
		remoteName = *input.Remote
	}
	spec := fmt.Sprintf("refs/heads/%s:refs/remotes/%s/%s", input.Branch, remoteName, input.Branch)
	force := true
	if input.Force != nil {
		force = *input.Force
	}
	if force {
		spec = "+" + spec
	}
	return g.operation(GitOperationFetch, repository.Worktree, []string{"fetch", remoteName, spec})
}

func (g *gitImpl) CheckoutRemoteBranch(repository GitRepository, input GitCheckoutRemoteBranchInput) error {
	remoteName := "origin"
	if input.Remote != nil && *input.Remote != "" {
		remoteName = *input.Remote
	}
	reset := true
	if input.Reset != nil {
		reset = *input.Reset
	}
	args := []string{"checkout"}
	if reset {
		args = append(args, "-B", input.Branch, remoteName+"/"+input.Branch)
	} else {
		args = append(args, input.Branch)
	}
	return g.operation(GitOperationCheckout, repository.Worktree, args)
}

func (g *gitImpl) ResetHard(repository GitRepository, revision string) error {
	return g.operation(GitOperationReset, repository.Worktree, []string{"reset", "--hard", revision})
}

func (g *gitImpl) Create(input GitCreateInput) (*GitRepository, error) {
	if err := EnsureDir(string(input.GitDirectory)); err != nil {
		return nil, &GitOperationError{
			Operation: GitOperationCreate,
			Directory: &input.GitDirectory,
			Message:   "Failed to create Git storage",
			Cause:     err,
		}
	}
	repository := GitRepository{
		Worktree:        input.Worktree,
		GitDirectory:    input.GitDirectory,
		CommonDirectory: input.GitDirectory,
	}
	if _, err := g.repositoryOperation(GitOperationCreate, repository, []string{"init"}); err != nil {
		return nil, err
	}
	configs := [][2]string{
		{"core.autocrlf", "false"},
		{"core.longpaths", "true"},
		{"core.symlinks", "true"},
		{"core.fsmonitor", "false"},
		{"feature.manyFiles", "true"},
		{"index.version", "4"},
		{"index.threads", "true"},
		{"core.untrackedCache", "true"},
	}
	for _, cfg := range configs {
		if _, err := g.repositoryOperation(GitOperationCreate, repository, []string{"config", cfg[0], cfg[1]}); err != nil {
			return nil, err
		}
	}
	if input.Seed == nil {
		return &repository, nil
	}
	if err := EnsureDir(filepath.Join(string(input.GitDirectory), "objects", "info")); err != nil {
		return nil, &GitOperationError{
			Operation: GitOperationCreate,
			Directory: &input.GitDirectory,
			Message:   "Failed to configure shared Git objects",
			Cause:     err,
		}
	}
	if err := WriteFileString(
		filepath.Join(string(input.GitDirectory), "objects", "info", "alternates"),
		filepath.Join(string(input.Seed.CommonDirectory), "objects")+"\n",
	); err != nil {
		return nil, &GitOperationError{
			Operation: GitOperationCreate,
			Directory: &input.GitDirectory,
			Message:   "Failed to configure shared Git objects",
			Cause:     err,
		}
	}
	_ = CopyFile(
		filepath.Join(string(input.Seed.GitDirectory), "index"),
		filepath.Join(string(input.GitDirectory), "index"),
	)
	return &repository, nil
}

func (g *gitImpl) listLines(kind GitOperationKind, repository GitRepository, args []string) ([]string, error) {
	result, err := g.repositoryOperation(kind, repository, args)
	if err != nil {
		return nil, err
	}
	var items []string
	for _, item := range strings.Split(result.Text, "\x00") {
		if item != "" {
			items = append(items, item)
		}
	}
	return items, nil
}

func (g *gitImpl) Refresh(input GitIndexRefreshInput) (*GitIndexRefreshResult, error) {
	tracked, err := g.listLines(GitOperationRefresh, input.Repository, []string{"diff-files", "--name-only", "-z", "--", string(input.Scope)})
	if err != nil {
		return nil, err
	}
	untracked, err := g.listLines(GitOperationRefresh, input.Repository, []string{"ls-files", "--others", "--exclude-standard", "-z", "--", string(input.Scope)})
	if err != nil {
		return nil, err
	}

	candidates := uniqueStrings(append(append([]string{}, tracked...), untracked...))
	if len(candidates) == 0 {
		return &GitIndexRefreshResult{Skipped: []RelativePath{}}, nil
	}

	ignored := g.ignoredCandidates(input, candidates)
	allowed := filterStrings(candidates, func(item string) bool { return !ignored[item] })
	skipped := g.oversizedUntracked(input, untracked, allowed)

	stage := filterStrings(allowed, func(item string) bool {
		return !slices.Contains(skipped, RelativePath(item))
	})
	remove := make([]string, 0, len(ignored)+len(skipped))
	for item := range ignored {
		remove = append(remove, item)
	}
	for _, item := range skipped {
		remove = append(remove, string(item))
	}

	if len(remove) > 0 {
		if _, err := g.repositoryOperation(
			GitOperationRefresh,
			input.Repository,
			[]string{"rm", "--cached", "-f", "--ignore-unmatch", "--pathspec-from-file=-", "--pathspec-file-nul"},
			strings.Join(remove, "\x00")+"\x00",
		); err != nil {
			return nil, err
		}
	}
	if len(stage) > 0 {
		if _, err := g.repositoryOperation(
			GitOperationRefresh,
			input.Repository,
			[]string{"add", "--all", "--sparse", "--pathspec-from-file=-", "--pathspec-file-nul"},
			strings.Join(stage, "\x00")+"\x00",
		); err != nil {
			return nil, err
		}
	}
	return &GitIndexRefreshResult{Skipped: skipped}, nil
}

// ignoredCandidates returns the candidate paths the ignore repository excludes.
func (g *gitImpl) ignoredCandidates(input GitIndexRefreshInput, candidates []string) map[string]bool {
	ignored := make(map[string]bool)
	if input.Ignores == nil {
		return ignored
	}
	stdin := strings.Join(candidates, "\x00") + "\x00"
	result, err := g.repositoryOperation(GitOperationRefresh, *input.Ignores, []string{"check-ignore", "--no-index", "--stdin", "-z"}, stdin)
	if err != nil {
		return ignored
	}
	for _, item := range strings.Split(result.Text, "\x00") {
		if item != "" {
			ignored[item] = true
		}
	}
	return ignored
}

// oversizedUntracked returns the allowed untracked files that exceed the size
// limit.
func (g *gitImpl) oversizedUntracked(input GitIndexRefreshInput, untracked, allowed []string) []RelativePath {
	if input.MaximumUntrackedFileBytes == nil {
		return nil
	}
	maximum := int64(*input.MaximumUntrackedFileBytes)
	var skipped []RelativePath
	for _, item := range untracked {
		if !slices.Contains(allowed, item) {
			continue
		}
		info, err := Stat(filepath.Join(string(input.Repository.Worktree), item))
		if err == nil && info.Type == FileKindFile && info.Size > maximum {
			skipped = append(skipped, RelativePath(item))
		}
	}
	return skipped
}

// uniqueStrings returns the distinct values of items in first-seen order.
func uniqueStrings(items []string) []string {
	seen := make(map[string]bool, len(items))
	result := make([]string, 0, len(items))
	for _, item := range items {
		if seen[item] {
			continue
		}
		seen[item] = true
		result = append(result, item)
	}
	return result
}

// filterStrings returns the items that keep returns true for.
func filterStrings(items []string, keep func(string) bool) []string {
	result := make([]string, 0, len(items))
	for _, item := range items {
		if keep(item) {
			result = append(result, item)
		}
	}
	return result
}

func (g *gitImpl) Ignored(input GitIndexIgnoredInput) (map[RelativePath]bool, error) {
	result := make(map[RelativePath]bool)
	if len(input.Paths) == 0 {
		return result, nil
	}
	paths := make([]string, len(input.Paths))
	for i, p := range input.Paths {
		paths[i] = string(p)
	}
	run, err := g.proc.Run(context.Background(), "git", g.repositoryArgs(input.Repository, []string{"check-ignore", "--no-index", "--stdin", "-z"}), RunOptions{
		Dir:       string(input.Repository.Worktree),
		ExtendEnv: true,
		Stdin:     []byte(strings.Join(paths, "\x00") + "\x00"),
	})
	if err != nil {
		return nil, &GitOperationError{Operation: GitOperationListFiles, Directory: &input.Repository.Worktree, Message: err.Error(), Cause: err}
	}
	if run.ExitCode != 0 && run.ExitCode != 1 {
		message := gitErrorMessage(string(run.Stderr), "", "Failed to check ignored paths")
		return nil, &GitOperationError{Operation: GitOperationListFiles, Directory: &input.Repository.Worktree, Message: message}
	}
	for _, item := range strings.Split(string(run.Stdout), "\x00") {
		if item != "" {
			result[RelativePath(item)] = true
		}
	}
	return result, nil
}

func (g *gitImpl) Write(repository GitRepository) (GitTreeID, error) {
	result, err := g.repositoryOperation(GitOperationWriteTree, repository, []string{"write-tree"})
	if err != nil {
		return "", err
	}
	return GitTreeID(strings.TrimSpace(result.Text)), nil
}

func (g *gitImpl) Capture(input GitTreeCaptureInput) (GitTreeID, error) {
	var tree GitTreeID
	var err error
	g.locks.WithLock(string(input.Repository.GitDirectory), func() {
		for _, scope := range input.Scopes {
			if _, err = g.Refresh(GitIndexRefreshInput{
				Repository:                input.Repository,
				Scope:                     scope,
				Ignores:                   input.Ignores,
				MaximumUntrackedFileBytes: input.MaximumUntrackedFileBytes,
			}); err != nil {
				return
			}
		}
		tree, err = g.Write(input.Repository)
	})
	return tree, err
}

func (g *gitImpl) Files(input GitTreeFilesInput) ([]RelativePath, error) {
	items, err := g.listLines(GitOperationListFiles, input.Repository, []string{"diff", "--name-only", "-z", string(input.From), string(input.To)})
	if err != nil {
		return nil, err
	}
	result := make([]RelativePath, len(items))
	for i, item := range items {
		result[i] = RelativePath(item)
	}
	return result, nil
}

func (g *gitImpl) Diff(input GitTreeDiffInput) ([]FileDiff, error) {
	paths := input.Paths
	if paths == nil {
		var err error
		paths, err = g.Files(GitTreeFilesInput{Repository: input.Repository, From: input.From, To: input.To})
		if err != nil {
			return nil, err
		}
	}
	contextLines := 3
	if input.Context != nil {
		contextLines = *input.Context
	}
	result := make([]FileDiff, 0, len(paths))
	for _, file := range paths {
		statusResult, err := g.repositoryOperation(GitOperationDiff, input.Repository, []string{"diff", "--name-status", "--no-renames", string(input.From), string(input.To), "--", string(file)})
		if err != nil {
			return nil, err
		}
		statusText := strings.TrimSpace(statusResult.Text)
		status := FileDiffModified
		if strings.HasPrefix(statusText, "A") {
			status = FileDiffAdded
		} else if strings.HasPrefix(statusText, "D") {
			status = FileDiffDeleted
		}
		statsResult, err := g.repositoryOperation(GitOperationDiff, input.Repository, []string{"diff", "--numstat", "--no-renames", string(input.From), string(input.To), "--", string(file)})
		if err != nil {
			return nil, err
		}
		stats := strings.Split(statsResult.Text, "\t")
		binary := len(stats) > 1 && (stats[0] == "-" || stats[1] == "-")
		patch := ""
		if !binary {
			patchResult, err := g.repositoryOperation(GitOperationDiff, input.Repository, []string{"diff", fmt.Sprintf("--unified=%d", contextLines), "--no-renames", string(input.From), string(input.To), "--", string(file)})
			if err != nil {
				return nil, err
			}
			patch = patchResult.Text
		}
		additions := 0
		deletions := 0
		if !binary {
			additions = parseStatCount(stats, 0)
			deletions = parseStatCount(stats, 1)
		}
		result = append(result, FileDiff{
			Path:      file,
			Status:    status,
			Additions: NonNegativeInt(additions),
			Deletions: NonNegativeInt(deletions),
			Patch:     patch,
		})
	}
	return result, nil
}

type gitTreeEntry struct {
	Mode   string
	Object string
}

func (g *gitImpl) entry(repository GitRepository, tree GitTreeID, file RelativePath) (*gitTreeEntry, error) {
	result, err := g.repositoryOperation(GitOperationRestore, repository, []string{"ls-tree", "-z", string(tree), "--", string(file)})
	if err != nil {
		return nil, err
	}
	text := strings.TrimSuffix(result.Text, "\x00")
	if text == "" {
		return nil, nil
	}
	match := regexp.MustCompile(`^(\d+)\s+\w+\s+([0-9a-f]+)\t`).FindStringSubmatch(text)
	if match == nil {
		return nil, &GitOperationError{
			Operation: GitOperationRestore,
			Directory: &repository.Worktree,
			Message:   fmt.Sprintf("Invalid tree entry for %s", file),
		}
	}
	return &gitTreeEntry{Mode: match[1], Object: match[2]}, nil
}

func (g *gitImpl) Preview(input GitTreePreviewInput) ([]FileDiff, error) {
	var result []FileDiff
	var err error
	g.locks.WithLock(string(input.Repository.GitDirectory), func() {
		index := filepath.Join(string(input.Repository.GitDirectory), "preview-"+randomHex()+".index")
		env := map[string]string{"GIT_INDEX_FILE": index}
		defer func() { _ = Remove(index) }()

		run := func(args []string, stdin ...string) (*gitResult, error) {
			var stdinBytes []byte
			if len(stdin) > 0 {
				stdinBytes = []byte(stdin[0])
			}
			r, e := g.proc.Run(context.Background(), "git", g.repositoryArgs(input.Repository, args), RunOptions{
				Dir:       string(input.Repository.Worktree),
				Env:       env,
				ExtendEnv: true,
				Stdin:     stdinBytes,
			})
			if e != nil {
				return nil, e
			}
			if r.ExitCode != 0 {
				return nil, fmt.Errorf("git %v failed", args)
			}
			return &gitResult{Text: string(r.Stdout)}, nil
		}

		if _, err = run([]string{"read-tree", string(input.Current)}); err != nil {
			return
		}
		for file, tree := range input.Files {
			source, e := g.entry(input.Repository, tree, file)
			if e != nil {
				err = e
				return
			}
			if source == nil {
				if _, err = run([]string{"update-index", "--force-remove", "--", string(file)}); err != nil {
					return
				}
				continue
			}
			if _, err = run([]string{"update-index", "--add", "--cacheinfo", source.Mode, source.Object, string(file)}); err != nil {
				return
			}
		}
		targetResult, e := run([]string{"write-tree"})
		if e != nil {
			err = e
			return
		}
		paths := make([]RelativePath, 0, len(input.Files))
		for file := range input.Files {
			paths = append(paths, file)
		}
		result, err = g.Diff(GitTreeDiffInput{
			Repository: input.Repository,
			From:       input.Current,
			To:         GitTreeID(strings.TrimSpace(targetResult.Text)),
			Context:    input.Context,
			Paths:      paths,
		})
	})
	return result, err
}

func (g *gitImpl) Restore(input GitTreeRestoreInput) error {
	var err error
	g.locks.WithLock(string(input.Repository.GitDirectory), func() {
		for file, tree := range input.Files {
			entry, e := g.entry(input.Repository, tree, file)
			if e != nil {
				err = e
				return
			}
			if entry != nil {
				if _, err = g.repositoryOperation(GitOperationRestore, input.Repository, []string{"checkout", string(tree), "--", string(file)}); err != nil {
					return
				}
				continue
			}
			if e := Remove(filepath.Join(string(input.Repository.Worktree), string(file))); e != nil {
				err = &GitOperationError{
					Operation: GitOperationRestore,
					Directory: &input.Repository.Worktree,
					Message:   fmt.Sprintf("Failed to remove %s", file),
					Cause:     e,
				}
				return
			}
		}
	})
	return err
}

func (g *gitImpl) Checkout(input GitTreeCheckoutInput) error {
	var err error
	g.locks.WithLock(string(input.Repository.GitDirectory), func() {
		if _, err = g.repositoryOperation(GitOperationRestore, input.Repository, []string{"read-tree", string(input.Tree)}); err != nil {
			return
		}
		_, err = g.repositoryOperation(GitOperationRestore, input.Repository, []string{"checkout-index", "--all", "--force"})
	})
	return err
}

func (g *gitImpl) CaptureChange(input GitChangeCaptureInput) (GitChangeSet, error) {
	scope := strings.ReplaceAll(relativeToBase(string(input.Repository.Worktree), string(input.Path)), "\\", "/")
	if scope == "" {
		scope = "."
	}
	tracked := gitRun(g.proc, string(input.Repository.Worktree), []string{"diff", "--binary", "HEAD", "--", scope})
	if tracked.ExitCode != 0 {
		message := gitErrorMessage(tracked.Stderr, tracked.Text, "Failed to capture tracked changes")
		return "", &GitPatchError{Operation: GitPatchCapture, Directory: input.Path, Message: message}
	}

	untracked := gitRun(g.proc, string(input.Repository.Worktree), []string{"ls-files", "--others", "--exclude-standard", "-z", "--", scope})
	if untracked.ExitCode != 0 {
		message := gitErrorMessage(untracked.Stderr, untracked.Text, "Failed to list untracked changes")
		return "", &GitPatchError{Operation: GitPatchCapture, Directory: input.Path, Message: message}
	}

	var created []string
	for _, file := range strings.Split(untracked.Text, "\x00") {
		if file == "" {
			continue
		}
		result := gitRun(g.proc, string(input.Repository.Worktree), []string{"diff", "--binary", "--no-index", "--", "/dev/null", file})
		if result.ExitCode == 0 || result.ExitCode == 1 {
			created = append(created, result.Text)
			continue
		}
		message := gitErrorMessage(result.Stderr, result.Text, fmt.Sprintf("Failed to capture untracked change: %s", file))
		return "", &GitPatchError{Operation: GitPatchCapture, Directory: input.Path, Message: message}
	}

	var parts []string
	if tracked.Text != "" {
		parts = append(parts, tracked.Text)
	}
	for _, item := range created {
		if item != "" {
			parts = append(parts, item)
		}
	}
	return GitChangeSet(strings.Join(parts, "\n")), nil
}

func (g *gitImpl) Apply(input GitChangeApplyInput) error {
	result, err := g.proc.Run(context.Background(), "git", []string{"apply", "-"}, RunOptions{
		Dir:       string(input.Path),
		ExtendEnv: true,
		Stdin:     []byte(input.Changes),
	})
	if err != nil {
		return &GitPatchError{Operation: GitPatchApply, Directory: input.Path, Message: err.Error(), Cause: err}
	}
	if result.ExitCode == 0 {
		return nil
	}
	message := gitErrorMessage(string(result.Stderr), string(result.Stdout), "Failed to apply changes")
	return &GitPatchError{Operation: GitPatchApply, Directory: input.Path, Message: message}
}

func (g *gitImpl) Discard(input GitChangeDiscardInput) error {
	scope := strings.ReplaceAll(relativeToBase(string(input.Repository.Worktree), string(input.Path)), "\\", "/")
	if scope == "" {
		scope = "."
	}
	var restoreArgs []string
	if input.Index == "reset" {
		restoreArgs = []string{"checkout", "HEAD", "--", scope}
	} else {
		restoreArgs = []string{"checkout", "--", scope}
	}
	restore := gitRun(g.proc, string(input.Repository.Worktree), restoreArgs)
	if restore.ExitCode != 0 {
		message := gitErrorMessage(restore.Stderr, restore.Text, "Failed to restore tracked changes")
		return &GitPatchError{Operation: GitPatchReset, Directory: input.Path, Message: message}
	}
	if input.Untracked == "preserve" {
		return nil
	}
	clean := gitRun(g.proc, string(input.Repository.Worktree), []string{"clean", "-fd", "--", scope})
	if clean.ExitCode == 0 {
		return nil
	}
	message := gitErrorMessage(clean.Stderr, clean.Text, "Failed to clean untracked changes")
	return &GitPatchError{Operation: GitPatchReset, Directory: input.Path, Message: message}
}

func (g *gitImpl) worktreeRun(operation GitWorktreeOperation, repository GitRepository, args []string, worktreeDirectory *AbsolutePath, cwd string) (string, error) {
	if cwd == "" {
		cwd = string(repository.Worktree)
	}
	result, err := g.proc.Run(context.Background(), "git", args, RunOptions{Dir: cwd, ExtendEnv: true})
	if err != nil {
		return "", &GitWorktreeError{Operation: operation, Directory: worktreeDirectory, Message: err.Error(), Cause: err}
	}
	if result.ExitCode == 0 {
		return string(result.Stdout), nil
	}
	message := gitErrorMessage(string(result.Stderr), string(result.Stdout), "Git failed")
	forceRequired := operation == GitWorktreeRemoveOp && regexp.MustCompile(`contains modified or untracked files|is dirty`).MatchString(message)
	return "", &GitWorktreeError{
		Operation:     operation,
		Directory:     worktreeDirectory,
		Message:       message,
		ForceRequired: forceRequired,
	}
}

func (g *gitImpl) CreateWorktree(input GitWorktreeCreateInput) (*GitRepository, error) {
	dir := input.Directory
	if _, err := g.worktreeRun(GitWorktreeCreateOp, input.Repository, []string{"worktree", "add", "--detach", string(input.Directory), "HEAD"}, &dir, ""); err != nil {
		return nil, err
	}
	repository, err := g.Discover(input.Directory)
	if err != nil {
		return nil, err
	}
	if repository != nil {
		return repository, nil
	}
	return nil, &GitWorktreeError{Operation: GitWorktreeCreateOp, Directory: &dir, Message: "Created worktree could not be opened"}
}

func (g *gitImpl) RemoveWorktree(input GitWorktreeRemoveInput) error {
	dir := input.Directory
	args := []string{"worktree", "remove"}
	if input.Force {
		args = append(args, "--force")
	}
	args = append(args, string(input.Directory))
	_, err := g.worktreeRun(GitWorktreeRemoveOp, input.Repository, args, &dir, string(input.Repository.CommonDirectory))
	return err
}

func (g *gitImpl) ListWorktrees(repository GitRepository) ([]GitWorktree, error) {
	output, err := g.worktreeRun(GitWorktreeListOp, repository, []string{"worktree", "list", "--porcelain"}, nil, "")
	if err != nil {
		return nil, err
	}
	var result []GitWorktree
	index := 0
	for _, line := range strings.Split(output, "\n") {
		if !strings.HasPrefix(line, "worktree ") {
			continue
		}
		kind := GitWorktreeLinked
		if index == 0 {
			kind = GitWorktreeMain
		}
		result = append(result, GitWorktree{
			Directory: AbsolutePath(resolveGitPath(string(repository.Worktree), strings.TrimSpace(line[len("worktree "):]))),
			Kind:      kind,
		})
		index++
	}
	return result, nil
}

func parseStatCount(items []string, index int) int {
	if index >= len(items) {
		return 0
	}
	value, err := strconv.Atoi(strings.TrimSpace(items[index]))
	if err != nil {
		return 0
	}
	return value
}

func relativeToBase(base, target string) string {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return target
	}
	if rel == "." {
		return ""
	}
	return rel
}

func randomHex() string {
	b := make([]byte, 16)
	// crypto/rand.Read never fails; the error return is always nil.
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
