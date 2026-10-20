package provider

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// Ollama Cloud reports the included monthly allowance and purchased credits through /api/balance.
// Older plans meter usage as session (or hourly) and weekly percentages, which only the signed-in
// settings page shows, so a browser cookie is read from OLLAMA_COOKIE or a local file to scrape it.

var (
	ollamaBalanceURL  = "https://ollama.com/api/balance"
	ollamaSettingsURL = "https://ollama.com/settings"
)

func ollamaCookiePath() string { return joinHome(".config", "quota-cli", "ollama-cookie") }

// ollamaCookie returns the Cookie header value, or "" when none is configured.
func ollamaCookie() (string, error) {
	v := firstEnv("OLLAMA_COOKIE")
	if v == "" {
		b, err := os.ReadFile(ollamaCookiePath())
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
		v = strings.TrimSpace(string(b))
	}
	if v == "" {
		return "", nil
	}
	if len(v) > 7 && strings.EqualFold(v[:7], "cookie:") {
		v = strings.TrimSpace(v[7:])
	}
	if strings.ContainsAny(v, "\r\n") {
		return "", errors.New("Ollama cookie must be a single line")
	}
	if !strings.Contains(v, "=") {
		// A bare value is the session cookie itself.
		v = "__Secure-session=" + v
	}
	return v, nil
}

func fetchOllama(ctx context.Context, c *http.Client) (*Report, error) {
	r := &Report{}
	cookie, err := ollamaCookie()
	if err != nil {
		return r, err
	}
	key := firstEnv("OLLAMA_API_KEY")
	if cookie == "" && key == "" {
		return r, unconfigured("set OLLAMA_API_KEY (create one at https://ollama.com/settings/keys), " +
			"or OLLAMA_COOKIE for older plans with session/weekly limits")
	}

	var webErr error
	if cookie != "" {
		r.Source = "cookie"
		if webErr = fetchOllamaSettings(ctx, c, cookie, r); webErr != nil && key == "" {
			return r, webErr
		}
	}
	if key == "" {
		return r, nil
	}

	api := &Report{}
	apiErr := fetchOllamaBalance(ctx, c, key, api)
	switch {
	case cookie == "":
		api.Source = "api-key"
		*r = *api
		if apiErr != nil {
			return r, fmt.Errorf("%w (older plans: set OLLAMA_COOKIE to read session/weekly usage)", apiErr)
		}
		return r, nil
	case webErr != nil && apiErr != nil:
		return r, fmt.Errorf("settings page: %v; api: %v", webErr, apiErr)
	case webErr != nil:
		api.Source = "api-key"
		api.Details = append(api.Details, Detail{Label: "Session / weekly", Value: "unavailable: " + webErr.Error()})
		*r = *api
		return r, nil
	case apiErr == nil:
		// The page is authoritative for meters; the API adds what the page lacks.
		r.Source = "cookie+api-key"
		if r.Plan == "" {
			r.Plan = api.Plan
		}
		for _, w := range api.Windows {
			if !hasWindow(r.Windows, w.ID) {
				r.Windows = append(r.Windows, w)
			}
		}
		for _, d := range api.Details {
			if !hasDetail(r.Details, d.Label) {
				r.Details = append(r.Details, d)
			}
		}
	}
	return r, nil
}

func fetchOllamaBalance(ctx context.Context, c *http.Client, key string, r *Report) error {
	var b struct {
		Plan      string         `json:"plan"`
		Included  map[string]any `json:"included"`
		Purchased map[string]any `json:"purchased"`
	}
	if err := getJSON(ctx, c, ollamaBalanceURL,
		map[string]string{"Authorization": "Bearer " + key}, &b); err != nil {
		return err
	}
	if b.Included == nil && b.Purchased == nil {
		return errors.New("unexpected balance response")
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
	return nil
}

func fetchOllamaSettings(ctx context.Context, c *http.Client, cookie string, r *Report) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ollamaSettingsURL, nil)
	if err != nil {
		return err
	}
	// The page is served to browsers; a plain client UA can be refused.
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 "+
		"(KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36")
	req.Header.Set("Cookie", cookie)
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode/100 != 2 {
		return &httpError{Status: resp.StatusCode}
	}
	// An expired session is redirected to the sign-in page (ollama.com/signin or WorkOS AuthKit).
	if u := resp.Request.URL; u.Path == "/signin" || u.Host != req.URL.Host {
		return errors.New("Ollama session cookie expired; sign in at https://ollama.com and copy a fresh cookie")
	}
	return parseOllamaSettings(string(body), r)
}

