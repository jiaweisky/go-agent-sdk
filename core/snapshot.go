package core

import (
	"errors"
	"path/filepath"
	"strings"

	"github.com/user/go-agent-sdk/core/project"
	"github.com/user/go-agent-sdk/core/util"
	"github.com/user/go-agent-sdk/schema"
)

type SnapshotID string

type SnapshotOperation string

const (
	SnapshotOperationCapture SnapshotOperation = "capture"
	SnapshotOperationFiles   SnapshotOperation = "files"
	SnapshotOperationDiff    SnapshotOperation = "diff"
	SnapshotOperationPreview SnapshotOperation = "preview"
	SnapshotOperationRestore SnapshotOperation = "restore"
)

// SnapshotError mirrors Snapshot.Error.
type SnapshotError struct {
	Operation SnapshotOperation
	Message   string
	Cause     error
}

func (e *SnapshotError) Error() string {
	return "snapshot " + string(e.Operation) + ": " + e.Message
}

func (e *SnapshotError) Unwrap() error {
	return e.Cause
}

type SnapshotCompareInput struct {
	From SnapshotID
	To   SnapshotID
}

type SnapshotDiffInput struct {
	SnapshotCompareInput
	Context *int
	Paths   []schema.RelativePath
}

type SnapshotRestoreInput struct {
	Files map[schema.RelativePath]SnapshotID
}

type SnapshotPreviewInput struct {
	SnapshotRestoreInput
	Context *int
}

// Snapshot mirrors Snapshot.Service.
type Snapshot struct {
	git          Git
	worktree     AbsolutePath
	directory    AbsolutePath
	gitDirectory AbsolutePath
	source       *GitRepository
	enabled      bool
}

func NewSnapshot(git Git, global Global, location *Location) (*Snapshot, error) {
	source, err := git.Repo.Discover(location.Project.Directory)
	if err != nil {
		return nil, err
	}
	worktree := location.Project.Directory
	if source != nil {
		real, err := RealPath(string(source.Worktree))
		if err != nil {
			return nil, err
		}
		worktree = AbsolutePath(real)
	}
	gitDirectory := AbsolutePath(filepath.Join(global.Data, "snapshot", string(location.Project.ID), util.HashFastString(string(worktree))))

	enabled := location.Vcs != nil && location.Vcs.Type == project.VcsTypeGit

	return &Snapshot{
		git:          git,
		worktree:     worktree,
		directory:    location.Directory,
		gitDirectory: gitDirectory,
		source:       source,
		enabled:      enabled,
	}, nil
}

func (s *Snapshot) scope() (schema.RelativePath, error) {
	relative, err := filepath.Rel(string(s.worktree), string(s.directory))
	if err != nil || strings.HasPrefix(relative, "..") || filepath.IsAbs(relative) {
		return "", &SnapshotError{Operation: SnapshotOperationCapture, Message: "Location is outside the project"}
	}
	relative = strings.ReplaceAll(relative, "\\", "/")
	if relative == "" {
		relative = "."
	}
	return schema.RelativePath(relative), nil
}

func (s *Snapshot) repository() (*GitRepository, error) {
	if s.source == nil {
		return nil, &SnapshotError{Operation: SnapshotOperationCapture, Message: "Project is not a Git repository"}
	}
	if exists, _ := Exists(filepath.Join(string(s.gitDirectory), "HEAD")); exists {
		return &GitRepository{Worktree: s.worktree, GitDirectory: s.gitDirectory, CommonDirectory: s.gitDirectory}, nil
	}
	repository, err := s.git.Repo.Create(GitCreateInput{
		Worktree:     s.worktree,
		GitDirectory: s.gitDirectory,
		Seed:         s.source,
	})
	if err != nil {
		return nil, snapshotFailure(SnapshotOperationCapture, err)
	}
	return repository, nil
}

// Capture mirrors Snapshot.capture: it returns nil when disabled or when the
// best-effort capture fails.
func (s *Snapshot) Capture() (*SnapshotID, error) {
	if !s.enabled {
		return nil, nil
	}
	repository, err := s.repository()
	if err != nil {
		return nil, nil
	}
	scope, err := s.scope()
	if err != nil {
		return nil, nil
	}
	maximum := 2 * 1024 * 1024
	tree, err := s.git.Tree.Capture(GitTreeCaptureInput{
		Repository:                *repository,
		Scopes:                    []schema.RelativePath{scope},
		Ignores:                   s.source,
		MaximumUntrackedFileBytes: &maximum,
	})
	if err != nil {
		return nil, nil
	}
	id := SnapshotID(tree)
	return &id, nil
}

type snapshotComparison struct {
	repository GitRepository
	from       GitTreeID
	to         GitTreeID
}

func (s *Snapshot) compare(operation SnapshotOperation, input SnapshotCompareInput) (snapshotComparison, error) {
	repository, err := s.repository()
	if err != nil {
		return snapshotComparison{}, snapshotFailure(operation, err)
	}
	return snapshotComparison{
		repository: *repository,
		from:       GitTreeID(input.From),
		to:         GitTreeID(input.To),
	}, nil
}

