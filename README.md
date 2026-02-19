# Miri - Autonomous Agent Service

Miri is a local autonomous agent written in Go. It integrates the xAI API as backend, saves its state in files under the user's profile directory (`~/.miri` by default, override with `MIRI_STORAGE_DIR`), and exposes a REST API for configuration, storing human information, and delegating prompts to xAI.

The agent has its own \"soul\" defined in `~/.miri/soul.txt` (bootstrapped from `templates/soul.txt` on first run if missing).

## Features

- **Persistent storage**: Config in `~/.miri/config.yaml`, human info in `~/.miri/human_info/*.json`, soul in `~/.miri/soul.txt`.
- **REST API** configurable via `server.addr` (default `:8080`):
  - `GET /config` - Get current config (incl. computed `effectiveHost`, `port`)
  - `POST /config` - Update config (persists to YAML)
  - `POST /human` - Store human info `{&quot;id&quot;: &quot;user123&quot;, &quot;data&quot;: {...}, &quot;notes&quot;: &quot;...&quot;}`
  - `GET /human` - List stored human infos
  - `POST /new` - Create new session bound to client: `{"client_id": "dev123"}` → `{"session_id": "uuid"}`
  - `GET /status` - Current primary model + active sessions: `{"primary_model": "xai/grok-4", "sessions": ["uuid1", ...]}`
  - `POST /prompt` - Delegate prompt to xAI: `{&quot;prompt&quot;: &quot;...&quot;}` (prepends soul + human context)
- **Logging**: Structured logs via `slog`.
- **Multi-model LLM**: Configurable providers/models (xAI grok-4, NVIDIA kimi-k2.5...) via OpenAI-compat `/chat/completions`. Supports fallback on primary failure via `agents.defaults.model.fallbacks` array (e.g., `["nvidia/kimi-k2.5"]`).
- **Sessions**: In-memory prompt/response history by `session_id` (UUID, optional in `/prompt`).
- **Persistent Memory**: Prompts with "write to memory" append response to `~/.miri/memory.txt`.


## Prerequisites

- Go 1.21+
- xAI API key (set via env `XAI_API_KEY` or `/config`)

## Build & Run

```bash
go build -o miri src/cmd/main.go
./miri
./miri -config /path/to/my-config.yaml  # loads specified YAML first, then ~/.miri/config.yaml or ./config.yaml
```

Server starts on `server.addr` (default `:8080`). Logs show bootstrap if needed.

## Example Usage

### 1. Update Config

```bash
curl -X POST http://localhost:8080/config \\
  -H 'Content-Type: application/json' \\
  -d '{
    \"xai\": {
      \"api_key\": \"your_xai_key\",
      \"model\": \"grok-beta\"
    },
    \"storage_dir\": \"'$HOME'/.miri\"
  }'
```

### 2. Store Human Info

```bash
curl -X POST http://localhost:8080/human \\
  -H 'Content-Type: application/json' \\
  -d '{
    \"id\": \"user123\",
    \"data\": {\"name\": \"Alice\", \"pref\": \"coffee\"},
    \"notes\": \"Loves dark roast\"
  }'
```

List:

```bash
curl http://localhost:8080/human
```

### 3. Delegate Prompt

```bash
curl -X POST http://localhost:8080/prompt \\
  -H 'Content-Type: application/json' \\
  -d '{\"prompt\": \"Plan my week with gym and coding.\"}'
```

## Authentication

If `server.key` is set, **all** requests require header `X-Server-Key: &lt;key&gt;`.

**Set key:**

```bash
curl -X POST http://localhost:8080/config \\
  -H 'Content-Type: application/json' \\
  -d '{
    "server": {
      "addr": ":8080",
      "key": "your-secret-key"
    }
  }'
```

**Use with key:**

```bash
curl -X POST http://localhost:8080/prompt \\
  -H 'Content-Type: application/json' \\
  -H 'X-Server-Key: your-secret-key' \\
  -d '{"prompt": "Plan my week."}'
```

