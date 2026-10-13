package util

import (
	"bufio"
	"errors"
	"io"
	"os"
	"strings"
)

// DescribeCommand renders a command and its arguments for a diagnostic message.
func DescribeCommand(command string, args []string) string {
	if len(args) == 0 {
		return command
	}
	return command + " " + strings.Join(args, " ")
}

// StreamReadLines yields each line read until EOF, reporting whether the
// consumer stopped early and any non-EOF read failure. A trailing partial line
// is dropped (the caller sees only complete lines).
func StreamReadLines(read *os.File, yield func(string, error) bool) (bool, error) {
	var readFailure error
	reader := bufio.NewReader(read)
	for {
		line, readErr := reader.ReadString('\n')
		if line = strings.TrimSuffix(line, "\n"); line != "" {
			if !yield(line, nil) {
				return true, nil
			}
		}
		if readErr != nil {
			if !errors.Is(readErr, io.EOF) {
				readFailure = readErr
			}
			return false, readFailure
		}
	}
}

// Truncate clips data to an optional byte limit, reporting whether it was cut.
// A nil limit is unbounded.
func Truncate(data []byte, max *int) ([]byte, bool) {
	if max == nil || len(data) <= *max {
		return data, false
	}
	return data[:*max], true
}
