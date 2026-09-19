package core

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

// PatchHunk is the Patch.Hunk union from patch.ts.
type PatchHunk interface {
	patchHunkTag()
}

type PatchAddHunk struct {
	Path     string
	Contents string
}

func (PatchAddHunk) patchHunkTag() {}

type PatchDeleteHunk struct {
	Path string
}

func (PatchDeleteHunk) patchHunkTag() {}

type PatchUpdateHunk struct {
	Path     string
	MovePath *string
	Chunks   []PatchUpdateFileChunk
}

func (PatchUpdateHunk) patchHunkTag() {}

type PatchUpdateFileChunk struct {
	OldLines      []string
	NewLines      []string
	ChangeContext *string
	EndOfFile     bool
}

type PatchFileUpdate struct {
	Content string
	Bom     bool
}

var (
	patchBeginMarker  = "*** Begin Patch"
	patchEndMarker    = "*** End Patch"
	patchAddPrefix    = "*** Add File:"
	patchDeletePrefix = "*** Delete File:"
	patchUpdatePrefix = "*** Update File:"
	patchMovePrefix   = "*** Move to:"
	patchEofMarker    = "*** End of File"
	patchHeredocOpen  = regexp.MustCompile(`^(?:cat\s+)?<<['"]?(\w+)['"]?\s*\n`)
)

// PatchParse mirrors Patch.parse.
func PatchParse(patchText string) ([]PatchHunk, error) {
	lines := strings.Split(patchStripHeredoc(patchTrimSpace(patchText)), "\n")
	begin := -1
	for i, line := range lines {
		if patchTrimSpace(line) == patchBeginMarker {
			begin = i
			break
		}
	}
	end := -1
	for i, line := range lines {
		if patchTrimSpace(line) == patchEndMarker {
			end = i
			break
		}
	}
	if begin == -1 || end == -1 || begin >= end {
		return nil, fmt.Errorf("Invalid patch format: missing Begin/End markers")
	}

	var hunks []PatchHunk
	index := begin + 1
	for index < end {
		line := lines[index]
		switch {
		case strings.HasPrefix(line, patchAddPrefix):
			path := patchTrimSpace(line[len(patchAddPrefix):])
			if path == "" {
				return nil, fmt.Errorf("Invalid add file path")
			}
			contents, next, err := patchParseAdd(lines, index+1)
			if err != nil {
				return nil, err
			}
			hunks = append(hunks, PatchAddHunk{Path: path, Contents: contents})
			index = next
		case strings.HasPrefix(line, patchDeletePrefix):
			path := patchTrimSpace(line[len(patchDeletePrefix):])
			if path == "" {
				return nil, fmt.Errorf("Invalid delete file path")
			}
			hunks = append(hunks, PatchDeleteHunk{Path: path})
			index++
		case strings.HasPrefix(line, patchUpdatePrefix):
			path := patchTrimSpace(line[len(patchUpdatePrefix):])
			if path == "" {
				return nil, fmt.Errorf("Invalid update file path")
			}
			next := index + 1
			var movePath *string
			if next < len(lines) && strings.HasPrefix(lines[next], patchMovePrefix) {
				move := patchTrimSpace(lines[next][len(patchMovePrefix):])
				if move == "" {
					return nil, fmt.Errorf("Invalid move file path")
				}
				movePath = &move
				next++
			}
			chunks, after, err := patchParseUpdate(lines, next)
			if err != nil {
				return nil, err
			}
			if len(chunks) == 0 {
				return nil, fmt.Errorf("Invalid update hunk for %s: expected at least one @@ chunk", path)
			}
			hunks = append(hunks, PatchUpdateHunk{Path: path, MovePath: movePath, Chunks: chunks})
			index = after
		default:
			return nil, fmt.Errorf("Invalid patch line: %s", line)
		}
	}
	return hunks, nil
}

func patchParseAdd(lines []string, start int) (string, int, error) {
	var content []string
	index := start
	for index < len(lines) && !strings.HasPrefix(lines[index], "***") {
		if !strings.HasPrefix(lines[index], "+") {
			return "", 0, fmt.Errorf("Invalid add file line: %s", lines[index])
		}
		content = append(content, lines[index][1:])
		index++
	}
	return strings.Join(content, "\n"), index, nil
}

