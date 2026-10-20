// Package provider fetches subscription usage from each supported service.
package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Window is one quota meter, such as a 5-hour session or a monthly credit grant.
type Window struct {
	ID          string     `json:"id"`
	Label       string     `json:"label"`
	UsedPercent float64    `json:"used_percent"`
	Used        *float64   `json:"used,omitempty"`
	Limit       *float64   `json:"limit,omitempty"`
	Remaining   *float64   `json:"remaining,omitempty"`
	Unit        string     `json:"unit,omitempty"`
	ResetsAt    *time.Time `json:"resets_at,omitempty"`
}

// Detail is an extra key/value line, such as a credit balance.
type Detail struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// Report is the result for one provider. Status is "ok", "unconfigured" or "error".
type Report struct {
	Provider  string    `json:"provider"`
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	Plan      string    `json:"plan,omitempty"`
	Account   string    `json:"account,omitempty"`
	Source    string    `json:"source,omitempty"`
	Windows   []Window  `json:"windows"`
	Details   []Detail  `json:"details,omitempty"`
	Error     string    `json:"error,omitempty"`
	FetchedAt time.Time `json:"fetched_at"`
}

// Provider fetches one service's usage.
type Provider struct {
	ID    string
	Name  string
	Fetch func(ctx context.Context, c *http.Client) (*Report, error)
}

// ErrUnconfigured marks a provider whose credentials are not present on this machine.
var ErrUnconfigured = errors.New("unconfigured")

func unconfigured(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrUnconfigured, fmt.Sprintf(format, a...))
}

// All returns every provider in display order.
func All() []Provider {
	return []Provider{
		{ID: "claude", Name: "Claude", Fetch: fetchClaude},
		{ID: "chatgpt", Name: "ChatGPT (Codex)", Fetch: fetchCodex},
		{ID: "commandcode", Name: "Command Code", Fetch: fetchCommandCode},
		{ID: "ollama", Name: "Ollama Cloud", Fetch: fetchOllama},
	}
}

// Run fetches one provider and always returns a report, folding failures into it.
func Run(ctx context.Context, c *http.Client, p Provider) Report {
	r, err := p.Fetch(ctx, c)
	if r == nil {
		r = &Report{}
	}
	r.Provider, r.Name, r.FetchedAt = p.ID, p.Name, time.Now().UTC()
	if r.Windows == nil {
		r.Windows = []Window{}
	}
	switch {
	case errors.Is(err, ErrUnconfigured):
		r.Status, r.Error = "unconfigured", strings.TrimPrefix(err.Error(), "unconfigured: ")
	case err != nil:
		r.Status, r.Error = "error", err.Error()
	default:
		r.Status = "ok"
	}
	return *r
}

// httpError is a non-2xx response.
type httpError struct {
	Status int
	Body   string
}

func (e *httpError) Error() string {
	msg := fmt.Sprintf("HTTP %d", e.Status)
	if e.Status == 401 || e.Status == 403 {
		msg += " (credentials rejected or expired)"
	}
	return msg
}

func getJSON(ctx context.Context, c *http.Client, url string, headers map[string]string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "quota-cli")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
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
		// The body is never echoed back, since error envelopes can contain account details.
		return &httpError{Status: resp.StatusCode}
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("unexpected response: %w", err)
	}
	return nil
}

func homeDir() string {
	if h, err := os.UserHomeDir(); err == nil {
		return h
	}
	return "."
}

func readJSONFile(path string, out any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}

func firstEnv(names ...string) string {
	for _, n := range names {
		if v := strings.TrimSpace(os.Getenv(n)); v != "" {
			return v
		}
	}
	return ""
}

func joinHome(parts ...string) string {
	return filepath.Join(append([]string{homeDir()}, parts...)...)
}

// num accepts JSON numbers or numeric strings.
func num(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case json.Number:
		f, err := x.Float64()
		return f, err == nil
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(x), 64)
		return f, err == nil
	}
	return 0, false
}

// timeOf accepts unix seconds, unix milliseconds or RFC 3339 strings.
func timeOf(v any) *time.Time {
	if f, ok := num(v); ok && f > 0 {
		if f > 1e11 {
			f /= 1000
		}
		t := time.Unix(int64(f), 0).UTC()
		return &t
	}
	if s, ok := v.(string); ok && s != "" {
		for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
			if t, err := time.Parse(layout, s); err == nil {
				t = t.UTC()
				return &t
			}
		}
	}
	return nil
}

func ptr(f float64) *float64 { return &f }

func clampPct(p float64) float64 {
	if p < 0 {
		return 0
	}
	if p > 100 {
		return 100
	}
	return p
}

func usd(f float64) string { return fmt.Sprintf("$%.2f", f) }
