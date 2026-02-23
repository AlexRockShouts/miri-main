# Miri - Autonomous Agent Service

Miri is a local autonomous agent written in Go. It integrates the xAI API as backend, saves its state in files under the user's profile directory (`~/.miri` by default, override with `MIRI_STORAGE_DIR`), and exposes a REST API for configuration, storing human information, and delegating prompts to xAI.

The agent has its own \"soul\" defined in `~/.miri/soul.txt` (bootstrapped from `templates/soul.txt` on first run if missing).

## Features

- **Eino Engine**: A powerful ReAct agent powered by [Eino](https://github.com/cloudwego/eino), supporting tool-augmented generation and autonomous reasoning loops.
- **Graph Orchestration**: Core logic is modeled as an Eino Graph with specialized nodes:
  - `retriever`: Proactively injects long-term memory into conversations.
  - `flush`: Automatically compacts and appends memory to disk when context usage is high (~65%).
  - `compact`: Summarizes older history into structured JSON when context is nearly full (~88%).
  - `agent`: Executes the ReAct loop with real-time tool calls and reasoning.
- **Checkpointing**: Eino-native graph persistence using `FileCheckPointStore` ensures long-running tasks can resume from the last successful tool execution.
- **Long-term Memory**: Durable storage in `memory.md`, `user.md`, and `facts.json` (NDJSON) with automated early-flush compaction.
- **System Awareness**: Automatically provides the LLM with system context (OS, Architecture, Go version) for more efficient command execution.
- **REST API** & **WebSocket**:
  - `POST /prompt`: Blocking prompt execution.
  - `GET /prompt/stream`: SSE streaming for real-time thoughts and tool execution.
  - `GET /ws`: WebSocket support for full-duplex interactive streaming.
  - Session and history management via `/sessions` endpoints.
- **Streamable Tools**: Real-time output streaming for installation tools like `curl_install` and `go_install`.
- **Logging**: Structured logs via `slog` with Eino callback integration for deep visibility.


## Prerequisites

- Go 1.25+
- xAI API key (set via env `XAI_API_KEY` or `config.yaml`)

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
curl -X POST http://localhost:8080/config \
  -H 'Content-Type: application/json' \
  -H 'X-Server-Key: local-dev-key' \
  -d '{
    "models": {
      "providers": {
        "xai": {
          "apiKey": "your_xai_key"
        }
      }
    },
    "storage_dir": "/home/user/.miri"
  }'
```

### 2. Store Human Info

```bash
curl -X POST http://localhost:8080/human \
  -H 'Content-Type: application/json' \
  -H 'X-Server-Key: local-dev-key' \
  -d '{
    "id": "user123",
    "data": {"name": "Alice", "pref": "coffee"},
    "notes": "Loves dark roast"
  }'
```

List:

```bash
curl -H 'X-Server-Key: local-dev-key' http://localhost:8080/human
```

### 3. Delegate Prompt

```bash
curl -X POST http://localhost:8080/prompt \
  -H 'Content-Type: application/json' \
  -H 'X-Server-Key: local-dev-key' \
  -d '{"prompt": "Plan my week with gym and coding."}'
```

Streaming with SSE:

```bash
curl -N "http://localhost:8080/prompt/stream?prompt=Plan+my+week&session_id=mysession" \
  -H 'X-Server-Key: local-dev-key'
```

## Authentication

If `server.key` is set, **all** requests require header `X-Server-Key: &lt;key&gt;`.

**Set key:**

```bash
curl -X POST http://localhost:8080/config \
  -H 'Content-Type: application/json' \
  -H 'X-Server-Key: local-dev-key' \
  -d '{
    "server": {
      "addr": ":8080",
      "key": "your-secret-key"
    }
  }'
```

**Use with key:**

```bash
curl -X POST http://localhost:8080/prompt \
  -H 'Content-Type: application/json' \
  -H 'X-Server-Key: your-secret-key' \
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
- `{"channel":"whatsapp","action":"enroll"}` → `{"status":"enroll started"}` (check logs for QR)
- `{"channel":"whatsapp","action":"devices"}` → `{"devices":[...]}`
- `{"channel":"whatsapp","action":"send","device":"123@s.whatsapp.net","message":"hi"}` → `{"status":"sent"}`
- `{"channel":"whatsapp","action":"chat","device":"123@s.whatsapp.net","prompt":"hi"}` → `{"response":"..."}`

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
