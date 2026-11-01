package render

import (
	"os"
	"testing"
	"time"
)

// TestWriteSamples regenerates the README images:
//
//	QUOTA_WRITE_SAMPLES=1 go test ./internal/render -run TestWriteSamples
func TestWriteSamples(t *testing.T) {
	if os.Getenv("QUOTA_WRITE_SAMPLES") == "" {
		t.Skip("set QUOTA_WRITE_SAMPLES=1 to regenerate examples/*.png")
	}
	out := loadSample(t)
	now := time.Date(2026, 10, 20, 7, 47, 0, 0, time.UTC)
	for theme, path := range map[string]string{"day": "../../examples/sample.png", "night": "../../examples/sample-night.png"} {
		p := DefaultPersona
		p.Theme = theme
		if err := renderPNG(path, out, p, now); err != nil {
			t.Fatal(err)
		}
	}
}
