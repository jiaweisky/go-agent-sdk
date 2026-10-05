package tool

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"golang.org/x/text/collate"
	"golang.org/x/text/language"

	"github.com/jiaweisky/go-agent-sdk/core"
	"github.com/jiaweisky/go-agent-sdk/schema"
)

const (
	ReadMaxReadLines        = 2000
	ReadMaxReadBytes        = 50 * 1024
	ReadMaxMediaIngestBytes = 20 * 1024 * 1024
	readMaxLineLength       = 2000
)

var readMaxLineSuffix = fmt.Sprintf("... (line truncated to %d chars)", readMaxLineLength)

type ReadBinaryFileError struct {
	Resource string
}

func (e *ReadBinaryFileError) Error() string {
	return "Cannot read binary file: " + e.Resource
}

type ReadMediaIngestLimitError struct {
	Resource     string
	MaximumBytes int
}

func (e *ReadMediaIngestLimitError) Error() string {
	return fmt.Sprintf("Media exceeds %d byte ingestion limit: %s", e.MaximumBytes, e.Resource)
}

type ReadMalformedUtf8Error struct {
	Resource string
}

func (e *ReadMalformedUtf8Error) Error() string {
	return "File is not valid UTF-8: " + e.Resource
}

type ReadOffsetOutOfRangeError struct {
	Offset int
}

func (e *ReadOffsetOutOfRangeError) Error() string {
	return fmt.Sprintf("Offset %d is out of range", e.Offset)
}

type ReadPathKindError struct {
	Resource string
	Expected string
}

func (e *ReadPathKindError) Error() string {
	return fmt.Sprintf("Path is not %s: %s", e.Expected, e.Resource)
}

type ReadPageInput struct {
	Offset *int
	Limit  *int
}

type ReadTextPage struct {
	Type      string `json:"type"`
	Content   string `json:"content"`
	MIME      string `json:"mime"`
	Offset    int    `json:"offset"`
	Truncated bool   `json:"truncated"`
	Next      *int   `json:"next,omitempty"`
}

type ReadListPage struct {
	Entries   []core.FileSystemEntry `json:"entries"`
	Truncated bool                   `json:"truncated"`
	Next      *int                   `json:"next,omitempty"`
}

type ReadFileSystem interface {
	Inspect(input schema.AbsolutePath) (string, error)
	Read(input schema.AbsolutePath, resource string, page ReadPageInput) (any, error)
	List(input schema.AbsolutePath, page ReadPageInput) (*ReadListPage, error)
}

func NewReadFileSystem(locale language.Tag) ReadFileSystem {
	return defaultReadFileSystem{locale: locale}
}

type defaultReadFileSystem struct{ locale language.Tag }

func (defaultReadFileSystem) Inspect(input schema.AbsolutePath) (string, error) {
	return inspectToolRead(input)
}

func (defaultReadFileSystem) Read(input schema.AbsolutePath, resource string, page ReadPageInput) (any, error) {
	return readToolRead(input, resource, page)
}

func (f defaultReadFileSystem) List(input schema.AbsolutePath, page ReadPageInput) (*ReadListPage, error) {
	return listToolRead(input, page, f.locale)
}

var readBinaryExtensions = map[string]bool{
	".zip":   true,
	".tar":   true,
	".gz":    true,
	".exe":   true,
	".dll":   true,
	".so":    true,
	".class": true,
	".jar":   true,
	".war":   true,
	".7z":    true,
	".doc":   true,
	".docx":  true,
	".xls":   true,
	".xlsx":  true,
	".ppt":   true,
	".pptx":  true,
	".odt":   true,
	".ods":   true,
	".odp":   true,
	".bin":   true,
	".dat":   true,
	".obj":   true,
	".o":     true,
	".a":     true,
	".lib":   true,
	".wasm":  true,
	".pyc":   true,
	".pyo":   true,
}

func readStartsWith(bytes []byte, prefix []byte) bool {
	for index, value := range prefix {
		if index >= len(bytes) || bytes[index] != value {
			return false
		}
	}
	return true
}

// readCharLength and readCharSlice measure and slice a string in UTF-16 code
// units for line truncation.
func readCharLength(value string) int {
	return len(utf16.Encode([]rune(value)))
}

func readCharSlice(value string, end int) string {
	units := utf16.Encode([]rune(value))
	if end >= len(units) {
		return value
	}
	return string(utf16.Decode(units[:end]))
}

