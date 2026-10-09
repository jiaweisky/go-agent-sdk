package skill

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	mathrand "math/rand/v2"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

const (
	skillConcurrency = 4
	fileConcurrency  = 8
	// skillVersionFile records the source version of a downloaded skill so a
	// later refresh can skip unchanged content.
	skillVersionFile = ".skill-version"
)

// FileSystem is the narrow filesystem surface SkillDiscovery needs. It is
// injected so this package does not import core (which imports this package).
type FileSystem interface {
	Exists(path string) (bool, error)
	ReadFileStringSafe(path string) (string, bool)
	WriteWithDirs(path string, content []byte, mode ...os.FileMode) error
	WriteFileString(path string, content string, mode ...os.FileMode) error
	Rename(oldPath, newPath string) error
	RemoveAll(path string) error
	Contains(parent, child string) bool
}

type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// IndexSkill is one skill entry of a remote index.
type IndexSkill struct {
	Name    string   `json:"name"`
	Version *string  `json:"version,omitempty"`
	Files   []string `json:"files"`
}

// Index is a remote skill index: its skills and their root.
type Index struct {
	Skills []IndexSkill `json:"skills"`
}

type Discovery struct {
	fs       FileSystem
	cacheDir string
	client   HTTPClient
}

func NewDiscovery(fs FileSystem, cacheDir string, client HTTPClient) *Discovery {
	if client == nil {
		client = http.DefaultClient
	}
	return &Discovery{fs: fs, cacheDir: cacheDir, client: client}
}

// Pull fetches a skill index and downloads the referenced skill directories,
// returning the ones that are present.
func (d *Discovery) Pull(rawURL string) ([]schema.AbsolutePath, error) {
	base := rawURL
	if !strings.HasSuffix(base, "/") {
		base += "/"
	}
	source, err := url.Parse(base)
	if err != nil {
		return nil, err
	}
	index := source.ResolveReference(&url.URL{Path: "index.json"}).String()

	data, ok := d.fetchIndex(index)
	if !ok {
		return nil, nil
	}

	sourceRoot := filepath.Join(d.cacheDir, "skills", discoveryHash(base))
	plans := d.plan(sourceRoot, source, data)

	results := make([][]schema.AbsolutePath, len(plans))
	sem := make(chan struct{}, skillConcurrency)
	var wg sync.WaitGroup
	for i, plan := range plans {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, plan skillPlan) {
			defer wg.Done()
			defer func() { <-sem }()
			results[i] = d.process(plan)
		}(i, plan)
	}
	wg.Wait()

	var directories []schema.AbsolutePath
	for _, result := range results {
		directories = append(directories, result...)
	}
	return directories, nil
}

func (d *Discovery) fetchIndex(index string) (Index, bool) {
	request, err := http.NewRequest(http.MethodGet, index, nil)
	if err != nil {
		return Index{}, false
	}
	request.Header.Set("Accept", "application/json")
	response, err := d.execute(request)
	if err != nil {
		return Index{}, false
	}
	defer response.Body.Close()
	var data Index
	if err := json.NewDecoder(response.Body).Decode(&data); err != nil {
		return Index{}, false
	}
	return data, true
}

type skillFile struct {
	url         string
	destination string
	file        string
}

type skillPlan struct {
	skill       IndexSkill
	root        string
	versionFile string
	files       []skillFile
}

func (d *Discovery) plan(sourceRoot string, source *url.URL, data Index) []skillPlan {
	var plans []skillPlan
	for _, entry := range data.Skills {
		if !isSafeSegment(entry.Name) {
			continue
		}
		if !slices.Contains(entry.Files, "SKILL.md") && !slices.Contains(entry.Files, entry.Name+".md") {
			continue
		}
		root := filepath.Join(sourceRoot, entry.Name)
		if !d.fs.Contains(sourceRoot, root) || root == sourceRoot {
			continue
		}
		skillURL := source.ResolveReference(&url.URL{Path: entry.Name + "/"})
		versionFile := filepath.Join(root, skillVersionFile)

		files := make([]skillFile, 0, len(entry.Files))
		invalid := false
		for _, file := range entry.Files {
			if !isSafeRelativePath(file) {
				invalid = true
				break
			}
			reference := (&url.URL{Path: file}).EscapedPath()
			resource, err := skillURL.Parse(reference)
			if err != nil || resource.Scheme != source.Scheme || resource.Host != source.Host {
				invalid = true
				break
			}
			destination := filepath.Join(root, filepath.FromSlash(file))
			if !d.fs.Contains(root, destination) || destination == root {
				invalid = true
				break
			}
			files = append(files, skillFile{url: resource.String(), destination: destination, file: file})
		}
		if invalid {
			continue
		}
		plans = append(plans, skillPlan{skill: entry, root: root, versionFile: versionFile, files: files})
	}
	return plans
}

