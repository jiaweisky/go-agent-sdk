package core

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	"image/png"
	"math"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

// ImageResizerUnavailableError mirrors Image.ResizerUnavailableError from
// packages/core/src/image.ts. The read tool treats it as "keep the original
// content" rather than a failure.
type ImageResizerUnavailableError struct{}

func (e *ImageResizerUnavailableError) Error() string { return "image resizer unavailable" }

type ImageDecodeError struct {
	Resource string
}

func (e *ImageDecodeError) Error() string {
	return fmt.Sprintf("Image could not be decoded: %s", e.Resource)
}

type ImageSizeError struct {
	Resource  string
	Width     int
	Height    int
	Bytes     int
	MaxWidth  int
	MaxHeight int
	MaxBytes  int
}

func (e *ImageSizeError) Error() string {
	return fmt.Sprintf(
		"Image %s is %dx%d with base64 size %d, exceeding configured limits %dx%d/%d bytes",
		e.Resource, e.Width, e.Height, e.Bytes, e.MaxWidth, e.MaxHeight, e.MaxBytes,
	)
}

// ImageConfig mirrors the resolved `attachments.image` config in image.ts.
type ImageConfig struct {
	AutoResize     bool
	MaxWidth       int
	MaxHeight      int
	MaxBase64Bytes int
}

func DefaultImageConfig() ImageConfig {
	return ImageConfig{
		AutoResize:     true,
		MaxWidth:       2000,
		MaxHeight:      2000,
		MaxBase64Bytes: 5 * 1024 * 1024,
	}
}

type Image interface {
	Normalize(resource string, content FileSystemContent) (FileSystemContent, error)
}

func NewImage(cfg ImageConfig) Image {
	return &imageService{cfg: cfg}
}

type imageService struct {
	cfg ImageConfig
}

var jpegQualities = []int{80, 85, 70, 55, 40}

func (s *imageService) Normalize(resource string, content FileSystemContent) (FileSystemContent, error) {
	raw, err := base64.StdEncoding.DecodeString(content.Content)
	if err != nil {
		return content, &ImageDecodeError{Resource: resource}
	}
	decoded, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return content, &ImageDecodeError{Resource: resource}
	}

	bounds := decoded.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	byteLength := len(content.Content)

	if width <= s.cfg.MaxWidth && height <= s.cfg.MaxHeight && byteLength <= s.cfg.MaxBase64Bytes {
		return content, nil
	}
	if !s.cfg.AutoResize {
		return content, s.sizeError(resource, width, height, byteLength)
	}

	scale := math.Min(1, math.Min(float64(s.cfg.MaxWidth)/float64(width), float64(s.cfg.MaxHeight)/float64(height)))
	for _, size := range imageSizes(width, height, scale) {
		resized := resizeImage(decoded, size[0], size[1])
		candidates := []struct {
			mime string
			data []byte
		}{{"image/png", encodePNG(resized)}}
		for _, quality := range jpegQualities {
			candidates = append(candidates, struct {
				mime string
				data []byte
			}{"image/jpeg", encodeJPEG(resized, quality)})
		}
		for _, candidate := range candidates {
			encoded := base64.StdEncoding.EncodeToString(candidate.data)
			if len(encoded) <= s.cfg.MaxBase64Bytes {
				content.Content = encoded
				content.Encoding = "base64"
				content.MIME = candidate.mime
				return content, nil
			}
		}
	}

	return content, s.sizeError(resource, width, height, byteLength)
}

func (s *imageService) sizeError(resource string, width, height, bytes int) error {
	return &ImageSizeError{
		Resource:  resource,
		Width:     width,
		Height:    height,
		Bytes:     bytes,
		MaxWidth:  s.cfg.MaxWidth,
		MaxHeight: s.cfg.MaxHeight,
		MaxBytes:  s.cfg.MaxBase64Bytes,
	}
}

func imageSizes(width, height int, scale float64) [][2]int {
	var sizes [][2]int
	for i := 0; i < 32; i++ {
		var next [2]int
		if len(sizes) == 0 {
			next = [2]int{
				max(1, int(math.Round(float64(width)*scale))),
				max(1, int(math.Round(float64(height)*scale))),
			}
		} else {
			previous := sizes[len(sizes)-1]
			nextWidth := 1
			if previous[0] != 1 {
				nextWidth = max(1, int(math.Floor(float64(previous[0])*0.75)))
			}
			nextHeight := 1
			if previous[1] != 1 {
				nextHeight = max(1, int(math.Floor(float64(previous[1])*0.75)))
			}
			next = [2]int{nextWidth, nextHeight}
		}
		duplicate := false
		for _, size := range sizes {
			if size == next {
				duplicate = true
				break
			}
		}
		if !duplicate {
			sizes = append(sizes, next)
		}
	}
	return sizes
}

func resizeImage(src image.Image, width, height int) image.Image {
	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Over, nil)
	return dst
}

func encodePNG(img image.Image) []byte {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil
	}
	return buf.Bytes()
}

func encodeJPEG(img image.Image, quality int) []byte {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
		return nil
	}
	return buf.Bytes()
}
