// Command quota collects subscription usage and remaining quota for Claude, ChatGPT (Codex),
// Command Code, Ollama Cloud and OpenCode Go, using credentials already on this machine.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/wuhanhao456/quota-cli/internal/provider"
	"github.com/wuhanhao456/quota-cli/internal/render"
)

var version = "dev"

func main() {
	os.Exit(run())
}

func run() int {
	var (
		asJSON   = flag.Bool("json", false, "print machine-readable JSON (for agents and scripts)")
		pngPath  = flag.String("png", "", "also render a dashboard image to this PNG file")
		only     = flag.String("providers", "", "comma-separated subset: "+ids())
		timeout  = flag.Duration("timeout", 20*time.Second, "per-request timeout")
		fromJSON = flag.String("from-json", "", "render a previously saved JSON report instead of fetching")
		showVer  = flag.Bool("version", false, "print version and exit")
	)
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: quota [flags]\n\nCollects subscription usage from local logins.\n\nFlags:\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	if *showVer {
		fmt.Println(version)
		return 0
	}

	var out render.Output
	if *fromJSON != "" {
		b, err := os.ReadFile(*fromJSON)
		if err == nil {
			err = json.Unmarshal(b, &out)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "quota:", err)
			return 2
		}
	} else {
		selected, err := selectProviders(*only)
		if err != nil {
			fmt.Fprintln(os.Stderr, "quota:", err)
			return 2
		}
		out = render.NewOutput(fetchAll(selected, *timeout))
	}

	if *asJSON {
		if err := render.JSON(os.Stdout, out); err != nil {
			fmt.Fprintln(os.Stderr, "quota:", err)
			return 1
		}
	} else {
		render.Text(os.Stdout, out)
	}
	if *pngPath != "" {
		if err := render.PNG(*pngPath, out); err != nil {
			fmt.Fprintln(os.Stderr, "quota: writing image:", err)
			return 1
		}
		if !*asJSON {
			fmt.Fprintf(os.Stdout, "\nimage saved to %s\n", *pngPath)
		}
	}

	// Exit 1 only when providers were configured and every one of them failed.
	failed, ok := 0, 0
	for _, r := range out.Providers {
		switch r.Status {
		case "ok":
			ok++
		case "error":
			failed++
		}
	}
	if ok == 0 && failed > 0 {
		return 1
	}
	return 0
}

func fetchAll(ps []provider.Provider, timeout time.Duration) []provider.Report {
	client := &http.Client{Timeout: timeout}
	reports := make([]provider.Report, len(ps))
	var wg sync.WaitGroup
	for i, p := range ps {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()
			reports[i] = provider.Run(ctx, client, p)
		}()
	}
	wg.Wait()
	return reports
}

func selectProviders(list string) ([]provider.Provider, error) {
	all := provider.All()
	if strings.TrimSpace(list) == "" {
		return all, nil
	}
	var out []provider.Provider
	for _, id := range strings.Split(list, ",") {
		id = strings.ToLower(strings.TrimSpace(id))
		if id == "codex" || id == "openai" {
			id = "chatgpt"
		}
		if id == "opencode-go" || id == "opencodego" {
			id = "opencode"
		}
		found := false
		for _, p := range all {
			if p.ID == id {
				out, found = append(out, p), true
			}
		}
		if !found {
			return nil, fmt.Errorf("unknown provider %q (known: %s)", id, ids())
		}
	}
	return out, nil
}

func ids() string {
	var s []string
	for _, p := range provider.All() {
		s = append(s, p.ID)
	}
	return strings.Join(s, ",")
}
