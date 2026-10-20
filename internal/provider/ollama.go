package provider

import (
	"context"
	"errors"
	"net/http"
)

// Ollama Cloud reports the included monthly allowance and purchased credits through /api/balance.

var ollamaBalanceURL = "https://ollama.com/api/balance"

func fetchOllama(ctx context.Context, c *http.Client) (*Report, error) {
	r := &Report{Source: "api-key"}
	key := firstEnv("OLLAMA_API_KEY")
	if key == "" {
		return r, unconfigured("set OLLAMA_API_KEY (create one at https://ollama.com/settings/keys)")
	}
	var b struct {
		Plan      string         `json:"plan"`
		Included  map[string]any `json:"included"`
		Purchased map[string]any `json:"purchased"`
	}
	if err := getJSON(ctx, c, ollamaBalanceURL,
		map[string]string{"Authorization": "Bearer " + key}, &b); err != nil {
		return r, err
	}
	if b.Included == nil && b.Purchased == nil {
		return r, errors.New("unexpected balance response")
	}
	r.Plan = planName(b.Plan)
	if inc := b.Included; inc != nil {
		allowance, okA := num(inc["allowance_usd"])
		balance, okB := num(inc["balance_usd"])
		if okA && okB && allowance > 0 {
			used := max(0, allowance-balance)
			var reset any
			if p, ok := inc["period"].(map[string]any); ok {
				reset = p["until"]
			}
			r.Windows = append(r.Windows, Window{
				ID: "monthly", Label: "Monthly included usage", UsedPercent: clampPct(used / allowance * 100),
				Used: ptr(used), Limit: ptr(allowance), Remaining: ptr(max(0, balance)), Unit: "usd",
				ResetsAt: timeOf(reset),
			})
		}
	}
	if p := b.Purchased; p != nil {
		if v, ok := num(p["balance_usd"]); ok {
			r.Details = append(r.Details, Detail{Label: "Credit balance", Value: usd(v)})
		}
	}
	return r, nil
}
