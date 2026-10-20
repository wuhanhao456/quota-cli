package provider

import (
	"context"
	"errors"
	"io/fs"
	"net/http"
	"strings"
)

// Command Code exposes credits through the billing routes its `cmd` CLI calls with an API key.

var commandCodeBaseURL = "https://api.commandcode.ai/alpha"

func commandCodeKey() (string, error) {
	if k := firstEnv("COMMANDCODE_API_KEY", "COMMAND_CODE_API_KEY", "CMD_API_KEY"); k != "" {
		return k, nil
	}
	path := joinHome(".commandcode", "auth.json")
	var raw map[string]any
	if err := readJSONFile(path, &raw); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", unconfigured("no Command Code login found at %s (run `cmd login` or set COMMANDCODE_API_KEY)", path)
		}
		return "", err
	}
	for _, k := range []string{"apiKey", "api_key", "key", "token", "accessToken", "access_token"} {
		if s, ok := raw[k].(string); ok && s != "" {
			return s, nil
		}
	}
	// Fall back to any value shaped like a Command Code user key.
	for _, v := range raw {
		if s, ok := v.(string); ok && strings.HasPrefix(s, "user_") {
			return s, nil
		}
	}
	return "", unconfigured("%s has no API key field", path)
}

func fetchCommandCode(ctx context.Context, c *http.Client) (*Report, error) {
	r := &Report{Source: "api-key"}
	key, err := commandCodeKey()
	if err != nil {
		return r, err
	}
	headers := map[string]string{"Authorization": "Bearer " + key}

	var root map[string]any
	if err := getJSON(ctx, c, commandCodeBaseURL+"/billing/credits", headers, &root); err != nil {
		return r, err
	}
	credits, _ := root["credits"].(map[string]any)
	if credits == nil {
		return r, errors.New("unexpected credits response: missing credits object")
	}
	windowLimits, _ := root["windowLimits"].(map[string]any)
	if windowLimits == nil {
		windowLimits, _ = credits["windowLimits"].(map[string]any)
	}
	for _, w := range []struct{ key, id, label string }{
		{"fiveHour", "session", "Session (5h)"},
		{"weekly", "weekly", "Weekly"},
	} {
		m, _ := windowLimits[w.key].(map[string]any)
		limit, ok := num(m["cap"])
		if !ok || limit <= 0 {
			continue
		}
		used, _ := num(m["used"])
		r.Windows = append(r.Windows, Window{
			ID: w.id, Label: w.label, UsedPercent: clampPct(used / limit * 100),
			Used: ptr(used), Limit: ptr(limit), Remaining: ptr(max(0, limit-used)), Unit: "usd",
			ResetsAt: timeOf(m["resetAt"]),
		})
	}

	remaining, hasRemaining := num(credits["monthlyCredits"])
	granted, hasGranted := num(credits["monthlyCreditsGranted"])

	// The subscription lookup is optional enrichment: plan name and billing-period end.
	var sub struct {
		Success bool           `json:"success"`
		Data    map[string]any `json:"data"`
	}
	var periodEnd any
	if err := getJSON(ctx, c, commandCodeBaseURL+"/billing/subscriptions", headers, &sub); err == nil && sub.Success {
		if sub.Data == nil {
			r.Plan = "Free"
		} else {
			if id, _ := sub.Data["planId"].(string); id != "" {
				r.Plan = commandCodePlanName(id)
			}
			periodEnd = sub.Data["currentPeriodEnd"]
		}
	}

	if hasRemaining && hasGranted && granted > 0 {
		used := max(0, granted-remaining)
		r.Windows = append(r.Windows, Window{
			ID: "monthly", Label: "Monthly credits", UsedPercent: clampPct(used / granted * 100),
			Used: ptr(used), Limit: ptr(granted), Remaining: ptr(remaining), Unit: "usd", ResetsAt: timeOf(periodEnd),
		})
	} else if hasRemaining {
		r.Details = append(r.Details, Detail{Label: "Monthly credits left", Value: usd(remaining)})
	}
	if p, ok := num(credits["purchasedCredits"]); ok && p > 0 {
		r.Details = append(r.Details, Detail{Label: "Purchased credits", Value: usd(p)})
	}
	return r, nil
}

func commandCodePlanName(id string) string {
	known := map[string]string{
		"individual-go": "Go", "individual-goat": "GOAT", "individual-pro": "Pro",
		"individual-max": "Max", "team-pro": "Team Pro", "provider": "Provider",
	}
	if n, ok := known[strings.ToLower(id)]; ok {
		return n
	}
	return id
}