func patchParseUpdate(lines []string, start int) ([]PatchUpdateFileChunk, int, error) {
	var chunks []PatchUpdateFileChunk
	index := start
	for index < len(lines) && !strings.HasPrefix(lines[index], "***") {
		if !strings.HasPrefix(lines[index], "@@") {
			return nil, 0, fmt.Errorf("Invalid update file line: %s", lines[index])
		}
		context := patchTrimSpace(lines[index][2:])
		var changeContext *string
		if context != "" {
			changeContext = &context
		}
		oldLines := []string{}
		newLines := []string{}
		endOfFile := false
		index++
		for index < len(lines) && !strings.HasPrefix(lines[index], "@@") {
			line := lines[index]
			if line == patchEofMarker {
				endOfFile = true
				index++
				break
			}
			if strings.HasPrefix(line, "***") {
				break
			}
			switch {
			case strings.HasPrefix(line, " "):
				oldLines = append(oldLines, line[1:])
				newLines = append(newLines, line[1:])
			case strings.HasPrefix(line, "-"):
				oldLines = append(oldLines, line[1:])
			case strings.HasPrefix(line, "+"):
				newLines = append(newLines, line[1:])
			default:
				return nil, 0, fmt.Errorf("Invalid update chunk line: %s", line)
			}
			index++
		}
		chunks = append(chunks, PatchUpdateFileChunk{
			OldLines:      oldLines,
			NewLines:      newLines,
			ChangeContext: changeContext,
			EndOfFile:     endOfFile,
		})
	}
	return chunks, index, nil
}

