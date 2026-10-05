package database

import (
	"fmt"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/jiaweisky/go-agent-sdk/schema"
)

func storagePath(input string) string {
	if runtime.GOOS != "windows" {
		return input
	}
	return strings.ReplaceAll(input, "\\", "/")
}

var windowsStoragePathPattern = regexp.MustCompile(`^[A-Za-z]:/`)

func isWindowsStoragePath(input string) bool {
	return windowsStoragePathPattern.MatchString(input) || strings.HasPrefix(input, "//")
}

func absolute(input string) (string, error) {
	result := storagePath(input)
	isAbs := filepath.IsAbs(result)
	if !isAbs && runtime.GOOS == "windows" && isWindowsStoragePath(result) {
		isAbs = true
	}
	if !isAbs {
		return "", fmt.Errorf("Path is not absolute: %s", input)
	}
	return result, nil
}

func toPlatform(input string) string {
	if runtime.GOOS != "windows" || !isWindowsStoragePath(input) {
		return input
	}
	return strings.ReplaceAll(input, "/", "\\")
}

func AbsolutePathColumn(input schema.AbsolutePath) (string, error) {
	return absolute(string(input))
}

func AbsolutePathFromDriver(input string) (schema.AbsolutePath, error) {
	value, err := absolute(input)
	if err != nil {
		return "", err
	}
	return schema.AbsolutePath(toPlatform(value)), nil
}
