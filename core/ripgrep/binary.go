// Package ripgrep locates a usable ripgrep executable, preferring one already
// on PATH or previously downloaded, and otherwise fetches the pinned release
// for the current platform.
package ripgrep

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/jiaweisky/go-agent-sdk/core"
)

const RipgrepBinaryVersion = "15.1.0"

type platformConfig struct {
	Platform  string
	Extension string
}

var ripgrepBinaryPlatforms = map[string]platformConfig{
	"arm64-darwin": {Platform: "aarch64-apple-darwin", Extension: "tar.gz"},
	"arm64-linux":  {Platform: "aarch64-unknown-linux-gnu", Extension: "tar.gz"},
	"x64-darwin":   {Platform: "x86_64-apple-darwin", Extension: "tar.gz"},
	"x64-linux":    {Platform: "x86_64-unknown-linux-musl", Extension: "tar.gz"},
	"arm64-win32":  {Platform: "aarch64-pc-windows-msvc", Extension: "zip"},
	"ia32-win32":   {Platform: "i686-pc-windows-msvc", Extension: "zip"},
	"x64-win32":    {Platform: "x86_64-pc-windows-msvc", Extension: "zip"},
}

type binaryService struct {
	global core.Global
	client *http.Client
	once   sync.Once
	path   string
	err    error
}

// NewBinary returns a RipgrepBinary that resolves and caches the executable
// path.
func NewBinary(global core.Global) core.RipgrepBinary {
	return &binaryService{
		global: global,
		client: &http.Client{Timeout: 5 * time.Minute},
	}
}

func (b *binaryService) Filepath() (string, error) {
	b.once.Do(func() {
		b.path, b.err = b.resolve()
	})
	return b.path, b.err
}

func ripgrepBinaryExecutable() string {
	if runtime.GOOS == "windows" {
		return "rg.exe"
	}
	return "rg"
}

func ripgrepBinaryPlatformKey() string {
	arch := runtime.GOARCH
	switch arch {
	case "amd64":
		arch = "x64"
	case "386":
		arch = "ia32"
	}
	platform := runtime.GOOS
	if platform == "windows" {
		platform = "win32"
	}
	return arch + "-" + platform
}

func (b *binaryService) resolve() (string, error) {
	executable := ripgrepBinaryExecutable()

	if system, err := exec.LookPath(executable); err == nil && core.IsFile(system) {
		return system, nil
	}

	target := filepath.Join(b.global.Bin, executable)
	if core.IsFile(target) {
		return target, nil
	}

	key := ripgrepBinaryPlatformKey()
	config, ok := ripgrepBinaryPlatforms[key]
	if !ok {
		return "", fmt.Errorf("unsupported platform for ripgrep: %s", key)
	}

	filename := fmt.Sprintf("ripgrep-%s-%s.%s", RipgrepBinaryVersion, config.Platform, config.Extension)
	url := fmt.Sprintf("https://github.com/BurntSushi/ripgrep/releases/download/%s/%s", RipgrepBinaryVersion, filename)
	archive := filepath.Join(b.global.Bin, filename)

	if err := core.EnsureDir(b.global.Bin); err != nil {
		return "", err
	}

	bytes, err := b.download(url)
	if err != nil {
		return "", err
	}
	if len(bytes) == 0 {
		return "", fmt.Errorf("failed to download ripgrep from %s", url)
	}
	if err := core.WriteWithDirs(archive, bytes); err != nil {
		return "", err
	}
	if err := b.extract(archive, config, target); err != nil {
		return "", err
	}
	_ = core.Remove(archive)
	return target, nil
}

func (b *binaryService) download(url string) ([]byte, error) {
	response, err := b.client.Get(url)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("failed to download ripgrep from %s: %s", url, response.Status)
	}
	return io.ReadAll(response.Body)
}

func (b *binaryService) extract(archive string, config platformConfig, target string) error {
	directory, err := os.MkdirTemp(b.global.Bin, "ripgrep-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(directory)

	switch config.Extension {
	case "zip":
		if err := extractZip(archive, directory); err != nil {
			return err
		}
	case "tar.gz":
		if err := extractTarGz(archive, directory); err != nil {
			return err
		}
	}

	extracted := filepath.Join(
		directory,
		fmt.Sprintf("ripgrep-%s-%s", RipgrepBinaryVersion, config.Platform),
		ripgrepBinaryExecutable(),
	)
	if !core.IsFile(extracted) {
		return fmt.Errorf("ripgrep archive did not contain executable: %s", extracted)
	}
	if err := core.CopyFile(extracted, target); err != nil {
		return err
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(target, 0o755); err != nil {
			return err
		}
	}
	return nil
}

func extractTarGz(archive, directory string) error {
	file, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer file.Close()

	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gzipReader.Close()

	reader := tar.NewReader(gzipReader)
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		target := filepath.Join(directory, header.Name)
		if !core.Contains(directory, target) {
			continue
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode))
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, reader); err != nil {
				out.Close()
				return err
			}
			if err := out.Close(); err != nil {
				return err
			}
		}
	}
	return nil
}

func extractZip(archive, directory string) error {
	reader, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer reader.Close()

	for _, file := range reader.File {
		target := filepath.Join(directory, file.Name)
		if !core.Contains(directory, target) {
			continue
		}
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		source, err := file.Open()
		if err != nil {
			return err
		}
		mode := file.Mode()
		if mode == 0 {
			mode = 0o644
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
		if err != nil {
			source.Close()
			return err
		}
		if _, err := io.Copy(out, source); err != nil {
			out.Close()
			source.Close()
			return err
		}
		if err := out.Close(); err != nil {
			source.Close()
			return err
		}
		if err := source.Close(); err != nil {
			return err
		}
	}
	return nil
}
