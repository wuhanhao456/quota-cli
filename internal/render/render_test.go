package render

import (
	"encoding/json"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func loadSample(t *testing.T) Output {
	t.Helper()
	b, err := os.ReadFile("../../examples/sample.json")
	if err != nil {
		t.Fatal(err)
	}
	var out Output
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestPNG(t *testing.T) {
	path := filepath.Join(t.TempDir(), "q.png")
	if err := PNG(path, loadSample(t), DefaultPersona); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != int(imgWidth*scale) {
		t.Fatalf("width = %d", img.Bounds().Dx())
	}
}

func TestPNGWithoutAvatar(t *testing.T) {
	path := filepath.Join(t.TempDir(), "q.png")
	if err := PNG(path, loadSample(t), Persona{Avatar: "none", Name: "WU"}); err != nil {
		t.Fatal(err)
	}
}

func TestPNGMissingAvatar(t *testing.T) {
	path := filepath.Join(t.TempDir(), "q.png")
	if err := PNG(path, loadSample(t), Persona{Avatar: filepath.Join(t.TempDir(), "nope.png")}); err == nil {
		t.Fatal("expected an error for a missing avatar file")
	}
}

func TestText(t *testing.T) {
	var sb strings.Builder
	Text(&sb, loadSample(t))
	for _, want := range []string{"Claude  [Max]", "$49.00 left", "not configured: set OLLAMA_API_KEY"} {
		if !strings.Contains(sb.String(), want) {
			t.Errorf("missing %q in:\n%s", want, sb.String())
		}
	}
}

func TestAutoTheme(t *testing.T) {
	at := func(h int) time.Time { return time.Date(2026, 11, 1, h, 0, 0, 0, time.Local) }
	for h, dark := range map[int]bool{5: true, 6: false, 12: false, 17: false, 18: true, 23: true} {
		p, err := themeFor("auto", at(h))
		if err != nil || p.dark != dark {
			t.Errorf("auto at %02d:00: dark=%v err=%v, want dark=%v", h, p.dark, err, dark)
		}
	}
	if _, err := themeFor("sepia", at(12)); err == nil {
		t.Error("expected an error for an unknown theme")
	}
}
