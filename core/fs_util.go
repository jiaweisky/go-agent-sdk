package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/funnyfactor/mimetype"
	"github.com/user/go-agent-sdk/core/util"
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

type DirEntry struct {
	Name string
	Type string
}

type FileInfo struct {
	Type string
	Size int64
}

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

func IsDir(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}

func IsFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

func ReadFileBytes(path string) ([]byte, error) {
	return os.ReadFile(path)
}

func ReadFileString(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

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

func EnsureDir(path string) error {
	return os.MkdirAll(path, 0755)
}

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

func WriteFileString(path string, content string, mode ...os.FileMode) error {
	return WriteWithDirs(path, []byte(content), mode...)
}

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

func Remove(path string) error {
	err := os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func RealPath(path string) (string, error) {
	return filepath.EvalSymlinks(path)
}

func Stat(path string) (*FileInfo, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	fileType := "File"
	if info.IsDir() {
		fileType = "Directory"
	} else if info.Mode()&os.ModeSymlink != 0 {
		fileType = "SymbolicLink"
	}
	return &FileInfo{Type: fileType, Size: info.Size()}, nil
}

func Up(targets []string, start string, stop ...string) []string {
	var result []string
	current := start
	for {
		for _, target := range targets {
			search := filepath.Join(current, target)
			if ok, _ := Exists(search); ok {
				result = append(result, search)
			}
		}
		if len(stop) > 0 && stop[0] == current {
			break
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	return result
}

func CopyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0644)
}

func ReadDir(dirPath string) ([]DirEntry, error) {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return nil, &FileSystemError{Method: "readDirectoryEntries", Cause: err}
	}
	result := make([]DirEntry, 0, len(entries))
	for _, e := range entries {
		typ := "other"
		if e.IsDir() {
			typ = "directory"
		} else if e.Type().IsRegular() {
			typ = "file"
		} else if e.Type()&os.ModeSymlink != 0 {
			typ = "symlink"
		}
		result = append(result, DirEntry{Name: e.Name(), Type: typ})
	}
	return result, nil
}

func Contains(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return rel == "." || (!filepath.IsAbs(rel) && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

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

// Glob returns the paths matching the pattern.
func Glob(pattern string, options util.GlobOptions) ([]string, error) {
	return util.GlobScan(pattern, options)
}

// GlobMatch reports whether the path matches the pattern.
func GlobMatch(pattern, filepath string) bool {
	return util.GlobMatch(pattern, filepath)
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
