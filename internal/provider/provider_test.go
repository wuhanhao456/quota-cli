package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func fakeHome(t *testing.T, files map[string]string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	for _, env := range []string{"CLAUDE_CONFIG_DIR", "CLAUDE_OAUTH_TOKEN", "CODEX_HOME", "COMMANDCODE_API_KEY", "COMMAND_CODE_API_KEY", "CMD_API_KEY", "OLLAMA_API_KEY", "OPENCODE_API_KEY", "OPENCODE_GO_API_KEY", "OPENCODE_DB", "XDG_DATA_HOME"} {
		t.Setenv(env, "")
	}
	for rel, body := range files {
		p := filepath.Join(home, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// serve answers each path with a fixed body and records the Authorization header it saw.
func serve(t *testing.T, routes map[string]string) (*httptest.Server, *string) {
	t.Helper()
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		body, ok := routes[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, &auth
}

func window(t *testing.T, r Report, id string) Window {
	t.Helper()
	for _, w := range r.Windows {
		if w.ID == id {
			return w
		}
	}
	t.Fatalf("window %q not found in %+v", id, r.Windows)
	return Window{}
}

func TestClaude(t *testing.T) {
	exp := time.Now().Add(time.Hour).UnixMilli()
	fakeHome(t, map[string]string{
		".claude/.credentials.json": `{"claudeAiOauth":{"accessToken":"tok","expiresAt":` + itoa(exp) + `,"subscriptionType":"max"}}`,
	})
	srv, auth := serve(t, map[string]string{"/api/oauth/usage": `{
		"five_hour":{"utilization":42.5,"resets_at":"2026-10-20T12:00:00Z"},
		"seven_day":{"utilization":10,"resets_at":"2026-10-25T00:00:00Z"},
		"seven_day_opus":null,
		"extra_usage":{"is_enabled":true,"monthly_limit":5000,"used_credits":1250}}`})
	claudeUsageURL = srv.URL + "/api/oauth/usage"

	r := Run(context.Background(), srv.Client(), All()[0])
	if r.Status != "ok" || *auth != "Bearer tok" || r.Plan != "Max" {
		t.Fatalf("got %+v auth=%q", r, *auth)
	}
	if w := window(t, r, "session"); w.UsedPercent != 42.5 || w.ResetsAt == nil {
		t.Fatalf("session = %+v", w)
	}
	if w := window(t, r, "extra_usage"); *w.Used != 12.5 || *w.Limit != 50 {
		t.Fatalf("extra = %+v", w)
	}
	if len(r.Windows) != 3 {
		t.Fatalf("windows = %d", len(r.Windows))
	}
}

// A past expiresAt must not stop the request: the server decides, and the timestamp may lag.
func TestClaudeStaleExpiresAtStillFetches(t *testing.T) {
	for _, exp := range []string{"1000", "1700000000", "1700000000000"} {
		fakeHome(t, map[string]string{".claude/.credentials.json": `{"claudeAiOauth":{"accessToken":"tok","expiresAt":` + exp + `}}`})
		srv, _ := serve(t, map[string]string{"/api/oauth/usage": `{"five_hour":{"utilization":5}}`})
		claudeUsageURL = srv.URL + "/api/oauth/usage"
		if r := Run(context.Background(), srv.Client(), All()[0]); r.Status != "ok" || len(r.Windows) != 1 {
			t.Fatalf("expiresAt=%s: %+v", exp, r)
		}
	}
}

func TestClaudeRejectedExpiredTokenExplains(t *testing.T) {
	fakeHome(t, map[string]string{".claude/.credentials.json": `{"claudeAiOauth":{"accessToken":"tok","expiresAt":1700000000000}}`})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	claudeUsageURL = srv.URL + "/api/oauth/usage"
	r := Run(context.Background(), srv.Client(), All()[0])
	if r.Status != "error" || !strings.Contains(r.Error, "HTTP 401") || !strings.Contains(r.Error, "expired at") {
		t.Fatalf("got %+v", r)
	}
}

func TestCodex(t *testing.T) {
	fakeHome(t, map[string]string{".codex/auth.json": `{"tokens":{"access_token":"ctok","account_id":"acc"}}`})
	srv, auth := serve(t, map[string]string{"/wham/usage": `{
		"plan_type":"plus","email":"a@b.c",
		"rate_limit":{"primary_window":{"used_percent":30,"reset_at":1792500000,"limit_window_seconds":18000},
		              "secondary_window":{"used_percent":55,"reset_at":1793000000,"limit_window_seconds":604800}},
		"credits":{"has_credits":true,"unlimited":false,"balance":"12.5"}}`})
	codexUsageURL = srv.URL + "/wham/usage"

	r := Run(context.Background(), srv.Client(), All()[1])
	if r.Status != "ok" || *auth != "Bearer ctok" || r.Plan != "Plus" || r.Account != "a@b.c" {
		t.Fatalf("got %+v", r)
	}
	if w := window(t, r, "weekly"); w.UsedPercent != 55 || w.Label != "Weekly" {
		t.Fatalf("weekly = %+v", w)
	}
	if len(r.Details) != 1 || r.Details[0].Value != "12.50" {
		t.Fatalf("details = %+v", r.Details)
	}
}

func TestCommandCode(t *testing.T) {
	fakeHome(t, map[string]string{".commandcode/auth.json": `{"apiKey":"user_abc"}`})
	srv, auth := serve(t, map[string]string{
		"/alpha/billing/credits": `{"credits":{"monthlyCredits":49,"monthlyCreditsGranted":70,"purchasedCredits":5,"freeCredits":2},
			"windowLimits":{"fiveHour":{"cap":14,"used":7,"resetAt":1792500000000},"weekly":{"cap":35,"used":3.5,"resetAt":0}}}`,
		"/alpha/billing/subscriptions": `{"success":true,"data":{"planId":"individual-goat","status":"active","currentPeriodEnd":"2026-11-01T00:00:00Z"}}`,
	})
	commandCodeBaseURL = srv.URL + "/alpha"

	r := Run(context.Background(), srv.Client(), All()[2])
	if r.Status != "ok" || *auth != "Bearer user_abc" || r.Plan != "GOAT" {
		t.Fatalf("got %+v", r)
	}
	if w := window(t, r, "session"); w.UsedPercent != 50 || w.ResetsAt == nil {
		t.Fatalf("session = %+v", w)
	}
	if w := window(t, r, "weekly"); w.ResetsAt != nil {
		t.Fatalf("weekly reset 0 should be absent: %+v", w)
	}
	if w := window(t, r, "monthly"); *w.Used != 21 || *w.Remaining != 49 || w.ResetsAt == nil {
		t.Fatalf("monthly = %+v", w)
	}
	if len(r.Details) != 2 || r.Details[1].Label != "Free credits" {
		t.Fatalf("details = %+v", r.Details)
	}
}

// The CLI's credits response omits the grant; the plan sizes the month, and org keys are scoped.
func TestCommandCodeOrgAndPlanGrant(t *testing.T) {
	fakeHome(t, nil)
	t.Setenv("COMMANDCODE_API_KEY", "user_env")
	queries := map[string]string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries[r.URL.Path] = r.URL.RawQuery
		switch r.URL.Path {
		case "/alpha/whoami":
			w.Write([]byte(`{"success":true,"user":{"email":"me@x.y"},"org":{"id":"org_7","login":"acme"}}`))
		case "/alpha/billing/credits":
			w.Write([]byte(`{"credits":{"monthlyCredits":10}}`))
		case "/alpha/billing/subscriptions":
			w.Write([]byte(`{"success":true,"data":{"planId":"teams-pro-monthly"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	commandCodeBaseURL = srv.URL + "/alpha"

	r := Run(context.Background(), srv.Client(), All()[2])
	if r.Status != "ok" || r.Plan != "Teams Pro" || r.Account != "acme" {
		t.Fatalf("got %+v", r)
	}
	for _, p := range []string{"/alpha/billing/credits", "/alpha/billing/subscriptions"} {
		if queries[p] != "orgId=org_7" {
			t.Errorf("%s query = %q", p, queries[p])
		}
	}
	if w := window(t, r, "monthly"); *w.Limit != 40 || *w.Used != 30 || w.UsedPercent != 75 {
		t.Fatalf("monthly = %+v", w)
	}
}

func TestOllama(t *testing.T) {
	fakeHome(t, nil)
	t.Setenv("OLLAMA_API_KEY", "okey")
	srv, auth := serve(t, map[string]string{"/api/balance": `{
		"included":{"allowance_usd":"20.00","balance_usd":"15.00","period":{"from":"2026-10-01T00:00:00Z","until":"2026-11-01T00:00:00Z"}},
		"purchased":{"balance_usd":3}}`})
	ollamaBalanceURL = srv.URL + "/api/balance"

	r := Run(context.Background(), srv.Client(), All()[3])
	if r.Status != "ok" || *auth != "Bearer okey" {
		t.Fatalf("got %+v", r)
	}
	if w := window(t, r, "monthly"); w.UsedPercent != 25 || *w.Remaining != 15 {
		t.Fatalf("monthly = %+v", w)
	}
}

// Legacy Pro/Max plans report session and weekly limits as remaining percentages.
func TestOllamaLegacy(t *testing.T) {
	fakeHome(t, nil)
	t.Setenv("OLLAMA_API_KEY", "okey")
	srv, _ := serve(t, map[string]string{"/api/balance": `{
		"included":{"session":{"remaining_percent":96.5,"resets_at":"2026-10-20T18:00:00Z"},
		            "weekly":{"remaining_percent":"60","resets_at":"2026-10-26T00:00:00Z"}},
		"purchased":{"balance_usd":0}}`})
	ollamaBalanceURL = srv.URL + "/api/balance"

	r := Run(context.Background(), srv.Client(), All()[3])
	if r.Status != "ok" || len(r.Windows) != 2 {
		t.Fatalf("got %+v", r)
	}
	if w := window(t, r, "session"); w.UsedPercent < 3.49 || w.UsedPercent > 3.51 || w.ResetsAt == nil || w.ResetsAt.Hour() != 18 {
		t.Fatalf("session = %+v", w)
	}
	if w := window(t, r, "weekly"); w.UsedPercent != 40 || w.ResetsAt == nil || w.ResetsAt.Day() != 26 {
		t.Fatalf("weekly = %+v", w)
	}
}

func TestUnconfigured(t *testing.T) {
	fakeHome(t, nil)
	for _, p := range All() {
		if r := Run(context.Background(), http.DefaultClient, p); r.Status != "unconfigured" || r.Error == "" {
			t.Errorf("%s: %+v", p.ID, r)
		}
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
