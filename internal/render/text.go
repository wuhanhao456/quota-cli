package render

import (
	"fmt"
	"io"
	"strings"
	"time"
)

// Text writes a compact human-readable report for terminals.
func Text(w io.Writer, out Output) {
	now := time.Now()
	for i, r := range out.Providers {
		if i > 0 {
			fmt.Fprintln(w)
		}
		title := r.Name
		if r.Plan != "" {
			title += "  [" + r.Plan + "]"
		}
		if r.Account != "" {
			title += "  " + r.Account
		}
		fmt.Fprintln(w, title)
		switch r.Status {
		case "unconfigured":
			fmt.Fprintln(w, "  未配置："+r.Error)
			continue
		case "error":
			fmt.Fprintln(w, "  出错："+r.Error)
			continue
		}
		if len(r.Windows) == 0 && len(r.Details) == 0 {
			fmt.Fprintln(w, "  没有返回用量数据")
		}
		for _, win := range r.Windows {
			parts := []string{remaining(win)}
			if a := amount(win); a != "" {
				parts = append(parts, a)
			}
			if rt := resetText(win.ResetsAt, now); rt != "" {
				parts = append(parts, rt)
			}
			fmt.Fprintf(w, "  %s %s %5.1f%%  %s\n", pad(zh(win.Label), 22), bar(win.UsedPercent, 20), win.UsedPercent, strings.Join(parts, " · "))
		}
		for _, d := range r.Details {
			fmt.Fprintf(w, "  %s %s\n", pad(zh(d.Label), 22), zh(d.Value))
		}
	}
}

func bar(pct float64, width int) string {
	n := int(pct/100*float64(width) + 0.5)
	return strings.Repeat("█", n) + strings.Repeat("░", width-n)
}

// pad right-pads s to n terminal columns, counting CJK characters as two columns.
func pad(s string, n int) string {
	w := 0
	for _, r := range s {
		w++
		if r >= 0x1100 && (r <= 0x115f || r >= 0x2e80) {
			w++
		}
	}
	if w >= n {
		return s
	}
	return s + strings.Repeat(" ", n-w)
}
