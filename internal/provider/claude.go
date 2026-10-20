package provider

import (
	"context"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Claude subscription limits come from the OAuth usage endpoint that Claude Code itself uses.
// The token is read from Claude Code's credential file and is never refreshed here, because
// refreshing would rotate the token Claude Code depends on.

var claudeUsageURL = "https://api.anthropic.com/api/oauth/usage"

type claudeCredentials struct {
	OAuth struct {
		AccessToken      string  `json:"accessToken"`
		ExpiresAt        float64 `json:"expiresAt"`
		SubscriptionType string  `json:"subscriptionType"`
		RateLimitTier    string  `json:"rateLimitTier"`
	} `json:"claudeAiOauth"`
}

func claudeCredentialPath() string {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return filepath.Join(dir, ".credentials.json")
	}
	return joinHome(".claude", ".credentials.json")
}

var claudeWindows = []struct{ key, id, label string }{
	{"five_hour", "session", "Session (5h)"},
	{"seven_day", "weekly", "Weekly (all models)"},
	{"seven_day_opus", "weekly_opus", "Weekly (Opus)"},
	{"seven_day_sonnet", "weekly_sonnet", "Weekly (Sonnet)"},
	{"seven_day_oauth_apps", "weekly_oauth_apps", "Weekly (OAuth apps)"},
}

func fetchClaude(ctx context.Context, c *http.Client) (*Report, error) {
	r := &Report{Source: "oauth"}
	token := firstEnv("CLAUDE_OAUTH_TOKEN")
	if token == "" {
		path := claudeCredentialPath()
		var creds claudeCredentials
		if err := readJSONFile(path, &creds); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return r, unconfigured("no Claude Code login found at %s (run `claude` and log in; on macOS the token is in Keychain, set CLAUDE_OAUTH_TOKEN)", path)
			}
			return r, err
		}
		token = creds.OAuth.AccessToken
		r.Plan = planName(creds.OAuth.SubscriptionType)
		if token == "" {
			return r, unconfigured("%s has no claudeAiOauth.accessToken", path)
		}
		if exp := creds.OAuth.ExpiresAt; exp > 0 && time.Now().After(time.UnixMilli(int64(exp))) {
			return r, errors.New("Claude Code token expired; run any `claude` command to refresh it")
		}
	}

	var raw map[string]any
	err := getJSON(ctx, c, claudeUsageURL, map[string]string{
		"Authorization":  "Bearer " + token,
		"anthropic-beta": "oauth-2025-04-20",
	}, &raw)
	if err != nil {
		return r, err
	}
	for _, w := range claudeWindows {
		m, ok := raw[w.key].(map[string]any)
		if !ok {
			continue
		}
		u, ok := num(m["utilization"])
		if !ok {
			continue
		}
		r.Windows = append(r.Windows, Window{
			ID: w.id, Label: w.label, UsedPercent: clampPct(u), Unit: "percent", ResetsAt: timeOf(m["resets_at"]),
		})
	}
	if extra, ok := raw["extra_usage"].(map[string]any); ok && extra["is_enabled"] == true {
		limit, okL := num(extra["monthly_limit"])
		used, okU := num(extra["used_credits"])
		if okL && okU && limit > 0 {
			// The API reports extra usage in cents.
			limit, used = limit/100, used/100
			r.Windows = append(r.Windows, Window{
				ID: "extra_usage", Label: "Extra usage (monthly)", UsedPercent: clampPct(used / limit * 100),
				Used: ptr(used), Limit: ptr(limit), Remaining: ptr(max(0, limit-used)), Unit: "usd",
			})
		}
	}
	return r, nil
}

func planName(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
