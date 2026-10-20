// Package render turns provider reports into JSON, terminal text and PNG images.
package render

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"time"

	"github.com/wuhanhao456/quota-cli/internal/provider"
)

// Output is the top-level JSON document. Its shape is stable for agents to parse.
type Output struct {
	SchemaVersion int               `json:"schema_version"`
	GeneratedAt   time.Time         `json:"generated_at"`
	Providers     []provider.Report `json:"providers"`
}

func NewOutput(reports []provider.Report) Output {
	return Output{SchemaVersion: 1, GeneratedAt: time.Now().UTC(), Providers: reports}
}

func JSON(w io.Writer, out Output) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

// amount formats a window's used/limit pair, or "" when the window is percent-only.
func amount(win provider.Window) string {
	if win.Used == nil || win.Limit == nil {
		return ""
	}
	if win.Unit == "usd" {
		return fmt.Sprintf("$%.2f / $%.2f", *win.Used, *win.Limit)
	}
	return fmt.Sprintf("%g / %g", *win.Used, *win.Limit)
}

func remaining(win provider.Window) string {
	if win.Remaining != nil && win.Unit == "usd" {
		return fmt.Sprintf("$%.2f left", *win.Remaining)
	}
	return fmt.Sprintf("%.0f%% left", 100-win.UsedPercent)
}

// resetText renders "resets in 3h 12m (Oct 21 14:00)" in the local time zone.
func resetText(t *time.Time, now time.Time) string {
	if t == nil {
		return ""
	}
	d := t.Sub(now)
	when := t.Local().Format("Jan 2 15:04")
	if d <= 0 {
		return "reset due (" + when + ")"
	}
	return "resets in " + duration(d) + " (" + when + ")"
}

func duration(d time.Duration) string {
	m := int(math.Ceil(d.Minutes()))
	days, hours, mins := m/1440, (m%1440)/60, m%60
	switch {
	case days > 0:
		return fmt.Sprintf("%dd %dh", days, hours)
	case hours > 0:
		return fmt.Sprintf("%dh %dm", hours, mins)
	default:
		return fmt.Sprintf("%dm", mins)
	}
}
