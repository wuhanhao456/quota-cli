package provider

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func openCodeProvider(t *testing.T) Provider {
	t.Helper()
	for _, p := range All() {
		if p.ID == "opencode" {
			return p
		}
	}
	t.Fatal("opencode provider not registered")
	return Provider{}
}

const openCodeUsageBody = `{"usage":{
	"rolling":{"percent":12.5,"resetInSec":3600},
	"weekly":{"percent":40,"resetsAt":"2026-10-27T00:00:00Z"},
	"monthly":{"percent":0,"resetInSec":1539100}}}`

func TestOpenCodeFromAuthJSON(t *testing.T) {
	fakeHome(t, map[string]string{
		".local/share/opencode/auth.json": `{"openai":{"type":"oauth","access":"x"},"opencode":{"type":"api","key":"zen_key"}}`,
	})
	srv, auth := serve(t, map[string]string{"/zen/go/v1/usage": openCodeUsageBody})
	openCodeUsageURL = srv.URL + "/zen/go/v1/usage"

	before := time.Now()
	r := Run(context.Background(), srv.Client(), openCodeProvider(t))
	if r.Status != "ok" || *auth != "Bearer zen_key" || r.Plan != "Go" {
		t.Fatalf("got %+v auth=%q", r, *auth)
	}
	if w := window(t, r, "session"); w.UsedPercent != 12.5 || w.ResetsAt == nil || w.ResetsAt.Sub(before) < 59*time.Minute {
		t.Fatalf("session = %+v", w)
	}
	if w := window(t, r, "weekly"); w.UsedPercent != 40 || w.ResetsAt == nil {
		t.Fatalf("weekly = %+v", w)
	}
	if w := window(t, r, "monthly"); w.UsedPercent != 0 || w.ResetsAt == nil {
		t.Fatalf("monthly = %+v", w)
	}
	if len(r.Windows) != 3 {
		t.Fatalf("windows = %+v", r.Windows)
	}
}

// Current opencode stores keys in its SQLite database; a Go key wins over a Zen key.
func TestOpenCodeFromDatabase(t *testing.T) {
	fakeHome(t, map[string]string{
		".local/share/opencode/auth.json": `{"opencode":{"type":"api","key":"stale"}}`,
	})
	home, _ := os.UserHomeDir()
	db, err := sql.Open("sqlite", filepath.Join(home, ".local", "share", "opencode", "opencode.db"))
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`CREATE TABLE credential (id text PRIMARY KEY, integration_id text, label text NOT NULL, value text NOT NULL,
			connector_id text, method_id text, active integer, time_created integer NOT NULL, time_updated integer NOT NULL)`,
		`INSERT INTO credential VALUES ('1','opencode','API key','{"type":"key","key":"zen_db"}',NULL,NULL,1,1,9)`,
		`INSERT INTO credential VALUES ('2','opencode-go','API key','{"type":"key","key":"go_old"}',NULL,NULL,0,1,2)`,
		`INSERT INTO credential VALUES ('3','opencode-go','API key','{"type":"key","key":"go_new"}',NULL,NULL,0,1,5)`,
		`INSERT INTO credential VALUES ('4','opencode-go','OAuth','{"type":"oauth","access":"a","refresh":"r","expires":1}',NULL,NULL,1,1,7)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()

	srv, auth := serve(t, map[string]string{"/zen/go/v1/usage": openCodeUsageBody})
	openCodeUsageURL = srv.URL + "/zen/go/v1/usage"

	r := Run(context.Background(), srv.Client(), openCodeProvider(t))
	if r.Status != "ok" || *auth != "Bearer go_new" || r.Source != "api-key (opencode.db)" {
		t.Fatalf("got %+v auth=%q", r, *auth)
	}
}

func TestOpenCodeNoSubscription(t *testing.T) {
	fakeHome(t, nil)
	t.Setenv("OPENCODE_API_KEY", "k")
	srv, _ := serve(t, map[string]string{"/zen/go/v1/usage": `{"usage":null}`})
	openCodeUsageURL = srv.URL + "/zen/go/v1/usage"

	if r := Run(context.Background(), srv.Client(), openCodeProvider(t)); r.Status != "error" {
		t.Fatalf("got %+v", r)
	}
}

func TestOpenCodeDBOverrideIsExclusive(t *testing.T) {
	fakeHome(t, nil)
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".local", "share", "opencode")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "opencode.db"), nil, 0o600)
	t.Setenv("OPENCODE_DB", filepath.Join(home, "other.db"))
	if got := openCodeDBPaths(dir); len(got) != 1 || got[0] != filepath.Join(home, "other.db") {
		t.Fatalf("paths = %v", got)
	}
}
