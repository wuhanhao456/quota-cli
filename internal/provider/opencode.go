package provider

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite" // pure-Go driver, so the binary still cross-compiles without cgo
)

// OpenCode Go reports its 5-hour, weekly and monthly meters through the same usage route
// that CodexBar uses, authenticated with an opencode.ai API key.

var openCodeUsageURL = "https://opencode.ai/zen/go/v1/usage"

// openCodeIntegrations are the opencode credential ids that hold an opencode.ai key, best first.
var openCodeIntegrations = []string{"opencode-go", "opencode"}

// openCodeDataDir mirrors opencode's XDG data dir, which it uses on Windows too.
func openCodeDataDir() string {
	if d := firstEnv("XDG_DATA_HOME"); d != "" {
		return filepath.Join(d, "opencode")
	}
	return joinHome(".local", "share", "opencode")
}

type openCodeCred struct {
	key, source string
}

func openCodeKey() (openCodeCred, error) {
	if k := firstEnv("OPENCODE_API_KEY", "OPENCODE_GO_API_KEY"); k != "" {
		return openCodeCred{k, "OPENCODE_API_KEY"}, nil
	}
	dir := openCodeDataDir()
	// Current opencode keeps credentials in its SQLite database; older releases used auth.json.
	for _, db := range openCodeDBPaths(dir) {
		if k, err := openCodeKeyFromDB(db); err == nil && k != "" {
			return openCodeCred{k, filepath.Base(db)}, nil
		}
	}
	authPath := filepath.Join(dir, "auth.json")
	var auth map[string]struct {
		Type   string `json:"type"`
		Key    string `json:"key"`
		Access string `json:"access"`
	}
	if err := readJSONFile(authPath, &auth); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return openCodeCred{}, fmt.Errorf("reading %s: %w", authPath, err)
	}
	for _, id := range openCodeIntegrations {
		if e, ok := auth[id]; ok && e.Type == "api" && e.Key != "" {
			return openCodeCred{e.Key, "auth.json"}, nil
		}
	}
	return openCodeCred{}, unconfigured("no OpenCode Go key found in %s (run `opencode auth login` and pick OpenCode Go, or set OPENCODE_API_KEY)", dir)
}

func openCodeDBPaths(dir string) []string {
	// An explicit OPENCODE_DB is the only database opencode uses, so never fall back past it.
	if p := firstEnv("OPENCODE_DB"); p != "" {
		if p == ":memory:" {
			return nil
		}
		if !filepath.IsAbs(p) {
			p = filepath.Join(dir, p)
		}
		return []string{p}
	}
	paths := []string{filepath.Join(dir, "opencode.db")}
	// Non-stable channels use opencode-<channel>.db.
	channels, _ := filepath.Glob(filepath.Join(dir, "opencode-*.db"))
	sort.Strings(channels)
	return append(paths, channels...)
}

func openCodeKeyFromDB(path string) (string, error) {
	if _, err := os.Stat(path); err != nil {
		return "", err
	}
	p := filepath.ToSlash(path)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p // Windows drive paths become file:///C:/...
	}
	dsn := (&url.URL{Scheme: "file", Path: p, RawQuery: "mode=ro&_pragma=busy_timeout(3000)"}).String()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return "", err
	}
	defer db.Close()

	rows, err := db.Query(`SELECT integration_id, value, COALESCE(active, 0), time_updated FROM credential
		WHERE integration_id IN ('opencode-go', 'opencode')`)
	if err != nil {
		// Databases from before the active column was added.
		rows, err = db.Query(`SELECT integration_id, value, 0, time_updated FROM credential
			WHERE integration_id IN ('opencode-go', 'opencode')`)
		if err != nil {
			return "", err
		}
	}
	defer rows.Close()

	type row struct {
		rank    int
		active  int64
		updated int64
		key     string
	}
	var found []row
	for rows.Next() {
		var id, value string
		var r row
		if err := rows.Scan(&id, &value, &r.active, &r.updated); err != nil {
			return "", err
		}
		var v struct {
			Type string `json:"type"`
			Key  string `json:"key"`
		}
		if json.Unmarshal([]byte(value), &v) != nil || v.Type != "key" || v.Key == "" {
			continue
		}
		r.key = v.Key
		for i, want := range openCodeIntegrations {
			if id == want {
				r.rank = i
			}
		}
		found = append(found, r)
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	sort.Slice(found, func(i, j int) bool {
		a, b := found[i], found[j]
		if a.rank != b.rank {
			return a.rank < b.rank
		}
		if a.active != b.active {
			return a.active > b.active
		}
		return a.updated > b.updated
	})
	if len(found) == 0 {
		return "", nil
	}
	return found[0].key, nil
}

func fetchOpenCode(ctx context.Context, c *http.Client) (*Report, error) {
	r := &Report{Source: "api-key"}
	cred, err := openCodeKey()
	if err != nil {
		return r, err
	}
	r.Source = "api-key (" + cred.source + ")"

	var root map[string]any
	if err := getJSON(ctx, c, openCodeUsageURL, map[string]string{"Authorization": "Bearer " + cred.key}, &root); err != nil {
		return r, err
	}
	usage, _ := root["usage"].(map[string]any)
	if usage == nil {
		return r, errors.New("no OpenCode Go usage in the response (is this key's workspace subscribed to Go?)")
	}
	for _, k := range []string{"plan", "tier", "planName"} {
		if s, ok := root[k].(string); ok && s != "" {
			r.Plan = s
			break
		}
	}
	if r.Plan == "" {
		r.Plan = "Go"
	}
	now := time.Now()
	for _, w := range []struct{ key, id, label string }{
		{"rolling", "session", "Session (5h)"},
		{"weekly", "weekly", "Weekly"},
		{"monthly", "monthly", "Monthly"},
	} {
		m, _ := usage[w.key].(map[string]any)
		pct, ok := openCodePercent(m)
		if !ok {
			continue
		}
		r.Windows = append(r.Windows, Window{
			ID: w.id, Label: w.label, UsedPercent: clampPct(pct), Unit: "percent",
			ResetsAt: openCodeReset(m, now),
		})
	}
	if len(r.Windows) == 0 {
		return r, errors.New("OpenCode Go usage response had no meters")
	}
	return r, nil
}

// openCodePercent reads a meter's used percentage (0-100), or derives it from used/limit.
func openCodePercent(m map[string]any) (float64, bool) {
	if m == nil {
		return 0, false
	}
	for _, k := range []string{"percent", "usagePercent", "usedPercent", "percentUsed", "usage_percent", "used_percent"} {
		if f, ok := num(m[k]); ok {
			return f, true
		}
	}
	used, okU := num(m["used"])
	limit, okL := num(m["limit"])
	if okU && okL && limit > 0 {
		return used / limit * 100, true
	}
	return 0, false
}

func openCodeReset(m map[string]any, now time.Time) *time.Time {
	for _, k := range []string{"resetInSec", "resetInSeconds", "resetsInSec", "reset_in_sec"} {
		if s, ok := num(m[k]); ok && s >= 0 {
			t := now.Add(time.Duration(s) * time.Second).UTC().Truncate(time.Second)
			return &t
		}
	}
	for _, k := range []string{"resetsAt", "resetAt", "resets_at", "reset_at"} {
		if t := timeOf(m[k]); t != nil {
			return t
		}
	}
	return nil
}
