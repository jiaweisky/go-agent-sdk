package core

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func encodeTestPNG(t *testing.T, width, height int) FileSystemContent {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 20), G: uint8(y * 20), B: 128, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return FileSystemContent{
		URI:      "file:///test.png",
		Content:  base64.StdEncoding.EncodeToString(buf.Bytes()),
		Encoding: "base64",
		MIME:     "image/png",
	}
}

func TestImage_NormalizeWithinLimits(t *testing.T) {
	content := encodeTestPNG(t, 4, 4)
	out, err := NewImage(DefaultImageConfig()).Normalize("test.png", content)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if out.Content != content.Content || out.MIME != content.MIME {
		t.Fatalf("expected content unchanged")
	}
}

func TestImage_NormalizeResizes(t *testing.T) {
	content := encodeTestPNG(t, 10, 10)
	cfg := DefaultImageConfig()
	cfg.MaxWidth = 5
	cfg.MaxHeight = 5

	out, err := NewImage(cfg).Normalize("test.png", content)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	raw, err := base64.StdEncoding.DecodeString(out.Content)
	if err != nil {
		t.Fatalf("decode base64: %v", err)
	}
	decoded, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("decode image: %v", err)
	}
	if decoded.Bounds().Dx() > 5 || decoded.Bounds().Dy() > 5 {
		t.Fatalf("expected resized image <= 5x5, got %v", decoded.Bounds())
	}
}

func TestImage_NormalizeDecodeError(t *testing.T) {
	content := FileSystemContent{
		URI:      "file:///test.png",
		Content:  base64.StdEncoding.EncodeToString([]byte("not an image")),
		Encoding: "base64",
		MIME:     "image/png",
	}
	if _, err := NewImage(DefaultImageConfig()).Normalize("test.png", content); err == nil {
		t.Fatalf("expected decode error")
	}
}
