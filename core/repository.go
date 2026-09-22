package core

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// RepositoryReference mirrors Repository.Reference (remote or file).
type RepositoryReference struct {
	Host     string
	Path     string
	Segments []string
	Owner    *string
	Repo     string
	Remote   string
	Label    string
	Protocol *string
}

var (
	repositoryGitHubShorthand = regexp.MustCompile(`^github:([^/\s]+)/([^/\s]+)$`)
	repositorySCP             = regexp.MustCompile(`^(?:[^@/\s]+@)?([^:/\s]+):(.+)$`)
	repositoryBranch          = regexp.MustCompile(`^[A-Za-z0-9/_.-]+$`)
	repositoryHostInvalid     = regexp.MustCompile(`[\s/\\]`)
	repositorySegmentInvalid  = regexp.MustCompile(`[\s/\\]`)
)

// RepositoryParse mirrors Repository.parse.
func RepositoryParse(input string) *RepositoryReference {
	cleaned := repositoryNormalizeInput(input)
	if cleaned == "" {
		return nil
	}
	if match := repositoryGitHubShorthand.FindStringSubmatch(cleaned); match != nil {
		return repositoryBuildRemote("github.com", []string{match[1], match[2]}, "", "")
	}
	if !strings.Contains(cleaned, "://") {
		if match := repositorySCP.FindStringSubmatch(cleaned); match != nil {
			return repositoryBuildRemote(match[1], repositoryParts(match[2]), cleaned, "")
		}
		direct := repositoryParts(cleaned)
		if len(direct) >= 2 && repositoryHostLike(direct[0]) {
			return repositoryBuildRemote(direct[0], direct[1:], "", "")
		}
		if len(direct) == 2 {
			return repositoryBuildRemote("github.com", direct, "", "")
		}
	}
	parsed, err := url.Parse(cleaned)
	if err != nil {
		return nil
	}
	if parsed.Scheme == "file" {
		return repositoryBuildFile(parsed, cleaned)
	}
	segments := repositoryParts(parsed.Path)
	remote := cleaned
	if parsed.Host == "github.com" {
		remote = repositoryGitHubRemote(strings.Join(segments, "/"))
	}
	return repositoryBuildRemote(parsed.Host, segments, remote, parsed.Scheme)
}

// RepositoryIsFile mirrors Repository.isFile.
func RepositoryIsFile(reference *RepositoryReference) bool {
	return reference != nil && reference.Protocol != nil && *reference.Protocol == "file:"
}

// RepositoryIsRemote mirrors Repository.isRemote.
func RepositoryIsRemote(reference *RepositoryReference) bool {
	return reference != nil && !RepositoryIsFile(reference)
}

// RepositoryValidateBranch mirrors Repository.validateBranch.
func RepositoryValidateBranch(branch string) error {
	if repositoryBranch.MatchString(branch) && !strings.HasPrefix(branch, "-") && !strings.Contains(branch, "..") {
		return nil
	}
	return fmt.Errorf("Branch must contain only alphanumeric characters, /, _, ., and -, and cannot start with - or contain ..")
}

// RepositoryCachePath mirrors Repository.cachePath.
func RepositoryCachePath(root string, reference *RepositoryReference, branch *string) string {
	parts := append(strings.Split(reference.Host, ":"), reference.Segments...)
	base := filepath.Join(append([]string{root}, parts...)...)
	if branch == nil {
		return base
	}
	return base + "@" + url.PathEscape(*branch)
}

// RepositoryCacheIdentity mirrors Repository.cacheIdentity.
func RepositoryCacheIdentity(reference *RepositoryReference) string {
	return reference.Host + "/" + reference.Path
}

// RepositorySame mirrors Repository.same.
func RepositorySame(left, right *RepositoryReference) bool {
	if left == nil || right == nil {
		return false
	}
	return RepositoryCacheIdentity(left) == RepositoryCacheIdentity(right)
}