func readImageMime(bytes []byte) string {
	if readStartsWith(bytes, []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}) {
		return "image/png"
	}
	if readStartsWith(bytes, []byte{0xff, 0xd8, 0xff}) {
		return "image/jpeg"
	}
	if readStartsWith(bytes, []byte{0x47, 0x49, 0x46, 0x38}) {
		return "image/gif"
	}
	if readStartsWith(bytes, []byte{0x52, 0x49, 0x46, 0x46}) && len(bytes) >= 8 && readStartsWith(bytes[8:], []byte{0x57, 0x45, 0x42, 0x50}) {
		return "image/webp"
	}
	return ""
}

func readExtensionBinary(resource string) bool {
	return readBinaryExtensions[strings.ToLower(filepath.Ext(resource))]
}

func readBinary(resource string, bytes []byte) bool {
	if readExtensionBinary(resource) {
		return true
	}
	if len(bytes) == 0 {
		return false
	}
	nonPrintable := 0
	for _, b := range bytes {
		if b == 0 {
			return true
		}
		if b < 9 || (b > 13 && b < 32) {
			nonPrintable++
		}
	}
	return float64(nonPrintable)/float64(len(bytes)) > 0.3
}

func readFileURL(path string) string {
	u := url.URL{Scheme: "file", Path: path}
	return u.String()
}

func readAlloc(file *os.File, size int64) ([]byte, error) {
	if size <= 0 {
		return nil, nil
	}
	buf := make([]byte, size)
	total := 0
	for total < len(buf) {
		n, err := file.Read(buf[total:])
		total += n
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
		if n == 0 {
			break
		}
	}
	if total == 0 {
		return nil, nil
	}
	return buf[:total], nil
}

type readUTF8Stream struct {
	resource string
	pending  []byte
	started  bool
}

func (d *readUTF8Stream) Decode(chunk []byte) (string, error) {
	data := make([]byte, 0, len(d.pending)+len(chunk))
	data = append(data, d.pending...)
	data = append(data, chunk...)
	d.pending = nil

	var builder strings.Builder
	for len(data) > 0 {
		r, size := utf8.DecodeRune(data)
		if r == utf8.RuneError && size == 1 {
			if !utf8.FullRune(data) {
				d.pending = append(d.pending, data...)
				return builder.String(), nil
			}
			return "", &ReadMalformedUtf8Error{Resource: d.resource}
		}
		builder.Write(data[:size])
		data = data[size:]
	}
	out := builder.String()
	if !d.started && out != "" {
		d.started = true
		out = strings.TrimPrefix(out, "\uFEFF")
	}
	return out, nil
}

func (d *readUTF8Stream) Flush() (string, error) {
	if len(d.pending) > 0 {
		return "", &ReadMalformedUtf8Error{Resource: d.resource}
	}
	return "", nil
}

func inspectToolRead(input schema.AbsolutePath) (string, error) {
	info, err := core.Stat(string(input))
	if err != nil {
		return "", err
	}
	switch info.Type {
	case core.FileKindFile:
		return "file", nil
	case core.FileKindDirectory:
		return "directory", nil
	default:
		return "", &ReadPathKindError{Resource: string(input), Expected: "a file or directory"}
	}
}

func readToolRead(input schema.AbsolutePath, resource string, page ReadPageInput) (any, error) {
	real, err := core.RealPath(string(input))
	if err != nil {
		return nil, err
	}
	file, err := os.Open(real)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, &ReadPathKindError{Resource: resource, Expected: "a file"}
	}
	size := info.Size()

	first, err := readAlloc(file, readFirstChunkSize(size))
	if err != nil {
		return nil, err
	}

	if mime := readImageMime(first); mime != "" {
		return readImageContent(file, first, size, resource, real, mime)
	}
	if readStartsWith(first, []byte{0x25, 0x50, 0x44, 0x46}) || readExtensionBinary(resource) {
		return nil, &ReadBinaryFileError{Resource: resource}
	}

	if size > ReadMaxReadBytes || page.Offset != nil || page.Limit != nil {
		return readPagedText(file, first, resource, real, page)
	}
	return readWholeText(file, first, resource, real)
}

// readFirstChunkSize bounds the initial read used to sniff the content.
func readFirstChunkSize(size int64) int64 {
	const defaultSize = int64(64 * 1024)
	switch {
	case size == 0:
		return 4 * 1024
	case size < defaultSize:
		return size
	default:
		return defaultSize
	}
}

