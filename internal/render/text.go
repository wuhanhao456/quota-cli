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
			fmt.Fprintln(w, "  not configured: "+r.Error)
			continue
		case "error":
			fmt.Fprintln(w, "  error: "+r.Error)
			continue
		}
		if len(r.Windows) == 0 && len(r.Details) == 0 {
			fmt.Fprintln(w, "  no usage data reported")
		}
		for _, win := range r.Windows {
			parts := []string{remaining(win)}
			if a := amount(win); a != "" {
				parts = append(parts, a)
			}
			if rt := resetText(win.ResetsAt, now); rt != "" {
				parts = append(parts, rt)
			}
			fmt.Fprintf(w, "  %-22s %s %5.1f%%  %s\n", win.Label, bar(win.UsedPercent, 20), win.UsedPercent, strings.Join(parts, " · "))
		}
		for _, d := range r.Details {
			fmt.Fprintf(w, "  %-22s %s\n", d.Label, d.Value)
		}
	}
}

func bar(pct float64, width int) string {
	n := int(pct/100*float64(width) + 0.5)
	return strings.Repeat("█", n) + strings.Repeat("░", width-n)
}
