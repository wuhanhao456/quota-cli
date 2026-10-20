---
name: quota-usage
description: Check remaining subscription quota and usage for Claude, ChatGPT (Codex), Command Code, Ollama Cloud and OpenCode Go with the `quota` CLI. Use when the user asks how much quota, credit or usage is left, when a limit resets, or wants a usage dashboard image.
version: 0.1.0
platforms: [linux, macos, windows]
metadata:
  hermes:
    tags: [quota, usage, billing, claude, codex, chatgpt, ollama, commandcode, opencode]
    category: devops
    requires_toolsets: [terminal]
required_environment_variables:
  - name: OLLAMA_API_KEY
    prompt: Ollama Cloud API key (optional)
    help: Create one at https://ollama.com/settings/keys. Skip if you don't use Ollama Cloud.
    required_for: Ollama Cloud quota only
  - name: OPENCODE_API_KEY
    prompt: OpenCode Go API key (optional)
    help: Only needed if opencode is not logged in on this machine. Get one at https://opencode.ai/auth.
    required_for: OpenCode Go quota only
---

# Subscription quota (quota-cli)

`quota` reads the logins already on this machine and reports, per service, each usage window
(5-hour session, weekly, monthly credits) with percent used, money used/left, and reset time.
It never prints tokens.

## When to Use

- "How much Claude / Codex / Command Code / Ollama / OpenCode quota do I have left?"
- "When does my weekly limit reset?", "Am I close to my limit?"
- "Send me a usage screenshot / dashboard."
- Before starting a long task, to pick a provider that still has headroom.

## Procedure

1. Make sure the binary exists: `command -v quota`. If it is missing, follow **Setup** below.
2. Fetch data as JSON (always prefer this; it is the stable interface):
   ```bash
   quota --json
   ```
   Limit to some providers with `--providers claude,chatgpt,commandcode,ollama,opencode`.
3. If the user wants an image, render one in the same run and deliver it by putting its
   absolute path on its own line in the reply (the gateway sends it as a photo):
   ```bash
   quota --json --png "$HOME/.hermes/cache/quota.png"
   ```
   Add `[[as_document]]` on the last line if the user wants the full-resolution file.
4. Answer from the JSON. For each provider with `status: "ok"`, report the plan and every window:
   `label`, `used_percent`, `remaining` (USD when `unit` is `usd`), and `resets_at` converted to
   the user's local time and as a relative time ("in 2h 15m"). List `details` (credit balances).
   Call out any window at 90% or more. Reply in the user's language.
5. For `status: "unconfigured"` or `"error"`, quote the `error` text and point to the matching
   fix in [references/setup.md](references/setup.md). Do not ask the user to paste tokens into chat.

### JSON shape (schema_version 1)

```json
{"schema_version":1,"generated_at":"…","providers":[
  {"provider":"claude","name":"Claude","status":"ok","plan":"Max","account":"",
   "windows":[{"id":"session","label":"Session (5h)","used_percent":42,"unit":"percent","resets_at":"2026-10-20T10:30:00Z"},
              {"id":"monthly","label":"Monthly credits","used_percent":30,"used":21,"limit":70,"remaining":49,"unit":"usd","resets_at":"…"}],
   "details":[{"label":"Purchased credits","value":"$5.00"}],"error":"","fetched_at":"…"}]}
```

Exit code is 0 if any provider succeeded, 1 if every configured provider failed, 2 for bad flags.

## Setup

Install or update the binary. Linux/macOS (or Git Bash on Windows), into `~/.local/bin`:

```bash
bash "${HERMES_SKILL_DIR}/scripts/install.sh"
```

Native Windows (PowerShell), into `%LOCALAPPDATA%\Programs\quota`:

```powershell
powershell -ExecutionPolicy Bypass -File "${HERMES_SKILL_DIR}/scripts/install.ps1"
```

Then make sure each service is logged in **on the machine where Hermes runs** (for a gateway
on a server, that is the server). Per-service steps are in [references/setup.md](references/setup.md):

| Provider | Needs |
| --- | --- |
| Claude | Claude Code logged in (`~/.claude/.credentials.json`) |
| ChatGPT | Codex CLI logged in with ChatGPT (`~/.codex/auth.json`) |
| Command Code | `cmd login` (`~/.commandcode/auth.json`) or `COMMANDCODE_API_KEY` |
| Ollama Cloud | `OLLAMA_API_KEY` (legacy plans get session/weekly percentages, current plans the monthly allowance) |
| OpenCode Go | `opencode auth login` → OpenCode Go (`~/.local/share/opencode/opencode.db`) or `OPENCODE_API_KEY` |

## Pitfalls

- **Wrong machine or sandbox.** `quota` reads files under the home directory of the user running it.
  If the terminal toolset runs in Docker/SSH/Modal, those files are not there; use the local
  terminal backend or log in inside that environment.
- **Expired tokens.** Claude and Codex tokens are refreshed by their own CLIs, not by `quota`.
  On "token expired" or HTTP 401, run any `claude` or `codex` command once (e.g. `claude -p hi`,
  `codex exec "hi"`), then retry.
- **ChatGPT web limits are not available**; the ChatGPT entry shows Codex limits for the plan.
- The endpoints are undocumented and can change. If a provider starts returning parse errors,
  update the binary (re-run the installer above) and report the issue at
  https://github.com/wuhanhao456/quota-cli/issues.
- The JSON may contain an account email (Codex, Command Code). Do not post it to group chats
  unless the user asked.

## Verification

`quota --json` prints a document with `schema_version: 1` and at least one provider whose
`status` is `ok`. `quota --version` prints the installed version.