// readImageContent reads a recognized image as base64, bounded by the media
// ingest limit.
func readImageContent(file *os.File, first []byte, size int64, resource, real, mime string) (core.FileSystemContent, error) {
	if size > ReadMaxMediaIngestBytes {
		return core.FileSystemContent{}, &ReadMediaIngestLimitError{Resource: resource, MaximumBytes: ReadMaxMediaIngestBytes}
	}
	chunks := [][]byte{first}
	total := len(first)
	for total <= ReadMaxMediaIngestBytes {
		remaining := int64(ReadMaxMediaIngestBytes + 1 - total)
		chunkSize := int64(64 * 1024)
		if remaining < chunkSize {
			chunkSize = remaining
		}
		chunk, err := readAlloc(file, chunkSize)
		if err != nil {
			return core.FileSystemContent{}, err
		}
		if chunk == nil {
			break
		}
		chunks = append(chunks, chunk)
		total += len(chunk)
	}
	if total > ReadMaxMediaIngestBytes {
		return core.FileSystemContent{}, &ReadMediaIngestLimitError{Resource: resource, MaximumBytes: ReadMaxMediaIngestBytes}
	}
	var buf bytes.Buffer
	for _, chunk := range chunks {
		buf.Write(chunk)
	}
	name := filepath.Base(real)
	return core.FileSystemContent{
		URI:      readFileURL(real),
		Name:     &name,
		Content:  base64.StdEncoding.EncodeToString(buf.Bytes()),
		Encoding: "base64",
		MIME:     mime,
	}, nil
}

// readWholeText reads an unpaged text file, rejecting binary or malformed
// content.
func readWholeText(file *os.File, first []byte, resource, real string) (core.FileSystemContent, error) {
	if readBinary(resource, first) {
		return core.FileSystemContent{}, &ReadBinaryFileError{Resource: resource}
	}
	rest, err := io.ReadAll(file)
	if err != nil {
		return core.FileSystemContent{}, err
	}
	if bytes.IndexByte(rest, 0) >= 0 {
		return core.FileSystemContent{}, &ReadBinaryFileError{Resource: resource}
	}
	all := make([]byte, 0, len(first)+len(rest))
	all = append(all, first...)
	all = append(all, rest...)
	if !utf8.Valid(all) {
		return core.FileSystemContent{}, &ReadMalformedUtf8Error{Resource: resource}
	}
	name := filepath.Base(real)
	return core.FileSystemContent{
		URI:      readFileURL(real),
		Name:     &name,
		Content:  strings.TrimPrefix(string(all), "\uFEFF"),
		Encoding: "utf8",
		MIME:     core.MimeType(real),
	}, nil
}

// readPagedText reads a bounded page of lines from a large text file.
// textPageReader accumulates lines from UTF-8 chunks under the read page limits.
type textPageReader struct {
	resource  string
	offset    int
	limit     int
	decoder   *readUTF8Stream
	lines     []string
	pending   string
	discard   bool
	line      int
	byteCount int
	next      *int
}

func newTextPageReader(resource string, page ReadPageInput) *textPageReader {
	offset := 1
	if page.Offset != nil {
		offset = *page.Offset
	}
	limit := ReadMaxReadLines
	if page.Limit != nil && *page.Limit < limit {
		limit = *page.Limit
	}
	return &textPageReader{
		resource: resource,
		offset:   offset,
		limit:    limit,
		decoder:  &readUTF8Stream{resource: resource},
		line:     1,
	}
}

func (r *textPageReader) setNext() {
	value := r.line
	r.next = &value
}

func (r *textPageReader) appendLine(value string) bool {
	if r.line < r.offset {
		r.line++
		return true
	}
	if len(r.lines) >= r.limit || r.byteCount >= ReadMaxReadBytes {
		r.setNext()
		return false
	}
	text := value
	if readCharLength(value) > readMaxLineLength {
		text = readCharSlice(value, readMaxLineLength) + readMaxLineSuffix
	}
	size := len([]byte(text))
	if len(r.lines) > 0 {
		size++
	}
	if r.byteCount+size > ReadMaxReadBytes {
		r.setNext()
		return false
	}
	r.lines = append(r.lines, text)
	r.byteCount += size
	r.line++
	return true
}

func (r *textPageReader) consume(input string) bool {
	text := input
	for {
		index := strings.IndexByte(text, '\n')
		if index == -1 {
			if !r.discard {
				r.pending += text
				if readCharLength(r.pending) > readMaxLineLength {
					r.pending = readCharSlice(r.pending, readMaxLineLength+1)
					r.discard = true
				}
			}
			break
		}
		current := r.pending
		if !r.discard {
			current += text[:index]
		}
		r.pending = ""
		r.discard = false
		text = text[index+1:]
		if strings.HasSuffix(current, "\r") {
			current = current[:len(current)-1]
		}
		if !r.appendLine(current) {
			return false
		}
	}
	return true
}

