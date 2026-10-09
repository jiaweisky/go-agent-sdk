package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/funnyfactor/mimetype"
)

type FileSystemError struct {
	Method string
	Cause  error
}

func (e *FileSystemError) Error() string {
	detail := ""
	if e.Cause != nil {
		detail = ": " + e.Cause.Error()
	}
	return fmt.Sprintf("Filesystem operation failed: %s%s", e.Method, detail)
}

func (e *FileSystemError) Unwrap() error {
	return e.Cause
}

// FileKind classifies a filesystem entry.
type FileKind string

const (
	FileKindFile      FileKind = "file"
	FileKindDirectory FileKind = "directory"
	FileKindSymlink   FileKind = "symlink"
	FileKindOther     FileKind = "other"
)

type DirEntry struct {
	Name string
	Type FileKind
}

type FileInfo struct {
	Type FileKind
	Size int64
}

// Exists reports whether path exists; a stat error other than "not found" is
// returned.
func Exists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

// IsDir reports whether path is an existing directory.
func IsDir(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}

// IsFile reports whether path exists and is not a directory.
func IsFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

// ReadFileBytes reads path into memory.
func ReadFileBytes(path string) ([]byte, error) {
	return os.ReadFile(path)
}

// ReadFileString reads path as a UTF-8 string.
func ReadFileString(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// ReadJson decodes a JSON file into a generic value.
func ReadJson(path string) (any, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return nil, &FileSystemError{Method: "readJson", Cause: err}
	}
	return v, nil
}

// WriteJson writes data as indented JSON, optionally overriding the mode.
func WriteJson(path string, data any, mode ...os.FileMode) error {
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return &FileSystemError{Method: "writeJson", Cause: err}
	}
	if err := os.WriteFile(path, b, 0644); err != nil {
		return err
	}
	if len(mode) > 0 {
		return os.Chmod(path, mode[0])
	}
	return nil
}

// EnsureDir creates path and any missing parents.
func EnsureDir(path string) error {
	return os.MkdirAll(path, 0755)
}

// WriteWithDirs writes content, creating parent directories when missing, and
// optionally overriding the mode.
func WriteWithDirs(path string, content []byte, mode ...os.FileMode) error {
	err := os.WriteFile(path, content, 0644)
	if err != nil && errors.Is(err, os.ErrNotExist) {
		if err2 := os.MkdirAll(filepath.Dir(path), 0755); err2 != nil {
			return err2
		}
		err = os.WriteFile(path, content, 0644)
	}
	if err != nil {
		return err
	}
	if len(mode) > 0 {
		return os.Chmod(path, mode[0])
	}
	return nil
}

// WriteFileString writes content as a UTF-8 string, creating parents.
func WriteFileString(path string, content string, mode ...os.FileMode) error {
	return WriteWithDirs(path, []byte(content), mode...)
}

// WriteFileBytes writes content. The "wx" flag fails when path already exists.
func WriteFileBytes(path string, content []byte, flags ...string) error {
	if len(flags) > 0 && flags[0] == "wx" {
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
		if err != nil {
			if os.IsExist(err) {
				return &FileSystemError{Method: "writeFile", Cause: fmt.Errorf("file already exists: %s", path)}
			}
			return err
		}
		if _, err := file.Write(content); err != nil {
			file.Close()
			return err
		}
		return file.Close()
	}
	return WriteWithDirs(path, content)
}

// Remove deletes path, treating a missing file as success.
func Remove(path string) error {
	err := os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// RealPath resolves symlinks in path.
func RealPath(path string) (string, error) {
	return filepath.EvalSymlinks(path)
}

// Stat follows symlinks, so its kind is FileKindFile or FileKindDirectory.
func Stat(path string) (*FileInfo, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	kind := FileKindFile
	if info.IsDir() {
		kind = FileKindDirectory
	}
	return &FileInfo{Type: kind, Size: info.Size()}, nil
}

// FindUp returns the paths of targets found in start or any ancestor directory,
// closest first, walking up to the filesystem root.
func FindUp(targets []string, start string) []string {
	var result []string
	current := start
	for {
		for _, target := range targets {
			search := filepath.Join(current, target)
			if ok, _ := Exists(search); ok {
				result = append(result, search)
			}
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	return result
}

// CopyFile copies src to dst with mode 0644.
func CopyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0644)
}

// ReadDir lists a directory's immediate entries.
func ReadDir(dirPath string) ([]DirEntry, error) {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return nil, &FileSystemError{Method: "readDirectoryEntries", Cause: err}
	}
	result := make([]DirEntry, 0, len(entries))
	for _, e := range entries {
		kind := FileKindOther
		switch {
		case e.IsDir():
			kind = FileKindDirectory
		case e.Type().IsRegular():
			kind = FileKindFile
		case e.Type()&os.ModeSymlink != 0:
			kind = FileKindSymlink
		}
		result = append(result, DirEntry{Name: e.Name(), Type: kind})
	}
	return result, nil
}

// Contains reports whether child is parent or lies under it.
func Contains(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return rel == "." || (!filepath.IsAbs(rel) && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// Overlaps reports whether either path contains the other.
func Overlaps(a, b string) bool {
	return Contains(a, b) || Contains(b, a)
}

// ReadFileStringSafe reads a file as a string; a missing or unreadable file
// yields ("", false) instead of an error.
func ReadFileStringSafe(path string) (string, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	return string(data), true
}

// Rename moves a file, creating the destination directory when needed.
func Rename(oldPath, newPath string) error {
	return os.Rename(oldPath, newPath)
}

// RemoveAll removes a path and everything under it.
func RemoveAll(path string) error {
	return os.RemoveAll(path)
}

// MimeType returns the MIME type for a path's extension, defaulting to
// "application/octet-stream".
// lookup is extension based; the "x." prefix replicates mime-types' path handling
// so a dotless name is treated as its own extension.
func MimeType(p string) string {
	if mime := mimetype.TypeByExtension(filepath.Ext("x." + p)); mime != "" {
		return mime
	}
	return "application/octet-stream"
}
