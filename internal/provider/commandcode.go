package provider

import (
	"context"
	"errors"
	"io/fs"
	"net/http"
	"net/url"
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

	// Like the cmd CLI, scope billing to the active organization when the key belongs to one.
	// whoami is best-effort: without it the calls fall back to the personal namespace.
	scope := ""
	var who struct {
		User struct {
			Email    string `json:"email"`
			UserName string `json:"userName"`
		} `json:"user"`
		Org *struct {
			ID    string `json:"id"`
			Login string `json:"login"`
		} `json:"org"`
	}
	if err := getJSON(ctx, c, commandCodeBaseURL+"/whoami", headers, &who); err == nil {
		r.Account = who.User.Email
		if r.Account == "" {
			r.Account = who.User.UserName
		}
		if who.Org != nil && who.Org.ID != "" {
			scope = "?orgId=" + url.QueryEscape(who.Org.ID)
			if who.Org.Login != "" {
				r.Account = who.Org.Login
			}
		}
	}

	var root map[string]any
	if err := getJSON(ctx, c, commandCodeBaseURL+"/billing/credits"+scope, headers, &root); err != nil {
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
	planID := ""
	if err := getJSON(ctx, c, commandCodeBaseURL+"/billing/subscriptions"+scope, headers, &sub); err == nil && sub.Success {
		if sub.Data == nil {
			r.Plan = "Free"
		} else {
			if id, _ := sub.Data["planId"].(string); id != "" {
				planID = id
				r.Plan = commandCodePlanName(id)
			}
			periodEnd = sub.Data["currentPeriodEnd"]
		}
	}

	// The CLI's credits response usually omits the grant, so size the month from the plan.
	if !hasGranted && planID != "" {
		granted, hasGranted = commandCodePlanCredits(planID)
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
	if f, ok := num(credits["freeCredits"]); ok && f > 0 {
		r.Details = append(r.Details, Detail{Label: "Free credits", Value: usd(f)})
	}
	return r, nil
}

type commandCodePlan struct {
	name    string
	credits float64
}

// Monthly USD allowance per plan, as used by the cmd CLI and CodexBar.
var commandCodePlans = map[string]commandCodePlan{
	"individual-go":       {"Go", 10},
	"individual-goat":     {"GOAT", 70},
	"individual-pro":      {"Pro", 30},
	"individual-pro-v1":   {"Pro", 80},
	"individual-provider": {"Provider", 15},
	"individual-max":      {"Max", 150},
	"individual-ultra":    {"Ultra", 300},
	"teams-pro":           {"Teams Pro", 40},
}

// lookupCommandCodePlan matches a plan id exactly, then by the longest known prefix
// (billing ids can carry suffixes such as an interval).
func lookupCommandCodePlan(id string) (commandCodePlan, bool) {
	id = strings.ToLower(id)
	if p, ok := commandCodePlans[id]; ok {
		return p, true
	}
	best, found := "", false
	for k := range commandCodePlans {
		if strings.HasPrefix(id, k+"-") && len(k) > len(best) {
			best, found = k, true
		}
	}
	return commandCodePlans[best], found
}

func commandCodePlanCredits(id string) (float64, bool) {
	p, ok := lookupCommandCodePlan(id)
	return p.credits, ok
}

func commandCodePlanName(id string) string {
	if p, ok := lookupCommandCodePlan(id); ok {
		return p.name
	}
	return id
}