On startup, warns if non-loopback bind (default `0.0.0.0`) without key.

Response includes xAI completion (soul + human context prepended).

## Project Structure

```
.
├── src/          # Go source
│   ├── cmd/main.go
│   └── internal/
├── templates/    # soul.txt template
├── go.mod
├── .gitignore
└── README.md
```

## Customization

- Edit `templates/soul.txt` for default agent behavior.
- Soul loaded on each `/prompt`.
- Human data indexed by ID, assembled into context.
- **API Key Overrides**: Provider API keys (`models.providers.&lt;provider&gt;.apiKey`) can be set via environment variables if empty in config. Use `&lt;PROVIDER&gt;_API_KEY` (e.g., `XAI_API_KEY=sk-...`, `NVIDIA_API_KEY=nvapi-...`). Runtime only, not persisted to YAML.

## Testing

**Unit tests:**
```bash
go test ./src/internal/tools/... -v
```

**Integration tests:**
```bash
chmod +x test_agent.sh
./test_agent.sh
```
Verifies endpoints without xAI key.

## Channels

### WhatsApp

Enable in config:

```yaml
channels:
  whatsapp:
    enabled: true
```

Restart server. QR code printed in logs via qrterminal (stdout), scan with WhatsApp > Linked Devices > Link a Device.

- Incoming text DMs (non-group) auto-chat via agent (LLM response sent back).
- Persistent sqlite DB `~/.miri/whatsapp/whatsapp.db`.

**Single POST /channels** (all channels: whatsapp/telegram/slack future):
- `{"channel":"whatsapp","action":"status"}` → `{"connected":bool,"logged_in":bool}`
- `{"channel":"whatsapp","action":"enroll"}` → `{"status":"enroll started"}` (check logs QR)
- `{"channel":"whatsapp","action":"devices"}` → `{"devices":[...]}`
- `{"channel":"whatsapp","action":"send","device":"123@s.whatsapp.net","message":"hi"}` → `{"status":"sent"}`
- `{"channel":"whatsapp","action":"chat","device":"123@s.whatsapp.net","prompt":"hi"}` → LLM resp + auto-send `{"response":"..."}`

**WS unchanged**: `ws://localhost:8080/ws?channel=whatsapp&device=123@s.whatsapp.net` → stream chat.

## Running as Daemon/Service

Miri supports daemon mode via PID file (`~/.miri/miri.pid` by default) and graceful shutdown.

### Linux (systemd)

1. Customize `templates/systemd/miri.service` (set `ExecStart` to your `miri` binary path).

2. ```bash
sudo cp templates/systemd/miri.service /etc/systemd/system/miri@`whoami`.service
sudo systemctl daemon-reload
sudo systemctl enable miri@`whoami`
sudo systemctl start miri@`whoami`
```

Status: `sudo systemctl status miri@`whoami``

### macOS (launchd)

1. Customize `templates/launchd/com.miri.agent.plist` (`Program` path).

2. ```bash
mkdir -p ~/Library/LaunchAgents
cp templates/launchd/com.miri.agent.plist ~/Library/LaunchAgents/
launchctl load ~/Library/LaunchAgents/com.miri.agent.plist
```

Status: `launchctl list | grep miri`

Unload: `launchctl unload ~/Library/LaunchAgents/com.miri.agent.plist`

Logs: `tail ~/.miri/miri.out.log`

### Windows (NSSM)

1. Download NSSM: [nssm.cc](https://nssm.cc/download)

2. ```cmd
nssm install Miri "C:\\path\\to\\miri.exe"
nssm start Miri
```

Status: `sc query Miri` or Services.msc

Stop: `nssm stop Miri`

PID file: `%USERPROFILE%\\.miri\\miri.pid`

## Notes

- `/prompt` requires valid xAI API key.
- All data local, no cloud storage.
- Structured logs: `slog.Info`, etc.

Built with modern Go idioms.
