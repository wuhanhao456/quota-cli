package provider

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
)

// ChatGPT plan limits are exposed only for Codex, through the endpoint the Codex CLI uses.
// The token comes from the Codex CLI login file and is never refreshed here.

var codexUsageURL = "https://chatgpt.com/backend-api/wham/usage"

type codexAuth struct {
	Tokens struct {
		AccessToken string `json:"access_token"`
		AccountID   string `json:"account_id"`
	} `json:"tokens"`
}

type codexWindow struct {
	UsedPercent        float64 `json:"used_percent"`
	ResetAt            any     `json:"reset_at"`
	LimitWindowSeconds float64 `json:"limit_window_seconds"`
}

type codexRateLimit struct {
	PrimaryWindow   *codexWindow `json:"primary_window"`
	SecondaryWindow *codexWindow `json:"secondary_window"`
}

type codexUsage struct {
	Email     string          `json:"email"`
	PlanType  string          `json:"plan_type"`
	RateLimit *codexRateLimit `json:"rate_limit"`
	Credits   *struct {
		HasCredits bool `json:"has_credits"`
		Unlimited  bool `json:"unlimited"`
		Balance    any  `json:"balance"`
	} `json:"credits"`
	AdditionalRateLimits []struct {
		LimitName string          `json:"limit_name"`
		RateLimit *codexRateLimit `json:"rate_limit"`
	} `json:"additional_rate_limits"`
}

func codexAuthPath() string {
	if dir := os.Getenv("CODEX_HOME"); dir != "" {
		return filepath.Join(dir, "auth.json")
	}
	return joinHome(".codex", "auth.json")
}

func fetchCodex(ctx context.Context, c *http.Client) (*Report, error) {
	r := &Report{Source: "codex-oauth"}
	path := codexAuthPath()
	var auth codexAuth
	if err := readJSONFile(path, &auth); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return r, unconfigured("no Codex CLI login found at %s (run `codex login` with your ChatGPT account)", path)
		}
		return r, err
	}
	if auth.Tokens.AccessToken == "" {
		return r, unconfigured("%s has no ChatGPT tokens (logged in with an API key instead of ChatGPT?)", path)
	}
	headers := map[string]string{"Authorization": "Bearer " + auth.Tokens.AccessToken}
	if auth.Tokens.AccountID != "" {
		headers["ChatGPT-Account-Id"] = auth.Tokens.AccountID
	}
	var u codexUsage
	if err := getJSON(ctx, c, codexUsageURL, headers, &u); err != nil {
		var he *httpError
		if errors.As(err, &he) && he.Status == 401 {
			return r, fmt.Errorf("%w; run any `codex` command to refresh the login", err)
		}
		return r, err
	}
	r.Plan, r.Account = planName(u.PlanType), u.Email
	r.Windows = append(r.Windows, codexWindows("", "", u.RateLimit)...)
	for _, extra := range u.AdditionalRateLimits {
		r.Windows = append(r.Windows, codexWindows(extra.LimitName+" ", slug(extra.LimitName)+"_", extra.RateLimit)...)
	}
	if cr := u.Credits; cr != nil && (cr.HasCredits || cr.Unlimited) {
		v := "unlimited"
		if !cr.Unlimited {
			if b, ok := num(cr.Balance); ok {
				v = fmt.Sprintf("%.2f", b)
			}
		}
		r.Details = append(r.Details, Detail{Label: "Credits", Value: v})
	}
	return r, nil
}

func codexWindows(labelPrefix, idPrefix string, rl *codexRateLimit) []Window {
	if rl == nil {
		return nil
	}
	var out []Window
	for i, w := range []*codexWindow{rl.PrimaryWindow, rl.SecondaryWindow} {
		if w == nil {
			continue
		}
		id, label := "session", "Session (5h)"
		if i == 1 {
			id, label = "weekly", "Weekly"
		}
		if s := w.LimitWindowSeconds; s > 0 {
			label = labelForSeconds(s)
		}
		out = append(out, Window{
			ID: idPrefix + id, Label: labelPrefix + label, UsedPercent: clampPct(w.UsedPercent),
			Unit: "percent", ResetsAt: timeOf(w.ResetAt),
		})
	}
	return out
}

func labelForSeconds(s float64) string {
	switch h := s / 3600; {
	case h == 5:
		return "Session (5h)"
	case h == 24*7:
		return "Weekly"
	case h < 24:
		return fmt.Sprintf("Window (%gh)", h)
	case h == 24*30 || h == 24*31:
		return "Monthly"
	default:
		return fmt.Sprintf("Window (%gd)", h/24)
	}
}

func slug(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			out = append(out, r)
		case r >= 'A' && r <= 'Z':
			out = append(out, r+32)
		default:
			if len(out) > 0 && out[len(out)-1] != '_' {
				out = append(out, '_')
			}
		}
	}
	return string(out)
}
