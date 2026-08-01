package tools

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTempImage writes a fake PNG file (valid enough for the read tool —
// it only inspects the extension and bytes, no decoding) and returns its path.
func writeTempImage(t *testing.T, dir, name string, content []byte) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, content, 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestReadToolImageDataURL verifies reading an image file returns a base64
// data URL when the read tool is in ImageModeDataURL (multimodal main model).
func TestReadToolImageDataURL(t *testing.T) {
	dir := t.TempDir()
	raw := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x01}
	path := writeTempImage(t, dir, "shot.png", raw)

	r := &ReadTool{ImageMode: ImageModeDataURL}
	res := r.Execute(map[string]interface{}{"path": path})
	if !res.Success {
		t.Fatalf("read failed: %s", res.Error)
	}
	want := "data:image/png;base64," + base64.StdEncoding.EncodeToString(raw)
	if res.Output != want {
		t.Errorf("output mismatch:\n got %q\nwant %q", res.Output, want)
	}
}

// TestReadToolImageHint verifies the default (hint) mode returns a pointer
// to the vision tool instead of a base64 blob — text main models can't read
// base64, so the raw image bytes would be token garbage.
func TestReadToolImageHint(t *testing.T) {
	dir := t.TempDir()
	path := writeTempImage(t, dir, "shot.png", []byte{0x89, 0x50, 0x4E, 0x47, 0x01})

	r := &ReadTool{} // zero value → hint mode
	res := r.Execute(map[string]interface{}{"path": path})
	if !res.Success {
		t.Fatalf("read failed: %s", res.Error)
	}
	if !strings.Contains(res.Output, "vision tool") {
		t.Errorf("expected vision-tool hint, got: %q", res.Output)
	}
	if strings.Contains(res.Output, "base64,") {
		t.Errorf("hint mode must not leak base64 data: %q", res.Output)
	}
}

// TestReadToolTextUnchanged verifies regular text files still read as before.
func TestReadToolTextUnchanged(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(path, []byte("line1\nline2\n"), 0644); err != nil {
		t.Fatal(err)
	}
	r := &ReadTool{}
	res := r.Execute(map[string]interface{}{"path": path})
	if !res.Success || res.Output != "line1\nline2\n" {
		t.Errorf("text read changed: %#v", res)
	}
}

// TestReadToolImageMimeTypes verifies the MIME mapping for common extensions.
func TestReadToolImageMimeTypes(t *testing.T) {
	cases := map[string]string{
		"a.png":  "image/png",
		"a.jpg":  "image/jpeg",
		"a.jpeg": "image/jpeg",
		"a.gif":  "image/gif",
		"a.webp": "image/webp",
		"a.bmp":  "image/bmp",
		"a.txt":  "",
		"a.go":   "",
		"a":      "",
	}
	for name, want := range cases {
		if got := ImageMime(name); got != want {
			t.Errorf("ImageMime(%q) = %q, want %q", name, got, want)
		}
	}
}

// TestReadToolImageTooLarge verifies oversized images are rejected.
func TestReadToolImageTooLarge(t *testing.T) {
	dir := t.TempDir()
	big := make([]byte, 5*1024*1024+1)
	path := writeTempImage(t, dir, "big.png", big)

	r := &ReadTool{}
	res := r.Execute(map[string]interface{}{"path": path})
	if res.Success {
		t.Error("expected oversized image to fail")
	}
	if !strings.Contains(res.Error, "too large") {
		t.Errorf("unexpected error: %q", res.Error)
	}
}