// PatchDerive mirrors Patch.derive.
func PatchDerive(path string, chunks []PatchUpdateFileChunk, original string) (PatchFileUpdate, error) {
	source := patchSplitBom(original)
	lines := strings.Split(source.Text, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	replacements, err := patchComputeReplacements(lines, path, chunks)
	if err != nil {
		return PatchFileUpdate{}, err
	}
	updated := append([]string{}, lines...)
	for i := len(replacements) - 1; i >= 0; i-- {
		replacement := replacements[i]
		updated = patchSplice(updated, replacement.Start, replacement.Remove, replacement.Insert)
	}
	if len(updated) == 0 || updated[len(updated)-1] != "" {
		updated = append(updated, "")
	}
	next := patchSplitBom(strings.Join(updated, "\n"))
	return PatchFileUpdate{Content: next.Text, Bom: source.Bom || next.Bom}, nil
}

// PatchJoinBom mirrors Patch.joinBom.
func PatchJoinBom(text string, bom bool) string {
	return patchJoinBom(text, bom)
}

type patchBom struct {
	Bom  bool
	Text string
}

// patchSplitBom mirrors patch.ts's splitBom, which removes at most one leading BOM.
func patchSplitBom(text string) patchBom {
	if strings.HasPrefix(text, "\ufeff") {
		return patchBom{Bom: true, Text: text[len("\ufeff"):]}
	}
	return patchBom{Bom: false, Text: text}
}

func patchJoinBom(text string, bom bool) string {
	stripped := patchSplitBom(text).Text
	if bom {
		return "\ufeff" + stripped
	}
	return stripped
}

type patchReplacement struct {
	Start  int
	Remove int
	Insert []string
}

func patchComputeReplacements(lines []string, path string, chunks []PatchUpdateFileChunk) ([]patchReplacement, error) {
	var replacements []patchReplacement
	lineIndex := 0
	for _, chunk := range chunks {
		if chunk.ChangeContext != nil {
			context := patchSeek(lines, []string{*chunk.ChangeContext}, lineIndex, false)
			if context == -1 {
				return nil, fmt.Errorf("Failed to find context '%s' in %s", *chunk.ChangeContext, path)
			}
			lineIndex = context + 1
		}
		if len(chunk.OldLines) == 0 {
			replacements = append(replacements, patchReplacement{Start: len(lines), Remove: 0, Insert: chunk.NewLines})
			continue
		}
		oldLines := chunk.OldLines
		newLines := chunk.NewLines
		found := patchSeek(lines, oldLines, lineIndex, chunk.EndOfFile)
		if found == -1 && oldLines[len(oldLines)-1] == "" {
			oldLines = oldLines[:len(oldLines)-1]
			if len(newLines) > 0 && newLines[len(newLines)-1] == "" {
				newLines = newLines[:len(newLines)-1]
			}
			found = patchSeek(lines, oldLines, lineIndex, chunk.EndOfFile)
		}
		if found == -1 {
			return nil, fmt.Errorf("Failed to find expected lines in %s:\n%s", path, strings.Join(chunk.OldLines, "\n"))
		}
		replacements = append(replacements, patchReplacement{Start: found, Remove: len(oldLines), Insert: newLines})
		lineIndex = found + len(oldLines)
	}
	sort.SliceStable(replacements, func(i, j int) bool {
		return replacements[i].Start < replacements[j].Start
	})
	return replacements, nil
}

func patchSeek(lines []string, pattern []string, start int, eof bool) int {
	if len(pattern) == 0 {
		return -1
	}
	compares := []func(string, string) bool{patchExact, patchRstrip, patchTrim, patchNormalized}
	for _, compare := range compares {
		if eof {
			offset := len(lines) - len(pattern)
			if offset >= start && patchMatches(lines, pattern, offset, compare) {
				return offset
			}
		}
		for offset := start; offset <= len(lines)-len(pattern); offset++ {
			if patchMatches(lines, pattern, offset, compare) {
				return offset
			}
		}
	}
	return -1
}

func patchMatches(lines []string, pattern []string, offset int, compare func(string, string) bool) bool {
	for i, line := range pattern {
		if offset+i >= len(lines) || !compare(lines[offset+i], line) {
			return false
		}
	}
	return true
}

func patchExact(left, right string) bool {
	return left == right
}

func patchRstrip(left, right string) bool {
	return strings.TrimRightFunc(left, patchIsTrimSpace) == strings.TrimRightFunc(right, patchIsTrimSpace)
}

func patchTrim(left, right string) bool {
	return patchTrimSpace(left) == patchTrimSpace(right)
}

func patchNormalized(left, right string) bool {
	return patchNormalize(patchTrimSpace(left)) == patchNormalize(patchTrimSpace(right))
}

var patchNormalizeReplacer = strings.NewReplacer(
	"\u2018", "'", "\u2019", "'", "\u201a", "'", "\u201b", "'",
	"\u201c", "\"", "\u201d", "\"", "\u201e", "\"", "\u201f", "\"",
	"\u2010", "-", "\u2011", "-", "\u2012", "-", "\u2013", "-", "\u2014", "-", "\u2015", "-",
	"\u2026", "...",
	"\u00a0", " ",
)

func patchNormalize(value string) string {
	return patchNormalizeReplacer.Replace(value)
}

// patchTrimSpace mirrors JavaScript's String.prototype.trim, which also treats
// U+FEFF as whitespace (unlike strings.TrimSpace).
func patchTrimSpace(value string) string {
	return strings.TrimFunc(value, patchIsTrimSpace)
}

func patchIsTrimSpace(r rune) bool {
	return unicode.IsSpace(r) || r == '\ufeff'
}

func patchSplice(lines []string, start, remove int, insert []string) []string {
	if start > len(lines) {
		start = len(lines)
	}
	if start < 0 {
		start = 0
	}
	end := start + remove
	if end > len(lines) {
		end = len(lines)
	}
	result := make([]string, 0, len(lines)-(end-start)+len(insert))
	result = append(result, lines[:start]...)
	result = append(result, insert...)
	result = append(result, lines[end:]...)
	return result
}

func patchStripHeredoc(input string) string {
	match := patchHeredocOpen.FindStringSubmatchIndex(input)
	if match == nil {
		return input
	}
	delimiter := input[match[2]:match[3]]
	rest := input[match[1]:]
	tail := regexp.MustCompile(`\n` + regexp.QuoteMeta(delimiter) + `\s*$`)
	index := tail.FindStringIndex(rest)
	if index == nil {
		return input
	}
	return rest[:index[0]]
}
