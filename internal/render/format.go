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
		return fmt.Sprintf("已用 $%.2f / $%.2f", *win.Used, *win.Limit)
	}
	return fmt.Sprintf("已用 %g / %g", *win.Used, *win.Limit)
}

func remaining(win provider.Window) string {
	if win.Remaining != nil && win.Unit == "usd" {
		return fmt.Sprintf("剩余 $%.2f", *win.Remaining)
	}
	return fmt.Sprintf("剩余 %.0f%%", 100-win.UsedPercent)
}

// resetText renders "3小时12分后重置 · 10月21日 14:00" in the local time zone.
func resetText(t *time.Time, now time.Time) string {
	if t == nil {
		return ""
	}
	d := t.Sub(now)
	when := t.Local().Format("1月2日 15:04")
	if d <= 0 {
		return "已到重置时间 · " + when
	}
	return duration(d) + "后重置 · " + when
}

func duration(d time.Duration) string {
	m := int(math.Ceil(d.Minutes()))
	days, hours, mins := m/1440, (m%1440)/60, m%60
	switch {
	case days > 0 && hours == 0:
		return fmt.Sprintf("%d天", days)
	case days > 0:
		return fmt.Sprintf("%d天%d小时", days, hours)
	case hours > 0:
		return fmt.Sprintf("%d小时%d分", hours, mins)
	default:
		return fmt.Sprintf("%d分钟", mins)
	}
}
