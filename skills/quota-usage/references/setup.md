# quota-cli setup

All steps run on the machine where `quota` runs (the Hermes host). Credentials stay in each
tool's own files; `quota` only reads them.

## Binary

Linux / macOS / Git Bash, into `~/.local/bin`:

```bash
curl -fsSL https://raw.githubusercontent.com/wuhanhao456/quota-cli/main/skills/quota-usage/scripts/install.sh | bash
# a specific release:
curl -fsSL https://raw.githubusercontent.com/wuhanhao456/quota-cli/main/skills/quota-usage/scripts/install.sh | QUOTA_VERSION=v0.1.0 bash
```

Windows PowerShell, into `%LOCALAPPDATA%\Programs\quota` (added to the user PATH):

```powershell
irm https://raw.githubusercontent.com/wuhanhao456/quota-cli/main/skills/quota-usage/scripts/install.ps1 | iex
```

The same scripts ship inside the skill under `scripts/`.

Without a release (or on an unsupported platform), build from source with Go 1.24+:

```bash
go install github.com/wuhanhao456/quota-cli/cmd/quota@latest   # binary lands in ~/go/bin
```

Make sure the install directory is on `PATH` for the Hermes process, not just your shell
(for a systemd service, add `Environment=PATH=...` or use an absolute path).

## Claude (Pro / Max subscription)

1. Install Claude Code: `npm install -g @anthropic-ai/claude-code`.
2. Run `claude`, then `/login` and choose the Claude subscription account. On a headless server
   it prints a URL; open it in any browser and paste the code back.
3. Check: `~/.claude/.credentials.json` exists. `CLAUDE_CONFIG_DIR` moves it if you set one.

Notes:
- The access token expires every few hours. Claude Code refreshes it when it runs, so on a
  server that rarely uses Claude Code, run `claude -p hi` (e.g. from cron) before checking.
- Tokens from `claude setup-token` usually lack the `user:profile` scope that the usage
  endpoint needs; prefer the interactive login. `CLAUDE_OAUTH_TOKEN` overrides the file if needed.
- macOS keeps the token in Keychain, so there set `CLAUDE_OAUTH_TOKEN` yourself.

## ChatGPT (Plus / Pro / Team via Codex)

1. Install the Codex CLI: `npm install -g @openai/codex`.
2. `codex login` and sign in with ChatGPT (not an API key). On a headless server use
   `codex login --device-auth` and finish in a browser.
3. Check: `~/.codex/auth.json` contains a `tokens` object. `CODEX_HOME` moves it.

If you get HTTP 401, run any `codex` command once so it refreshes the token.

## Command Code

Either log in with the CLI:

```bash
npm install -g command-code   # provides `cmd`
cmd login
```

or create an API key in Command Code settings and expose it to Hermes as
`COMMANDCODE_API_KEY` (in `~/.hermes/.env`). Organization keys are detected automatically.

## Ollama Cloud

1. Create a key at https://ollama.com/settings/keys.
2. Put it in `~/.hermes/.env` as `OLLAMA_API_KEY=...` (or run `hermes setup`). Hermes passes
   it through to the terminal because this skill declares it.

## Troubleshooting

| Error text | Fix |
| --- | --- |
| `no Claude Code login found` | Log in to Claude Code as above, as the same OS user Hermes runs as. |
| `Claude Code token expired` | Run `claude -p hi`, then retry. |
| `no Codex CLI login found` / `has no ChatGPT tokens` | `codex login` with a ChatGPT account. |
| `HTTP 401 (credentials rejected or expired)` | Re-run that tool's CLI or log in again. |
| `set OLLAMA_API_KEY` | Add the key to `~/.hermes/.env`. |
| `unexpected response` | The service changed its API; update `quota`. |