func (r *textPageReader) consumeChunk(chunk []byte) (bool, error) {
	start := 0
	for start < len(chunk) {
		if len(r.lines) >= r.limit || r.byteCount >= ReadMaxReadBytes {
			r.setNext()
			return false, nil
		}
		newline := bytes.IndexByte(chunk[start:], '\n')
		end := len(chunk)
		if newline != -1 {
			end = start + newline + 1
		}
		segment := chunk[start:end]
		if readBinary(r.resource, segment) {
			return false, &ReadBinaryFileError{Resource: r.resource}
		}
		decoded, err := r.decoder.Decode(segment)
		if err != nil {
			return false, err
		}
		if !r.consume(decoded) {
			return false, nil
		}
		start = end
	}
	return true, nil
}

func (r *textPageReader) run(file *os.File, first []byte) error {
	ok, err := r.consumeChunk(first)
	if err != nil {
		return err
	}
	for done := !ok; !done; {
		chunk, err := readAlloc(file, 64*1024)
		if err != nil {
			return err
		}
		if chunk == nil {
			break
		}
		ok, err := r.consumeChunk(chunk)
		if err != nil {
			return err
		}
		done = !ok
	}
	// When the reader stopped early at the limit there is no tail to flush.
	if r.next != nil {
		return nil
	}
	tail, err := r.decoder.Flush()
	if err != nil {
		return err
	}
	if !r.discard {
		r.pending += tail
	}
	if r.pending != "" {
		value := r.pending
		if strings.HasSuffix(value, "\r") {
			value = value[:len(value)-1]
		}
		r.appendLine(value)
	}
	return nil
}

func (r *textPageReader) result(real string) (ReadTextPage, error) {
	if len(r.lines) == 0 && r.offset != 1 {
		return ReadTextPage{}, &ReadOffsetOutOfRangeError{Offset: r.offset}
	}
	return ReadTextPage{
		Type:      "text-page",
		Content:   strings.Join(r.lines, "\n"),
		MIME:      core.MimeType(real),
		Offset:    r.offset,
		Truncated: r.next != nil,
		Next:      r.next,
	}, nil
}

func readPagedText(file *os.File, first []byte, resource, real string, page ReadPageInput) (ReadTextPage, error) {
	reader := newTextPageReader(resource, page)
	if err := reader.run(file, first); err != nil {
		return ReadTextPage{}, err
	}
	return reader.result(real)
}

func listToolRead(input schema.AbsolutePath, page ReadPageInput, locale language.Tag) (*ReadListPage, error) {
	real, err := core.RealPath(string(input))
	if err != nil {
		return nil, err
	}
	items, err := core.ReadDir(real)
	if err != nil {
		return nil, err
	}

	offset := 1
	if page.Offset != nil {
		offset = *page.Offset
	}
	limit := ReadMaxReadLines
	if page.Limit != nil && *page.Limit < limit {
		limit = *page.Limit
	}

	visible := readListEntries(real, items)
	sortListEntries(visible, locale)

	start, end := readPageBounds(offset, limit, len(visible))
	selected := visible[start:end]
	if selected == nil {
		selected = []core.FileSystemEntry{}
	}
	truncated := end < len(visible)

	result := &ReadListPage{Entries: selected, Truncated: truncated}
	if truncated {
		next := offset + len(selected)
		result.Next = &next
	}
	return result, nil
}

// readListEntries resolves each directory entry, dropping those outside the
// directory or of unknown kind.
func readListEntries(real string, items []core.DirEntry) []core.FileSystemEntry {
	var visible []core.FileSystemEntry
	for _, item := range items {
		absolute := filepath.Join(real, item.Name)
		target, err := core.RealPath(absolute)
		if err != nil || !core.Contains(real, target) {
			continue
		}
		info, err := core.Stat(target)
		if err != nil {
			continue
		}
		typ := core.FileKind("")
		switch info.Type {
		case core.FileKindDirectory:
			typ = core.FileKindDirectory
		case core.FileKindFile:
			typ = core.FileKindFile
		default:
			continue
		}
		suffix := ""
		if typ == core.FileKindDirectory {
			suffix = string(filepath.Separator)
		}
		visible = append(visible, core.FileSystemEntry{Path: item.Name + suffix, Type: typ})
	}
	return visible
}

// sortListEntries orders directories first, then paths by locale.
func sortListEntries(visible []core.FileSystemEntry, locale language.Tag) {
	collator := collate.New(locale)
	sort.SliceStable(visible, func(i, j int) bool {
		if visible[i].Type == visible[j].Type {
			return collator.CompareString(visible[i].Path, visible[j].Path) < 0
		}
		return visible[i].Type == core.FileKindDirectory
	})
}

// readPageBounds clamps a 1-based offset and limit to the visible entries.
func readPageBounds(offset, limit, total int) (int, int) {
	start := offset - 1
	if start < 0 {
		start = 0
	}
	if start > total {
		start = total
	}
	end := start + limit
	if end > total {
		end = total
	}
	return start, end
}