func (d *Discovery) process(plan skillPlan) []schema.AbsolutePath {
	version := plan.skill.Version
	var current *string
	if version != nil {
		if value, ok := d.fs.ReadFileStringSafe(plan.versionFile); ok {
			current = &value
		}
	}
	if version == nil || (current != nil && *current == *version) {
		d.downloadAll(plan.files, func(file skillFile) string { return file.destination })
	} else {
		d.refresh(plan, *version)
	}

	if d.fileExists(filepath.Join(plan.root, "SKILL.md")) ||
		d.fileExists(filepath.Join(plan.root, plan.skill.Name+".md")) {
		return []schema.AbsolutePath{schema.AbsolutePath(plan.root)}
	}
	return nil
}

func (d *Discovery) refresh(plan skillPlan, version string) {
	token := randomUUID()
	staging := plan.root + ".tmp-" + token
	backup := plan.root + ".old-" + token
	defer d.fs.RemoveAll(staging)

	downloaded := d.downloadAll(plan.files, func(file skillFile) string {
		return filepath.Join(staging, filepath.FromSlash(file.file))
	})
	for _, ok := range downloaded {
		if !ok {
			return
		}
	}
	if !d.fileExists(filepath.Join(staging, "SKILL.md")) &&
		!d.fileExists(filepath.Join(staging, plan.skill.Name+".md")) {
		return
	}
	if err := d.fs.WriteFileString(filepath.Join(staging, skillVersionFile), version); err != nil {
		return
	}

	cached, err := d.fs.Exists(plan.root)
	if err != nil {
		cached = false
	}
	if cached {
		if err := d.fs.Rename(plan.root, backup); err != nil {
			return
		}
	}
	if err := d.fs.Rename(staging, plan.root); err != nil {
		if cached {
			d.fs.Rename(backup, plan.root)
		}
		return
	}
	if cached {
		d.fs.RemoveAll(backup)
	}
}

func (d *Discovery) downloadAll(files []skillFile, destination func(skillFile) string) []bool {
	results := make([]bool, len(files))
	sem := make(chan struct{}, fileConcurrency)
	var wg sync.WaitGroup
	for i, file := range files {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, file skillFile) {
			defer wg.Done()
			defer func() { <-sem }()
			results[i] = d.download(file.url, destination(file))
		}(i, file)
	}
	wg.Wait()
	return results
}

func (d *Discovery) download(rawURL, destination string) bool {
	if exists, err := d.fs.Exists(destination); err == nil && exists {
		return true
	}
	request, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return false
	}
	response, err := d.execute(request)
	if err != nil {
		return false
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return false
	}
	return d.fs.WriteWithDirs(destination, body) == nil
}

func (d *Discovery) execute(request *http.Request) (*http.Response, error) {
	var lastErr error
	for attempt := 0; attempt <= 2; attempt++ {
		if attempt > 0 {
			backoff := 200 * time.Millisecond * time.Duration(1<<(attempt-1))
			time.Sleep(backoff + time.Duration(mathrand.Int64N(int64(backoff)+1)))
		}
		response, err := d.client.Do(request)
		if err != nil {
			lastErr = err
			continue
		}
		if response.StatusCode >= 200 && response.StatusCode < 300 {
			return response, nil
		}
		lastErr = fmt.Errorf("StatusCodeError: %d", response.StatusCode)
		response.Body.Close()
		if !transientStatus(response.StatusCode) {
			return nil, lastErr
		}
	}
	return nil, lastErr
}

func (d *Discovery) fileExists(path string) bool {
	exists, err := d.fs.Exists(path)
	return err == nil && exists
}

func transientStatus(status int) bool {
	return status == http.StatusRequestTimeout || status == http.StatusTooManyRequests || status >= 500
}

func discoveryHash(value string) string {
	hash := fnv.New64a()
	hash.Write([]byte(value))
	return strconv.FormatUint(hash.Sum64(), 16)
}

func randomUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return hex.EncodeToString(b[0:4]) + "-" + hex.EncodeToString(b[4:6]) + "-" +
		hex.EncodeToString(b[6:8]) + "-" + hex.EncodeToString(b[8:10]) + "-" + hex.EncodeToString(b[10:16])
}

func isSafeSegment(value string) bool {
	return len(value) > 0 && value != "." && value != ".." &&
		!strings.Contains(value, "/") && !strings.Contains(value, "\\") && !strings.Contains(value, "\x00")
}

func isSafeRelativePath(value string) bool {
	if len(value) == 0 {
		return false
	}
	if strings.ContainsAny(value, "\\?#") || strings.Contains(value, "\x00") {
		return false
	}
	if urlCanParse(value) {
		return false
	}
	if path.IsAbs(value) || isAbsoluteWin(value) {
		return false
	}
	for _, segment := range strings.Split(value, "/") {
		decoded, err := url.PathUnescape(segment)
		if err != nil {
			return false
		}
		if len(decoded) == 0 || decoded == "." || decoded == ".." ||
			strings.Contains(decoded, "/") || strings.Contains(decoded, "\\") || strings.Contains(decoded, "\x00") {
			return false
		}
	}
	return true
}

func urlCanParse(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && parsed.IsAbs()
}

func isAbsoluteWin(value string) bool {
	if value == "" {
		return false
	}
	if len(value) >= 3 {
		c := value[0]
		if ((c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')) && value[1] == ':' &&
			(value[2] == '\\' || value[2] == '/') {
			return true
		}
	}
	return value[0] == '\\' || value[0] == '/'
}