func repositoryNormalizeInput(input string) string {
	cleaned := strings.TrimSpace(input)
	cleaned = strings.TrimPrefix(cleaned, "git+")
	if index := strings.Index(cleaned, "#"); index >= 0 {
		cleaned = cleaned[:index]
	}
	return strings.TrimRight(cleaned, "/")
}

func repositoryTrimGitSuffix(input string) string {
	return strings.TrimSuffix(input, ".git")
}

func repositoryParts(input string) []string {
	var result []string
	for _, item := range strings.Split(input, "/") {
		trimmed := repositoryTrimGitSuffix(strings.TrimSpace(item))
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func repositorySafeHost(input string) bool {
	return input != "" && !strings.HasPrefix(input, "-") && !repositoryHostInvalid.MatchString(input)
}

func repositorySafeSegment(input string) bool {
	return input != "." && input != ".." && !strings.Contains(input, ":") && !repositorySegmentInvalid.MatchString(input)
}

func repositoryHostLike(input string) bool {
	return strings.Contains(input, ".") || strings.Contains(input, ":") || input == "localhost"
}

func repositoryWithSlash(input string) string {
	if strings.HasSuffix(input, "/") {
		return input
	}
	return input + "/"
}

func repositoryGitHubRemote(pathname string) string {
	base := os.Getenv("OPENCODE_REPO_CLONE_GITHUB_BASE_URL")
	if base == "" {
		return "https://github.com/" + pathname + ".git"
	}
	reference, err := url.Parse(pathname + ".git")
	if err != nil {
		return "https://github.com/" + pathname + ".git"
	}
	root, err := url.Parse(repositoryWithSlash(base))
	if err != nil {
		return "https://github.com/" + pathname + ".git"
	}
	return root.ResolveReference(reference).String()
}

func repositoryBuildRemote(host string, segments []string, remote, protocol string) *RepositoryReference {
	filtered := make([]string, 0, len(segments))
	for _, segment := range segments {
		trimmed := repositoryTrimGitSuffix(segment)
		if trimmed != "" {
			filtered = append(filtered, trimmed)
		}
	}
	if !repositorySafeHost(host) || len(filtered) == 0 {
		return nil
	}
	for _, segment := range filtered {
		if !repositorySafeSegment(segment) {
			return nil
		}
	}
	repositoryPath := strings.Join(filtered, "/")
	lowerHost := strings.ToLower(host)
	var owner *string
	if len(filtered) == 2 {
		owner = &filtered[0]
	}
	if remote == "" {
		if lowerHost == "github.com" {
			remote = repositoryGitHubRemote(repositoryPath)
		} else {
			remote = "https://" + lowerHost + "/" + repositoryPath + ".git"
		}
	}
	label := lowerHost + "/" + repositoryPath
	if lowerHost == "github.com" && len(filtered) == 2 {
		label = repositoryPath
	}
	reference := &RepositoryReference{
		Host: lowerHost, Path: repositoryPath, Segments: filtered,
		Owner: owner, Repo: filtered[len(filtered)-1], Remote: remote, Label: label,
	}
	if protocol != "" {
		reference.Protocol = &protocol
	}
	return reference
}

func repositoryBuildFile(parsed *url.URL, remote string) *RepositoryReference {
	filePath := filepath.FromSlash(parsed.Path)
	segments := strings.FieldsFunc(filePath, func(r rune) bool { return r == '\\' || r == '/' })
	if len(segments) == 0 {
		return nil
	}
	cleaned := make([]string, len(segments))
	for i, segment := range segments {
		cleaned[i] = strings.TrimSuffix(segment, ":")
	}
	protocol := "file:"
	return &RepositoryReference{
		Host: "file", Path: filePath, Segments: cleaned,
		Repo: repositoryTrimGitSuffix(cleaned[len(cleaned)-1]), Remote: remote, Label: filePath, Protocol: &protocol,
	}
}
