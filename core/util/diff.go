package util

import (
	"fmt"
	"math"
	"regexp"
	"strings"
)

// DiffChange is one changed block: whether it was added, removed or kept, and its
// lines.
type DiffChange struct {
	Count   int
	Added   bool
	Removed bool
	Value   string
}

var diffLineSeparator = regexp.MustCompile(`(\n|\r\n)`)

// diffTokenize splits text into lines, keeping each line's trailing newline. The
// empty token a final newline produces is dropped.
func diffTokenize(value string) []string {
	matches := diffLineSeparator.FindAllStringIndex(value, -1)
	parts := make([]string, 0, len(matches)*2+1)
	last := 0
	for _, m := range matches {
		parts = append(parts, value[last:m[0]], value[m[0]:m[1]])
		last = m[1]
	}
	parts = append(parts, value[last:])
	if len(parts) > 0 && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}

	out := make([]string, 0, len(parts))
	for i, part := range parts {
		if i%2 == 1 && len(out) > 0 {
			out[len(out)-1] += part
			continue
		}
		out = append(out, part)
	}
	return out
}

func diffRemoveEmpty(tokens []string) []string {
	out := make([]string, 0, len(tokens))
	for _, token := range tokens {
		if token != "" {
			out = append(out, token)
		}
	}
	return out
}

type diffComponent struct {
	count    int
	added    bool
	removed  bool
	previous *diffComponent
	value    string
}

type diffPath struct {
	oldPos        int
	lastComponent *diffComponent
}

// DiffLines returns the line-level changes between two texts.
func DiffLines(oldStr, newStr string) []DiffChange {
	oldTokens := diffRemoveEmpty(diffTokenize(oldStr))
	newTokens := diffRemoveEmpty(diffTokenize(newStr))
	return diffTokens(oldTokens, newTokens)
}

func diffTokens(oldTokens, newTokens []string) []DiffChange {
	newLen := len(newTokens)
	oldLen := len(oldTokens)
	bestPath := map[int]*diffPath{0: {oldPos: -1}}

	extractCommon := func(basePath *diffPath, diagonalPath int) int {
		oldPos := basePath.oldPos
		newPos := oldPos - diagonalPath
		commonCount := 0
		for newPos+1 < newLen && oldPos+1 < oldLen && oldTokens[oldPos+1] == newTokens[newPos+1] {
			newPos++
			oldPos++
			commonCount++
		}
		if commonCount > 0 {
			basePath.lastComponent = &diffComponent{count: commonCount, previous: basePath.lastComponent}
		}
		basePath.oldPos = oldPos
		return newPos
	}

	addToPath := func(path *diffPath, added, removed bool, oldPosInc int) *diffPath {
		last := path.lastComponent
		if last != nil && last.added == added && last.removed == removed {
			return &diffPath{
				oldPos: path.oldPos + oldPosInc,
				lastComponent: &diffComponent{
					count:    last.count + 1,
					added:    added,
					removed:  removed,
					previous: last.previous,
				},
			}
		}
		return &diffPath{
			oldPos:        path.oldPos + oldPosInc,
			lastComponent: &diffComponent{count: 1, added: added, removed: removed, previous: last},
		}
	}

	buildValues := func(lastComponent *diffComponent) []DiffChange {
		var components []*diffComponent
		for component := lastComponent; component != nil; component = component.previous {
			components = append(components, component)
		}
		for i, j := 0, len(components)-1; i < j; i, j = i+1, j-1 {
			components[i], components[j] = components[j], components[i]
		}

		newPos, oldPos := 0, 0
		for _, component := range components {
			if !component.removed {
				component.value = strings.Join(newTokens[newPos:newPos+component.count], "")
				newPos += component.count
				if !component.added {
					oldPos += component.count
				}
				continue
			}
			component.value = strings.Join(oldTokens[oldPos:oldPos+component.count], "")
			oldPos += component.count
		}

		result := make([]DiffChange, len(components))
		for i, component := range components {
			result[i] = DiffChange{
				Count:   component.count,
				Added:   component.added,
				Removed: component.removed,
				Value:   component.value,
			}
		}
		return result
	}

	newPos := extractCommon(bestPath[0], 0)
	if bestPath[0].oldPos+1 >= oldLen && newPos+1 >= newLen {
		return buildValues(bestPath[0].lastComponent)
	}

	editLength := 1
	maxEditLength := newLen + oldLen
	minDiagonalToConsider := math.MinInt
	maxDiagonalToConsider := math.MaxInt

	execEditLength := func() []DiffChange {
		for diagonalPath := max(minDiagonalToConsider, -editLength); diagonalPath <= min(maxDiagonalToConsider, editLength); diagonalPath += 2 {
			removePath := bestPath[diagonalPath-1]
			addPath := bestPath[diagonalPath+1]
			if removePath != nil {
				delete(bestPath, diagonalPath-1)
			}

			canAdd := false
			if addPath != nil {
				addPathNewPos := addPath.oldPos - diagonalPath
				canAdd = 0 <= addPathNewPos && addPathNewPos < newLen
			}
			canRemove := removePath != nil && removePath.oldPos+1 < oldLen
			if !canAdd && !canRemove {
				delete(bestPath, diagonalPath)
				continue
			}

			var basePath *diffPath
			if !canRemove || (canAdd && removePath.oldPos < addPath.oldPos) {
				basePath = addToPath(addPath, true, false, 0)
			} else {
				basePath = addToPath(removePath, false, true, 1)
			}

			newPos = extractCommon(basePath, diagonalPath)
			if basePath.oldPos+1 >= oldLen && newPos+1 >= newLen {
				return buildValues(basePath.lastComponent)
			}
			bestPath[diagonalPath] = basePath
			if basePath.oldPos+1 >= oldLen {
				maxDiagonalToConsider = min(maxDiagonalToConsider, diagonalPath-1)
			}
			if newPos+1 >= newLen {
				minDiagonalToConsider = max(minDiagonalToConsider, diagonalPath+1)
			}
		}
		return nil
	}

	for editLength <= maxEditLength {
		if values := execEditLength(); values != nil {
			return values
		}
		editLength++
	}
	return nil
}