// Files mirrors Snapshot.files.
func (s *Snapshot) Files(input SnapshotCompareInput) ([]schema.RelativePath, error) {
	comparison, err := s.compare(SnapshotOperationFiles, input)
	if err != nil {
		return nil, err
	}
	files, err := s.git.Tree.Files(GitTreeFilesInput{
		Repository: comparison.repository, From: comparison.from, To: comparison.to,
	})
	if err != nil {
		return nil, snapshotFailure(SnapshotOperationFiles, err)
	}
	if s.source == nil {
		return files, nil
	}
	ignored, err := s.git.Index.Ignored(GitIndexIgnoredInput{Repository: *s.source, Paths: files})
	if err != nil {
		return nil, snapshotFailure(SnapshotOperationFiles, err)
	}
	result := make([]schema.RelativePath, 0, len(files))
	for _, file := range files {
		if !ignored[file] {
			result = append(result, file)
		}
	}
	return result, nil
}

// Diff mirrors Snapshot.diff.
func (s *Snapshot) Diff(input SnapshotDiffInput) ([]FileDiff, error) {
	comparison, err := s.compare(SnapshotOperationDiff, input.SnapshotCompareInput)
	if err != nil {
		return nil, err
	}
	files, err := s.git.Tree.Files(GitTreeFilesInput{
		Repository: comparison.repository, From: comparison.from, To: comparison.to,
	})
	if err != nil {
		return nil, snapshotFailure(SnapshotOperationDiff, err)
	}
	ignored := map[schema.RelativePath]bool{}
	if s.source != nil {
		ignored, err = s.git.Index.Ignored(GitIndexIgnoredInput{Repository: *s.source, Paths: files})
		if err != nil {
			return nil, snapshotFailure(SnapshotOperationDiff, err)
		}
	}
	paths := input.Paths
	if paths == nil {
		paths = files
	}
	filtered := make([]schema.RelativePath, 0, len(paths))
	for _, file := range paths {
		if !ignored[file] {
			filtered = append(filtered, file)
		}
	}
	diff, err := s.git.Tree.Diff(GitTreeDiffInput{
		Repository: comparison.repository, From: comparison.from, To: comparison.to,
		Context: input.Context, Paths: filtered,
	})
	if err != nil {
		return nil, snapshotFailure(SnapshotOperationDiff, err)
	}
	return diff, nil
}

// Preview mirrors Snapshot.preview.
func (s *Snapshot) Preview(input SnapshotPreviewInput) ([]FileDiff, error) {
	if !s.enabled {
		return nil, &SnapshotError{Operation: SnapshotOperationPreview, Message: "Snapshots are disabled"}
	}
	repository, err := s.repository()
	if err != nil {
		return nil, snapshotFailure(SnapshotOperationPreview, err)
	}
	files, err := s.plan(SnapshotOperationPreview, input.SnapshotRestoreInput)
	if err != nil {
		return nil, err
	}
	scopes := make([]schema.RelativePath, 0, len(files))
	for file := range files {
		scopes = append(scopes, file)
	}
	maximum := 2 * 1024 * 1024
	current, err := s.git.Tree.Capture(GitTreeCaptureInput{
		Repository: *repository, Scopes: scopes, Ignores: s.source, MaximumUntrackedFileBytes: &maximum,
	})
	if err != nil {
		return nil, snapshotFailure(SnapshotOperationPreview, err)
	}
	preview, err := s.git.Tree.Preview(GitTreePreviewInput{
		Repository: *repository, Current: current, Files: files, Context: input.Context,
	})
	if err != nil {
		return nil, snapshotFailure(SnapshotOperationPreview, err)
	}
	return preview, nil
}

// Restore mirrors Snapshot.restore.
func (s *Snapshot) Restore(input SnapshotRestoreInput) error {
	if !s.enabled {
		return &SnapshotError{Operation: SnapshotOperationRestore, Message: "Snapshots are disabled"}
	}
	repository, err := s.repository()
	if err != nil {
		return snapshotFailure(SnapshotOperationRestore, err)
	}
	files, err := s.plan(SnapshotOperationRestore, input)
	if err != nil {
		return err
	}
	if err := s.git.Tree.Restore(GitTreeRestoreInput{Repository: *repository, Files: files}); err != nil {
		return snapshotFailure(SnapshotOperationRestore, err)
	}
	return nil
}

// Checkout mirrors Snapshot.checkout.
func (s *Snapshot) Checkout(snapshot SnapshotID) error {
	repository, err := s.repository()
	if err != nil {
		return snapshotFailure(SnapshotOperationRestore, err)
	}
	if err := s.git.Tree.Checkout(GitTreeCheckoutInput{Repository: *repository, Tree: GitTreeID(snapshot)}); err != nil {
		return snapshotFailure(SnapshotOperationRestore, err)
	}
	return nil
}

func (s *Snapshot) plan(operation SnapshotOperation, input SnapshotRestoreInput) (map[schema.RelativePath]GitTreeID, error) {
	files := map[schema.RelativePath]GitTreeID{}
	for file, snapshot := range input.Files {
		absolute := filepath.Join(string(s.worktree), filepath.FromSlash(string(file)))
		if !Contains(string(s.worktree), absolute) {
			return nil, &SnapshotError{Operation: operation, Message: "Path escapes the project: " + string(file)}
		}
		files[file] = GitTreeID(snapshot)
	}
	return files, nil
}

func snapshotFailure(operation SnapshotOperation, cause error) error {
	var snapshotError *SnapshotError
	if errors.As(cause, &snapshotError) && snapshotError.Operation == operation {
		return cause
	}
	return &SnapshotError{Operation: operation, Message: cause.Error(), Cause: cause}
}
