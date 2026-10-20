package render

import (
	"encoding/json"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	if err := PNG(path, loadSample(t)); err != nil {
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

func TestText(t *testing.T) {
	var sb strings.Builder
	Text(&sb, loadSample(t))
	for _, want := range []string{"Claude  [Max]", "$49.00 left", "not configured: set OLLAMA_API_KEY"} {
		if !strings.Contains(sb.String(), want) {
			t.Errorf("missing %q in:\n%s", want, sb.String())
		}
	}
}