type diffHunk struct {
	oldStart int
	oldLines int
	newStart int
	newLines int
	lines    []string
}

// diffSplitLines splits text into lines, keeping the trailing newline on every
// line except a final line that has none.
func diffSplitLines(text string) []string {
	hasTrailingNewline := strings.HasSuffix(text, "\n")
	parts := strings.Split(text, "\n")
	result := make([]string, len(parts))
	for i, line := range parts {
		result[i] = line + "\n"
	}
	if hasTrailingNewline {
		return result[:len(result)-1]
	}
	last := result[len(result)-1]
	result[len(result)-1] = last[:len(last)-1]
	return result
}

func diffContextLines(lines []string) []string {
	out := make([]string, len(lines))
	for i, line := range lines {
		out[i] = " " + line
	}
	return out
}

func structuredPatch(oldFileName, newFileName, oldStr, newStr string) []diffHunk {
	const context = 4

	changes := DiffLines(oldStr, newStr)

	type changeLines struct {
		change DiffChange
		lines  []string
	}
	entries := make([]changeLines, 0, len(changes)+1)
	for _, change := range changes {
		entries = append(entries, changeLines{change: change, lines: diffSplitLines(change.Value)})
	}
	// Append an empty sentinel change to make cleanup easier. jsdiff seeds it
	// with an explicit empty `lines` array, so it contributes no lines.
	entries = append(entries, changeLines{change: DiffChange{Value: ""}, lines: []string{}})

	var hunks []diffHunk
	oldRangeStart, newRangeStart := 0, 0
	var curRange []string
	oldLine, newLine := 1, 1

	for i := 0; i < len(entries); i++ {
		current := entries[i]
		lines := current.lines
		if current.change.Added || current.change.Removed {
			if oldRangeStart == 0 {
				var prev *changeLines
				if i > 0 {
					prev = &entries[i-1]
				}
				oldRangeStart = oldLine
				newRangeStart = newLine
				if prev != nil {
					if context > 0 {
						start := len(prev.lines) - context
						if start < 0 {
							start = 0
						}
						curRange = diffContextLines(prev.lines[start:])
					} else {
						curRange = nil
					}
					oldRangeStart -= len(curRange)
					newRangeStart -= len(curRange)
				}
			}
			prefix := "+"
			if current.change.Removed {
				prefix = "-"
			}
			for _, line := range lines {
				curRange = append(curRange, prefix+line)
			}
			if current.change.Added {
				newLine += len(lines)
			} else {
				oldLine += len(lines)
			}
			continue
		}

		if oldRangeStart != 0 {
			if len(lines) <= context*2 && i < len(entries)-2 {
				curRange = append(curRange, diffContextLines(lines)...)
			} else {
				contextSize := len(lines)
				if context < contextSize {
					contextSize = context
				}
				curRange = append(curRange, diffContextLines(lines[:contextSize])...)
				hunks = append(hunks, diffHunk{
					oldStart: oldRangeStart,
					oldLines: oldLine - oldRangeStart + contextSize,
					newStart: newRangeStart,
					newLines: newLine - newRangeStart + contextSize,
					lines:    curRange,
				})
				oldRangeStart = 0
				newRangeStart = 0
				curRange = nil
			}
		}
		oldLine += len(lines)
		newLine += len(lines)
	}

	for h := range hunks {
		for i := 0; i < len(hunks[h].lines); i++ {
			line := hunks[h].lines[i]
			if strings.HasSuffix(line, "\n") {
				hunks[h].lines[i] = line[:len(line)-1]
				continue
			}
			rest := append([]string{"\\ No newline at end of file"}, hunks[h].lines[i+1:]...)
			hunks[h].lines = append(hunks[h].lines[:i+1], rest...)
			i++
		}
	}
	return hunks
}

func formatPatch(oldFileName, newFileName string, hunks []diffHunk) string {
	ret := make([]string, 0, 4+len(hunks))
	if oldFileName == newFileName {
		ret = append(ret, "Index: "+oldFileName)
	}
	ret = append(ret, "===================================================================")
	ret = append(ret, "--- "+oldFileName)
	ret = append(ret, "+++ "+newFileName)
	for _, hunk := range hunks {
		if hunk.oldLines == 0 {
			hunk.oldStart--
		}
		if hunk.newLines == 0 {
			hunk.newStart--
		}
		ret = append(ret, fmt.Sprintf("@@ -%d,%d +%d,%d @@", hunk.oldStart, hunk.oldLines, hunk.newStart, hunk.newLines))
		ret = append(ret, hunk.lines...)
	}
	return strings.Join(ret, "\n") + "\n"
}

// CreateTwoFilesPatch renders a unified diff between two texts.
func CreateTwoFilesPatch(oldFileName, newFileName, oldStr, newStr string) string {
	return formatPatch(oldFileName, newFileName, structuredPatch(oldFileName, newFileName, oldStr, newStr))
}