var (
	ollamaPlanRes = []*regexp.Regexp{
		regexp.MustCompile(`(?s)Included usage\s*</span>\s*<span[^>]*>([^<]+)</span`),
		regexp.MustCompile(`(?s)Cloud Usage\s*</span>\s*<span[^>]*>([^<]+)</span`),
		regexp.MustCompile(`(?is)<h[1-6][^>]*>\s*Usage credits\s*<span[^>]*>\s*([^<]+)</span`),
	}
	ollamaEmailRe   = regexp.MustCompile(`id="header-email"[^>]*>([^<]+)<`)
	ollamaPctRe     = regexp.MustCompile(`(?i)([0-9]+(?:\.[0-9]+)?)\s*%\s*used`)
	ollamaDollarRe  = regexp.MustCompile(`(?i)\$([0-9][0-9,]*(?:\.[0-9]+)?)\s+of\s+\$([0-9][0-9,]*(?:\.[0-9]+)?)\s+used`)
	ollamaWidthRe   = regexp.MustCompile(`(?i)width:\s*([0-9]+(?:\.[0-9]+)?)%`)
	ollamaResetRe   = regexp.MustCompile(`data-time="([^"]+)"`)
	ollamaSignInRe  = regexp.MustCompile(`(?i)(sign in to ollama|log in to ollama|action="/(signin|login)")`)
	ollamaBlockRe   = map[string]*regexp.Regexp{}
	ollamaMeterList = []struct{ id, label, page string }{
		{"monthly", "Monthly included usage", "Monthly usage"},
		{"monthly", "Free usage", "Free usage"},
		{"session", "Session usage", "Session usage"},
		{"hourly", "Hourly usage", "Hourly usage"},
		{"weekly", "Weekly usage", "Weekly usage"},
	}
)

func init() {
	for _, m := range ollamaMeterList {
		ollamaBlockRe[m.page] = regexp.MustCompile(`>\s*` + m.page + `\s*<`)
	}
}

// parseOllamaSettings reads the usage meters from the settings page. Each meter's block runs from
// its label to the next meter label, and holds "N% used" (or "$X of $Y used") and a data-time reset.
func parseOllamaSettings(html string, r *Report) error {
	for _, re := range ollamaPlanRes {
		if m := re.FindStringSubmatch(html); m != nil && strings.TrimSpace(m[1]) != "" {
			r.Plan = planName(strings.TrimSpace(m[1]))
			break
		}
	}
	if m := ollamaEmailRe.FindStringSubmatch(html); m != nil && strings.Contains(m[1], "@") {
		r.Account = strings.TrimSpace(m[1])
	}

	type found struct{ start, end int }
	starts := map[string]found{}
	for _, m := range ollamaMeterList {
		if loc := ollamaBlockRe[m.page].FindStringIndex(html); loc != nil {
			starts[m.page] = found{loc[0], loc[1]}
		}
	}
	for _, m := range ollamaMeterList {
		loc, ok := starts[m.page]
		if !ok || hasWindow(r.Windows, m.id) {
			continue
		}
		end := min(len(html), loc.end+4000)
		for _, other := range starts {
			if other.start > loc.start && other.start < end {
				end = other.start
			}
		}
		block := html[loc.end:end]
		w := Window{ID: m.id, Label: m.label}
		if p := ollamaPctRe.FindStringSubmatch(block); p != nil {
			w.UsedPercent, _ = strconv.ParseFloat(p[1], 64)
		} else if d := ollamaDollarRe.FindStringSubmatch(block); d != nil {
			used, _ := strconv.ParseFloat(strings.ReplaceAll(d[1], ",", ""), 64)
			limit, _ := strconv.ParseFloat(strings.ReplaceAll(d[2], ",", ""), 64)
			if limit <= 0 {
				continue
			}
			w.UsedPercent = used / limit * 100
			w.Used, w.Limit, w.Remaining, w.Unit = ptr(used), ptr(limit), ptr(max(0, limit-used)), "usd"
		} else if p := ollamaWidthRe.FindStringSubmatch(block); p != nil {
			w.UsedPercent, _ = strconv.ParseFloat(p[1], 64)
		} else {
			continue
		}
		w.UsedPercent = clampPct(w.UsedPercent)
		if t := ollamaResetRe.FindStringSubmatch(block); t != nil {
			w.ResetsAt = timeOf(t[1])
		}
		r.Windows = append(r.Windows, w)
	}

	if len(r.Windows) == 0 {
		if ollamaSignInRe.MatchString(html) {
			return errors.New("Ollama session cookie expired; sign in at https://ollama.com and copy a fresh cookie")
		}
		return errors.New("no usage meters found on the Ollama settings page")
	}
	return nil
}

func hasWindow(ws []Window, id string) bool {
	for _, w := range ws {
		if w.ID == id {
			return true
		}
	}
	return false
}

func hasDetail(ds []Detail, label string) bool {
	for _, d := range ds {
		if d.Label == label {
			return true
		}
	}
	return false
}
