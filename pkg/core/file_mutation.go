package core

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jiaweisky/go-agent-sdk/internal/core/effect"
)

// FileOp identifies the built-in tool performing a file mutation.
type FileOp string

const (
	FileOpWrite      FileOp = "write"
	FileOpEdit       FileOp = "edit"
	FileOpApplyPatch FileOp = "apply_patch"
)

// FileWriteGuard, when supplied to ClientOptions, is consulted before a built-in
// file mutation (write/edit/apply_patch). A non-nil error aborts the mutation
// without touching the file; the tool reports it to the model. Nil disables the
// guard. It complements the process sandbox (ClientOptions.Process), which covers
// spawned commands but not the file-mutation tools.
type FileWriteGuard interface {
	Allow(ctx context.Context, path string, op FileOp) error
}

// FileReadGuard, when supplied to ClientOptions, is consulted before a built-in
// read (the read tool). A non-nil error aborts the read without touching the
// file; the tool reports it to the model. Nil disables the guard. It complements
// the process sandbox, which covers spawned commands (including ripgrep) but not
// the in-process read tool.
type FileReadGuard interface {
	AllowRead(ctx context.Context, path string) error
}

type FileMutationTarget struct {
	Canonical string
	Resource  string
}

type FileMutationWriteInput struct {
	Target  FileMutationTarget
	Content []byte
}

type FileMutationTextWriteInput struct {
	Target  FileMutationTarget
	Content string
}

type FileMutationConditionalWriteInput struct {
	Target   FileMutationTarget
	Content  []byte
	Expected []byte
}

type FileMutationRemoveInput struct {
	Target FileMutationTarget
}

type FileMutationWriteResult struct {
	Operation string
	Target    string
	Resource  string
	Existed   bool
}

type FileMutationRemoveResult struct {
	Operation string
	Target    string
	Resource  string
	Existed   bool
}

type FileMutationStaleContentError struct {
	Path string
}

func (e *FileMutationStaleContentError) Error() string {
	return fmt.Sprintf("stale content: %s", e.Path)
}

type FileMutationTargetExistsError struct {
	Path string
}

func (e *FileMutationTargetExistsError) Error() string {
	return fmt.Sprintf("target already exists: %s", e.Path)
}

// fileMutationIsTargetExists recognizes the "wx" write failure raised by
// fs_util.WriteFileBytes when the target already exists. The message lives on
// the wrapped cause, not on FileSystemError.Error(), which prefixes the method.
func fileMutationIsTargetExists(err error) bool {
	var fserr *FileSystemError
	if !errors.As(err, &fserr) {
		return false
	}
	return fserr.Cause != nil && strings.HasPrefix(fserr.Cause.Error(), "file already exists")
}

type FileMutation interface {
	Create(input FileMutationWriteInput) (*FileMutationWriteResult, error)
	Write(input FileMutationWriteInput) (*FileMutationWriteResult, error)
	WriteTextPreservingBom(input FileMutationTextWriteInput) (*FileMutationWriteResult, error)
	WriteIfUnchanged(input FileMutationConditionalWriteInput) (*FileMutationWriteResult, error)
	Remove(input FileMutationRemoveInput) (*FileMutationRemoveResult, error)
}

func NewFileMutation() FileMutation {
	return &fileMutation{locks: effect.MakeUnsafe[string]()}
}

type fileMutation struct {
	locks *effect.KeyedMutex[string]
}

func (fm *fileMutation) Create(input FileMutationWriteInput) (*FileMutationWriteResult, error) {
	var result *FileMutationWriteResult
	var err error
	fm.locks.WithLock(input.Target.Canonical, func() {
		doWrite := func() error {
			return WriteFileBytes(input.Target.Canonical, input.Content, "wx")
		}
		e := doWrite()
		if e != nil {
			if fileMutationIsTargetExists(e) {
				err = &FileMutationTargetExistsError{Path: input.Target.Canonical}
				return
			}
			if os.IsNotExist(e) {
				if e2 := os.MkdirAll(filepath.Dir(input.Target.Canonical), 0755); e2 != nil {
					err = e2
					return
				}
				e = doWrite()
				if e != nil && fileMutationIsTargetExists(e) {
					err = &FileMutationTargetExistsError{Path: input.Target.Canonical}
					return
				}
			}
		}
		if e != nil {
			err = e
			return
		}
		result = &FileMutationWriteResult{
			Operation: "write",
			Target:    input.Target.Canonical,
			Resource:  input.Target.Resource,
			Existed:   false,
		}
	})
	return result, err
}

func (fm *fileMutation) Write(input FileMutationWriteInput) (*FileMutationWriteResult, error) {
	var result *FileMutationWriteResult
	var err error
	fm.locks.WithLock(input.Target.Canonical, func() {
		existed, _ := Exists(input.Target.Canonical)
		if err = WriteWithDirs(input.Target.Canonical, input.Content); err != nil {
			return
		}
		result = &FileMutationWriteResult{
			Operation: "write",
			Target:    input.Target.Canonical,
			Resource:  input.Target.Resource,
			Existed:   existed,
		}
	})
	return result, err
}

func (fm *fileMutation) WriteTextPreservingBom(input FileMutationTextWriteInput) (*FileMutationWriteResult, error) {
	var result *FileMutationWriteResult
	var err error
	fm.locks.WithLock(input.Target.Canonical, func() {
		next := splitBom(input.Content)
		// Mirror fs.readFile(...).catchReason(NotFound => undefined): only a
		// missing file is treated as absent, other read failures propagate.
		current, readErr := ReadFileBytes(input.Target.Canonical)
		var currentBytes []byte
		if readErr == nil {
			currentBytes = current
		} else if !os.IsNotExist(readErr) {
			err = readErr
			return
		}

		content := joinBom(next.Text, (currentBytes != nil && hasUtf8Bom(currentBytes)) || next.Bom)
		if err = WriteWithDirs(input.Target.Canonical, []byte(content)); err != nil {
			return
		}
		result = &FileMutationWriteResult{
			Operation: "write",
			Target:    input.Target.Canonical,
			Resource:  input.Target.Resource,
			Existed:   currentBytes != nil,
		}
	})
	return result, err
}

func (fm *fileMutation) WriteIfUnchanged(input FileMutationConditionalWriteInput) (*FileMutationWriteResult, error) {
	var result *FileMutationWriteResult
	var err error
	fm.locks.WithLock(input.Target.Canonical, func() {
		current, readErr := ReadFileBytes(input.Target.Canonical)
		if readErr != nil {
			err = readErr
			return
		}
		if !bytes.Equal(current, input.Expected) {
			err = &FileMutationStaleContentError{Path: input.Target.Canonical}
			return
		}
		if err = WriteWithDirs(input.Target.Canonical, input.Content); err != nil {
			return
		}
		result = &FileMutationWriteResult{
			Operation: "write",
			Target:    input.Target.Canonical,
			Resource:  input.Target.Resource,
			Existed:   true,
		}
	})
	return result, err
}

func (fm *fileMutation) Remove(input FileMutationRemoveInput) (*FileMutationRemoveResult, error) {
	var result *FileMutationRemoveResult
	var err error
	fm.locks.WithLock(input.Target.Canonical, func() {
		existed := true
		if e := os.Remove(input.Target.Canonical); e != nil {
			if os.IsNotExist(e) {
				existed = false
			} else {
				err = e
				return
			}
		}
		result = &FileMutationRemoveResult{
			Operation: "remove",
			Target:    input.Target.Canonical,
			Resource:  input.Target.Resource,
			Existed:   existed,
		}
	})
	return result, err
}

type bomResult struct {
	Bom  bool
	Text string
}

func splitBom(text string) bomResult {
	stripped := strings.TrimLeft(text, "\ufeff")
	return bomResult{Bom: len(stripped) != len(text), Text: stripped}
}

func joinBom(text string, bom bool) string {
	stripped := splitBom(text).Text
	if bom {
		return "\ufeff" + stripped
	}
	return stripped
}

func hasUtf8Bom(content []byte) bool {
	return len(content) >= 3 && content[0] == 0xef && content[1] == 0xbb && content[2] == 0xbf
}
